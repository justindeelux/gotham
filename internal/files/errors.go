package files

import "errors"

var (
	ErrNotFound            = errors.New("files: not found")
	ErrValidation          = errors.New("files: invalid request")
	ErrForbidden           = errors.New("files: insufficient team role")
	ErrTooLarge            = errors.New("files: file is too large to edit")
	ErrArchiveLimit        = errors.New("files: archive exceeds the node limits")
	ErrArchiveEntry        = errors.New("files: archive contains an unsupported entry")
	ErrInvalidArchive      = errors.New("files: archive is invalid")
	ErrNotEditable         = errors.New("files: file is not editable text")
	ErrConflict            = errors.New("files: conflict")
	ErrBusy                = errors.New("files: node is busy")
	ErrInsufficientStorage = errors.New("files: insufficient storage")
	ErrAgentOutdated       = errors.New("files: node agent is outdated")
	ErrAgentUnavailable    = errors.New("files: agent unavailable")
	ErrDisabled            = errors.New("files: feature disabled")
	ErrHistoryUnavailable  = errors.New("files: file history is unavailable")
)
