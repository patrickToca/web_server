// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer: gzip compression, per-client rate limiting, request IDs,
// access logging, security headers, and CSRF protection.
package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/config"
)

// CSRF configuration constants. These are defaults; override via
// LoadCSRFConfig if the deployment needs different values.
const (
	// defaultCSRFCookieName is the name of the readable CSRF cookie.
	// Distinct from the auth cookie name (auth_token) so a
	// confused-deputy attack cannot swap one for the other.
	defaultCSRFCookieName = "csrf_token"

	// defaultCSRFFormField is the name of the hidden input field in
	// every mutating form.
	defaultCSRFFormField = "csrf_token"

	// defaultCSRFHeaderName is the request header HTMX sends on every
	// request (injected by the htmx:configRequest listener in the
	// shell templates). API clients can also use this header.
	defaultCSRFHeaderName = "X-CSRF-Token"

	// defaultCSRFTokenLength is the number of random bytes in a token,
	// before base64url encoding. 32 bytes = 256 bits of entropy.
	defaultCSRFTokenLength = 32

	// csrfContextKey is the unexported key under which the current
	// request's CSRF token is stored in the gin context.
	csrfContextKey = "csrf_token"

	// bearerAuthenticatedKey is set by AuthMiddleware when a request
	// was authenticated via an Authorization: Bearer header.
	// CSRFMiddleware reads this to decide the exemption. A request
	// that merely carries a Bearer header without being authenticated
	// does not qualify.
	bearerAuthenticatedKey = "csrf_bearer_authenticated"

	// minimumCSRFKeyBytes is the shortest HMAC key accepted for token
	// signing. Matches the JWT minimum.
	minimumCSRFKeyBytes = 32
)

// ErrCSRFMismatch is returned internally when the submitted token does
// not match the cookie. Handlers do not see this; the middleware aborts
// with 403 before the handler runs.
var ErrCSRFMismatch = errors.New("csrf token mismatch")

// ErrWeakCSRFSecret is returned by LoadCSRFConfig when CSRF_SECRET is
// missing in production or too short to be safe.
var ErrWeakCSRFSecret = errors.New("csrf secret is missing or too weak")

// CSRFConfig holds the CSRF middleware's configuration.
type CSRFConfig struct {
	CookieName   string
	FormField    string
	HeaderName   string
	TokenLength  int
	CookiePath   string
	CookieDomain string
	CookieSecure bool

	// CookieMaxAge is the cookie's Max-Age in seconds.
	//
	//   0  = session cookie (deleted when the browser closes).
	//   >0 = persistent cookie, seconds until expiry.
	//
	// A negative value is not used: net/http treats it as an
	// immediate deletion. The default 0 is the session-cookie value.
	CookieMaxAge int

	// SameSite must be a valid http.SameSite value. Defaults to Lax.
	// Lax is correct for this app: blocks cross-site POSTs, allows
	// top-level navigations to carry the cookie.
	SameSite http.SameSite

	// HMACKey signs tokens so the server can distinguish a token it
	// issued from an arbitrary string an attacker may have planted.
	// Loaded from CSRF_SECRET. In production and staging the key is
	// mandatory; in development a random per-process key is generated.
	HMACKey []byte
}

// LoadCSRFConfig builds a CSRFConfig from environment variables.
//
// It deliberately reuses the same env vars as LoadCookieConfig
// (COOKIE_SECURE, COOKIE_SAMESITE, COOKIE_DOMAIN) so the two cookies
// never disagree about their transport-level attributes. New env vars
// specific to CSRF (CSRF_SECRET, CSRF_TOKEN_LENGTH, CSRF_HEADER_NAME,
// CSRF_FORM_FIELD) default to safe values.
//
// Returns ErrWeakCSRFSecret when CSRF_SECRET is unset or too short in
// production or staging. Callers must treat that as fatal.
func LoadCSRFConfig() (CSRFConfig, error) {
	key, err := loadCSRFKey()
	if err != nil {
		return CSRFConfig{}, err
	}
	return CSRFConfig{
		CookieName:   defaultCSRFCookieName,
		FormField:    getEnv("CSRF_FORM_FIELD", defaultCSRFFormField),
		HeaderName:   getEnv("CSRF_HEADER_NAME", defaultCSRFHeaderName),
		TokenLength:  getEnvInt("CSRF_TOKEN_LENGTH", defaultCSRFTokenLength),
		CookiePath:   "/",
		CookieDomain: getEnv("COOKIE_DOMAIN", ""),
		CookieSecure: getEnvBool("COOKIE_SECURE", true),
		CookieMaxAge: getEnvInt("CSRF_COOKIE_MAX_AGE", 0),
		SameSite:     parseSameSiteFromEnv("COOKIE_SAMESITE", "lax"),
		HMACKey:      key,
	}, nil
}

