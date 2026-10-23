package githubapp

import "errors"

var (
	ErrNotFound            = errors.New("githubapp: not found")
	ErrValidation          = errors.New("githubapp: validation failed")
	ErrExpiredState        = errors.New("githubapp: invalid or expired state")
	ErrNoInstallationGrant = errors.New("githubapp: no installation grants the repository")
	ErrGrantsUnverifiable  = errors.New("githubapp: installation grants unverifiable")
	ErrUnauthorized        = errors.New("githubapp: unauthorized")
	ErrTooManyRequests     = errors.New("githubapp: too many pending requests")
)
