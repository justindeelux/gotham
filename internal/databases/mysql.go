package databases

import "time"

// MySQLEngine maps MySQL onto its container shape. The official image boots
// the server with a generated root login (MYSQL_ROOT_PASSWORD) and creates a
// regular user, database and password from the standard environment — two
// credential sets, which is why Credentials carries a RootPassword.
type MySQLEngine struct{}

// NewMySQLEngine returns the MySQL engine.
func NewMySQLEngine() *MySQLEngine { return &MySQLEngine{} }

// Compile-time guarantees.
var _ DatabaseEngine = (*MySQLEngine)(nil)

// Image implements DatabaseEngine.
func (e *MySQLEngine) Image(version string) string {
	return engineImage("mysql", defaultMySQLVersion, version)
}

// EnvSpec implements DatabaseEngine.
func (e *MySQLEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MYSQL_ROOT_PASSWORD=" + rootPassword(c),
		"MYSQL_USER=" + c.Username,
		"MYSQL_PASSWORD=" + c.Password,
		"MYSQL_DATABASE=" + c.Database,
	}
}

// PortSpec implements DatabaseEngine.
func (e *MySQLEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 3306, Protocol: "tcp"}
}

// VolumeSpec implements DatabaseEngine.
func (e *MySQLEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/mysql"}
}

// Healthcheck implements DatabaseEngine: mysqladmin ping answers as soon as
// the server accepts connections; the window covers first-boot initialisation.
func (e *MySQLEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-p" + placeholderRootPassword},
		Timeout: 90 * time.Second,
	}
}
