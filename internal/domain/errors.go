package domain

import "errors"

var (
	ErrNotFound           = errors.New("resource not found")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)
