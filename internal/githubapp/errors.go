package githubapp

import "errors"

// Sentinel errors. HTTP handlers map these to status codes; anything else is
// an unexpected internal failure.
var (
	// ErrNotFound is returned when a GitHub App is unknown or not owned by
	// the caller.
	ErrNotFound = errors.New("githubapp: not found")
	// ErrValidation is returned when user-supplied input fails validation.
	ErrValidation = errors.New("githubapp: validation failed")
	// ErrExpiredState is returned when a single-use state is missing,
	// reused or expired. It is distinct from ErrValidation so the browser
	// callback can tell "start over" from "something broke" without
	// leaking details.
	ErrExpiredState = errors.New("githubapp: invalid or expired state")
	// ErrNoInstallationGrant is returned when every installation listed
	// successfully and none grants a repository. The deploy cloner treats it
	// as "not backed by a GitHub App connection" and keeps the legacy
	// deploy-key/anonymous behaviour for provider=github applications.
	ErrNoInstallationGrant = errors.New("githubapp: no installation grants the repository")
	// ErrGrantsUnverifiable is returned when an installation list or refresh
	// failed before a grant was found: the grants could not be verified, so
	// the deploy fails instead of degrading to a silent anonymous clone.
	ErrGrantsUnverifiable = errors.New("githubapp: installation grants unverifiable")
	// ErrUnauthorized is returned when a webhook delivery fails signature
	// verification.
	ErrUnauthorized = errors.New("githubapp: unauthorized")
	// ErrTooManyRequests bounds outstanding manifest/install states.
	ErrTooManyRequests = errors.New("githubapp: too many pending requests")
)
