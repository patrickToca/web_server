package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
	"mywebapp/internal/testutil"
)

func setupHomeRouter(t *testing.T, claims *auth.Claims) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	svc := service.NewUserServiceWithPool(pool)
	h := NewHomeHandler(svc, nil)

	r := gin.New()
	r.SetFuncMap(GetTemplateFuncs())
	r.LoadHTMLGlob("../templates/**/*.html")

	if claims != nil {
		r.Use(func(c *gin.Context) {
			c.Set(middleware.ContextClaimsKey, claims)
			c.Next()
		})
	}

	r.GET("/", h.HomePage)
	r.GET("/web/home", h.HomePage)
	r.GET("/web/members", h.MembersPage)
	r.GET("/web/members/partial", h.MembersPartial)
	return r
}

func TestHomePage_AnonymousRendersPublicVariant(t *testing.T) {
	r := setupHomeRouter(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "const IS_AUTHED = false") {
		t.Error("expected IS_AUTHED = false")
	}
	if !strings.Contains(body, "Welcome to Go HTMX App") {
		t.Error("expected public hero")
	}
	if !strings.Contains(body, "/web/login") {
		t.Error("expected Sign in link")
	}
}

func TestHomePage_AuthenticatedAdmin(t *testing.T) {
	claims := &auth.Claims{UserID: 1, Username: "alice", Role: "admin"}
	r := setupHomeRouter(t, claims)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/home", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "const IS_AUTHED = true") {
		t.Error("expected IS_AUTHED = true")
	}
	if !strings.Contains(body, "Welcome back") {
		t.Error("expected greeting")
	}
	if !strings.Contains(body, "/web/users") {
		t.Error("admin should see Manage Users link")
	}
}

func TestHomePage_AuthenticatedNonAdmin(t *testing.T) {
	claims := &auth.Claims{UserID: 2, Username: "bob", Role: "user"}
	r := setupHomeRouter(t, claims)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/home", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "const IS_AUTHED = true") {
		t.Error("expected IS_AUTHED = true")
	}
	if strings.Contains(body, `href="/web/users"`) {
		t.Error("non-admin should not see Manage Users link")
	}
}

func TestMembersPage_RequiresAuth(t *testing.T) {
	r := setupHomeRouter(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/members", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestMembersPage_AuthenticatedRendersShell(t *testing.T) {
	claims := &auth.Claims{UserID: 1, Username: "alice", Role: "admin"}
	r := setupHomeRouter(t, claims)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/members", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="member-list"`) {
		t.Error("expected stable container")
	}
	if !strings.Contains(body, `hx-get="/web/members/partial"`) {
		t.Error("expected partial endpoint")
	}
}

func TestMembersPartial_RequiresAuth(t *testing.T) {
	r := setupHomeRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/web/members/partial", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestMembersPartial_ReturnsFragment(t *testing.T) {
	claims := &auth.Claims{UserID: 1, Username: "alice", Role: "admin"}
	r := setupHomeRouter(t, claims)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/web/members/partial", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected fragment, not full page")
	}
	if !strings.Contains(body, "Members") {
		t.Error("expected Members heading")
	}
}
