package deploy

import "errors"

var (
	ErrNotFound             = errors.New("deploy: not found")
	ErrValidation           = errors.New("deploy: validation")
	ErrConflict             = errors.New("deploy: conflict")
	ErrDeployInFlight       = errors.New("deploy: a deploy is in progress")
	ErrPreviewsOpen         = errors.New("deploy: close the open previews first")
	ErrNameConflict         = errors.New("deploy: name already exists in the target environment")
	ErrDomainConflict       = errors.New("deploy: domain already in use on the target server")
	ErrServerNotFound       = errors.New("deploy: server not found")
	ErrAgentUnavailable     = errors.New("deploy: agent unavailable")
	ErrHealthcheck          = errors.New("deploy: healthcheck failed")
	ErrDisabled             = errors.New("deploy: feature disabled")
	ErrNotConnected         = errors.New("deploy: provider is not connected")
	ErrProvider             = errors.New("deploy: provider call failed")
	ErrSourceNotImplemented = errors.New("deploy: source type is not supported yet")
	ErrNoRunningDeployment  = errors.New("deploy: application has no running deployment")
	ErrContainerNotFound    = errors.New("deploy: container is not part of the running deployment")
	ErrContainerNotRunning  = errors.New("deploy: container is not running")
	ErrBackupInFlight       = errors.New("deploy: a backup or restore is in progress")
)
