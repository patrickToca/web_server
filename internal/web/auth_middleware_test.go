package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

// newTestJWTServiceForMiddleware builds a JWTService for middleware
// tests. TestMain pins APP_ENV and JWT_SECRET, so construction cannot
// fail; the t.Fatalf is a guard against someone removing TestMain.
func newTestJWTServiceForMiddleware(t *testing.T) *auth.JWTService {
	t.Helper()
	svc, err := auth.NewJWTService()
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

// newCookieConfigForTest returns cookie attributes suitable for
// httptest. Secure=false so a test recorder accepts Set-Cookie over
// plain HTTP.
func newCookieConfigForTest() CookieConfig {
	return CookieConfig{
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		Domain:   "",
		Path:     "/",
	}
}

// newAuthMiddlewareTestRouter wires the auth middleware and a trivial
// protected handler. The handler records the claims it saw so tests
// can assert on them.
func newAuthMiddlewareTestRouter(
	jwtSvc *auth.JWTService,
	userSvc *service.UserService,
	sessSvc *service.SessionService,
	cfg CookieConfig,
) (*gin.Engine, *auth.Claims) {
	gin.SetMode(gin.TestMode)
	var observed *auth.Claims
	r := gin.New()
	r.Use(AuthMiddleware(jwtSvc, userSvc, sessSvc, cfg))
	r.GET("/protected", func(c *gin.Context) {
		if claims, ok := middleware.ClaimsFromGin(c); ok {
			observed = claims
		}
		c.Status(http.StatusOK)
	})
	return r, observed
}

// -----------------------------------------------------------------------------
// No credentials
// -----------------------------------------------------------------------------

func TestAuthMiddleware_NoCredentials_APIPathGets401JSON(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	// Register the route under /api/ so the middleware takes the API
	// branch. The helper registers /protected; a dedicated router is
	// simplest here.
	gin.SetMode(gin.TestMode)
	apiRouter := gin.New()
	apiRouter.Use(AuthMiddleware(jwtSvc, nil, nil, newCookieConfigForTest()))
	apiRouter.GET("/api/v1/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	apiRouter.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	// Use r to avoid an unused-variable compile error when the test
	// is edited to add assertions on the browser branch.
	_ = r
}

func TestAuthMiddleware_NoCredentials_BrowserGets302(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/web/login" {
		t.Errorf("Location = %q, want /web/login", loc)
	}
	// The redirect must not be cached. See abortUnauthenticated.
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestAuthMiddleware_NoCredentials_HTMXGetsHXRedirect(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if hx := w.Header().Get("HX-Redirect"); hx != "/web/login" {
		t.Errorf("HX-Redirect = %q, want /web/login", hx)
	}
}

// -----------------------------------------------------------------------------
// Bearer token
// -----------------------------------------------------------------------------

func TestAuthMiddleware_ValidBearerToken_AllowsRequest(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	tok, err := jwtSvc.GenerateAccessToken(1, "alice", "admin")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestAuthMiddleware_InvalidBearerToken_Rejects(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (browser path)", w.Code)
	}
}

func TestAuthMiddleware_BearerRejectedOnAPIPathWithoutToken(t *testing.T) {
	// A request to /api/* with no Bearer token and no cookie must
	// receive JSON 401, not a redirect. The API group has no CSRF
	// middleware, so cookie auth is refused there by design.
	jwtSvc := newTestJWTServiceForMiddleware(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthMiddleware(jwtSvc, nil, nil, newCookieConfigForTest()))
	r.GET("/api/v1/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "some-cookie-value"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}

// -----------------------------------------------------------------------------
// Access cookie
// -----------------------------------------------------------------------------

func TestAuthMiddleware_ValidAccessCookie_AllowsRequest(t *testing.T) {
	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	tok, err := jwtSvc.GenerateAccessToken(1, "alice", "user")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestAuthMiddleware_ExpiredAccessCookie_NoRefresh_Rejects(t *testing.T) {
	// Build a service with a 1-second access TTL so the token expires
	// before it is presented. Refresh TTL stays long so the refresh
	// path is not the thing under test.
	t.Setenv("JWT_ACCESS_TTL", "1s")
	t.Setenv("JWT_REFRESH_TTL", "1h")

	jwtSvc := newTestJWTServiceForMiddleware(t)
	r, _ := newAuthMiddlewareTestRouter(jwtSvc, nil, nil, newCookieConfigForTest())

	tok, err := jwtSvc.GenerateAccessToken(1, "alice", "user")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	time.Sleep(2 * time.Second)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect to login)", w.Code)
	}
}

// -----------------------------------------------------------------------------
// OptionalAuthMiddleware
// -----------------------------------------------------------------------------

func TestOptionalAuthMiddleware_NoCookie_DoesNotAbort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtSvc := newTestJWTServiceForMiddleware(t)

	r := gin.New()
	r.Use(OptionalAuthMiddleware(jwtSvc))
	r.GET("/public", func(c *gin.Context) {
		if _, ok := middleware.ClaimsFromGin(c); ok {
			t.Error("claims should be absent for an anonymous request")
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestOptionalAuthMiddleware_ValidCookie_SetsClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtSvc := newTestJWTServiceForMiddleware(t)

	tok, err := jwtSvc.GenerateAccessToken(42, "alice", "moderator")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	var seen *auth.Claims
	r := gin.New()
	r.Use(OptionalAuthMiddleware(jwtSvc))
	r.GET("/public", func(c *gin.Context) {
		if claims, ok := middleware.ClaimsFromGin(c); ok {
			seen = claims
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if seen == nil {
		t.Fatal("claims were not set on the context")
	}
	if seen.UserID != 42 || seen.Role != "moderator" {
		t.Errorf("claims mismatch: %+v", seen)
	}
}

func TestOptionalAuthMiddleware_InvalidCookie_DoesNotAbort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtSvc := newTestJWTServiceForMiddleware(t)

	r := gin.New()
	r.Use(OptionalAuthMiddleware(jwtSvc))
	r.GET("/public", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "garbage"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (invalid cookie is not an error on optional auth)", w.Code)
	}
}

// -----------------------------------------------------------------------------
// GetClaims
// -----------------------------------------------------------------------------

func TestGetClaims_ReturnsFalseWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if _, ok := GetClaims(c); ok {
		t.Error("GetClaims should return false when no claims are set")
	}
}