// loadCSRFKey returns the HMAC key for CSRF tokens.
//
// In production and staging CSRF_SECRET is mandatory and must be at
// least 32 bytes. A missing secret in a clustered deployment is worse
// than a missing JWT secret: a per-process key means tokens issued by
// one node are rejected by every other node, producing intermittent
// 403s that look like client bugs. Refusing to start is the only
// correct behaviour.
//
// In development a random per-process key is generated. Tokens do not
// survive a restart, which is fine on a dev machine.
func loadCSRFKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("CSRF_SECRET"))

	if raw == "" {
		if config.IsProduction() || config.IsStaging() {
			return nil, fmt.Errorf(
				"%w: CSRF_SECRET is unset (APP_ENV=%s)",
				ErrWeakCSRFSecret, config.Current(),
			)
		}
		key := make([]byte, minimumCSRFKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("csrf: generate dev key: %w", err)
		}
		slog.Warn("csrf: CSRF_SECRET is unset; using a random per-process key. " +
			"Tokens will be invalidated on restart and will not work " +
			"across multiple instances. Set CSRF_SECRET before deploying.")
		return key, nil
	}

	key := []byte(raw)
	if len(key) < minimumCSRFKeyBytes {
		if config.IsProduction() || config.IsStaging() {
			return nil, fmt.Errorf(
				"%w: CSRF_SECRET is %d bytes; need at least %d",
				ErrWeakCSRFSecret, len(key), minimumCSRFKeyBytes,
			)
		}
		slog.Warn("csrf: CSRF_SECRET is shorter than the recommended minimum; "+
			"this is tolerated in development only",
			"length", len(key), "minimum", minimumCSRFKeyBytes)
	}
	return key, nil
}

