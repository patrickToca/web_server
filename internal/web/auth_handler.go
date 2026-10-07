package web

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/auth"
	"mywebapp/internal/domain"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
)

// AuthHandler handles login, logout, registration, and token refresh.
type AuthHandler struct {
	userService    *service.UserService
	sessionService *service.SessionService
	jwtService     *auth.JWTService
	cookieCfg      CookieConfig
	csrfCfg        middleware.CSRFConfig
}

// NewAuthHandler constructs an AuthHandler.
//
// The CSRF configuration is passed in rather than loaded here. main.go
// loads it once at boot and fails fast when it is invalid; loading it
// again in the constructor would either duplicate that check or, worse,
// paper over it by ignoring the returned error. Passing the already-
// validated value also makes the handler testable without setting
// environment variables.
func NewAuthHandler(
	userService *service.UserService,
	sessionService *service.SessionService,
	jwtService *auth.JWTService,
	csrfCfg middleware.CSRFConfig,
) *AuthHandler {
	return &AuthHandler{
		userService:    userService,
		sessionService: sessionService,
		jwtService:     jwtService,
		cookieCfg:      LoadCookieConfig(),
		csrfCfg:        csrfCfg,
	}
}

// =============================================================================
// Login
// =============================================================================

// LoginPage renders the login form.
func (h *AuthHandler) LoginPage(c *gin.Context) {
	if h.authenticatedRole(c) != "" {
		h.redirectAfterAuth(c)
		return
	}

	csrfToken := middleware.TokenFromContext(c)

	if IsHTMX(c) {
		c.HTML(http.StatusOK, "login_form.html", gin.H{
			"csrf_token": csrfToken,
		})
		return
	}

	c.HTML(http.StatusOK, "login.html", gin.H{
		"title":      "Sign In",
		"csrf_token": csrfToken,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	logger := middleware.LoggerFromGin(c)
	email := c.PostForm("email")
	password := c.PostForm("password")

	if email == "" || password == "" {
		h.renderLoginError(c, "Email and password are required", email)
		return
	}

	user, err := h.userService.GetUserByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			h.renderLoginError(c, "Invalid email or password", email)
			return
		}
		logger.Error("login: GetUserByEmail failed", "email", email, "error", err)
		h.renderLoginError(c, "Something went wrong. Please try again.", email)
		return
	}

	if !auth.CheckPasswordHash(password, user.PasswordHash) {
		h.renderLoginError(c, "Invalid email or password", email)
		return
	}

	if !user.IsActive {
		h.renderLoginError(c, "Account is disabled. Contact an administrator.", email)
		return
	}

	if err := h.issueTokens(c, user); err != nil {
		logger.Error("login: issueTokens failed", "user_id", user.ID, "error", err)
		h.renderLoginError(c, "Something went wrong. Please try again.", email)
		return
	}

	if err := h.userService.UpdateUserLastLogin(c.Request.Context(), user.ID); err != nil {
		logger.Warn("login: UpdateUserLastLogin failed", "user_id", user.ID, "error", err)
	}

	h.redirectAfterAuth(c)
}

// =============================================================================
// Register
// =============================================================================

// RegisterPage renders the registration form.
func (h *AuthHandler) RegisterPage(c *gin.Context) {
	if h.authenticatedRole(c) != "" {
		h.redirectAfterAuth(c)
		return
	}

	csrfToken := middleware.TokenFromContext(c)

	if IsHTMX(c) {
		c.HTML(http.StatusOK, "register_form.html", gin.H{
			"csrf_token": csrfToken,
		})
		return
	}

	c.HTML(http.StatusOK, "register.html", gin.H{
		"title":      "Create Account",
		"csrf_token": csrfToken,
	})
}

