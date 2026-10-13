package files

import "errors"

// Sentinel errors returned by the file service and mapped to HTTP statuses by
// the route handlers. The plan's error contract is
// 400/403/404/409/413/415/428/429/501/502/503.
var (
	// ErrNotFound reports an unknown server ID, a path the agent does not
	// know, an entry outside the caller's team, or a consumed/expired
	// download ticket. A foreign team and a missing node are deliberately
	// indistinguishable (404) so server IDs cannot be probed.
	ErrNotFound = errors.New("files: not found")
	// ErrValidation reports a malformed request (empty root, bad path) or an
	// agent InvalidArgument.
	ErrValidation = errors.New("files: invalid request")
	// ErrForbidden reports a caller whose team role does not permit the read
	// (a read_only member asking for content or a download ticket) or an
	// agent PermissionDenied on the entry itself.
	ErrForbidden = errors.New("files: insufficient team role")
	// ErrTooLarge reports a file larger than the editor's readable ceiling
	// (413); the browser should offer a download instead.
	ErrTooLarge = errors.New("files: file is too large to edit")
	// ErrArchiveLimit reports an archive that exceeds the node's archive limits
	// (entry count, uncompressed bytes, compression ratio or nesting): 413 with
	// its own message, distinct from the editor's ErrTooLarge.
	ErrArchiveLimit = errors.New("files: archive exceeds the node limits")
	// ErrArchiveEntry reports an archive member the agent refuses to unpack
	// safely (a symbolic link, hard link, device, FIFO or unsupported type):
	// 409 with its own message, not a plain conflict.
	ErrArchiveEntry = errors.New("files: archive contains an unsupported entry")
	// ErrInvalidArchive reports an archive the agent cannot parse: a truncated
	// or corrupt zip/tar/tar.gz body, or a member name the format forbids (400
	// with its own message, so a broken upload does not read as "internal
	// error").
	ErrInvalidArchive = errors.New("files: archive is invalid")
	// ErrNotEditable reports a file the editor cannot load as text, for
	// example a binary body or invalid UTF-8 (415).
	ErrNotEditable = errors.New("files: file is not editable text")
	// ErrConflict reports an agent FailedPrecondition (409); the write side
	// maps etag mismatches here.
	ErrConflict = errors.New("files: conflict")
	// ErrBusy reports the node's per-node transfer semaphore being full, or the
	// control plane's own upload-staging semaphore being full (429); the caller
	// should retry.
	ErrBusy = errors.New("files: node is busy")
	// ErrInsufficientStorage reports the target filesystem is low on free space
	// or a quota is exhausted — an agent ResourceExhausted about free space, or
	// the control plane's own upload staging volume being too full (507). It is
	// distinct from ErrBusy so the browser can tell "retry later" from "free
	// space first".
	ErrInsufficientStorage = errors.New("files: insufficient storage")
	// ErrAgentOutdated reports a node agent that predates the FileService
	// (agent codes.Unimplemented) — the caller must update the agent (501).
	ErrAgentOutdated = errors.New("files: node agent is outdated")
	// ErrAgentUnavailable reports that the node agent could not be reached:
	// no dialer is configured, the connection failed, or the RPC timed out
	// (502).
	ErrAgentUnavailable = errors.New("files: agent unavailable")
	// ErrDisabled reports FEATURE_FILES=false (503): the routes are mounted
	// but the feature is off.
	ErrDisabled = errors.New("files: feature disabled")
)
