package web

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Cookie configuration, driven by environment variables.
//
//	COOKIE_SECURE   - "true" (default) sets the Secure attribute.
//	                  Set to "false" for local HTTP dev, or the browser
//	                  will refuse to send the cookie back.
//	COOKIE_SAMESITE - "lax" (default) | "strict" | "none".
//	COOKIE_DOMAIN   - optional. Empty (default) means "host-only".
//
// See docs/security-headers.adoc for the full rationale behind these
// attributes.
const (
	// CookieName is the access-token JWT cookie.
	CookieName = "auth_token"

	// RefreshCookieName is the refresh-token JWT cookie. It has a
	// longer Max-Age than the access cookie but otherwise mirrors
	// its transport attributes. Distinct name so a confusion attack
	// (presenting one as the other) fails at the cookie level before
	// it even reaches JWT validation.
	RefreshCookieName = "refresh_token"

	// CSRFCookieName is the CSRF token cookie.
	CSRFCookieName = "csrf_token"

	// ContextUserKey is the gin context key for the authenticated claims.
	ContextUserKey = "auth_claims"
)

// CookieConfig holds the resolved cookie attributes.
type CookieConfig struct {
	Secure   bool
	SameSite http.SameSite
	Domain   string
	Path     string
}

// LoadCookieConfig reads cookie attributes from the environment.
func LoadCookieConfig() CookieConfig {
	return CookieConfig{
		Secure:   getEnvBool("COOKIE_SECURE", true),
		SameSite: parseSameSite(getEnv("COOKIE_SAMESITE", "lax")),
		Domain:   getEnv("COOKIE_DOMAIN", ""),
		Path:     "/",
	}
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "lax", "":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteLaxMode
	}
}

// setAuthCookie writes the access-token JWT cookie. Its Max-Age matches
// the access token's TTL (default 15 min), so the browser stops sending
// it at the same instant the server stops accepting it.
func setAuthCookie(c *gin.Context, cfg CookieConfig, token string, ttl time.Duration) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(
		CookieName,
		token,
		int(ttl.Seconds()),
		cfg.Path,
		cfg.Domain,
		cfg.Secure,
		true, // HttpOnly — never readable from JS
	)
}

// clearAuthCookie expires the access-token cookie.
func clearAuthCookie(c *gin.Context, cfg CookieConfig) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(CookieName, "", -1, cfg.Path, cfg.Domain, cfg.Secure, true)
}

// setRefreshCookie writes the refresh-token JWT cookie. Its Max-Age
// matches the refresh token's TTL (default 7 days). HttpOnly for the
// same reason as the access cookie: the browser must never expose it
// to JavaScript.
func setRefreshCookie(c *gin.Context, cfg CookieConfig, token string, ttl time.Duration) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(
		RefreshCookieName,
		token,
		int(ttl.Seconds()),
		cfg.Path,
		cfg.Domain,
		cfg.Secure,
		true, // HttpOnly
	)
}

// clearRefreshCookie expires the refresh-token cookie.
func clearRefreshCookie(c *gin.Context, cfg CookieConfig) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(RefreshCookieName, "", -1, cfg.Path, cfg.Domain, cfg.Secure, true)
}

// setCSRFCookie writes the CSRF token cookie. Not HttpOnly: the JS in
// base.html reads it to inject X-CSRF-Token on HTMX requests.
func setCSRFCookie(c *gin.Context, cfg CookieConfig, token string) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(CSRFCookieName, token, 0, cfg.Path, cfg.Domain, cfg.Secure, false)
}

// clearCSRFCookie expires the CSRF cookie.
func clearCSRFCookie(c *gin.Context, cfg CookieConfig) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(CSRFCookieName, "", -1, cfg.Path, cfg.Domain, cfg.Secure, false)
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
