package databases

import "github.com/justindeelux/gotham/internal/backups"

// The backup core owns the lease registry; database lifecycle actions still
// consult it so a start, stop or update cannot race a running job.
type JobLeases = backups.JobLeases
type JobLeaseKind = backups.JobLeaseKind

const (
	JobLeaseBackup    = backups.JobLeaseBackup
	JobLeaseRestore   = backups.JobLeaseRestore
	JobLeaseLifecycle = backups.JobLeaseLifecycle
)

var NewJobLeases = backups.NewJobLeases
