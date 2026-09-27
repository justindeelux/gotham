package databases

import "time"

// MariaDBEngine maps MariaDB onto its container shape. It speaks the same
// MYSQL_* environment as MySQL (the images share an entrypoint), so the
// credential handling is identical.
type MariaDBEngine struct{}

// NewMariaDBEngine returns the MariaDB engine.
func NewMariaDBEngine() *MariaDBEngine { return &MariaDBEngine{} }

// Compile-time guarantees.
var _ DatabaseEngine = (*MariaDBEngine)(nil)

// Image implements DatabaseEngine.
func (e *MariaDBEngine) Image(version string) string {
	return engineImage("mariadb", defaultMariaDBVersion, version)
}

// EnvSpec implements DatabaseEngine.
func (e *MariaDBEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MYSQL_ROOT_PASSWORD=" + rootPassword(c),
		"MYSQL_USER=" + c.Username,
		"MYSQL_PASSWORD=" + c.Password,
		"MYSQL_DATABASE=" + c.Database,
	}
}

// PortSpec implements DatabaseEngine.
func (e *MariaDBEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 3306, Protocol: "tcp"}
}

// VolumeSpec implements DatabaseEngine.
func (e *MariaDBEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/mysql"}
}

// Healthcheck implements DatabaseEngine.
func (e *MariaDBEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeState,
		Command: []string{"mariadb-admin", "ping", "-h", "127.0.0.1", "-uroot", "-p" + placeholderRootPassword},
		Timeout: 90 * time.Second,
	}
}