// Register handles the registration form submission.
//
// The error message for the two conflict cases — email already used
// and username already taken — is deliberately identical. Distinguishing
// them would let an unauthenticated caller enumerate valid emails or
// usernames by observing which message comes back. The distinction is
// preserved in the server log via the wrapped error.
func (h *AuthHandler) Register(c *gin.Context) {
	logger := middleware.LoggerFromGin(c)
	username := c.PostForm("username")
	fullName := c.PostForm("full_name")
	email := c.PostForm("email")
	password := c.PostForm("password")

	if username == "" || fullName == "" || email == "" || password == "" {
		h.renderRegisterError(c, "All fields are required", username, fullName, email)
		return
	}

	if len(password) < 8 {
		h.renderRegisterError(c, "Password must be at least 8 characters", username, fullName, email)
		return
	}

	req := domain.CreateUserRequest{
		Email:    email,
		Password: password,
		Username: username,
		FullName: fullName,
	}

	user, err := h.userService.CreateUser(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrEmailExists):
			logger.Info("register: email already in use", "email", email)
			h.renderRegisterError(c,
				"An account with those details already exists. "+
					"If this is your account, sign in instead.",
				username, fullName, email)
			return
		case errors.Is(err, domain.ErrUsernameExists):
			logger.Info("register: username already in use", "username", username)
			h.renderRegisterError(c,
				"An account with those details already exists. "+
					"If this is your account, sign in instead.",
				username, fullName, email)
			return
		default:
			logger.Warn("register: CreateUser failed",
				"email", email, "username", username, "error", err)
			h.renderRegisterError(c, "Registration failed. Please try again.",
				username, fullName, email)
			return
		}
	}

	if err := h.issueTokens(c, user); err != nil {
		logger.Error("register: issueTokens failed", "user_id", user.ID, "error", err)
		h.renderRegisterError(c, "Account created. Please sign in.", username, fullName, email)
		return
	}

	h.redirectAfterAuth(c)
}

// =============================================================================
// Refresh
// =============================================================================

// Refresh exchanges a valid, unrevoked refresh token for a new access
// token.
func (h *AuthHandler) Refresh(c *gin.Context) {
	logger := middleware.LoggerFromGin(c)

	refreshToken, err := c.Cookie(RefreshCookieName)
	if err != nil || refreshToken == "" {
		h.abortRefresh(c, http.StatusUnauthorized, "no_refresh_token")
		return
	}

	claims, err := h.jwtService.ValidateRefreshToken(refreshToken)
	if err != nil {
		logger.Info("refresh: token rejected", "error", err)
		h.abortRefresh(c, http.StatusUnauthorized, "invalid_refresh_token")
		return
	}

	if err := h.sessionService.IsActive(c.Request.Context(), claims.ID); err != nil {
		logger.Info("refresh: session not active",
			"jti", claims.ID, "user_id", claims.UserID, "error", err)
		h.abortRefresh(c, http.StatusUnauthorized, "session_inactive")
		return
	}

	user, err := h.userService.GetUserByID(c.Request.Context(), claims.UserID)
	if err != nil {
		logger.Warn("refresh: user lookup failed",
			"user_id", claims.UserID, "error", err)
		h.abortRefresh(c, http.StatusUnauthorized, "user_unavailable")
		return
	}
	if !user.IsActive {
		h.abortRefresh(c, http.StatusUnauthorized, "user_inactive")
		return
	}

	accessToken, err := h.jwtService.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		logger.Error("refresh: GenerateAccessToken failed",
			"user_id", user.ID, "error", err)
		h.abortRefresh(c, http.StatusInternalServerError, "token_generation_failed")
		return
	}

	setAuthCookie(c, h.cookieCfg, accessToken, h.jwtService.AccessTTL())

	c.Status(http.StatusNoContent)
}

// abortRefresh terminates a refresh attempt.
func (h *AuthHandler) abortRefresh(c *gin.Context, status int, reason string) {
	clearAuthCookie(c, h.cookieCfg)
	clearRefreshCookie(c, h.cookieCfg)

	if IsHTMX(c) {
		c.AbortWithStatusJSON(status, gin.H{"error": reason})
		return
	}
	c.AbortWithStatus(status)
}

// =============================================================================
// Logout
// =============================================================================

