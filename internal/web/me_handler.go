package web

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/domain"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
	"mywebapp/pkg/r2"
)

// MeHandler serves the self-service surface for an authenticated user.
type MeHandler struct {
	service  *service.UserService
	r2Client *r2.Client
	imageKey string
}

// NewMeHandler constructs the handler. r2Client may be nil, in which
// case the welcome image is skipped and only the text banner renders.
// imageKey is the object key of the welcome image in the R2 bucket;
// if empty, "welcome.jpg" is used.
func NewMeHandler(service *service.UserService, r2Client *r2.Client) *MeHandler {
	imageKey := "welcome.jpg"
	if r2Client != nil {
		// Only read the env var when R2 is actually configured,
		// so an unconfigured deployment isn't affected by it.
		if v := getEnv("R2_IMAGE_KEY", ""); v != "" {
			imageKey = v
		}
	}
	return &MeHandler{
		service:  service,
		r2Client: r2Client,
		imageKey: imageKey,
	}
}

// ProfilePage renders the welcome/landing page.
func (h *MeHandler) ProfilePage(c *gin.Context) {
	claims, ok := middleware.ClaimsFromGin(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	user, err := h.service.GetUserByID(c.Request.Context(), claims.UserID)
	if err != nil {
		if err == domain.ErrUserNotFound {
			cfg := LoadCookieConfig()
			clearAuthCookie(c, cfg)
			clearRefreshCookie(c, cfg)
			c.Redirect(http.StatusFound, "/web/login")
			return
		}
		middleware.LoggerFromGin(c).Error("me: GetUserByID failed",
			"user_id", claims.UserID, "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Presign a URL for the welcome image. Nil client or a presign
	// error leaves the URL empty; the template's {{if .imageURL}}
	// branch then skips the <img> tag.
	var imageURL string
	if h.r2Client != nil {
		url, err := h.r2Client.PresignGetObject(c.Request.Context(), h.imageKey, 15*time.Minute)
		if err != nil {
			middleware.LoggerFromGin(c).Warn("me: failed to presign image", "error", err)
		} else {
			imageURL = url
		}
	}

	c.HTML(http.StatusOK, "me.html", gin.H{
		"title":      "My Account",
		"user":       ToUserViewModel(user),
		"csrf_token": middleware.TokenFromContext(c),
		"imageURL":   imageURL,
	})
}
