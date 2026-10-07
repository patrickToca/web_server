package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
	"mywebapp/internal/middleware"
	"mywebapp/internal/repository"
	"mywebapp/internal/service"
	"mywebapp/internal/testutil"
)

// setupAuthRouter builds a router with the auth handler mounted on the
// same paths as main.go. It uses the shared test pool from testutil,
// so the tests run against mywebapp_test, not against the production
// database.
func setupAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// Pin the CSRF HMAC key for the duration of the test. Without
	// this, loadCSRFKey generates a fresh random key on every call,
	// and the router's middleware and the handler end up with two
	// different keys. That makes any token the handler rotates fail
	// signature verification in the middleware.
	//
	// 36 bytes, above the 32-byte minimum the loader now enforces.
	t.Setenv("CSRF_SECRET", "test-csrf-secret-do-not-use-in-prod!")

	pool := testutil.NewPool(t)
	queries := repository.New(pool)
	userSvc := service.NewUserService(queries)
	sessSvc := service.NewSessionService(pool)

	// 35 bytes, above the 32-byte minimum. The previous value was
	// 30 bytes and only worked because the loader tolerates a short
	// secret in development; a developer with APP_ENV=production
	// exported in their shell would have seen every test in this file
	// fail at construction.
	t.Setenv("JWT_SECRET", "test-secret-do-not-use-in-prod!!!!")
	t.Setenv("JWT_ACCESS_TTL", "15m")
	t.Setenv("JWT_REFRESH_TTL", "168h")

	jwtSvc, err := auth.NewJWTService()
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}

	csrfCfg, err := middleware.LoadCSRFConfig()
	if err != nil {
		t.Fatalf("LoadCSRFConfig: %v", err)
	}

	h := NewAuthHandler(userSvc, sessSvc, jwtSvc, csrfCfg)

	r := gin.New()
	r.SetFuncMap(GetTemplateFuncs())
	r.LoadHTMLGlob("../templates/**/*.html")

	r.GET("/web/login", middleware.CSRFMiddleware(csrfCfg), h.LoginPage)
	r.POST("/web/login", middleware.CSRFMiddleware(csrfCfg), h.Login)
	r.GET("/web/register", middleware.CSRFMiddleware(csrfCfg), h.RegisterPage)
	r.POST("/web/register", middleware.CSRFMiddleware(csrfCfg), h.Register)
	r.POST("/web/refresh", h.Refresh)
	r.POST("/web/logout", middleware.CSRFMiddleware(csrfCfg), h.Logout)
	r.POST("/api/v1/auth/token", h.IssueAPIToken)

	return r
}

// primeCSRF performs a GET on the login page and returns the CSRF
// cookie the response sets. The CSRF middleware issues it on the first
// safe-method request.
func primeCSRF(t *testing.T, r *gin.Engine) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/login", nil))
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c
		}
	}
	t.Fatal("login page did not issue a CSRF cookie")
	return nil
}

// postForm submits a form to the given path, attaching the CSRF token
// as both a cookie and a header. That satisfies the double-submit
// check without needing the hidden form field.
func postForm(r *gin.Engine, path string, csrf *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(csrf)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// cookiesByName indexes a response's Set-Cookie headers by name. The
// last occurrence for each name wins, matching browser behaviour.
func cookiesByName(w *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range w.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

// -----------------------------------------------------------------------------
// LoginPage
// -----------------------------------------------------------------------------

func TestLoginPage_AnonymousRendersForm(t *testing.T) {
	r := setupAuthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/login", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected full login page")
	}
	if !strings.Contains(body, "Welcome Back") {
		t.Error("expected the login form heading")
	}
	if !strings.Contains(body, `hx-post="/web/login"`) {
		t.Error("expected the form to POST to /web/login")
	}
}

func TestLoginPage_HTMXReturnsPartial(t *testing.T) {
	r := setupAuthRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/web/login", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected partial, got full page")
	}
	if !strings.Contains(body, "Welcome Back") {
		t.Error("expected the login form heading")
	}
}

// -----------------------------------------------------------------------------
// Login (POST)
// -----------------------------------------------------------------------------

func TestLogin_ValidCredentialsSetsCookiesAndRedirects(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}

	w := postForm(r, "/web/login", csrf, form)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/web/home" {
		t.Errorf("Location = %q, want /web/home", loc)
	}

	cs := cookiesByName(w)
	for _, name := range []string{CookieName, RefreshCookieName, CSRFCookieName} {
		if cs[name] == nil {
			t.Errorf("expected Set-Cookie for %s", name)
		}
	}
}

