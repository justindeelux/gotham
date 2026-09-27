package databases

import "time"

// Default image tags used when a request pins no version. They are pinned in
// one place so an engine's default can be bumped without touching the service.
const (
	defaultPostgresVersion = "16-alpine"
	defaultMySQLVersion    = "8.4"
	defaultMariaDBVersion  = "11.4"
	defaultMongoDBVersion  = "7.0"
	defaultRedisVersion    = "7.2-alpine"
)

// PostgresEngine maps PostgreSQL onto its container shape: the official
// image initialises a superuser, a database and a password from three
// standard environment variables, and keeps the data directory on the mounted
// volume.
type PostgresEngine struct{}

// NewPostgresEngine returns the PostgreSQL engine.
func NewPostgresEngine() *PostgresEngine { return &PostgresEngine{} }

// Compile-time guarantees.
var _ DatabaseEngine = (*PostgresEngine)(nil)

// Image implements DatabaseEngine.
func (e *PostgresEngine) Image(version string) string {
	return engineImage("postgres", defaultPostgresVersion, version)
}

// EnvSpec implements DatabaseEngine.
func (e *PostgresEngine) EnvSpec(c Credentials) []string {
	return []string{
		"POSTGRES_USER=" + c.Username,
		"POSTGRES_PASSWORD=" + c.Password,
		"POSTGRES_DB=" + c.Database,
	}
}

// PortSpec implements DatabaseEngine.
func (e *PostgresEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 5432, Protocol: "tcp"}
}

// VolumeSpec implements DatabaseEngine.
func (e *PostgresEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/var/lib/postgresql/data"}
}

// Healthcheck implements DatabaseEngine: pg_isready is the canonical probe,
// and the state wait allows for first-boot initialisation of the cluster.
func (e *PostgresEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeState,
		Command: []string{"pg_isready", "-U", placeholderUser, "-d", placeholderDatabase},
		Timeout: 60 * time.Second,
	}
}
