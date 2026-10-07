package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/domain"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
)

// WebHandler serves the HTMX-driven admin surface for user management.
type WebHandler struct {
	service *service.UserService
}

func NewWebHandler(service *service.UserService) *WebHandler {
	return &WebHandler{service: service}
}

// triggerHeader serialises an HX-Trigger payload and sets it on the
// response. HTMX clients dispatch the named event on the page, which
// the shell templates listen for (for example clearUserForm).
func triggerHeader(c *gin.Context, event string, detail map[string]any) {
	payload := map[string]any{event: detail}
	b, err := json.Marshal(payload)
	if err != nil {
		middleware.LoggerFromGin(c).Error("triggerHeader marshal error",
			"event", event, "error", err)
		return
	}
	c.Header("HX-Trigger", string(b))
}

// ---------- List pages ----------

// ListUsersPage renders the default user list, sorted by creation
// date (the repository's default order). It responds with the partial
// when the request carries HX-Request, and with the full page
// otherwise.
func (h *WebHandler) ListUsersPage(c *gin.Context) {
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
			h.renderError(c, err)
			return
		}
		total = int64(len(users))
	} else {
		users, err = h.service.ListUsers(c.Request.Context(), int32(limit), int32(offset))
		if err != nil {
			h.renderError(c, err)
			return
		}
		total, err = h.service.CountUsers(c.Request.Context())
		if err != nil {
			total = int64(len(users))
		}
	}

	viewUsers := make([]UserViewModel, len(users))
	for i, u := range users {
		viewUsers[i] = ToUserViewModel(&u)
	}

	data := gin.H{
		"users":      viewUsers,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": (total + int64(limit) - 1) / int64(limit),
		"search":     search,
		"csrf_token": middleware.TokenFromContext(c),
	}

	if IsHTMX(c) {
		c.HTML(http.StatusOK, "user_list.html", data)
		return
	}
	c.HTML(http.StatusOK, "base.html", gin.H{
		"title":        "User Management",
		"listEndpoint": "/web/users",
		"csrf_token":   middleware.TokenFromContext(c),
	})
}

// ListUsersByIDPage renders the user list sorted by ID.
func (h *WebHandler) ListUsersByIDPage(c *gin.Context) {
	page := GetPage(c)
	limit := GetLimit(c)
	offset := (page - 1) * limit

	users, err := h.service.ListUsersByID(c.Request.Context(), int32(limit), int32(offset))
	if err != nil {
		h.renderError(c, err)
		return
	}
	total, err := h.service.CountUsers(c.Request.Context())
	if err != nil {
		total = int64(len(users))
	}

	viewUsers := make([]UserViewModel, len(users))
	for i, u := range users {
		viewUsers[i] = ToUserViewModel(&u)
	}

	data := gin.H{
		"users":      viewUsers,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": (total + int64(limit) - 1) / int64(limit),
		"csrf_token": middleware.TokenFromContext(c),
	}

	if IsHTMX(c) {
		c.HTML(http.StatusOK, "user_list_by_id.html", data)
		return
	}
	c.HTML(http.StatusOK, "base.html", gin.H{
		"title":        "Users by ID",
		"listEndpoint": "/web/users/by-id",
		"csrf_token":   middleware.TokenFromContext(c),
	})
}

// ---------- Form rendering ----------

