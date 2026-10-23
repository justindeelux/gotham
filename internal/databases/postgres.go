package databases

import (
	"strconv"
	"strings"
	"time"
)

const (
	defaultPostgresVersion = "16-alpine"
	defaultMySQLVersion    = "8.4"
	defaultMariaDBVersion  = "11.4"
	defaultMongoDBVersion  = "7.0"
	defaultRedisVersion    = "7.2-alpine"
)

type PostgresEngine struct{}

func NewPostgresEngine() *PostgresEngine { return &PostgresEngine{} }

var _ DatabaseEngine = (*PostgresEngine)(nil)

func (e *PostgresEngine) Image(version string) string {
	return engineImage("postgres", defaultPostgresVersion, version)
}

func (e *PostgresEngine) EnvSpec(c Credentials) []string {
	return []string{
		"POSTGRES_USER=" + c.Username,
		"POSTGRES_PASSWORD=" + c.Password,
		"POSTGRES_DB=" + c.Database,
	}
}

func (e *PostgresEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 5432, Protocol: "tcp"}
}

func (e *PostgresEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: postgresLegacyMountPath}
}

const (
	postgresLegacyMountPath = "/var/lib/postgresql/data"
	postgres18MountPath     = "/var/lib/postgresql"
	postgres18Major         = 18
)

func (e *PostgresEngine) VolumeSpecFor(version string) VolumeSpec {
	if postgresMajor(version) >= postgres18Major {
		return VolumeSpec{MountPath: postgres18MountPath}
	}
	return VolumeSpec{MountPath: postgresLegacyMountPath}
}

func postgresMajor(version string) int {
	tag := strings.TrimSpace(version)
	if tag == "" {
		tag = defaultPostgresVersion
	}
	base := tag
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	digits := base
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		digits = digits[:i]
	}
	major, err := strconv.Atoi(digits)
	if err != nil {
		return postgres18Major
	}
	return major
}

var _ VersionedVolumeSpec = (*PostgresEngine)(nil)

func (e *PostgresEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"pg_isready", "-h", "127.0.0.1", "-U", placeholderUser, "-d", placeholderDatabase},
		Timeout: 60 * time.Second,
	}
}