func TestLogin_ValidCredentialsHTMXSetsHXRedirect(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/web/login",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(csrf)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HTMX login should respond 200, got %d", w.Code)
	}
	if loc := w.Header().Get("HX-Redirect"); loc != "/web/home" {
		t.Errorf("HX-Redirect = %q, want /web/home", loc)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":    {"alice@example.com"},
		"password": {"not-the-right-password"},
	}

	w := postForm(r, "/web/login", csrf, form)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with re-rendered form, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Invalid email or password") {
		t.Error("expected the invalid-credentials message")
	}
	cs := cookiesByName(w)
	if cs[CookieName] != nil && cs[CookieName].Value != "" {
		t.Error("auth cookie should not be set on a failed login")
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":    {"nobody@example.com"},
		"password": {"password123"},
	}

	w := postForm(r, "/web/login", csrf, form)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid email or password") {
		t.Error("expected the invalid-credentials message")
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{}
	w := postForm(r, "/web/login", csrf, form)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Email and password are required") {
		t.Error("expected the required-fields message")
	}
}

func TestLogin_RotatesCSRFToken(t *testing.T) {
	// The token presented before login must be replaced by a fresh one
	// on success. If it is not, the session-fixation window stays open.
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	w := postForm(r, "/web/login", csrf, form)

	if w.Code != http.StatusFound {
		t.Fatalf("login failed: %d", w.Code)
	}
	newCSRF := cookiesByName(w)[CSRFCookieName]
	if newCSRF == nil {
		t.Fatal("login did not set a CSRF cookie")
	}
	if newCSRF.Value == csrf.Value {
		t.Error("CSRF token was not rotated on login")
	}
}

// -----------------------------------------------------------------------------
// Register (GET and POST)
// -----------------------------------------------------------------------------

func TestRegisterPage_AnonymousRendersForm(t *testing.T) {
	r := setupAuthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/register", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected full page")
	}
	if !strings.Contains(body, "Create Account") {
		t.Error("expected the register heading")
	}
}

func TestRegister_ValidPayloadCreatesUserAndLogsIn(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	email := fmt.Sprintf("reg-%d@example.com", time.Now().UnixNano())
	username := fmt.Sprintf("reguser%d", time.Now().UnixNano())

	form := url.Values{
		"email":     {email},
		"username":  {username},
		"full_name": {"New Registrant"},
		"password":  {"password123"},
	}

	w := postForm(r, "/web/register", csrf, form)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/web/home" {
		t.Errorf("Location = %q, want /web/home", loc)
	}
	cs := cookiesByName(w)
	if cs[CookieName] == nil || cs[RefreshCookieName] == nil {
		t.Error("expected auth and refresh cookies after register")
	}
}

func TestRegister_ShortPasswordRejected(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{
		"email":     {fmt.Sprintf("short-%d@example.com", time.Now().UnixNano())},
		"username":  {fmt.Sprintf("short%d", time.Now().UnixNano())},
		"full_name": {"Short Pass"},
		"password":  {"short"},
	}

	w := postForm(r, "/web/register", csrf, form)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with error form, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Password must be at least 8 characters") {
		t.Error("expected the short-password message")
	}
}

func TestRegister_MissingFieldsRejected(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	form := url.Values{}
	w := postForm(r, "/web/register", csrf, form)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "All fields are required") {
		t.Error("expected the required-fields message")
	}
}

// -----------------------------------------------------------------------------
// Refresh
// -----------------------------------------------------------------------------

func TestRefresh_ValidTokenIssuesNewAccessCookie(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	// Log in to obtain a real refresh cookie.
	form := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	loginResp := postForm(r, "/web/login", csrf, form)
	if loginResp.Code != http.StatusFound {
		t.Fatalf("login failed: %d", loginResp.Code)
	}
	refresh := cookiesByName(loginResp)[RefreshCookieName]
	if refresh == nil {
		t.Fatal("login did not set refresh cookie")
	}

	// Hit refresh with that cookie.
	req := httptest.NewRequest(http.MethodPost, "/web/refresh", nil)
	req.AddCookie(refresh)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d\nbody: %s", w.Code, w.Body.String())
	}
	cs := cookiesByName(w)
	if cs[CookieName] == nil || cs[CookieName].Value == "" {
		t.Error("expected a new auth_token cookie")
	}
}

func TestRefresh_MissingTokenUnauthorized(t *testing.T) {
	r := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/web/refresh", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_InvalidTokenUnauthorized(t *testing.T) {
	r := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/web/refresh", nil)
	req.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "not-a-jwt"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_HTMXGetsJSONError(t *testing.T) {
	r := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/web/refresh", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("HTMX refresh error should be JSON, got %q", ct)
	}
}

