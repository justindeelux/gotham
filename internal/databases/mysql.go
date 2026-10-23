package databases

import "time"

type MySQLEngine struct{}

func NewMySQLEngine() *MySQLEngine { return &MySQLEngine{} }

var _ DatabaseEngine = (*MySQLEngine)(nil)

func (e *MySQLEngine) Image(version string) string {
	return engineImage("mysql", defaultMySQLVersion, version)
}

func (e *MySQLEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MYSQL_ROOT_PASSWORD=" + rootPassword(c),
		"MYSQL_USER=" + c.Username,
		"MYSQL_PASSWORD=" + c.Password,
		"MYSQL_DATABASE=" + c.Database,
	}
}

func (e *MySQLEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 3306, Protocol: "tcp"}
}

func (e *MySQLEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/mysql"}
}

func (e *MySQLEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-p" + placeholderRootPassword},
		Timeout: 90 * time.Second,
	}
}
