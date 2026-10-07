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

// setupMeRouter mounts the profile page behind a claims injector so
// that the handler sees an authenticated caller without needing the
// full JWT middleware.
func setupMeRouter(t *testing.T, claims *auth.Claims) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	svc := service.NewUserServiceWithPool(pool)
	h := NewMeHandler(svc, nil) // nil R2 client: the handler skips the image

	r := gin.New()
	r.SetFuncMap(GetTemplateFuncs())
	r.LoadHTMLGlob("../templates/**/*.html")
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set(middleware.ContextClaimsKey, claims)
		}
		c.Next()
	})
	r.GET("/web/me", h.ProfilePage)
	return r
}

func TestProfilePage_AuthenticatedUser(t *testing.T) {
	claims := &auth.Claims{UserID: 1, Username: "alice", Role: "admin"}
	r := setupMeRouter(t, claims)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/me", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Your Account") {
		t.Error("expected the account section")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Error("expected the user's email")
	}
	if !strings.Contains(body, "Administration") {
		t.Error("admin should see the Administration block")
	}
}

func TestProfilePage_AuthenticatedNonAdmin(t *testing.T) {
	// Bob is the seeded non-admin user.
	claims := &auth.Claims{UserID: 2, Username: "bob", Role: "user"}
	r := setupMeRouter(t, claims)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/me", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "Administration") {
		t.Error("non-admin should not see the Administration block")
	}
}

func TestProfilePage_NoClaimsUnauthorized(t *testing.T) {
	r := setupMeRouter(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/me", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without claims, got %d", w.Code)
	}
}
