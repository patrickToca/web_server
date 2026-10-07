package web

import (
	"context"
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
	"mywebapp/internal/service"
	"mywebapp/internal/testutil"
)

// setupTestRouter wires a router for the admin web surface against
// the shared test pool. The pool is owned by testutil: it is created
// once in TestMain and closed once when the test binary exits. Do not
// close it here — doing so breaks every test that runs after the
// first.
func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	svc := service.NewUserServiceWithPool(pool)
	h := NewWebHandler(svc)

	r := gin.New()
	r.SetFuncMap(GetTemplateFuncs())
	r.LoadHTMLGlob("../templates/**/*.html")

	// Read routes.
	r.GET("/web/users", h.ListUsersPage)
	r.GET("/web/users/by-id", h.ListUsersByIDPage)
	r.GET("/web/users/new", h.NewUserForm)
	r.GET("/web/users/:id/edit", h.EditUserForm)
	r.GET("/web/users/:id/role", h.EditUserRoleForm)
	r.GET("/web/users/stats", h.GetUserStats)

	// Mutation routes.
	r.POST("/web/users", h.CreateUserWeb)
	r.PUT("/web/users/:id", h.UpdateUserWeb)
	r.PUT("/web/users/:id/role", h.UpdateUserRoleWeb)
	r.DELETE("/web/users/:id", h.DeleteUserWeb)

	return r
}

// -----------------------------------------------------------------------------
// Read pages
// -----------------------------------------------------------------------------

func TestListUsersByIDPage_HTMXPartial(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/web/users/by-id", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d\nbody:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Users by ID") {
		t.Error("expected 'Users by ID' heading")
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected partial, got full page")
	}
	assertStablePagination(t, body)
}

func TestListUsersPage_HTMXPartial(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/web/users", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected partial, got full page")
	}
	if !strings.Contains(body, "Users") {
		t.Error("expected 'Users' heading")
	}
	assertStablePagination(t, body)
}

func TestListUsersByIDPage_FullPage(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/by-id", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected full page")
	}
	if !strings.Contains(body, `id="user-list"`) {
		t.Error("expected #user-list container")
	}
	if !strings.Contains(body, `hx-get="/web/users/by-id"`) {
		t.Error("expected listEndpoint to be /web/users/by-id")
	}
}

func TestListUsersPage_FullPage(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected full page")
	}
	if !strings.Contains(body, `hx-get="/web/users"`) {
		t.Error("expected listEndpoint to be /web/users")
	}
}

func TestNewUserForm(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/new", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Create New User") {
		t.Error("expected 'Create New User'")
	}
	if !strings.Contains(body, `name="password"`) {
		t.Error("expected password field")
	}
}

func TestEditUserForm_ValidID(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/1/edit", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Edit User") {
		t.Error("expected 'Edit User' heading")
	}
	if !strings.Contains(body, `name="username"`) {
		t.Error("expected username field")
	}
}

func TestEditUserForm_InvalidID(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/abc/edit", nil))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestEditUserForm_NotFound(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/999999/edit", nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestEditUserRoleForm_ValidID(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/1/role", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Change Role") {
		t.Error("expected 'Change Role' heading")
	}
	if !strings.Contains(body, `name="role"`) {
		t.Error("expected role select")
	}
}

func TestEditUserRoleForm_InvalidID(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/users/abc/role", nil))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetUserStats(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/web/users/stats", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "total users") {
		t.Error("expected the stats fragment")
	}
	if !strings.Contains(body, "active") {
		t.Error("expected the active count")
	}
}

// -----------------------------------------------------------------------------
// Mutations
// -----------------------------------------------------------------------------

