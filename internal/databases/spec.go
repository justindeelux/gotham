package databases

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

const (
	volumePrefix    = "gotham-db-"
	containerPrefix = "gotham-db-"
	labelManaged    = "gotham.managed"
	labelDatabaseID = "gotham.db_id"
	labelEngine     = "gotham.engine"
	portsLabel      = "gotham.ports"
)

func volumeSpecFor(engine DatabaseEngine, version string) VolumeSpec {
	if versioned, ok := engine.(VersionedVolumeSpec); ok {
		return versioned.VolumeSpecFor(version)
	}
	return engine.VolumeSpec()
}

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func VolumeName(id uuid.UUID) string {
	return volumePrefix + id.String()
}

func buildRunOptions(db Database, engine DatabaseEngine, secrets []Secret, secretKey string) (containers.RunOptions, error) {
	credentials, err := openCredentials(secretKey, secrets)
	if err != nil {
		return containers.RunOptions{}, err
	}

	port := engine.PortSpec()
	if port.Internal <= 0 || port.Internal > 65535 {
		return containers.RunOptions{}, fmt.Errorf("%w: engine reports an invalid internal port", ErrValidation)
	}
	volume := volumeSpecFor(engine, db.Version)
	if strings.TrimSpace(volume.MountPath) == "" {
		return containers.RunOptions{}, fmt.Errorf("%w: engine reports no data directory", ErrValidation)
	}
	if !strings.HasPrefix(db.StoragePath, volumePrefix) {
		return containers.RunOptions{}, fmt.Errorf("%w: database has no gotham volume", ErrValidation)
	}

	labels := map[string]string{
		labelManaged:    "true",
		labelDatabaseID: db.ID.String(),
		labelEngine:     db.Engine,
	}
	options := containers.RunOptions{
		Image:       engine.Image(db.Version),
		Name:        containerName(db),
		Env:         engine.EnvSpec(credentials),
		Labels:      labels,
		Volumes:     []string{db.StoragePath + ":" + volume.MountPath},
		Healthcheck: runHealthcheck(engine.Healthcheck(), credentials),
	}
	if commander, ok := engine.(CommandSpec); ok {
		options.Command = commander.Command(credentials)
	}
	if db.PublicPort > 0 {
		spec := fmt.Sprintf("%d:%d", db.PublicPort, port.Internal)
		options.Ports = []string{spec}
		labels[portsLabel] = spec
	}
	return options, nil
}

const (
	healthcheckInterval = 10 * time.Second
	healthcheckTimeout  = 5 * time.Second
	healthcheckRetries  = 3
)

func runHealthcheck(health Healthcheck, credentials Credentials) *containers.Healthcheck {
	command := health.CommandFor(credentials)
	if len(command) == 0 {
		return nil
	}
	return &containers.Healthcheck{
		Test:        command,
		Interval:    healthcheckInterval,
		Timeout:     healthcheckTimeout,
		Retries:     healthcheckRetries,
		StartPeriod: health.Timeout,
	}
}

func containerName(db Database) string {
	name := strings.ToLower(db.Name)
	name = strings.Trim(invalidNameChars.ReplaceAllString(name, "-"), "-")
	if len(name) > 32 {
		name = strings.Trim(name[:32], "-")
	}
	suffix := db.ID.String()[:8]
	if name == "" {
		return containerPrefix + suffix
	}
	return containerPrefix + name + "-" + suffix
}
