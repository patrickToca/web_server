package web

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mywebapp/internal/domain"
)

// timeNow is a package-level indirection over time.Now. It exists so
// tests can stub the clock without monkey-patching the standard
// library. Production code that computes expiry should call
// timeNow() rather than time.Now() directly; code that merely
// formats a timestamp can use time.Now() freely.
var timeNow = time.Now

// UserViewModel represents a user for the admin templates. It carries
// every field, including the email and the role. It is used only on
// the admin-only surface under /web/users, where the caller has been
// authorized by RequireRole("admin").
type UserViewModel struct {
	ID          int32     `json:"id"`
	Email       string    `json:"email"`
	Username    string    `json:"username"`
	FullName    string    `json:"full_name"`
	IsActive    bool      `json:"is_active"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Status      string    `json:"status"`
	StatusBadge string    `json:"status_badge"`
}

// PublicUserViewModel is the shape rendered on the member directory.
//
// The member directory is served to every authenticated user with a
// non-default role (moderator and admin). It deliberately omits Email
// and Role: the email is the primary credential-stuffing target and
// the role tag identifies which accounts are high-value. Neither is
// needed for a directory whose purpose is to show who is a member.
//
// Keeping this type distinct from UserViewModel means the sensitive
// fields are not merely unrendered — they are never constructed for a
// directory response. A future template edit that references
// {{.Email}} on a PublicUserViewModel will not compile.
type PublicUserViewModel struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	FullName    string `json:"full_name"`
	Status      string `json:"status"`
	StatusBadge string `json:"status_badge"`
}

// ToUserViewModel converts domain.User to UserViewModel. Use on the
// admin-only surface where every field is safe to render.
func ToUserViewModel(user *domain.User) UserViewModel {
	vm := UserViewModel{
		ID:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		FullName:  user.FullName,
		IsActive:  user.IsActive,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.Time(),
		UpdatedAt: user.UpdatedAt.Time(),
	}

	if user.IsActive {
		vm.Status = "Active"
		vm.StatusBadge = "bg-green-100 text-green-800"
	} else {
		vm.Status = "Inactive"
		vm.StatusBadge = "bg-red-100 text-red-800"
	}

	return vm
}

// ToPublicUserViewModel converts domain.User to PublicUserViewModel.
// Use on the member directory, which is available to more than just
// administrators.
func ToPublicUserViewModel(user *domain.User) PublicUserViewModel {
	vm := PublicUserViewModel{
		ID:       user.ID,
		Username: user.Username,
		FullName: user.FullName,
	}

	if user.IsActive {
		vm.Status = "Active"
		vm.StatusBadge = "bg-green-100 text-green-800"
	} else {
		vm.Status = "Inactive"
		vm.StatusBadge = "bg-red-100 text-red-800"
	}

	return vm
}

// ToUserViewModelFromRequest creates a view model from a create
// request.
//
// Role is hardcoded to "user" because the DTO no longer carries a
// role: new accounts are always created with the default role. If the
// create request fails and the form is re-rendered, this view model is
// what fills the form fields; the role field is not part of the create
// form.
func ToUserViewModelFromRequest(req domain.CreateUserRequest) UserViewModel {
	return UserViewModel{
		Email:    req.Email,
		Username: req.Username,
		FullName: req.FullName,
		Role:     "user",
		IsActive: true,
	}
}

// ToUserViewModelFromUpdateRequest creates a view model from an update
// request.
//
// Role is not part of UpdateUserRequest, so it must be supplied by the
// caller — typically the existing user's role, read before the update
// was attempted. Passing it through preserves the role badge in the
// row template when the update fails and the form is re-rendered.
func ToUserViewModelFromUpdateRequest(req domain.UpdateUserRequest, id int32, role string) UserViewModel {
	isActive := false
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if role == "" {
		role = "user"
	}
	return UserViewModel{
		ID:       id,
		Email:    req.Email,
		Username: req.Username,
		FullName: req.FullName,
		Role:     role,
		IsActive: isActive,
	}
}

// ParseID parses a string ID to int32.
func ParseID(idStr string) (int32, error) {
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(id), nil
}

// GetPage gets the page parameter from the request.
func GetPage(c *gin.Context) int {
	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// GetLimit gets the limit parameter from the request.
func GetLimit(c *gin.Context) int {
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		return 10
	}
	return limit
}

// IsHTMX checks if the request is from HTMX.
func IsHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") != ""
}

// HTMXMiddleware adds HTMX context to the request.
func HTMXMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("is_htmx", IsHTMX(c))
		c.Next()
	}
}

// FormatTimestamp formats a time.Time for display.
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// TruncateText truncates text to a certain length.
func TruncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// SafeString returns a safe string for HTML.
func SafeString(s string) string {
	return strings.ReplaceAll(s, "\"", "&quot;")
}
