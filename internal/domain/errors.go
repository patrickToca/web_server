package domain

import "errors"

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrEmailExists    = errors.New("email already exists")
	ErrUsernameExists = errors.New("username already exists")
)
