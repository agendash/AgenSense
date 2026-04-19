package device

import "errors"

var (
	ErrNotFound      = errors.New("device: not found")
	ErrConflict      = errors.New("device: conflict")
	ErrInvalidInput  = errors.New("device: invalid input")
	ErrUnauthorized  = errors.New("device: unauthorized")
	ErrExpired       = errors.New("device: expired")
	ErrConfigMissing = errors.New("device: config snapshot missing")
)