// parseSameSiteFromEnv mirrors the parsing in cookies.go but lives here
// because middleware cannot import web (web imports middleware).
func parseSameSiteFromEnv(key, def string) http.SameSite {
	v := strings.ToLower(strings.TrimSpace(getEnv(key, def)))
	switch v {
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

// generateToken returns a signed, URL-safe token of the form
//
//	<random-bytes>.<hmac>
//
// where the HMAC covers the random bytes. The signature lets the server
// verify that a token was issued by it, closing the cookie-tossing path
// in which an attacker plants a valid-looking cookie.
func generateToken(length int, key []byte) (string, error) {
	if length < 16 {
		return "", errors.New("csrf: token length must be >= 16 bytes")
	}
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

// verifyToken reports whether tok was issued by this server under key.
// It is constant-time in the HMAC comparison.
func verifyToken(tok string, key []byte) bool {
	dot := strings.IndexByte(tok, '.')
	if dot <= 0 || dot == len(tok)-1 {
		return false
	}
	body, sig := tok[:dot], tok[dot+1:]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

// GenerateToken is exported for the auth handler, which rotates the
// token after login. Callers pass the configured key.
func GenerateToken(length int, key []byte) (string, error) {
	return generateToken(length, key)
}

// CSRFMiddleware enforces the double-submit cookie pattern with a
// signed token.
//
// Safe methods (GET, HEAD, OPTIONS, TRACE) are passed through, and if
// the CSRF cookie is absent or its signature is invalid, a fresh token
// is generated and set on the response. The token is also stashed in
// the gin context so handlers can pass it to templates.
//
// Mutating methods (POST, PUT, PATCH, DELETE) are validated: the token
// in the form field or X-CSRF-Token header must match the cookie
// exactly (constant-time comparison) and the cookie must carry a valid
// signature. On failure, the request is aborted with 403 before the
// handler runs.
//
// Requests authenticated by Bearer token are exempt. The exemption is
// applied only when AuthMiddleware has confirmed the token's validity
// and called MarkBearerAuthenticated; a request that merely carries an
// Authorization header without a valid token is treated like any other
// request.
func CSRFMiddleware(cfg CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if v, ok := c.Get(bearerAuthenticatedKey); ok {
			if b, _ := v.(bool); b {
				c.Next()
				return
			}
		}

		if isSafeMethod(c.Request.Method) {
			ensureCSRFCookie(c, cfg)
			c.Next()
			return
		}

		if err := validateCSRF(c, cfg); err != nil {
			abortCSRF(c)
			return
		}

		c.Next()
	}
}

// MarkBearerAuthenticated is called by AuthMiddleware immediately after
// it has validated a Bearer token. CSRFMiddleware will then skip
// validation for that request. Requests that only carry an Authorization
// header without a valid token do not receive this mark.
func MarkBearerAuthenticated(c *gin.Context) {
	c.Set(bearerAuthenticatedKey, true)
}

// isSafeMethod reports whether an HTTP method is exempt from CSRF
// validation. Per RFC 7231, GET, HEAD, OPTIONS, and TRACE are defined
// as safe (no side effects on the server).
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// ensureCSRFCookie sets a CSRF cookie on the response if the request
// did not carry one, or if the one it carried fails signature
// verification. It stashes the token in the gin context either way so
// handlers can embed it in templates.
func ensureCSRFCookie(c *gin.Context, cfg CSRFConfig) {
	token, err := c.Cookie(cfg.CookieName)
	if err != nil || token == "" || !verifyToken(token, cfg.HMACKey) {
		token, err = generateToken(cfg.TokenLength, cfg.HMACKey)
		if err != nil {
			// Extremely unlikely (crypto/rand failure). Abort rather
			// than serve a page with no CSRF protection.
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		setCSRFCookie(c, cfg, token)
	}
	c.Set(csrfContextKey, token)
}

// validateCSRF compares the submitted token against the cookie and
// verifies the cookie's signature. Returns nil on success,
// ErrCSRFMismatch on any failure.
func validateCSRF(c *gin.Context, cfg CSRFConfig) error {
	cookieToken, err := c.Cookie(cfg.CookieName)
	if err != nil || cookieToken == "" {
		return ErrCSRFMismatch
	}
	if !verifyToken(cookieToken, cfg.HMACKey) {
		return ErrCSRFMismatch
	}

	submitted := extractSubmittedToken(c, cfg)
	if submitted == "" {
		return ErrCSRFMismatch
	}

	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(submitted)) != 1 {
		return ErrCSRFMismatch
	}

	// Stash the token so handlers can rotate it if needed.
	c.Set(csrfContextKey, cookieToken)
	return nil
}

// extractSubmittedToken reads the token from the header first (HTMX and
// API clients), then from the form body (native HTML form submissions).
// Using the header first means an HTMX request that also happens to
// carry a form body does not accidentally pick up a stale field.
func extractSubmittedToken(c *gin.Context, cfg CSRFConfig) string {
	if h := c.GetHeader(cfg.HeaderName); h != "" {
		return h
	}
	return c.PostForm(cfg.FormField)
}

// setCSRFCookie writes the CSRF cookie. Unlike the auth cookie, it is
// NOT HttpOnly: the client-side JS reads it to inject the token into
// HTMX requests that have no form body.
func setCSRFCookie(c *gin.Context, cfg CSRFConfig, token string) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(
		cfg.CookieName,
		token,
		cfg.CookieMaxAge,
		cfg.CookiePath,
		cfg.CookieDomain,
		cfg.CookieSecure,
		false, // HttpOnly=false — must be readable by JS
	)
}

// TokenFromContext returns the current request's CSRF token, or "" if
// none. Handlers use this to pass the token into templates.
func TokenFromContext(c *gin.Context) string {
	if v, ok := c.Get(csrfContextKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// abortCSRF terminates a request whose CSRF validation failed.
//
// HTMX requests receive 403 with a small JSON body; the HTMX
// htmx:responseError listener will surface it as a toast. Regular
// browser requests receive a plain 403. API clients (which never reach
// here, thanks to the Bearer exemption) would also receive JSON.
func abortCSRF(c *gin.Context) {
	if isHTMX(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "CSRF token mismatch or missing",
		})
		return
	}
	c.AbortWithStatus(http.StatusForbidden)
}
