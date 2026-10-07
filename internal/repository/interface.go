package repository

import (
	"context"
)

// UserRepository defines the interface for user data operations
type UserRepository interface {
	CreateUser(ctx context.Context, arg CreateUserParams) (CreateUserRow, error)
	GetUserByID(ctx context.Context, id int32) (GetUserByIDRow, error)
	GetUserByEmail(ctx context.Context, email string) (GetUserByEmailRow, error)
	UpdateUser(ctx context.Context, arg UpdateUserParams) (UpdateUserRow, error)
	DeleteUser(ctx context.Context, id int32) error
	ListUsers(ctx context.Context, arg ListUsersParams) ([]ListUsersRow, error)
}
