package databases

import "time"

type RedisEngine struct{}

func NewRedisEngine() *RedisEngine { return &RedisEngine{} }

var (
	_ DatabaseEngine = (*RedisEngine)(nil)
	_ CommandSpec    = (*RedisEngine)(nil)
)

func (e *RedisEngine) Image(version string) string {
	return engineImage("redis", defaultRedisVersion, version)
}

func (e *RedisEngine) EnvSpec(c Credentials) []string {
	return []string{"REDIS_PASSWORD=" + c.Password}
}

func (e *RedisEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 6379, Protocol: "tcp"}
}

func (e *RedisEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data"}
}

func (e *RedisEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"redis-cli", "-a", placeholderPassword, "ping"},
		Timeout: 30 * time.Second,
	}
}

func (e *RedisEngine) Command(_ Credentials) []string {
	return []string{
		"sh", "-c",
		`exec redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
	}
}
