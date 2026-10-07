package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/domain"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
)

type UserHandler struct {
	service *service.UserService
}

func NewUserHandler(service *service.UserService) *UserHandler {
	return &UserHandler{service: service}
}

// CreateUser handles POST /api/v1/users. The caller must be an admin
// (enforced by RequireRole on the route group). The new account's role
// is always "user"; the service sets it, and the request DTO cannot
// carry a role.
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req domain.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.service.CreateUser(c.Request.Context(), req)
	if err != nil {
		middleware.LoggerFromGin(c).Error("api: CreateUser failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, domain.ToUserResponse(user))
}

func (h *UserHandler) GetUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	user, err := h.service.GetUserByID(c.Request.Context(), int32(id))
	if err != nil {
		if err == domain.ErrUserNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		middleware.LoggerFromGin(c).Error("api: GetUser failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, domain.ToUserResponse(user))
}

// UpdateUser handles PUT /api/v1/users/:id. Role is not part of this
// operation; the service leaves the role column unchanged. Role changes
// go through UpdateUserRole.
func (h *UserHandler) UpdateUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var req domain.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.service.UpdateUser(c.Request.Context(), int32(id), req)
	if err != nil {
		if err == domain.ErrUserNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		middleware.LoggerFromGin(c).Error("api: UpdateUser failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, domain.ToUserResponse(user))
}

// UpdateUserRole handles PUT /api/v1/users/:id/role. Admin-only. This
// is the sole path by which a user's role changes. The service revokes
// the user's sessions so a demoted user cannot keep acting on their
// previous role until the access token's TTL expires.
func (h *UserHandler) UpdateUserRole(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var req domain.UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.service.UpdateUserRole(c.Request.Context(), int32(id), req.Role)
	if err != nil {
		if err == domain.ErrUserNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		if err == domain.ErrInvalidInput {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
			return
		}
		middleware.LoggerFromGin(c).Error("api: UpdateUserRole failed",
			"id", id, "role", req.Role, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, domain.ToUserResponse(user))
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.service.DeleteUser(c.Request.Context(), int32(id))
	if err != nil {
		if err == domain.ErrUserNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		middleware.LoggerFromGin(c).Error("api: DeleteUser failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *UserHandler) ListUsers(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 20
	}

	offset := (page - 1) * limit

	users, err := h.service.ListUsers(c.Request.Context(), int32(limit), int32(offset))
	if err != nil {
		middleware.LoggerFromGin(c).Error("api: ListUsers failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	responses := make([]domain.UserResponse, len(users))
	for i, user := range users {
		responses[i] = domain.ToUserResponse(&user)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": responses,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": len(responses),
		},
	})
}