func TestCreateUserWeb_Succeeds(t *testing.T) {
	r := setupTestRouter(t)

	form := url.Values{
		"email":     {fmt.Sprintf("web-%d@example.com", time.Now().UnixNano())},
		"username":  {fmt.Sprintf("webuser%d", time.Now().UnixNano())},
		"full_name": {"Web Created"},
		"password":  {"password123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/web/users",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Web Created") {
		t.Logf("response body (%d bytes):\n%s", len(body), body)
		t.Error("expected the created user's name in the row response")
	}
	if got := w.Header().Get("HX-Trigger"); !strings.Contains(got, "clearUserForm") {
		t.Errorf("expected clearUserForm trigger, got %q", got)
	}
}

func TestCreateUserWeb_DuplicateEmailFails(t *testing.T) {
	r := setupTestRouter(t)

	form := url.Values{
		"email":     {"alice@example.com"},
		"username":  {fmt.Sprintf("dup%d", time.Now().UnixNano())},
		"full_name": {"Duplicate"},
		"password":  {"password123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/web/users",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "already exists") {
		t.Error("expected a duplicate message")
	}
}

func TestUpdateUserWeb_Succeeds(t *testing.T) {
	r := setupTestRouter(t)

	// Create a target first.
	createForm := url.Values{
		"email":     {fmt.Sprintf("upd-%d@example.com", time.Now().UnixNano())},
		"username":  {fmt.Sprintf("upduser%d", time.Now().UnixNano())},
		"full_name": {"Before Update"},
		"password":  {"password123"},
	}
	creq := httptest.NewRequest(http.MethodPost, "/web/users",
		strings.NewReader(createForm.Encode()))
	creq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, creq)
	if cw.Code != http.StatusOK {
		t.Fatalf("create: %d %s", cw.Code, cw.Body.String())
	}

	// Extract the new user's ID from the row markup: id="user-N".
	body := cw.Body.String()
	idx := strings.Index(body, `id="user-`)
	if idx < 0 {
		t.Fatalf("created row missing id: %s", body)
	}
	rest := body[idx+len(`id="user-`):]
	end := strings.IndexByte(rest, '"')
	id := rest[:end]

	// Update.
	updateForm := url.Values{
		"email":     {createForm.Get("email")},
		"username":  {createForm.Get("username")},
		"full_name": {"After Update"},
		"is_active": {"on"},
	}
	ureq := httptest.NewRequest(http.MethodPut, "/web/users/"+id,
		strings.NewReader(updateForm.Encode()))
	ureq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	uw := httptest.NewRecorder()
	r.ServeHTTP(uw, ureq)

	if uw.Code != http.StatusOK {
		t.Fatalf("update: %d %s", uw.Code, uw.Body.String())
	}
	if !strings.Contains(uw.Body.String(), "After Update") {
		t.Error("expected the updated name")
	}
}

func TestUpdateUserRoleWeb_Succeeds(t *testing.T) {
	r := setupTestRouter(t)

	// Create a target.
	createForm := url.Values{
		"email":     {fmt.Sprintf("role-%d@example.com", time.Now().UnixNano())},
		"username":  {fmt.Sprintf("roleuser%d", time.Now().UnixNano())},
		"full_name": {"Role Target"},
		"password":  {"password123"},
	}
	creq := httptest.NewRequest(http.MethodPost, "/web/users",
		strings.NewReader(createForm.Encode()))
	creq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, creq)
	if cw.Code != http.StatusOK {
		t.Fatalf("create: %d %s", cw.Code, cw.Body.String())
	}

	body := cw.Body.String()
	idx := strings.Index(body, `id="user-`)
	rest := body[idx+len(`id="user-`):]
	end := strings.IndexByte(rest, '"')
	id := rest[:end]

	// Change role.
	form := url.Values{"role": {"moderator"}}
	req := httptest.NewRequest(http.MethodPut, "/web/users/"+id+"/role",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update role: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "moderator") {
		t.Error("expected the new role in the row")
	}
}

func TestUpdateUserRoleWeb_InvalidRole(t *testing.T) {
	r := setupTestRouter(t)

	form := url.Values{"role": {"not-a-role"}}
	req := httptest.NewRequest(http.MethodPut, "/web/users/1/role",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteUserWeb_Succeeds(t *testing.T) {
	r := setupTestRouter(t)

	// Create a target.
	createForm := url.Values{
		"email":     {fmt.Sprintf("del-%d@example.com", time.Now().UnixNano())},
		"username":  {fmt.Sprintf("deluser%d", time.Now().UnixNano())},
		"full_name": {"Delete Target"},
		"password":  {"password123"},
	}
	creq := httptest.NewRequest(http.MethodPost, "/web/users",
		strings.NewReader(createForm.Encode()))
	creq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, creq)
	if cw.Code != http.StatusOK {
		t.Fatalf("create: %d", cw.Code)
	}

	body := cw.Body.String()
	idx := strings.Index(body, `id="user-`)
	rest := body[idx+len(`id="user-`):]
	end := strings.IndexByte(rest, '"')
	id := rest[:end]

	// Delete.
	req := httptest.NewRequest(http.MethodDelete, "/web/users/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("HX-Trigger"); !strings.Contains(got, "userDeleted") {
		t.Errorf("expected userDeleted trigger, got %q", got)
	}
}

func TestDeleteUserWeb_InvalidID(t *testing.T) {
	r := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodDelete, "/web/users/not-an-id", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------------
// Non-admin denial
// -----------------------------------------------------------------------------

func TestHTMX_NonAdminForbiddenOnUsersPages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	svc := service.NewUserServiceWithPool(pool)
	h := NewWebHandler(svc)

	claims := &auth.Claims{UserID: 1, Username: "u", Role: "user"}

	r := gin.New()
	r.SetFuncMap(GetTemplateFuncs())
	r.LoadHTMLGlob("../templates/**/*.html")
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextClaimsKey, claims)
		c.Next()
	})
	group := r.Group("/web", middleware.RequireRole("admin"))
	{
		group.GET("/users", h.ListUsersPage)
		group.PUT("/users/:id", h.UpdateUserWeb)
		group.PUT("/users/:id/role", h.UpdateUserRoleWeb)
		group.DELETE("/users/:id", h.DeleteUserWeb)
	}

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/web/users"},
		{http.MethodPut, "/web/users/1"},
		{http.MethodPut, "/web/users/1/role"},
		{http.MethodDelete, "/web/users/1"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("HX-Request", "true")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 for non-admin, got %d",
				tc.method, tc.path, w.Code)
		}
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func assertStablePagination(t *testing.T, body string) {
	t.Helper()

	if strings.Contains(body, `hx-target=`) {
		if !strings.Contains(body, `hx-target="#user-list"`) {
			t.Error("expected pagination hx-target to be '#user-list'")
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, `hx-target="#user-list"`) &&
				strings.Contains(line, `hx-swap="outerHTML"`) {
				t.Errorf("pagination must use innerHTML on #user-list: %s",
					strings.TrimSpace(line))
			}
		}
	}
}

var _ = context.Background
