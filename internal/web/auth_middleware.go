package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
	"mywebapp/internal/domain"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
)

// AuthMiddleware validates the JWT from the Authorization header (API)
// or the access cookie (HTMX/web). On expiry or absence of the access
// token, it attempts a silent refresh using the refresh cookie.
//
// API paths (/api/*) accept only Bearer tokens. Cookie authentication
// is not accepted on those paths: the API group has no CSRF middleware,
// so allowing cookies there would expose cookie-authenticated requests
// to CSRF on the JSON endpoints.
func AuthMiddleware(
	jwtSvc *auth.JWTService,
	userService *service.UserService,
	sessionService *service.SessionService,
	cookieCfg CookieConfig,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		isAPIPath := strings.HasPrefix(c.Request.URL.Path, "/api/")

		// Bearer first. It is the only auth accepted on API paths.
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token := strings.TrimPrefix(h, "Bearer ")
			claims, err := jwtSvc.ValidateAccessToken(token)
			if err != nil {
				abortUnauthenticated(c)
				return
			}
			c.Set(middleware.ContextClaimsKey, claims)
			// Tell the CSRF middleware that this request is exempt.
			middleware.MarkBearerAuthenticated(c)
			c.Next()
			return
		}

		if isAPIPath {
			// Cookie auth is refused on API paths. A Bearer token is
			// the only accepted credential for /api/*.
			abortUnauthenticated(c)
			return
		}

		// Cookie auth for browser and HTMX requests.
		accessToken, err := c.Cookie(CookieName)
		if err == nil && accessToken != "" {
			if claims, err := jwtSvc.ValidateAccessToken(accessToken); err == nil {
				c.Set(middleware.ContextClaimsKey, claims)
				c.Next()
				return
			}
		}

		if trySilentRefresh(c, jwtSvc, userService, sessionService, cookieCfg) {
			c.Next()
			return
		}

		abortUnauthenticated(c)
	}
}

// trySilentRefresh exchanges a valid refresh cookie for a fresh access
// token. It reloads the user from the database so that a change made
// outside the application (a role edit via SQL, a deactivation) is
// reflected in the new token.
func trySilentRefresh(
	c *gin.Context,
	jwtSvc *auth.JWTService,
	userService *service.UserService,
	sessionService *service.SessionService,
	cookieCfg CookieConfig,
) bool {
	refreshToken, err := c.Cookie(RefreshCookieName)
	if err != nil || refreshToken == "" {
		return false
	}

	refreshClaims, err := jwtSvc.ValidateRefreshToken(refreshToken)
	if err != nil {
		return false
	}

	if err := sessionService.IsActive(c.Request.Context(), refreshClaims.ID); err != nil {
		return false
	}

	// Reload the user. The refresh token carries stale claims; only
	// the database has the current role and is_active flag.
	user, err := userService.GetUserByID(c.Request.Context(), refreshClaims.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return false
		}
		middleware.LoggerFromGin(c).Error("silent refresh: user lookup failed",
			"user_id", refreshClaims.UserID, "error", err)
		return false
	}
	if !user.IsActive {
		return false
	}

	accessToken, err := jwtSvc.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		return false
	}

	setAuthCookie(c, cookieCfg, accessToken, jwtSvc.AccessTTL())

	freshClaims, err := jwtSvc.ValidateAccessToken(accessToken)
	if err != nil {
		return false
	}
	c.Set(middleware.ContextClaimsKey, freshClaims)
	return true
}

// OptionalAuthMiddleware reads claims when a valid access cookie is
// present and does nothing otherwise. It never aborts, which is what
// allows anonymous visitors to reach the public Home page.
func OptionalAuthMiddleware(jwtSvc *auth.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := c.Cookie(CookieName); err == nil && token != "" {
			if claims, err := jwtSvc.ValidateAccessToken(token); err == nil {
				c.Set(middleware.ContextClaimsKey, claims)
			}
		}
		c.Next()
	}
}

// abortUnauthenticated terminates a request whose authentication failed.
//
// It sets Cache-Control: no-store on every response: Chrome will
// heuristically cache a headerless 302 for localhost and replay it on
// the next visit, producing a phantom redirect that survives redeploys.
func abortUnauthenticated(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")

	if IsHTMX(c) {
		c.Header("HX-Redirect", "/web/login")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Redirect(http.StatusFound, "/web/login")
	c.Abort()
}

// GetClaims returns the authenticated user's claims. Delegates to the
// middleware package so the context key has a single definition.
func GetClaims(c *gin.Context) (*auth.Claims, bool) {
	return middleware.ClaimsFromGin(c)
}