// NewUserForm renders the empty create-user form.
func (h *WebHandler) NewUserForm(c *gin.Context) {
	c.HTML(http.StatusOK, "user_form.html", gin.H{
		"user":       UserViewModel{},
		"isNew":      true,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

// EditUserForm renders the edit form for an existing user.
func (h *WebHandler) EditUserForm(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		c.HTML(http.StatusBadRequest, "user_form.html", gin.H{
			"user":       UserViewModel{},
			"isNew":      false,
			"error":      "Invalid user ID",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}
	user, err := h.service.GetUserByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.HTML(http.StatusNotFound, "user_form.html", gin.H{
				"user":       UserViewModel{ID: id},
				"isNew":      false,
				"error":      "User not found",
				"csrf_token": middleware.TokenFromContext(c),
			})
			return
		}
		middleware.LoggerFromGin(c).Error("web: EditUserForm failed",
			"id", id, "error", err)
		c.HTML(http.StatusInternalServerError, "user_form.html", gin.H{
			"user":       UserViewModel{ID: id},
			"isNew":      false,
			"error":      "Something went wrong. Please try again.",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}
	c.HTML(http.StatusOK, "user_form.html", gin.H{
		"user":       ToUserViewModel(user),
		"isNew":      false,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

// EditUserRoleForm renders the admin-only role-change form.
func (h *WebHandler) EditUserRoleForm(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		c.HTML(http.StatusBadRequest, "user_role_form.html", gin.H{
			"user":       UserViewModel{},
			"roles":      domain.ValidRoles,
			"error":      "Invalid user ID",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}
	user, err := h.service.GetUserByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.HTML(http.StatusNotFound, "user_role_form.html", gin.H{
				"user":       UserViewModel{ID: id},
				"roles":      domain.ValidRoles,
				"error":      "User not found",
				"csrf_token": middleware.TokenFromContext(c),
			})
			return
		}
		middleware.LoggerFromGin(c).Error("web: EditUserRoleForm failed",
			"id", id, "error", err)
		c.HTML(http.StatusInternalServerError, "user_role_form.html", gin.H{
			"user":       UserViewModel{ID: id},
			"roles":      domain.ValidRoles,
			"error":      "Something went wrong. Please try again.",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}
	c.HTML(http.StatusOK, "user_role_form.html", gin.H{
		"user":       ToUserViewModel(user),
		"roles":      domain.ValidRoles,
		"csrf_token": middleware.TokenFromContext(c),
	})
}

// ---------- Mutations ----------

// CreateUserWeb handles the create-user form submission.
//
// On success it renders the new user's row with user_row.html, which
// uses the view model directly (fields like {{.ID}}, {{.FullName}}).
// The clearUserForm trigger tells the client to clear the form and
// show a toast.
//
// On failure it re-renders the form with an error message. The status
// code distinguishes two categories:
//
//   - 400 Bad Request when the payload conflicts with existing data
//     (duplicate email or username).
//   - 500 Internal Server Error for anything else, with a generic
//     message so no implementation detail leaks to the client.
func (h *WebHandler) CreateUserWeb(c *gin.Context) {
	req := domain.CreateUserRequest{
		Email:    c.PostForm("email"),
		Password: c.PostForm("password"),
		Username: c.PostForm("username"),
		FullName: c.PostForm("full_name"),
		// No Role: the DTO does not carry one, and the service sets
		// "user" unconditionally.
	}

	user, err := h.service.CreateUser(c.Request.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		msg := "Something went wrong. Please try again."

		switch {
		case errors.Is(err, domain.ErrEmailExists):
			status = http.StatusBadRequest
			msg = "Email already exists"
		case errors.Is(err, domain.ErrUsernameExists):
			status = http.StatusBadRequest
			msg = "Username already exists"
		default:
			middleware.LoggerFromGin(c).Error("web: CreateUserWeb failed",
				"email", req.Email, "username", req.Username, "error", err)
		}

		c.HTML(status, "user_form.html", gin.H{
			"user":       ToUserViewModelFromRequest(req),
			"isNew":      true,
			"error":      msg,
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	triggerHeader(c, "clearUserForm", map[string]any{
		"message": "User created successfully",
		"type":    "success",
	})

	c.HTML(http.StatusOK, "user_row.html", ToUserViewModel(user))
}

// UpdateUserWeb handles the edit-user form submission.
//
// The role is not editable through this form; the role-change form
// handles that separately. The handler reads the existing user first
// so the re-rendered form preserves the current role on failure.
func (h *WebHandler) UpdateUserWeb(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		c.HTML(http.StatusBadRequest, "user_form.html", gin.H{
			"user":       UserViewModel{},
			"isNew":      false,
			"error":      "Invalid User ID",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	existing, err := h.service.GetUserByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.HTML(http.StatusNotFound, "user_form.html", gin.H{
				"user":       UserViewModel{ID: id},
				"isNew":      false,
				"error":      "User not found",
				"csrf_token": middleware.TokenFromContext(c),
			})
			return
		}
		middleware.LoggerFromGin(c).Error("web: UpdateUserWeb lookup failed",
			"id", id, "error", err)
		c.HTML(http.StatusInternalServerError, "user_form.html", gin.H{
			"user":       UserViewModel{ID: id},
			"isNew":      false,
			"error":      "Something went wrong. Please try again.",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	req := domain.UpdateUserRequest{
		Email:    c.PostForm("email"),
		Username: c.PostForm("username"),
		FullName: c.PostForm("full_name"),
	}
	isActive := c.PostForm("is_active") == "on"
	req.IsActive = &isActive

	user, err := h.service.UpdateUser(c.Request.Context(), id, req)
	if err != nil {
		status := http.StatusInternalServerError
		msg := "Something went wrong. Please try again."

		switch {
		case errors.Is(err, domain.ErrUserNotFound):
			status = http.StatusNotFound
			msg = "User not found"
		case errors.Is(err, domain.ErrEmailExists):
			status = http.StatusBadRequest
			msg = "Email already exists"
		case errors.Is(err, domain.ErrUsernameExists):
			status = http.StatusBadRequest
			msg = "Username already exists"
		default:
			middleware.LoggerFromGin(c).Error("web: UpdateUserWeb failed",
				"id", id, "error", err)
		}

		c.HTML(status, "user_form.html", gin.H{
			"user":       ToUserViewModelFromUpdateRequest(req, id, existing.Role),
			"isNew":      false,
			"error":      msg,
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	triggerHeader(c, "clearUserForm", map[string]any{
		"message": "User updated successfully",
		"type":    "success",
	})

	c.HTML(http.StatusOK, "user_row.html", ToUserViewModel(user))
}

// UpdateUserRoleWeb handles the admin-only role-change form. It is the
// sole web path that changes a role.
//
// The service revokes the target user's sessions when the role
// changes, so a demoted user cannot keep acting on the old role via
// their existing refresh token.
func (h *WebHandler) UpdateUserRoleWeb(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		c.HTML(http.StatusBadRequest, "user_role_form.html", gin.H{
			"user":       UserViewModel{},
			"roles":      domain.ValidRoles,
			"error":      "Invalid user ID",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	role := c.PostForm("role")
	if !domain.IsValidRole(role) {
		c.HTML(http.StatusBadRequest, "user_role_form.html", gin.H{
			"user":       UserViewModel{ID: id},
			"roles":      domain.ValidRoles,
			"error":      "Invalid role",
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	user, err := h.service.UpdateUserRole(c.Request.Context(), id, role)
	if err != nil {
		status := http.StatusInternalServerError
		msg := "Something went wrong. Please try again."

		switch {
		case errors.Is(err, domain.ErrUserNotFound):
			status = http.StatusNotFound
			msg = "User not found"
		case errors.Is(err, domain.ErrInvalidInput):
			status = http.StatusBadRequest
			msg = "Invalid role"
		default:
			middleware.LoggerFromGin(c).Error("web: UpdateUserRoleWeb failed",
				"id", id, "role", role, "error", err)
		}

		c.HTML(status, "user_role_form.html", gin.H{
			"user":       UserViewModel{ID: id},
			"roles":      domain.ValidRoles,
			"error":      msg,
			"csrf_token": middleware.TokenFromContext(c),
		})
		return
	}

	triggerHeader(c, "roleChanged", map[string]any{
		"message": "Role updated. The user must sign in again.",
		"type":    "success",
	})

	c.HTML(http.StatusOK, "user_row.html", ToUserViewModel(user))
}

// DeleteUserWeb removes a user. HTMX sends this as a DELETE with the
// row's id as the target. On success the response has no body, and the
// client's hx-swap="outerHTML" leaves the target as-is when the body
// is empty. The userDeleted trigger tells the shell to refresh the nav
// stats.
func (h *WebHandler) DeleteUserWeb(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	if err := h.service.DeleteUser(c.Request.Context(), id); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		middleware.LoggerFromGin(c).Error("web: DeleteUserWeb failed",
			"id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong."})
		return
	}

	triggerHeader(c, "userDeleted", map[string]any{
		"message": "User deleted successfully",
		"type":    "success",
	})

	c.Status(http.StatusOK)
}

// ---------- Stats ----------

// GetUserStats renders the small count widget used in the nav and on
// the home page.
func (h *WebHandler) GetUserStats(c *gin.Context) {
	total, _ := h.service.CountUsers(c.Request.Context())
	active, _ := h.service.CountActiveUsers(c.Request.Context())

	c.HTML(http.StatusOK, "user_stats.html", gin.H{
		"total":    total,
		"active":   active,
		"inactive": total - active,
	})
}

// ---------- Helpers ----------

// renderError logs the failure and returns a response appropriate for
// the caller. HTMX requests receive a short text body; non-HTMX
// requests receive the base shell with an error title.
func (h *WebHandler) renderError(c *gin.Context, err error) {
	middleware.LoggerFromGin(c).Error("web handler error",
		"path", c.Request.URL.Path, "error", err)
	if IsHTMX(c) {
		c.String(http.StatusInternalServerError, "Error: %s", err.Error())
		return
	}
	c.HTML(http.StatusInternalServerError, "base.html", gin.H{
		"title":        "Error",
		"listEndpoint": "/web/users",
		"csrf_token":   middleware.TokenFromContext(c),
	})
}
