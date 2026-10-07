package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

// setEnv sets an environment variable for the duration of the test and
// restores the previous value on cleanup.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("setenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// unsetEnv removes an environment variable for the duration of the
// test, restoring it on cleanup. t.Setenv cannot express "unset", which
// is why this helper exists.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

// testCSRFConfig returns a config suitable for httptest.
//
// It sets a fixed HMAC key so signed tokens are reproducible across
// tests, and Secure=false so the test recorder accepts the Set-Cookie
// header without HTTPS.
func testCSRFConfig() CSRFConfig {
	return CSRFConfig{
		CookieName:   defaultCSRFCookieName,
		FormField:    defaultCSRFFormField,
		HeaderName:   defaultCSRFHeaderName,
		TokenLength:  defaultCSRFTokenLength,
		CookiePath:   "/",
		CookieSecure: false,
		CookieMaxAge: 0,
		SameSite:     http.SameSiteLaxMode,
		HMACKey:      []byte("test-csrf-hmac-key-do-not-use-in-prod"),
	}
}

// buildCSRFRouter creates a router with the CSRF middleware and a
// trivial handler for both GET (to seed the cookie) and POST (to
// exercise validation).
func buildCSRFRouter(cfg CSRFConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CSRFMiddleware(cfg))
	r.GET("/form", func(c *gin.Context) {
		c.String(http.StatusOK, TokenFromContext(c))
	})
	r.POST("/submit", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

// extractCSRFCookie pulls the CSRF cookie out of a response recorder.
// The last occurrence for the name wins, matching browser behaviour
// when a response sets the same cookie twice (clear then set).
func extractCSRFCookie(w *httptest.ResponseRecorder, name string) string {
	var value string
	for _, raw := range w.Result().Cookies() {
		if raw.Name == name {
			value = raw.Value
		}
	}
	return value
}

// -----------------------------------------------------------------------------
// Token generation and signature verification
// -----------------------------------------------------------------------------

func TestGenerateToken_Length(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	tok, err := GenerateToken(32, key)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	// The token is "<body>.<signature>", where each part is 32 bytes of
	// base64url-encoded data = ceil(32*4/3) = 43 characters, plus the
	// separator. Total: 87 characters.
	if len(tok) != 87 {
		t.Errorf("expected 87 chars, got %d (%q)", len(tok), tok)
	}
	if strings.Count(tok, ".") != 1 {
		t.Errorf("expected exactly one separator in %q", tok)
	}
}

func TestGenerateToken_Unique(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok, err := GenerateToken(32, key)
		if err != nil {
			t.Fatalf("GenerateToken error: %v", err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = true
	}
}

func TestGenerateToken_RejectsShortLength(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	if _, err := GenerateToken(8, key); err == nil {
		t.Error("expected error for length < 16, got nil")
	}
}

func TestVerifyToken_AcceptsOwnSignature(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	tok, err := GenerateToken(32, key)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if !verifyToken(tok, key) {
		t.Errorf("token generated with the key must verify: %q", tok)
	}
}

func TestVerifyToken_RejectsTamperedSignature(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	tok, err := GenerateToken(32, key)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	// Flip the last character of the signature.
	last := tok[len(tok)-1]
	var replacement byte = 'A'
	if last == 'A' {
		replacement = 'B'
	}
	tampered := tok[:len(tok)-1] + string(replacement)
	if verifyToken(tampered, key) {
		t.Error("tampered token must not verify")
	}
}

func TestVerifyToken_RejectsTamperedBody(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	tok, err := GenerateToken(32, key)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	// Flip the first character of the body.
	first := tok[0]
	var replacement byte = 'A'
	if first == 'A' {
		replacement = 'B'
	}
	tampered := string(replacement) + tok[1:]
	if verifyToken(tampered, key) {
		t.Error("tampered token body must not verify")
	}
}

func TestVerifyToken_RejectsDifferentKey(t *testing.T) {
	keyA := []byte("key-a")
	keyB := []byte("key-b")
	tok, err := GenerateToken(32, keyA)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if verifyToken(tok, keyB) {
		t.Error("token signed with key-a must not verify under key-b")
	}
}

func TestVerifyToken_RejectsMalformed(t *testing.T) {
	key := []byte("test-csrf-hmac-key-do-not-use-in-prod")
	cases := []string{
		"",
		"no-dot",
		".starting-with-dot",
		"ending-with-dot.",
		".",
	}
	for _, c := range cases {
		if verifyToken(c, key) {
			t.Errorf("malformed token %q must not verify", c)
		}
	}
}

// -----------------------------------------------------------------------------
// Cookie handling on safe methods
// -----------------------------------------------------------------------------

func TestCSRF_SafeMethodSetsCookieWhenAbsent(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/form", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	tok := extractCSRFCookie(w, cfg.CookieName)
	if tok == "" {
		t.Fatal("expected CSRF cookie to be set on first GET")
	}
	if !verifyToken(tok, cfg.HMACKey) {
		t.Errorf("cookie does not carry a valid signature: %q", tok)
	}
	if w.Body.Len() == 0 {
		t.Error("expected handler to receive token from context")
	}
}

func TestCSRF_SafeMethodReusesExistingCookie(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/form", nil))
	tok1 := extractCSRFCookie(w1, cfg.CookieName)
	if tok1 == "" {
		t.Fatal("no cookie set on first GET")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/form", nil)
	req2.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok1})
	r.ServeHTTP(w2, req2)

	if got := w2.Body.String(); got != tok1 {
		t.Errorf("expected handler to see existing token %q, got %q", tok1, got)
	}
}

func TestCSRF_SafeMethodReplacesUnsignedCookie(t *testing.T) {
	// A cookie that is syntactically plausible but carries no valid
	// signature must be replaced, not echoed. This is the cookie-
	// tossing defence: an attacker who plants a cookie of their
	// choosing does not get to keep it.
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/form", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "planted-by-attacker"})
	r.ServeHTTP(w, req)

	newTok := extractCSRFCookie(w, cfg.CookieName)
	if newTok == "" || newTok == "planted-by-attacker" {
		t.Errorf("expected a fresh signed cookie, got %q", newTok)
	}
	if !verifyToken(newTok, cfg.HMACKey) {
		t.Errorf("replacement cookie does not verify: %q", newTok)
	}
}

// -----------------------------------------------------------------------------
// Mutation validation
// -----------------------------------------------------------------------------

func TestCSRF_MutationRejectsMissingCookie(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	form := url.Values{cfg.FormField: {"anything"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_MutationRejectsMissingField(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/form", nil))
	tok := extractCSRFCookie(wGet, cfg.CookieName)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_MutationRejectsMismatch(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/form", nil))
	tok := extractCSRFCookie(wGet, cfg.CookieName)

	form := url.Values{cfg.FormField: {"wrong-token"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_MutationRejectsUnsignedCookie(t *testing.T) {
	// The submitted token matches the cookie value exactly. Under the
	// old equality-only check, this would pass; under the signed-token
	// check, the missing signature causes a rejection.
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	tok := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	form := url.Values{cfg.FormField: {tok}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unsigned cookie, got %d", w.Code)
	}
}

func TestCSRF_MutationAcceptsFormField(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/form", nil))
	tok := extractCSRFCookie(wGet, cfg.CookieName)

	form := url.Values{cfg.FormField: {tok}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCSRF_MutationAcceptsHeader(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/form", nil))
	tok := extractCSRFCookie(wGet, cfg.CookieName)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	req.Header.Set(cfg.HeaderName, tok)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Bearer exemption
// -----------------------------------------------------------------------------

func TestCSRF_BareAuthorizationHeaderDoesNotBypass(t *testing.T) {
	// A request that carries an Authorization header without having
	// been validated by AuthMiddleware must still pass CSRF. The
	// exemption applies only when AuthMiddleware has explicitly
	// marked the request.
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	req.Header.Set("Authorization", "Bearer some-api-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 without a validated Bearer, got %d", w.Code)
	}
}

func TestCSRF_MarkedBearerRequestIsExempt(t *testing.T) {
	// When AuthMiddleware validates a Bearer token and calls
	// MarkBearerAuthenticated, CSRF validation is skipped.
	cfg := testCSRFConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		MarkBearerAuthenticated(c)
		c.Next()
	})
	r.Use(CSRFMiddleware(cfg))
	r.POST("/submit", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for marked Bearer request, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Response shape
// -----------------------------------------------------------------------------

func TestCSRF_HTMXRequestGetsJSONOnFailure(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("expected JSON content-type for HTMX, got %q", ct)
	}
}

func TestCSRF_NonHTMXRequestGetsEmptyBody(t *testing.T) {
	cfg := testCSRFConfig()
	r := buildCSRFRouter(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body for browser request, got %q", w.Body.String())
	}
}

// -----------------------------------------------------------------------------
// Secret policy: LoadCSRFConfig fails closed in production
// -----------------------------------------------------------------------------
//
// These tests mirror the JWT secret policy tests in internal/auth. The
// two secrets share a policy: mandatory in production and staging,
// random per-process fallback in development. Keeping the tests
// parallel means a future change to one policy is visibly a change to
// both.

func TestLoadCSRFConfig_ProductionMissingSecretFails(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	unsetEnv(t, "CSRF_SECRET")

	_, err := LoadCSRFConfig()
	if err == nil {
		t.Fatal("expected error when CSRF_SECRET is unset in production, got nil")
	}
	if !errors.Is(err, ErrWeakCSRFSecret) {
		t.Fatalf("expected ErrWeakCSRFSecret, got %v", err)
	}
}

func TestLoadCSRFConfig_ProductionShortSecretFails(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	setEnv(t, "CSRF_SECRET", strings.Repeat("x", 16))

	_, err := LoadCSRFConfig()
	if err == nil {
		t.Fatal("expected error for short CSRF_SECRET in production, got nil")
	}
	if !errors.Is(err, ErrWeakCSRFSecret) {
		t.Fatalf("expected ErrWeakCSRFSecret, got %v", err)
	}
}

func TestLoadCSRFConfig_StagingMissingSecretFails(t *testing.T) {
	setEnv(t, "APP_ENV", "staging")
	unsetEnv(t, "CSRF_SECRET")

	if _, err := LoadCSRFConfig(); !errors.Is(err, ErrWeakCSRFSecret) {
		t.Fatalf("expected ErrWeakCSRFSecret, got %v", err)
	}
}

func TestLoadCSRFConfig_ProductionValidSecretSucceeds(t *testing.T) {
	setEnv(t, "APP_ENV", "production")
	secret := strings.Repeat("a", minimumCSRFKeyBytes)
	setEnv(t, "CSRF_SECRET", secret)

	cfg, err := LoadCSRFConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(cfg.HMACKey) != secret {
		t.Fatal("HMAC key not loaded from env")
	}
}

func TestLoadCSRFConfig_DevelopmentMissingSecretUsesRandomKey(t *testing.T) {
	setEnv(t, "APP_ENV", "development")
	unsetEnv(t, "CSRF_SECRET")

	cfg, err := LoadCSRFConfig()
	if err != nil {
		t.Fatalf("unexpected error in development: %v", err)
	}
	if len(cfg.HMACKey) < minimumCSRFKeyBytes {
		t.Fatalf("dev key too short: %d bytes", len(cfg.HMACKey))
	}

	cfg2, err := LoadCSRFConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(cfg.HMACKey) == string(cfg2.HMACKey) {
		t.Fatal("two dev instances generated the same key; randomness is broken")
	}
}

func TestLoadCSRFConfig_UnknownEnvironmentIsTreatedAsProduction(t *testing.T) {
	setEnv(t, "APP_ENV", "prod-typo")
	unsetEnv(t, "CSRF_SECRET")

	if _, err := LoadCSRFConfig(); err == nil {
		t.Fatal("expected error for unset secret under unknown env, got nil")
	}
}
