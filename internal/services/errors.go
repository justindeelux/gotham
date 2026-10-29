package services

import "errors"

var (
	ErrNotFound         = errors.New("services: not found")
	ErrValidation       = errors.New("services: validation")
	ErrConflict         = errors.New("services: conflict")
	ErrServerNotFound   = errors.New("services: server not found")
	ErrDeployInFlight   = errors.New("services: a deploy is in progress")
	ErrServerPinned     = errors.New("services: a deployed service cannot change server")
	ErrNameConflict     = errors.New("services: name already exists in the target environment")
	ErrDomainConflict   = errors.New("services: domain already in use")
	ErrAgentUnavailable = errors.New("services: agent unavailable")
	ErrDeployFailed     = errors.New("services: deploy failed")
	ErrBackupInFlight   = errors.New("services: a backup or restore is in progress")
	ErrDisabled         = errors.New("services: feature disabled")
)
