// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer.
package middleware

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/config"
)

// ErrInsecureProductionConfig is returned by LoadSecurityHeadersConfig
// when the deployment is production or staging and the configuration
// disables a control that must be on in those environments.
var ErrInsecureProductionConfig = errors.New("insecure security-headers configuration")

// SecurityHeadersConfig controls which headers are emitted and how strict
// the Content-Security-Policy is.
type SecurityHeadersConfig struct {
	// CSPReportOnly, when true, sends Content-Security-Policy-Report-Only
	// instead of the enforcing header. Use this in dev/staging to observe
	// violations before enforcing. Controlled by CSP_REPORT_ONLY env var.
	//
	// In production and staging the loader refuses to return a config
	// with this set to true. Report-only is a development posture; a
	// deployed environment that ships without an enforcing CSP has no
	// protection at all.
	CSPReportOnly bool

	// CSPReportURI, when non-empty, is added as a report-uri directive so
	// browsers POST violation reports to it. Controlled by CSP_REPORT_URI.
	CSPReportURI string

	// HSTSMaxAge is the max-age (seconds) for Strict-Transport-Security.
	// 0 disables HSTS. Controlled by HSTS_MAX_AGE (default 0).
	//
	// In production and staging the loader refuses to return a config
	// with this set to 0. HSTS off in a deployed environment means the
	// browser will happily follow a downgrade to HTTP, which is the
	// exact attack HSTS exists to prevent.
	HSTSMaxAge int

	// HSTSIncludeSubdomains adds includeSubDomains to HSTS.
	// Controlled by HSTS_INCLUDE_SUBDOMAINS (default false).
	HSTSIncludeSubdomains bool

	// HSTSPreload adds preload to HSTS. Implies includeSubdomains.
	// Controlled by HSTS_PRELOAD (default false).
	//
	// Preload is a one-way commitment: submission to the preload list
	// is permanent for a browser release cycle and cannot be undone in
	// under six weeks. It is not enabled by default. Enable it only
	// once you are certain the domain will serve HTTPS forever.
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
//
// In production and staging the loader enforces two constraints and
// returns ErrInsecureProductionConfig if either is violated:
//
//   - HSTS_MAX_AGE must be greater than zero.
//   - CSP_REPORT_ONLY must be false.
//
// Both defaults are safe for development (HSTS off, CSP report-only)
// and unsafe for a deployed environment. The check is here rather than
// in a comment because a comment does not fail a boot.
func LoadSecurityHeadersConfig() (SecurityHeadersConfig, error) {
	cfg := SecurityHeadersConfig{
		CSPReportOnly:             getEnvBool("CSP_REPORT_ONLY", true),
		CSPReportURI:              getEnv("CSP_REPORT_URI", ""),
		HSTSMaxAge:                getEnvInt("HSTS_MAX_AGE", 0),
		HSTSIncludeSubdomains:     getEnvBool("HSTS_INCLUDE_SUBDOMAINS", false),
		HSTSPreload:               getEnvBool("HSTSPRELOAD", false),
		FrameOptions:              getEnv("X_FRAME_OPTIONS", "DENY"),
		ReferrerPolicy:            getEnv("REFERRER_POLICY", "strict-origin-when-cross-origin"),
		PermissionsPolicy:         getEnv("PERMISSIONS_POLICY", defaultPermissionsPolicy),
		CrossOriginOpenerPolicy:   getEnv("COOP", "same-origin"),
		CrossOriginResourcePolicy: getEnv("CORP", "same-origin"),
	}

	if config.IsProductionOrStaging() {
		if cfg.HSTSMaxAge == 0 {
			return cfg, fmt.Errorf(
				"%w: HSTS_MAX_AGE is 0 in %s; set it to at least 31536000",
				ErrInsecureProductionConfig, config.Current())
		}
		if cfg.CSPReportOnly {
			return cfg, fmt.Errorf(
				"%w: CSP_REPORT_ONLY is true in %s; set it to false once you have confirmed "+
					"the CSP does not break the application",
				ErrInsecureProductionConfig, config.Current())
		}
		if cfg.HSTSPreload && !cfg.HSTSIncludeSubdomains {
			return cfg, fmt.Errorf(
				"%w: HSTS_PRELOAD is true but HSTS_INCLUDE_SUBDOMAINS is false; "+
					"preload requires includeSubDomains",
				ErrInsecureProductionConfig)
		}
	}

	return cfg, nil
}

// defaultPermissionsPolicy disables features the app does not use. Browsers
// ignore unknown features, so this is forward-compatible. Camera/mic/geo
// are disabled outright; we do not ship any code that calls them.
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

		// HSTS is only meaningful over HTTPS. The loader refuses to
		// return 0 in production, so this branch is reached only in
		// development.
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
// Env helpers
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
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}
