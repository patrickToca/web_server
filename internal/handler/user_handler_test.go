package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"mywebapp/internal/auth"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
	"mywebapp/internal/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.Setup()
	defer cleanup()
	m.Run()
}

func setupRouterWithRole(t *testing.T, role string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testutil.NewPool(t)
	svc := service.NewUserServiceWithPool(pool)
	h := NewUserHandler(svc)

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me-in-production"
	}
	claims := &auth.Claims{
		UserID:   1,
		Username: "test-" + role,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextClaimsKey, claims)
		c.Next()
	})
	api := r.Group("/api/v1", middleware.RequireRole("admin"))
	{
		users := api.Group("/users")
		users.POST("", h.CreateUser)
		users.GET("", h.ListUsers)
		users.GET("/:id", h.GetUser)
		users.PUT("/:id", h.UpdateUser)
		users.PUT("/:id/role", h.UpdateUserRole)
		users.DELETE("/:id", h.DeleteUser)
	}
	r.GET("/_token", func(c *gin.Context) { c.String(http.StatusOK, signed) })
	return r
}

func uniqueEmail() string {
	return fmt.Sprintf("apitest-%d@example.com", time.Now().UnixNano())
}

func uniqueUsername() string {
	return fmt.Sprintf("apiuser%d", time.Now().UnixNano())
}

// Non-admin denial

func TestCreateUser_NonAdminForbidden(t *testing.T) {
	r := setupRouterWithRole(t, "user")

	body, _ := json.Marshal(map[string]any{
		"email": uniqueEmail(), "password": "password123",
		"username": uniqueUsername(), "full_name": "Evil",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestUpdateUserRole_NonAdminForbidden(t *testing.T) {
	r := setupRouterWithRole(t, "user")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/1/role",
		bytes.NewReader([]byte(`{"role":"admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestDeleteUser_NonAdminForbidden(t *testing.T) {
	r := setupRouterWithRole(t, "user")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

// Admin happy paths

func TestCreateUser_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	payload := map[string]any{
		"email": uniqueEmail(), "password": "password123",
		"username": uniqueUsername(), "full_name": "API Created",
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d\nbody: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["role"] != "user" {
		t.Errorf("role = %v, want user", resp["role"])
	}
	if resp["password_hash"] != nil {
		t.Error("password_hash must not leak")
	}
}

func TestCreateUser_AdminInvalidJSON(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestGetUser_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["id"]; !ok {
		t.Error("missing id")
	}
}

func TestGetUser_AdminInvalidID(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestGetUser_AdminNotFound(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/999999", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestListUsers_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["data"]; !ok {
		t.Error("missing data")
	}
	if _, ok := resp["pagination"]; !ok {
		t.Error("missing pagination")
	}
}

func TestUpdateUser_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	create := map[string]any{
		"email": uniqueEmail(), "password": "password123",
		"username": uniqueUsername(), "full_name": "Update Target",
	}
	cbody, _ := json.Marshal(create)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(cbody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	update := map[string]any{
		"email": create["email"], "username": create["username"],
		"full_name": "Updated Name", "is_active": true,
	}
	ubody, _ := json.Marshal(update)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d", id), bytes.NewReader(ubody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated["full_name"] != "Updated Name" {
		t.Errorf("full_name = %v", updated["full_name"])
	}
}

func TestUpdateUserRole_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	create := map[string]any{
		"email": uniqueEmail(), "password": "password123",
		"username": uniqueUsername(), "full_name": "Role Target",
	}
	cbody, _ := json.Marshal(create)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(cbody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d/role", id),
		bytes.NewReader([]byte(`{"role":"moderator"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update role: %d %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated["role"] != "moderator" {
		t.Errorf("role = %v", updated["role"])
	}
}

func TestUpdateUserRole_InvalidRole(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/1/role",
		bytes.NewReader([]byte(`{"role":"superadmin"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestDeleteUser_AdminSucceeds(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	create := map[string]any{
		"email": uniqueEmail(), "password": "password123",
		"username": uniqueUsername(), "full_name": "Delete Target",
	}
	cbody, _ := json.Marshal(create)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(cbody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/users/%d", id), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestDeleteUser_AdminNotFound(t *testing.T) {
	r := setupRouterWithRole(t, "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/999999", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}
