package databases

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Engine names accepted by the API and stored in the databases.engine column.
const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
	EngineMariaDB  = "mariadb"
	EngineMongoDB  = "mongodb"
	EngineRedis    = "redis"
)

// Credentials are the generated login details of one database. They are shown
// to their owner (create response and the credentials endpoint) and never
// stored in the clear. RootPassword is the separate MySQL/MariaDB root login;
// engines without a second account leave it empty.
type Credentials struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Database     string `json:"database"`
	RootPassword string `json:"root_password,omitempty"`
}

// PortSpec is the engine's listener inside the container. The control plane
// publishes it on the host only when the database requested a public port.
type PortSpec struct {
	// Internal is the container port the engine listens on.
	Internal int32
	// Protocol is the transport ("tcp" for every supported engine).
	Protocol string
}

// VolumeSpec locates the engine's data directory inside the container. The
// volume name itself is a Gotham convention owned by the service: the named
// volume "gotham-db-{id}" is mounted at MountPath, so removing the container
// cannot remove the data.
type VolumeSpec struct {
	MountPath string
}

// Probe is how readiness is judged while a database is provisioned.
type Probe string

const (
	// ProbeHealth judges readiness from the container's native Docker
	// healthcheck, which runs the engine's probe command inside the container
	// and reports "healthy" only once it passes. It is the only probe the
	// current agent contract can evaluate end to end.
	ProbeHealth Probe = "health"
	// ProbeState judges readiness from the bare container state. It is the
	// pre-healthcheck behavior, kept only as the fallback for a container with
	// no healthcheck.
	ProbeState Probe = "state"
	// ProbeExec is an in-container command probe driven by the control plane.
	// It is carried by the spec for an agent contract that gains an Exec RPC.
	ProbeExec Probe = "exec"
)

// Placeholders substituted by Healthcheck.CommandFor when a command needs the
// generated credentials. {{root_password}} resolves to the dedicated
// MySQL/MariaDB root login (falling back to the regular password).
const (
	placeholderUser         = "{{username}}"
	placeholderPassword     = "{{password}}"
	placeholderDatabase     = "{{database}}"
	placeholderRootPassword = "{{root_password}}"
)

// Healthcheck describes how the control plane waits for a fresh container.
type Healthcheck struct {
	// Probe is the readiness strategy (ProbeHealth today).
	Probe Probe
	// Command is the canonical in-container probe with credential
	// placeholders, e.g. pg_isready -U {{username}}. Use CommandFor to render
	// it; the rendered command becomes the container's native Docker
	// healthcheck, so Docker evaluates the engine readiness signal.
	Command []string
	// Timeout bounds the wait for the engine to report healthy.
	Timeout time.Duration
}

// CommandFor renders the probe command with the database's credentials. It
// returns an empty slice when the engine declared no command.
func (h Healthcheck) CommandFor(c Credentials) []string {
	if len(h.Command) == 0 {
		return []string{}
	}
	command := make([]string, 0, len(h.Command))
	for _, part := range h.Command {
		part = strings.ReplaceAll(part, placeholderUser, c.Username)
		part = strings.ReplaceAll(part, placeholderPassword, c.Password)
		part = strings.ReplaceAll(part, placeholderDatabase, c.Database)
		part = strings.ReplaceAll(part, placeholderRootPassword, rootPassword(c))
		command = append(command, part)
	}
	return command
}

// DatabaseEngine maps one engine onto its container shape. Implementations are
// stateless and shared; every method must be safe for concurrent use.
type DatabaseEngine interface {
	// Image returns the image reference for version; an empty version selects
	// the engine's default tag. Callers validate the version first — see
	// ValidateVersion.
	Image(version string) string
	// EnvSpec returns the standard environment the image initialises from
	// (user, password, database) as KEY=VALUE entries.
	EnvSpec(c Credentials) []string
	// PortSpec returns the engine's internal listener.
	PortSpec() PortSpec
	// VolumeSpec returns the data directory to mount the named volume on.
	VolumeSpec() VolumeSpec
	// Healthcheck returns the readiness probe for a fresh container.
	Healthcheck() Healthcheck
}

// CommandSpec is an optional extension of DatabaseEngine for engines whose
// official image needs an explicit run command (Redis has no password
// environment variable of its own). The service type-asserts it when it builds
// the run payload; engines without it run the image's default entrypoint.
type CommandSpec interface {
	// Command returns the container command for the given credentials.
	Command(c Credentials) []string
}

// VersionedVolumeSpec is an optional extension of DatabaseEngine for engines
// whose data directory moved in a later major version. The service type-asserts
// it when it builds the run payload and falls back to VolumeSpec otherwise.
// PostgreSQL 18 moved the declared VOLUME from /var/lib/postgresql/data to
// /var/lib/postgresql (PGDATA becomes /var/lib/postgresql/18/docker), and the
// 18+ entrypoint refuses to start when the old path is mounted.
type VersionedVolumeSpec interface {
	// VolumeSpecFor returns the data directory for a specific image tag.
	VolumeSpecFor(version string) VolumeSpec
}

// engineOrder fixes the registry order used by EngineNames (the API list and
// the UI picker read it).
var engineOrder = []string{EnginePostgres, EngineMySQL, EngineMariaDB, EngineMongoDB, EngineRedis}

// engineRegistry holds the five engines, keyed by their canonical name.
var engineRegistry = map[string]DatabaseEngine{
	EnginePostgres: NewPostgresEngine(),
	EngineMySQL:    NewMySQLEngine(),
	EngineMariaDB:  NewMariaDBEngine(),
	EngineMongoDB:  NewMongoDBEngine(),
	EngineRedis:    NewRedisEngine(),
}

// EngineNames returns every supported engine name in registry order.
func EngineNames() []string {
	names := make([]string, 0, len(engineOrder))
	return append(names, engineOrder...)
}

// LookupEngine returns the engine registered for name (case-insensitive).
func LookupEngine(name string) (DatabaseEngine, bool) {
	engine, ok := engineRegistry[strings.ToLower(strings.TrimSpace(name))]
	return engine, ok
}

// parseEngine resolves an engine name from a request, honouring the usual
// aliases ("postgresql", "mongo") and rejecting everything else with
// ErrValidation. It returns the canonical name next to the engine so the row
// stores one spelling.
func parseEngine(name string) (DatabaseEngine, string, error) {
	canonical := strings.ToLower(strings.TrimSpace(name))
	switch canonical {
	case "postgresql", "pg":
		canonical = EnginePostgres
	case "mongo":
		canonical = EngineMongoDB
	}
	engine, ok := LookupEngine(canonical)
	if !ok {
		return nil, "", fmt.Errorf("%w: unsupported engine %q (supported: %s)",
			ErrValidation, name, strings.Join(EngineNames(), ", "))
	}
	return engine, canonical, nil
}

// versionPattern pins the image tag alphabet: a tag may not carry a colon, a
// slash or whitespace, which keeps a request from smuggling a registry
// reference into the image field.
var versionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// ValidateVersion accepts an empty version (the engine default) or a plain
// image tag.
func ValidateVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("%w: invalid version %q", ErrValidation, version)
	}
	return nil
}

// engineImage renders "repo:tag", falling back to defaultTag when version is
// empty. version must already pass ValidateVersion.
func engineImage(repo, defaultTag, version string) string {
	if tag := strings.TrimSpace(version); tag != "" {
		return repo + ":" + tag
	}
	return repo + ":" + defaultTag
}
