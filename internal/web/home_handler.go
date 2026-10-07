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

// HomeHandler serves the public landing page and the authenticated
// member directory.
//
// The landing page is public. The member directory requires a
// non-default role; the route group that registers MembersPage and
// MembersPartial is wrapped in RequireRole("moderator", "admin").
type HomeHandler struct {
	service  *service.UserService
	r2Client *r2.Client
	imageKey string
}

// NewHomeHandler constructs the handler. r2Client may be nil, in
// which case the welcome image is skipped.
func NewHomeHandler(service *service.UserService, r2Client *r2.Client) *HomeHandler {
	imageKey := "welcome.jpg"
	if r2Client != nil {
		if v := getEnv("R2_IMAGE_KEY", ""); v != "" {
			imageKey = v
		}
	}
	return &HomeHandler{service: service, r2Client: r2Client, imageKey: imageKey}
}

// HomePage renders the public landing page.
//
// Registered behind OptionalAuthMiddleware: it must not assume the
// caller is authenticated. When claims are present the page
// personalises the nav and greeting; when they are absent, it renders
// the anonymous variant with a "Sign in" call to action.
func (h *HomeHandler) HomePage(c *gin.Context) {
	var (
		fullName string
		username string
		role     string
		authed   bool
	)

	if claims, ok := middleware.ClaimsFromGin(c); ok && claims != nil {
		authed = true
		username = claims.Username
		role = claims.Role

		// Best-effort: if the user row has been deleted since the
		// token was issued, fall back to the claims-only view rather
		// than failing the public page.
		if user, err := h.service.GetUserByID(c.Request.Context(), claims.UserID); err == nil {
			fullName = user.FullName
			username = user.Username
			role = user.Role
		} else {
			middleware.LoggerFromGin(c).Warn("home: user lookup failed; using claims",
				"user_id", claims.UserID, "error", err)
		}
	}

	var imageURL string
	if h.r2Client != nil {
		url, err := h.r2Client.PresignGetObject(c.Request.Context(), h.imageKey, 15*time.Minute)
		if err != nil {
			middleware.LoggerFromGin(c).Warn("home: failed to presign image", "error", err)
		} else {
			imageURL = url
		}
	}

	c.HTML(http.StatusOK, "home.html", gin.H{
		"title":      "Home",
		"authed":     authed,
		"fullName":   fullName,
		"username":   username,
		"role":       role,
		"csrf_token": middleware.TokenFromContext(c),
		"imageURL":   imageURL,
	})
}

// MembersPage renders the read-only member directory shell. The route
// group registers this handler behind RequireRole("moderator",
// "admin"), so a caller with role "user" never reaches it.
func (h *HomeHandler) MembersPage(c *gin.Context) {
	claims, ok := middleware.ClaimsFromGin(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	c.HTML(http.StatusOK, "members.html", gin.H{
		"title":      "Members",
		"role":       claims.Role,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

// MembersPartial renders the member table fragment. It uses the
// PublicUserViewModel, which carries only the fields the directory is
// allowed to render. The email and the role are never present in the
// response body.
func (h *HomeHandler) MembersPartial(c *gin.Context) {
	claims, ok := middleware.ClaimsFromGin(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	page := GetPage(c)
	limit := GetLimit(c)
	search := c.Query("search")
	offset := (page - 1) * limit

	var users []domain.User
	var total int64
	var err error

	if search != "" {
		users, err = h.service.SearchUsers(c.Request.Context(), search, int32(limit), int32(offset))
		if err != nil {
			h.renderMembersError(c, err)
			return
		}
		total = int64(len(users))
	} else {
		users, err = h.service.ListUsers(c.Request.Context(), int32(limit), int32(offset))
		if err != nil {
			h.renderMembersError(c, err)
			return
		}
		total, err = h.service.CountUsers(c.Request.Context())
		if err != nil {
			total = int64(len(users))
		}
	}

	// The directory uses the public view model. The email and role
	// fields are absent by construction — not merely unrendered by the
	// template.
	viewUsers := make([]PublicUserViewModel, len(users))
	for i, u := range users {
		viewUsers[i] = ToPublicUserViewModel(&u)
	}

	c.HTML(http.StatusOK, "members_list.html", gin.H{
		"members":    viewUsers,
		"role":       claims.Role,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": (total + int64(limit) - 1) / int64(limit),
		"search":     search,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

func (h *HomeHandler) renderMembersError(c *gin.Context, err error) {
	middleware.LoggerFromGin(c).Error("members handler error",
		"path", c.Request.URL.Path, "error", err)
	if IsHTMX(c) {
		c.String(http.StatusInternalServerError, "Error: %s", err.Error())
		return
	}
	c.HTML(http.StatusInternalServerError, "members_list.html", gin.H{
		"members": []PublicUserViewModel{},
		"role":    "",
		"total":   0,
	})
}
