package webhooks

import "errors"

// Sentinel errors. HTTP handlers map these to status codes; anything else is
// an unexpected internal failure.
var (
	// ErrNotFound is returned for an unknown provider path or an application
	// outside the caller's active team (another team's application is
	// indistinguishable from a missing one, so IDs cannot be probed).
	ErrNotFound = errors.New("webhooks: not found")
	// ErrForbidden is returned when the caller's team role does not permit
	// managing the application's hooks (read_only).
	ErrForbidden = errors.New("webhooks: insufficient team role")
	// ErrValidation is returned when user-supplied input fails validation: an
	// application without a repository, an unusable callback URL or a provider
	// connection problem.
	ErrValidation = errors.New("webhooks: validation failed")
	// ErrBadRequest is returned when a delivery body cannot be read or names
	// no repository.
	ErrBadRequest = errors.New("webhooks: malformed delivery")
	// ErrUnauthorized is returned when a delivery's signature does not verify
	// against any hook watching the repository. An unknown repository answers
	// the same way: the two cases must stay indistinguishable.
	ErrUnauthorized = errors.New("webhooks: signature verification failed")
	// ErrRateLimited is returned when a client exceeds its delivery budget.
	ErrRateLimited = errors.New("webhooks: too many deliveries")
	// ErrDuplicate is returned by the repository when a commit SHA or delivery
	// ID was already claimed: the push has been handled and must not deploy
	// again.
	ErrDuplicate = errors.New("webhooks: delivery already handled")
	// ErrConflict is returned when the application already has a hook or an
	// in-flight deployment.
	ErrConflict = errors.New("webhooks: already exists")
	// ErrNotConnected is returned when the caller has no usable connection for
	// the application's provider, so no hook can be installed or removed.
	ErrNotConnected = errors.New("webhooks: provider is not connected")
	// ErrProvider wraps a failure from the Git-host API (installing or
	// removing a hook). Handlers answer it with a gateway status instead of
	// leaking the provider's response.
	ErrProvider = errors.New("webhooks: provider call failed")
)
