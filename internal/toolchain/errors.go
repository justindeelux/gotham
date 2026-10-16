package toolchain

import "errors"

// Sentinel errors the HTTP layer maps onto status codes. The store and agent
// errors are wrapped, never replaced, so callers can use errors.Is.
var (
	// ErrNotFound means the node or job does not exist, or the caller may not
	// see it (a foreign node answers not-found, so ids cannot be probed).
	ErrNotFound = errors.New("toolchain: not found")
	// ErrForbidden means the caller is a member of the node's team but lacks the
	// owner/admin role a mutation needs.
	ErrForbidden = errors.New("toolchain: forbidden")
	// ErrValidation means the request was malformed: an unknown action, an
	// unknown tool, a tool that cannot be installed/uninstalled on the node.
	ErrValidation = errors.New("toolchain: invalid request")
	// ErrConflict means the requested transition is not allowed in the job's
	// current state (for example cancelling a job that is already running).
	ErrConflict = errors.New("toolchain: conflict")
	// ErrAgentUnavailable means the node's agent could not be dialed or the
	// catalog/stream RPC failed, so the request can be retried.
	ErrAgentUnavailable = errors.New("toolchain: node agent unavailable")
)
