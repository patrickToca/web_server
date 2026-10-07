package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestParseSameSite(t *testing.T) {
	cases := []struct {
		in   string
		want http.SameSite
	}{
		{"lax", http.SameSiteLaxMode},
		{"Lax", http.SameSiteLaxMode},
		{"LAX", http.SameSiteLaxMode},
		{"strict", http.SameSiteStrictMode},
		{"STRICT", http.SameSiteStrictMode},
		{"none", http.SameSiteNoneMode},
		{"", http.SameSiteLaxMode},
		{"garbage", http.SameSiteLaxMode},
		{" lax ", http.SameSiteLaxMode},
	}
	for _, tc := range cases {
		if got := parseSameSite(tc.in); got != tc.want {
			t.Errorf("parseSameSite(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLoadCookieConfig_Defaults(t *testing.T) {
	for _, k := range []string{"COOKIE_SECURE", "COOKIE_SAMESITE", "COOKIE_DOMAIN"} {
		t.Setenv(k, "")
	}

	cfg := LoadCookieConfig()
	if !cfg.Secure {
		t.Error("expected Secure=true")
	}
	if cfg.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v", cfg.SameSite)
	}
	if cfg.Path != "/" {
		t.Errorf("Path = %q", cfg.Path)
	}
	if cfg.Domain != "" {
		t.Errorf("Domain = %q", cfg.Domain)
	}
}

func TestLoadCookieConfig_Overrides(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("COOKIE_SAMESITE", "strict")
	t.Setenv("COOKIE_DOMAIN", ".example.com")

	cfg := LoadCookieConfig()
	if cfg.Secure {
		t.Error("Secure should be false")
	}
	if cfg.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v", cfg.SameSite)
	}
	if cfg.Domain != ".example.com" {
		t.Errorf("Domain = %q", cfg.Domain)
	}
}

func TestGetEnv(t *testing.T) {
	t.Setenv("TEST_GET_ENV", "value")
	if got := getEnv("TEST_GET_ENV", "def"); got != "value" {
		t.Errorf("got %q", got)
	}
	t.Setenv("TEST_GET_ENV", "")
	if got := getEnv("TEST_GET_ENV", "def"); got != "def" {
		t.Errorf("got %q", got)
	}
}

func TestGetEnvBool(t *testing.T) {
	cases := []struct {
		val, def string
		want     bool
	}{
		{"true", "false", true},
		{"1", "false", true},
		{"yes", "false", true},
		{"on", "false", true},
		{"false", "true", false},
		{"0", "true", false},
		{"no", "true", false},
		{"off", "true", false},
		{"", "true", true},
		{"", "false", false},
		{"garbage", "true", true},
	}
	for _, tc := range cases {
		t.Setenv("TEST_BOOL", tc.val)
		def := tc.def == "true"
		if got := getEnvBool("TEST_BOOL", def); got != tc.want {
			t.Errorf("getEnvBool(%q, %v) = %v, want %v", tc.val, def, got, tc.want)
		}
	}
}

func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestSetAuthCookie_Attributes(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	setAuthCookie(c, cfg, "test-token-abc", 15*time.Minute)

	raw := w.Header().Get("Set-Cookie")
	for _, want := range []string{
		CookieName + "=test-token-abc",
		"Path=/", "Max-Age=900", "HttpOnly", "SameSite=Lax",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in %s", want, raw)
		}
	}
	if strings.Contains(raw, "Secure") {
		t.Errorf("unexpected Secure: %s", raw)
	}
}

func TestSetAuthCookie_Secure(t *testing.T) {
	cfg := CookieConfig{Secure: true, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	setAuthCookie(c, cfg, "tok", 15*time.Minute)

	if !strings.Contains(w.Header().Get("Set-Cookie"), "Secure") {
		t.Error("expected Secure")
	}
}

func TestClearAuthCookie(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	clearAuthCookie(c, cfg)

	raw := w.Header().Get("Set-Cookie")
	if !strings.Contains(raw, CookieName+"=") {
		t.Errorf("missing name: %s", raw)
	}
	if !strings.Contains(raw, "HttpOnly") {
		t.Errorf("missing HttpOnly: %s", raw)
	}
}

func TestSetRefreshCookie_Attributes(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	setRefreshCookie(c, cfg, "refresh-tok", 7*24*time.Hour)

	raw := w.Header().Get("Set-Cookie")
	for _, want := range []string{
		RefreshCookieName + "=refresh-tok",
		"Max-Age=604800", "HttpOnly", "SameSite=Lax",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in %s", want, raw)
		}
	}
}

func TestClearRefreshCookie(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	clearRefreshCookie(c, cfg)

	if !strings.Contains(w.Header().Get("Set-Cookie"), RefreshCookieName+"=") {
		t.Error("missing name")
	}
}

func TestSetCSRFCookie_NotHttpOnly(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	setCSRFCookie(c, cfg, "csrf-tok")

	raw := w.Header().Get("Set-Cookie")
	if !strings.Contains(raw, CSRFCookieName+"=csrf-tok") {
		t.Errorf("missing name: %s", raw)
	}
	if strings.Contains(raw, "HttpOnly") {
		t.Errorf("CSRF must not be HttpOnly: %s", raw)
	}
}

func TestClearCSRFCookie(t *testing.T) {
	cfg := CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode, Path: "/"}
	c, w := newTestContext()

	clearCSRFCookie(c, cfg)

	if !strings.Contains(w.Header().Get("Set-Cookie"), CSRFCookieName+"=") {
		t.Error("missing name")
	}
}
