package databases

import "time"

type ClickHouseEngine struct{}

func NewClickHouseEngine() *ClickHouseEngine { return &ClickHouseEngine{} }

var _ DatabaseEngine = (*ClickHouseEngine)(nil)

func (e *ClickHouseEngine) Image(version string) string {
	return engineImage("clickhouse/clickhouse-server", defaultClickHouseVersion, version)
}

func (e *ClickHouseEngine) EnvSpec(c Credentials) []string {
	return []string{
		"CLICKHOUSE_USER=" + c.Username,
		"CLICKHOUSE_PASSWORD=" + c.Password,
		"CLICKHOUSE_DB=" + c.Database,
	}
}

func (e *ClickHouseEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 9000, Protocol: "tcp"}
}

func (e *ClickHouseEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/clickhouse"}
}

func (e *ClickHouseEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe: ProbeHealth,
		Command: []string{
			"clickhouse-client",
			"--user", placeholderUser,
			"--password", placeholderPassword,
			"--query", "SELECT 1",
		},
		Timeout: 90 * time.Second,
	}
}