// Logout revokes the refresh token and clears all three cookies.
func (h *AuthHandler) Logout(c *gin.Context) {
	logger := middleware.LoggerFromGin(c)

	if refreshToken, err := c.Cookie(RefreshCookieName); err == nil && refreshToken != "" {
		if claims, err := h.jwtService.ValidateRefreshToken(refreshToken); err == nil {
			if err := h.sessionService.Revoke(c.Request.Context(), claims.ID); err != nil {
				logger.Warn("logout: revoke failed",
					"jti", claims.ID, "user_id", claims.UserID, "error", err)
			}
		}
	}

	clearAuthCookie(c, h.cookieCfg)
	clearRefreshCookie(c, h.cookieCfg)
	clearCSRFCookie(c, h.cookieCfg)

	const destination = "/web/login"
	if IsHTMX(c) {
		c.Header("HX-Redirect", destination)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, destination)
}

// =============================================================================
// API token endpoint
// =============================================================================

// IssueAPIToken exchanges credentials for a Bearer access token.
func (h *AuthHandler) IssueAPIToken(c *gin.Context) {
	logger := middleware.LoggerFromGin(c)

	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	user, err := h.userService.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		logger.Error("api token: lookup failed", "email", req.Email, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if !auth.CheckPasswordHash(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "account is disabled"})
		return
	}

	accessToken, err := h.jwtService.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		logger.Error("api token: GenerateAccessToken failed",
			"user_id", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(h.jwtService.AccessTTL().Seconds()),
	})
}

// =============================================================================
// Internal helpers
// =============================================================================

func (h *AuthHandler) authenticatedRole(c *gin.Context) string {
	token, err := c.Cookie(CookieName)
	if err != nil || token == "" {
		return ""
	}
	claims, err := h.jwtService.ValidateAccessToken(token)
	if err != nil {
		return ""
	}
	return claims.Role
}

func (h *AuthHandler) issueTokens(c *gin.Context, user *domain.User) error {
	accessToken, err := h.jwtService.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		return err
	}

	refreshToken, jti, err := h.jwtService.GenerateRefreshToken(user.ID, user.Username, user.Role)
	if err != nil {
		return err
	}

	if err := h.sessionService.Create(c.Request.Context(), service.CreateSessionParams{
		JTI:       jti,
		UserID:    user.ID,
		ExpiresAt: timeNow().Add(h.jwtService.RefreshTTL()),
		UserAgent: c.Request.UserAgent(),
		ClientIP:  c.ClientIP(),
	}); err != nil {
		return err
	}

	h.rotateCSRFToken(c)

	setAuthCookie(c, h.cookieCfg, accessToken, h.jwtService.AccessTTL())
	setRefreshCookie(c, h.cookieCfg, refreshToken, h.jwtService.RefreshTTL())
	return nil
}

func (h *AuthHandler) rotateCSRFToken(c *gin.Context) {
	clearCSRFCookie(c, h.cookieCfg)
	token, err := middleware.GenerateToken(h.csrfCfg.TokenLength, h.csrfCfg.HMACKey)
	if err != nil {
		middleware.LoggerFromGin(c).Error("rotateCSRFToken: generate failed", "error", err)
		return
	}
	setCSRFCookie(c, h.cookieCfg, token)
}

func (h *AuthHandler) redirectAfterAuth(c *gin.Context) {
	const destination = "/web/home"
	if IsHTMX(c) {
		c.Header("HX-Redirect", destination)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, destination)
}

func (h *AuthHandler) renderLoginError(c *gin.Context, msg, email string) {
	c.HTML(http.StatusOK, "login_form.html", gin.H{
		"error":      msg,
		"email":      email,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

func (h *AuthHandler) renderRegisterError(c *gin.Context, msg, username, fullName, email string) {
	c.HTML(http.StatusOK, "register_form.html", gin.H{
		"error":      msg,
		"username":   username,
		"full_name":  fullName,
		"email":      email,
		"csrf_token": middleware.TokenFromContext(c),
	})
}
