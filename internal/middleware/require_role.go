// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
)

// ContextClaimsKey is the gin context key under which AuthMiddleware
// stashes the authenticated *auth.Claims. Exported so the web package
// can delegate to ClaimsFromGin without duplicating the key.
const ContextClaimsKey = "auth_claims"

// RequireRole returns middleware that permits the request only if the
// authenticated caller's role matches one of the allowed roles. It must
// be registered after AuthMiddleware, which populates the claims.
//
// Failure behaviour:
//   - HTMX requests: 403 + JSON, surfaced as a toast by base.html's
//     htmx:responseError listener.
//   - API requests:  403 + JSON.
//   - Browser navigation: 302 to /web/me.
//
// Fail-closed: if no claims are in the context (AuthMiddleware was not
// registered), the request is rejected. A misconfiguration must not
// become an authorization bypass.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(c *gin.Context) {
		claims, ok := ClaimsFromGin(c)
		if !ok {
			abortForbidden(c)
			return
		}
		if _, ok := allowed[claims.Role]; !ok {
			abortForbidden(c)
			return
		}
		c.Next()
	}
}

// ClaimsFromGin returns the *auth.Claims stashed by AuthMiddleware, or
// (nil, false) if none. Kept in this package because middleware cannot
// import web (web imports middleware). The web package delegates here.
func ClaimsFromGin(c *gin.Context) (*auth.Claims, bool) {
	v, ok := c.Get(ContextClaimsKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*auth.Claims)
	return claims, ok
}

// abortForbidden terminates an unauthorized request. The response shape
// depends on the client:
//
//   - HTMX: JSON, surfaced as a toast by base.html's error listener.
//   - API:  JSON.
//   - Browser navigation: a 302 to /web/me. The caller is authenticated
//     (otherwise AuthMiddleware would have rejected them with a 401
//     before RequireRole ran), so sending them to their own profile page
//     is friendlier than the login form. It also avoids the
//     redirect-then-login-then-redirect bounce that would occur if the
//     caller were shown the login page while still authenticated.
//
// A guard prevents a redirect loop when the request is already targeting
// /web/me; in that case a bare 403 is returned instead.
func abortForbidden(c *gin.Context) {
	if isHTMX(c) || strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "insufficient privileges",
		})
		return
	}
	if c.Request.URL.Path == "/web/me" {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Redirect(http.StatusFound, "/web/me")
	c.Abort()
}
