// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer: gzip compression, per-client rate limiting, request IDs,
// access logging, and security headers.
package middleware

import (
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersConfig controls which headers are emitted and how strict
// the Content-Security-Policy is. Zero value is safe for development.
type SecurityHeadersConfig struct {
	// CSPReportOnly, when true, sends Content-Security-Policy-Report-Only
	// instead of the enforcing header. Use this in dev/staging to observe
	// violations before enforcing. Controlled by CSP_REPORT_ONLY env var.
	CSPReportOnly bool

	// CSPReportURI, when non-empty, is added as a report-uri directive so
	// browsers POST violation reports to it. Controlled by CSP_REPORT_URI.
	CSPReportURI string

	// HSTSMaxAge is the max-age (seconds) for Strict-Transport-Security.
	// 0 disables HSTS (correct for local dev over plain HTTP).
	// Controlled by HSTS_MAX_AGE (default 0).
	HSTSMaxAge int

	// HSTSIncludeSubdomains adds includeSubDomains to HSTS.
	// Controlled by HSTS_INCLUDE_SUBDOMAINS (default false).
	HSTSIncludeSubdomains bool

	// HSTSPreload adds preload to HSTS. Implies includeSubDomains.
	// Controlled by HSTS_PRELOAD (default false).
	HSTSPreload bool

	// FrameOptions is the X-Frame-Options value: DENY or SAMEORIGIN.
	// Controlled by X_FRAME_OPTIONS (default DENY).
	FrameOptions string

	// ReferrerPolicy is the Referrer-Policy value.
	// Controlled by REFERRER_POLICY (default strict-origin-when-cross-origin).
	ReferrerPolicy string

	// PermissionsPolicy is the Permissions-Policy value.
	// Controlled by PERMISSIONS_POLICY.
	PermissionsPolicy string

	// CrossOriginOpenerPolicy is the Cross-Origin-Opener-Policy value.
	// Controlled by COOP (default same-origin).
	CrossOriginOpenerPolicy string

	// CrossOriginResourcePolicy is the Cross-Origin-Resource-Policy value.
	// Controlled by CORP (default same-origin).
	CrossOriginResourcePolicy string
}

// LoadSecurityHeadersConfig builds a config from environment variables,
// falling back to safe defaults.
func LoadSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		CSPReportOnly:             getEnvBool("CSP_REPORT_ONLY", true),
		CSPReportURI:              getEnv("CSP_REPORT_URI", ""),
		HSTSMaxAge:                getEnvInt("HSTS_MAX_AGE", 0),
		HSTSIncludeSubdomains:     getEnvBool("HSTS_INCLUDE_SUBDOMAINS", false),
		HSTSPreload:               getEnvBool("HSTS_PRELOAD", false),
		FrameOptions:              getEnv("X_FRAME_OPTIONS", "DENY"),
		ReferrerPolicy:            getEnv("REFERRER_POLICY", "strict-origin-when-cross-origin"),
		PermissionsPolicy:         getEnv("PERMISSIONS_POLICY", defaultPermissionsPolicy),
		CrossOriginOpenerPolicy:   getEnv("COOP", "same-origin"),
		CrossOriginResourcePolicy: getEnv("CORP", "same-origin"),
	}
}

// defaultPermissionsPolicy disables features the app does not use. Browsers
// ignore unknown features, so this is forward-compatible. Camera/mic/geo
// are disabled outright; we don't ship any code that calls them.
const defaultPermissionsPolicy = "accelerometer=(), camera=(), geolocation=(), " +
	"gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"

// buildCSP assembles the Content-Security-Policy header value.
//
// Notes for this specific stack:
//   - 'unsafe-inline' in script-src: HTMX 1.9 injects inline <script> for
//     hx-on attributes and template fragments, and your base.html/login.html
//     have inline <script> blocks (toasts, password toggle, HTMX hooks).
//   - 'unsafe-eval' in script-src: HTMX evaluates hx-on expressions and
//     hx-vals/hx-vars JSON via `new Function(...)`.
//   - 'unsafe-inline' in style-src: Tailwind's base reset + your inline
//     <style> blocks in base.html/login.html/register.html.
//   - connect-src 'self': HTMX uses XMLHttpRequest/fetch to same origin.
//   - img-src 'self' data:: favicon is a data: SVG URI.
//   - frame-ancestors 'none': matches X-Frame-Options DENY.
func buildCSP(reportURI string) string {
	directives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' 'unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"media-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"upgrade-insecure-requests",
	}
	if reportURI != "" {
		directives = append(directives, "report-uri "+reportURI)
	}
	return strings.Join(directives, "; ")
}

// SecurityHeaders returns a Gin middleware that sets defence-in-depth HTTP
// security headers on every response.
//
// Headers set:
//   - Content-Security-Policy (or ...-Report-Only)
//   - Strict-Transport-Security (only when HSTSMaxAge > 0)
//   - X-Frame-Options
//   - X-Content-Type-Options
//   - Referrer-Policy
//   - Permissions-Policy
//   - Cross-Origin-Opener-Policy
//   - Cross-Origin-Resource-Policy
//
// Register this middleware after RequestID/AccessLog so that any CSP
// violation reports can be correlated with the request_id in access logs.
func SecurityHeaders(cfg SecurityHeadersConfig) gin.HandlerFunc {
	csp := buildCSP(cfg.CSPReportURI)

	// Precompute HSTS header value once; it's static per process.
	hsts := ""
	if cfg.HSTSMaxAge > 0 {
		hsts = "max-age=" + strconv.Itoa(cfg.HSTSMaxAge)
		if cfg.HSTSPreload {
			hsts += "; includeSubDomains; preload"
		} else if cfg.HSTSIncludeSubdomains {
			hsts += "; includeSubDomains"
		}
	}

	return func(c *gin.Context) {
		h := c.Writer.Header()

		// CSP: report-only in dev/staging, enforcing in prod.
		if cfg.CSPReportOnly {
			h.Set("Content-Security-Policy-Report-Only", csp)
		} else {
			h.Set("Content-Security-Policy", csp)
		}

		// HSTS is only meaningful over HTTPS. Skip on plain HTTP so
		// localhost dev isn't pinned to https://.
		if hsts != "" {
			h.Set("Strict-Transport-Security", hsts)
		}

		// Clickjacking defence (legacy browsers that ignore frame-ancestors).
		if cfg.FrameOptions != "" {
			h.Set("X-Frame-Options", cfg.FrameOptions)
		}

		// MIME sniffing defence.
		h.Set("X-Content-Type-Options", "nosniff")

		// Referrer leakage control.
		if cfg.ReferrerPolicy != "" {
			h.Set("Referrer-Policy", cfg.ReferrerPolicy)
		}

		// Feature policy / permissions policy.
		if cfg.PermissionsPolicy != "" {
			h.Set("Permissions-Policy", cfg.PermissionsPolicy)
		}

		// Cross-origin isolation.
		if cfg.CrossOriginOpenerPolicy != "" {
			h.Set("Cross-Origin-Opener-Policy", cfg.CrossOriginOpenerPolicy)
		}
		if cfg.CrossOriginResourcePolicy != "" {
			h.Set("Cross-Origin-Resource-Policy", cfg.CrossOriginResourcePolicy)
		}

		c.Next()
	}
}

// -----------------------------------------------------------------------------
// Env helpers (kept local to avoid importing pkg/db from middleware)
// -----------------------------------------------------------------------------

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return def
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return def
		}
	}
	return n
}
