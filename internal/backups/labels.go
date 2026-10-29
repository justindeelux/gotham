package backups

import "strings"

// Container labels shared by the core (sweep) and the drivers (temp job
// containers). The key names are a compatibility contract with containers a
// previous control plane left behind.
const (
	LabelManaged    = "gotham.managed"
	LabelResourceID = "gotham.db_id"
	LabelRole       = "gotham.role"
	LabelBackupID   = "gotham.backup_id"

	RoleBackup  = "backup"
	RoleRestore = "restore"
	RoleStage   = "restore-stage"
)

const jobDiagLimit = 600

// boundedDiag keeps container output short enough to store in an error column.
func boundedDiag(diag string) string {
	diag = strings.TrimSpace(strings.ReplaceAll(diag, "\x00", ""))
	if len(diag) <= jobDiagLimit {
		return diag
	}
	return "…" + diag[len(diag)-jobDiagLimit:]
}
