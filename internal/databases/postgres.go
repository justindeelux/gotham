package databases

import (
	"strconv"
	"strings"
	"time"
)

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

// VolumeSpec implements DatabaseEngine: the data directory of every
// PostgreSQL major before 18.
func (e *PostgresEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: postgresLegacyMountPath}
}

// PostgreSQL data directory mounts. The official image moved its declared
// VOLUME and PGDATA in major 18: PGDATA is /var/lib/postgresql/18/docker and
// the entrypoint refuses to start when the old /var/lib/postgresql/data path
// is mounted (it treats it as an unused volume). Mounting PG18+ at
// /var/lib/postgresql keeps the whole version tree on the named volume.
const (
	postgresLegacyMountPath = "/var/lib/postgresql/data"
	postgres18MountPath     = "/var/lib/postgresql"
	postgres18Major         = 18
)

// VolumeSpecFor implements VersionedVolumeSpec: choose the mount path from the
// image tag's major version. A tag whose major cannot be parsed (e.g.
// "latest" or "alpine") is treated as the current major, which is 18+ — the
// only alternative would be silently mounting a path the image rejects.
func (e *PostgresEngine) VolumeSpecFor(version string) VolumeSpec {
	if postgresMajor(version) >= postgres18Major {
		return VolumeSpec{MountPath: postgres18MountPath}
	}
	return VolumeSpec{MountPath: postgresLegacyMountPath}
}

// postgresMajor parses the leading integer of an image tag ("18",
// "18-alpine", "18.1-alpine"). An empty tag is the engine default; a tag with
// no leading integer reports postgres18Major so an unknown tag targets the
// current image layout rather than the path the 18+ entrypoint rejects.
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

// Compile-time guarantee that PostgreSQL resolves its volume by version.
var _ VersionedVolumeSpec = (*PostgresEngine)(nil)

// Healthcheck implements DatabaseEngine: pg_isready is the canonical probe.
// The provisioning window (rendered as the healthcheck start period) allows
// for first-boot initialisation of the cluster before failures count.
func (e *PostgresEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"pg_isready", "-U", placeholderUser, "-d", placeholderDatabase},
		Timeout: 60 * time.Second,
	}
}
