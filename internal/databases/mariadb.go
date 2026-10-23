package databases

import "time"

type MariaDBEngine struct{}

func NewMariaDBEngine() *MariaDBEngine { return &MariaDBEngine{} }

var _ DatabaseEngine = (*MariaDBEngine)(nil)

func (e *MariaDBEngine) Image(version string) string {
	return engineImage("mariadb", defaultMariaDBVersion, version)
}

func (e *MariaDBEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MYSQL_ROOT_PASSWORD=" + rootPassword(c),
		"MYSQL_USER=" + c.Username,
		"MYSQL_PASSWORD=" + c.Password,
		"MYSQL_DATABASE=" + c.Database,
	}
}

func (e *MariaDBEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 3306, Protocol: "tcp"}
}

func (e *MariaDBEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/mysql"}
}

func (e *MariaDBEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"mariadb-admin", "ping", "-h", "127.0.0.1", "-uroot", "-p" + placeholderRootPassword},
		Timeout: 90 * time.Second,
	}
}
