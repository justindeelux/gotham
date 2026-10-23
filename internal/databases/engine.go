package databases

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
	EngineMariaDB  = "mariadb"
	EngineMongoDB  = "mongodb"
	EngineRedis    = "redis"
)

type Credentials struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Database     string `json:"database"`
	RootPassword string `json:"root_password,omitempty"`
}

type PortSpec struct {
	Internal int32
	Protocol string
}

type VolumeSpec struct {
	MountPath string
}

type Probe string

const (
	ProbeHealth Probe = "health"
	ProbeState  Probe = "state"
	ProbeExec   Probe = "exec"
)

const (
	placeholderUser         = "{{username}}"
	placeholderPassword     = "{{password}}"
	placeholderDatabase     = "{{database}}"
	placeholderRootPassword = "{{root_password}}"
)

type Healthcheck struct {
	Probe   Probe
	Command []string
	Timeout time.Duration
}

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

type DatabaseEngine interface {
	Image(version string) string
	EnvSpec(c Credentials) []string
	PortSpec() PortSpec
	VolumeSpec() VolumeSpec
	Healthcheck() Healthcheck
}

type CommandSpec interface {
	Command(c Credentials) []string
}

type VersionedVolumeSpec interface {
	VolumeSpecFor(version string) VolumeSpec
}

var engineOrder = []string{EnginePostgres, EngineMySQL, EngineMariaDB, EngineMongoDB, EngineRedis}

var engineRegistry = map[string]DatabaseEngine{
	EnginePostgres: NewPostgresEngine(),
	EngineMySQL:    NewMySQLEngine(),
	EngineMariaDB:  NewMariaDBEngine(),
	EngineMongoDB:  NewMongoDBEngine(),
	EngineRedis:    NewRedisEngine(),
}

func EngineNames() []string {
	names := make([]string, 0, len(engineOrder))
	return append(names, engineOrder...)
}

func LookupEngine(name string) (DatabaseEngine, bool) {
	engine, ok := engineRegistry[strings.ToLower(strings.TrimSpace(name))]
	return engine, ok
}

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

var versionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

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

func engineImage(repo, defaultTag, version string) string {
	if tag := strings.TrimSpace(version); tag != "" {
		return repo + ":" + tag
	}
	return repo + ":" + defaultTag
}
