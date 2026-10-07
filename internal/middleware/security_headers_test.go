package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders_Defaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := SecurityHeadersConfig{
		CSPReportOnly:             true,
		FrameOptions:              "DENY",
		ReferrerPolicy:            "strict-origin-when-cross-origin",
		PermissionsPolicy:         defaultPermissionsPolicy,
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
	}

	r := gin.New()
	r.Use(SecurityHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	h := w.Header()

	// CSP should be report-only in this config.
	if h.Get("Content-Security-Policy") != "" {
		t.Error("expected no enforcing CSP when ReportOnly is true")
	}
	csp := h.Get("Content-Security-Policy-Report-Only")
	if csp == "" {
		t.Fatal("expected Content-Security-Policy-Report-Only header")
	}
	for _, want := range []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' 'unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"upgrade-insecure-requests",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q\nCSP: %s", want, csp)
		}
	}

	// HSTS must be absent when MaxAge is 0 (dev over plain HTTP).
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("expected no HSTS header when MaxAge=0")
	}

	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if got := h.Get("Permissions-Policy"); got != defaultPermissionsPolicy {
		t.Errorf("Permissions-Policy = %q", got)
	}
	if got := h.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Errorf("COOP = %q", got)
	}
	if got := h.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Errorf("CORP = %q", got)
	}
}

func TestSecurityHeaders_EnforcingCSP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := SecurityHeadersConfig{
		CSPReportOnly:             false,
		FrameOptions:              "SAMEORIGIN",
		ReferrerPolicy:            "no-referrer",
		PermissionsPolicy:         "",
		CrossOriginOpenerPolicy:   "",
		CrossOriginResourcePolicy: "",
	}

	r := gin.New()
	r.Use(SecurityHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	h := w.Header()
	if h.Get("Content-Security-Policy-Report-Only") != "" {
		t.Error("expected no report-only header when enforcing")
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("expected enforcing Content-Security-Policy header")
	}
	if got := h.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := h.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if h.Get("Permissions-Policy") != "" {
		t.Error("expected no Permissions-Policy when configured empty")
	}
}

func TestSecurityHeaders_HSTS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := SecurityHeadersConfig{
		CSPReportOnly:         true,
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: true,
	}
	r := gin.New()
	r.Use(SecurityHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Get("Strict-Transport-Security")
	want := "max-age=31536000; includeSubDomains"
	if got != want {
		t.Errorf("HSTS = %q, want %q", got, want)
	}
}

func TestSecurityHeaders_HSTSPreload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := SecurityHeadersConfig{
		CSPReportOnly: true,
		HSTSMaxAge:    63072000,
		HSTSPreload:   true,
	}
	r := gin.New()
	r.Use(SecurityHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Get("Strict-Transport-Security")
	want := "max-age=63072000; includeSubDomains; preload"
	if got != want {
		t.Errorf("HSTS = %q, want %q", got, want)
	}
}

func TestSecurityHeaders_CSPReportURI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := SecurityHeadersConfig{
		CSPReportOnly: true,
		CSPReportURI:  "https://example.com/csp-report",
	}
	r := gin.New()
	r.Use(SecurityHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy-Report-Only")
	if !strings.Contains(csp, "report-uri https://example.com/csp-report") {
		t.Errorf("CSP missing report-uri\nCSP: %s", csp)
	}
}

func TestLoadSecurityHeadersConfig_Defaults(t *testing.T) {
	// Clear env vars that could leak from the host.
	for _, k := range []string{
		"CSP_REPORT_ONLY", "CSP_REPORT_URI", "HSTS_MAX_AGE",
		"HSTS_INCLUDE_SUBDOMAINS", "HSTS_PRELOAD", "X_FRAME_OPTIONS",
		"REFERRER_POLICY", "PERMISSIONS_POLICY", "COOP", "CORP",
	} {
		t.Setenv(k, "")
	}

	cfg := LoadSecurityHeadersConfig()
	if !cfg.CSPReportOnly {
		t.Error("expected CSP_REPORT_ONLY default true")
	}
	if cfg.HSTSMaxAge != 0 {
		t.Errorf("expected HSTS_MAX_AGE default 0, got %d", cfg.HSTSMaxAge)
	}
	if cfg.FrameOptions != "DENY" {
		t.Errorf("expected X_FRAME_OPTIONS default DENY, got %q", cfg.FrameOptions)
	}
	if cfg.ReferrerPolicy != "strict-origin-when-cross-origin" {
		t.Errorf("unexpected ReferrerPolicy %q", cfg.ReferrerPolicy)
	}
}

func TestLoadSecurityHeadersConfig_Overrides(t *testing.T) {
	t.Setenv("CSP_REPORT_ONLY", "false")
	t.Setenv("HSTS_MAX_AGE", "31536000")
	t.Setenv("HSTS_INCLUDE_SUBDOMAINS", "true")
	t.Setenv("X_FRAME_OPTIONS", "SAMEORIGIN")

	cfg := LoadSecurityHeadersConfig()
	if cfg.CSPReportOnly {
		t.Error("expected CSP_REPORT_ONLY false")
	}
	if cfg.HSTSMaxAge != 31536000 {
		t.Errorf("HSTS_MAX_AGE = %d, want 31536000", cfg.HSTSMaxAge)
	}
	if !cfg.HSTSIncludeSubdomains {
		t.Error("expected HSTSIncludeSubdomains true")
	}
	if cfg.FrameOptions != "SAMEORIGIN" {
		t.Errorf("X_FRAME_OPTIONS = %q, want SAMEORIGIN", cfg.FrameOptions)
	}
}
