package domain

import (
	"time"
)

// UTCTime wraps time.Time to ensure UTC marshaling
type UTCTime time.Time

func (t UTCTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(t).UTC().Format(time.RFC3339) + `"`), nil
}

func (t *UTCTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*t = UTCTime(time.Time{})
		return nil
	}
	parsed, err := time.Parse(`"`+time.RFC3339+`"`, string(data))
	if err != nil {
		return err
	}
	*t = UTCTime(parsed.UTC())
	return nil
}

func (t UTCTime) Time() time.Time {
	return time.Time(t).UTC()
}

type User struct {
	ID           int32    `json:"id"`
	Email        string   `json:"email"`
	PasswordHash string   `json:"-"`
	Username     string   `json:"username"`
	FullName     string   `json:"full_name"`
	IsActive     bool     `json:"is_active"`
	Role         string   `json:"role"`
	LastLogin    *UTCTime `json:"last_login,omitempty"`
	CreatedAt    UTCTime  `json:"created_at"`
	UpdatedAt    UTCTime  `json:"updated_at"`
}

// CreateUserRequest is the payload for creating a user.
//
// Role is deliberately absent. New accounts are always created with
// role "user"; promotion is a separate, admin-only operation exposed
// through UpdateUserRoleRequest. This makes the attacker-controlled-role
// vulnerability impossible to reintroduce: the DTO cannot carry a role,
// and any handler that tries to read req.Role will not compile.
type CreateUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Username string `json:"username" binding:"required"`
	FullName string `json:"full_name" binding:"required"`
}

// UpdateUserRequest is the payload for updating a user's mutable
// profile fields. Role is not among them; see UpdateUserRoleRequest.
type UpdateUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Username string `json:"username" binding:"required"`
	FullName string `json:"full_name" binding:"required"`
	IsActive *bool  `json:"is_active"`
}

// UpdateUserRoleRequest is the payload for the dedicated role-change
// endpoint. The binding tag restricts the value to the allowlist, so an
// unknown role is rejected at validation time, before the handler runs.
type UpdateUserRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=user moderator admin"`
}

type UserResponse struct {
	ID        int32     `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	FullName  string    `json:"full_name"`
	IsActive  bool      `json:"is_active"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToUserResponse converts User to UserResponse with proper time handling
func ToUserResponse(user *User) UserResponse {
	return UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		FullName:  user.FullName,
		IsActive:  user.IsActive,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.Time(),
		UpdatedAt: user.UpdatedAt.Time(),
	}
}

// ValidRoles is the canonical allowlist. Kept in the domain package so
// both the service and any handler-side validation can share it.
var ValidRoles = []string{"user", "moderator", "admin"}

// IsValidRole reports whether s is a member of ValidRoles.
func IsValidRole(s string) bool {
	for _, r := range ValidRoles {
		if r == s {
			return true
		}
	}
	return false
}