func TestRefresh_AfterLogoutFails(t *testing.T) {
	// Log in, capture the refresh cookie, log out, then attempt to
	// refresh with the captured cookie. The revocation must take
	// effect: the refresh must return 401.
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	loginForm := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	loginResp := postForm(r, "/web/login", csrf, loginForm)
	if loginResp.Code != http.StatusFound {
		t.Fatalf("login failed: %d", loginResp.Code)
	}
	loginCookies := cookiesByName(loginResp)
	authCookie := loginCookies[CookieName]
	refreshCookie := loginCookies[RefreshCookieName]
	newCSRF := loginCookies[CSRFCookieName]
	if authCookie == nil || refreshCookie == nil || newCSRF == nil {
		t.Fatal("login did not set all three cookies")
	}

	// Log out.
	req := httptest.NewRequest(http.MethodPost, "/web/logout", nil)
	req.AddCookie(authCookie)
	req.AddCookie(refreshCookie)
	req.AddCookie(newCSRF)
	req.Header.Set("X-CSRF-Token", newCSRF.Value)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("logout failed: %d", w.Code)
	}

	// Attempt to refresh with the old cookie.
	req = httptest.NewRequest(http.MethodPost, "/web/refresh", nil)
	req.AddCookie(refreshCookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout must fail; got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Logout
// -----------------------------------------------------------------------------

func TestLogout_ClearsCookiesAndRedirects(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	// Log in.
	loginForm := url.Values{
		"email":    {"alice@example.com"},
		"password": {"password123"},
	}
	loginResp := postForm(r, "/web/login", csrf, loginForm)
	if loginResp.Code != http.StatusFound {
		t.Fatalf("login failed: %d", loginResp.Code)
	}
	loginCookies := cookiesByName(loginResp)
	authCookie := loginCookies[CookieName]
	refreshCookie := loginCookies[RefreshCookieName]
	newCSRF := loginCookies[CSRFCookieName]

	if authCookie == nil || refreshCookie == nil || newCSRF == nil {
		t.Fatal("login did not set all three cookies")
	}
	// Login rotates the CSRF token: the new one must be used on the
	// subsequent mutation. This also pins the rotation behaviour.
	if newCSRF.Value == csrf.Value {
		t.Fatal("CSRF token was not rotated on login")
	}

	// Log out with the rotated token.
	req := httptest.NewRequest(http.MethodPost, "/web/logout", nil)
	req.AddCookie(authCookie)
	req.AddCookie(refreshCookie)
	req.AddCookie(newCSRF)
	req.Header.Set("X-CSRF-Token", newCSRF.Value)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/web/login" {
		t.Errorf("Location = %q, want /web/login", loc)
	}
	cs := cookiesByName(w)
	for _, name := range []string{CookieName, RefreshCookieName, CSRFCookieName} {
		if c := cs[name]; c == nil || c.Value != "" {
			t.Errorf("cookie %s was not cleared (got %+v)", name, c)
		}
	}
}

func TestLogout_HTMXRedirect(t *testing.T) {
	r := setupAuthRouter(t)
	csrf := primeCSRF(t, r)

	req := httptest.NewRequest(http.MethodPost, "/web/logout", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(csrf)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HTMX logout should respond 200, got %d", w.Code)
	}
	if loc := w.Header().Get("HX-Redirect"); loc != "/web/login" {
		t.Errorf("HX-Redirect = %q, want /web/login", loc)
	}
}

func TestLogout_WithoutCSRFTokenIsRejected(t *testing.T) {
	// A POST to /web/logout without a valid CSRF token must be
	// rejected before the handler runs.
	r := setupAuthRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/web/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for logout without CSRF, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// API token endpoint
// -----------------------------------------------------------------------------

func TestIssueAPIToken_ValidCredentials(t *testing.T) {
	r := setupAuthRouter(t)

	body := `{"email":"alice@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "access_token") {
		t.Error("response does not contain an access_token")
	}
	if !strings.Contains(w.Body.String(), "Bearer") {
		t.Error("response does not declare the Bearer type")
	}
}

func TestIssueAPIToken_WrongPassword(t *testing.T) {
	r := setupAuthRouter(t)

	body := `{"email":"alice@example.com","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIssueAPIToken_UnknownEmail(t *testing.T) {
	r := setupAuthRouter(t)

	body := `{"email":"nobody@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIssueAPIToken_InvalidJSON(t *testing.T) {
	r := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
