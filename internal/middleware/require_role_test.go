package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
)

func setClaims(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextClaimsKey, &auth.Claims{
			UserID:   1,
			Username: "test",
			Role:     role,
		})
		c.Next()
	}
}

func buildRouter(role string, roles ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(setClaims(role))
	r.Use(RequireRole(roles...))
	r.GET("/protected", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestRequireRole_AllowsMatchingRole(t *testing.T) {
	r := buildRouter("admin", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestRequireRole_RedirectsBrowserNavigation asserts that a non-admin
// browser navigation is redirected to /web/me rather than receiving a
// bare 403 or being sent to the login form. The caller is authenticated
// (RequireRole only runs after AuthMiddleware), so their own profile
// page is the correct destination.
func TestRequireRole_RedirectsBrowserNavigation(t *testing.T) {
	r := buildRouter("user", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/web/me" {
		t.Errorf("expected Location: /web/me, got %q", loc)
	}
}

func TestRequireRole_AllowsAnyOfMultiple(t *testing.T) {
	for _, role := range []string{"admin", "moderator"} {
		r := buildRouter(role, "admin", "moderator")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
		if w.Code != http.StatusOK {
			t.Errorf("role %q: expected 200, got %d", role, w.Code)
		}
	}
}

// TestRequireRole_FailsClosedWhenNoClaims asserts that a route with
// RequireRole but no AuthMiddleware rejects the request. On the browser
// path that means a redirect to /web/me; the fail-closed property is
// that the request does not proceed to the handler.
func TestRequireRole_FailsClosedWhenNoClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequireRole("admin"))
	r.GET("/protected", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if w.Code != http.StatusFound {
		t.Errorf("expected fail-closed 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/web/me" {
		t.Errorf("expected Location: /web/me, got %q", loc)
	}
}

// TestRequireRole_DoesNotRedirectToItself asserts that a request that
// already targets /web/me receives a bare 403 rather than a redirect to
// itself, which would loop.
func TestRequireRole_DoesNotRedirectToItself(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(setClaims("user"))
	r.Use(RequireRole("admin"))
	r.GET("/web/me", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/me", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 to avoid a redirect loop, got %d", w.Code)
	}
}

func TestRequireRole_HTMXGetsJSON(t *testing.T) {
	r := buildRouter("user", "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("expected JSON content-type for HTMX, got %q", ct)
	}
}

func TestRequireRole_APIGetsJSON(t *testing.T) {
	r := buildRouter("user", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("expected JSON content-type for API, got %q", ct)
	}
}
