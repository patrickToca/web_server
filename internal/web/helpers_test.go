package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/domain"
)

func TestToUserViewModel_ActiveUser(t *testing.T) {
	u := &domain.User{
		ID:       1,
		Email:    "alice@example.com",
		Username: "alice",
		FullName: "Alice Johnson",
		IsActive: true,
		Role:     "admin",
	}
	vm := ToUserViewModel(u)

	if vm.Status != "Active" {
		t.Errorf("Status = %q, want Active", vm.Status)
	}
	if vm.StatusBadge != "bg-green-100 text-green-800" {
		t.Errorf("StatusBadge = %q", vm.StatusBadge)
	}
	if vm.ID != 1 || vm.Email != "alice@example.com" || vm.Role != "admin" {
		t.Errorf("fields mismatch: %+v", vm)
	}
}

func TestToUserViewModel_InactiveUser(t *testing.T) {
	u := &domain.User{ID: 2, IsActive: false}
	vm := ToUserViewModel(u)

	if vm.Status != "Inactive" {
		t.Errorf("Status = %q, want Inactive", vm.Status)
	}
	if vm.StatusBadge != "bg-red-100 text-red-800" {
		t.Errorf("StatusBadge = %q", vm.StatusBadge)
	}
}

func TestToUserViewModelFromRequest(t *testing.T) {
	req := domain.CreateUserRequest{
		Email:    "new@example.com",
		Username: "newuser",
		FullName: "New User",
		Password: "password123",
	}
	vm := ToUserViewModelFromRequest(req)

	if vm.Email != "new@example.com" || vm.Username != "newuser" || vm.FullName != "New User" {
		t.Errorf("fields mismatch: %+v", vm)
	}
	if vm.Role != "user" {
		t.Errorf("Role = %q, want user", vm.Role)
	}
	if !vm.IsActive {
		t.Error("new users should be active by default in the view model")
	}
}

func TestToUserViewModelFromUpdateRequest_Active(t *testing.T) {
	active := true
	req := domain.UpdateUserRequest{
		Email:    "u@example.com",
		Username: "u",
		FullName: "U",
		IsActive: &active,
	}
	vm := ToUserViewModelFromUpdateRequest(req, 7, "moderator")

	if vm.ID != 7 {
		t.Errorf("ID = %d, want 7", vm.ID)
	}
	if vm.Role != "moderator" {
		t.Errorf("Role = %q, want moderator", vm.Role)
	}
	if !vm.IsActive {
		t.Error("expected IsActive to be true")
	}
}

func TestToUserViewModelFromUpdateRequest_NoRoleFallsBackToUser(t *testing.T) {
	active := false
	req := domain.UpdateUserRequest{
		Email:    "u@example.com",
		Username: "u",
		FullName: "U",
		IsActive: &active,
	}
	vm := ToUserViewModelFromUpdateRequest(req, 1, "")

	if vm.Role != "user" {
		t.Errorf("Role = %q, want user", vm.Role)
	}
	if vm.IsActive {
		t.Error("expected IsActive to be false")
	}
}

func TestParseID(t *testing.T) {
	id, err := ParseID("42")
	if err != nil || id != 42 {
		t.Errorf("ParseID(42) = %d, %v", id, err)
	}
	if _, err := ParseID("not-a-number"); err == nil {
		t.Error("expected error for non-numeric ID")
	}
	if _, err := ParseID(""); err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestGetPage(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 1},
		{"?page=3", 3},
		{"?page=0", 1},
		{"?page=-5", 1},
		{"?page=abc", 1},
	}
	for _, tc := range cases {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/"+tc.query, nil)
		if got := GetPage(c); got != tc.want {
			t.Errorf("GetPage(%q) = %d, want %d", tc.query, got, tc.want)
		}
	}
}

func TestGetLimit(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 10},
		{"?limit=25", 25},
		{"?limit=0", 10},
		{"?limit=200", 10},
		{"?limit=abc", 10},
	}
	for _, tc := range cases {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/"+tc.query, nil)
		if got := GetLimit(c); got != tc.want {
			t.Errorf("GetLimit(%q) = %d, want %d", tc.query, got, tc.want)
		}
	}
}

func TestFormatTimestamp(t *testing.T) {
	if got := FormatTimestamp(time.Time{}); got != "" {
		t.Errorf("zero time should be empty, got %q", got)
	}
	ts := time.Date(2026, 10, 4, 12, 34, 56, 0, time.UTC)
	if got := FormatTimestamp(ts); got != "2026-10-04 12:34:56" {
		t.Errorf("FormatTimestamp = %q", got)
	}
}

func TestTruncateText(t *testing.T) {
	if got := TruncateText("hello", 10); got != "hello" {
		t.Errorf("short text altered: %q", got)
	}
	if got := TruncateText("hello world", 5); got != "hello..." {
		t.Errorf("TruncateText = %q", got)
	}
}

func TestSafeString(t *testing.T) {
	if got := SafeString(`say "hi"`); got != `say &quot;hi&quot;` {
		t.Errorf("SafeString = %q", got)
	}
}
