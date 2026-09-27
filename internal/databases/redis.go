package databases

import "time"

// RedisEngine maps Redis onto its container shape. The official image has no
// password environment variable of its own, so the generated password reaches
// the server through the --requirepass argument of the run command while
// REDIS_PASSWORD stays the single source of truth in the environment (clients
// and future tooling read the same key).
type RedisEngine struct{}

// NewRedisEngine returns the Redis engine.
func NewRedisEngine() *RedisEngine { return &RedisEngine{} }

// Compile-time guarantees.
var (
	_ DatabaseEngine = (*RedisEngine)(nil)
	_ CommandSpec    = (*RedisEngine)(nil)
)

// Image implements DatabaseEngine.
func (e *RedisEngine) Image(version string) string {
	return engineImage("redis", defaultRedisVersion, version)
}

// EnvSpec implements DatabaseEngine. There is no user or database to create:
// Redis authenticates the built-in "default" ACL user with the password, and
// the database index concept does not map onto a name — both are reported
// through Credentials instead.
func (e *RedisEngine) EnvSpec(c Credentials) []string {
	return []string{"REDIS_PASSWORD=" + c.Password}
}

// PortSpec implements DatabaseEngine.
func (e *RedisEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 6379, Protocol: "tcp"}
}

// VolumeSpec implements DatabaseEngine: /data holds the RDB dumps and the AOF
// file, so both persistence modes survive a container replacement.
func (e *RedisEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data"}
}

// Healthcheck implements DatabaseEngine: an authenticated PING is the
// canonical probe, bounded tightly because Redis starts without initialisation.
func (e *RedisEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeState,
		Command: []string{"redis-cli", "-a", placeholderPassword, "ping"},
		Timeout: 30 * time.Second,
	}
}

// Command implements CommandSpec: the image's default command starts an open
// server, so the engine passes the password (from REDIS_PASSWORD) and turns on
// append-only persistence explicitly.
func (e *RedisEngine) Command(_ Credentials) []string {
	return []string{
		"sh", "-c",
		`exec redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
	}
}
