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
	// volumePrefix names the persistent volume of a database: "gotham-db-{id}".
	// The volume outlives the container — it is what a delete keeps for the
	// grace window and what a later restore mounts back.
	volumePrefix = "gotham-db-"
	// containerPrefix names the container so an operator can spot a
	// Gotham-managed database on a node.
	containerPrefix = "gotham-db-"
	// Label keys applied to every database container. They mirror the deploy
	// package's gotham.* vocabulary (the constants are duplicated rather than
	// imported to keep this payload independent of the deploy package).
	labelManaged    = "gotham.managed"
	labelDatabaseID = "gotham.db_id"
	labelEngine     = "gotham.engine"
	// portsLabel mirrors containers.portsLabel: the ListContainers contract
	// carries no Docker port bindings, so the mapping is recorded on the
	// container and read back when listing.
	portsLabel = "gotham.ports"
)

// volumeSpecFor resolves the engine's data directory for a specific image
// tag, honouring VersionedVolumeSpec. Every path that mounts a database volume
// must go through it: the run payload, the backup/restore job containers and
// the restore staging directory all have to agree on the mount, or a
// PostgreSQL 18 database is mounted at the path its entrypoint refuses.
func volumeSpecFor(engine DatabaseEngine, version string) VolumeSpec {
	if versioned, ok := engine.(VersionedVolumeSpec); ok {
		return versioned.VolumeSpecFor(version)
	}
	return engine.VolumeSpec()
}

// invalidNameChars matches anything outside the Docker container-name alphabet
// ([a-zA-Z0-9][a-zA-Z0-9_.-]).
var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// VolumeName returns the named volume that backs the database with id. It is
// exported so sibling work (backups, restore) mounts the same volume instead
// of deriving its own spelling.
func VolumeName(id uuid.UUID) string {
	return volumePrefix + id.String()
}

// buildRunOptions assembles the agent payload for a database: image, standard
// environment, internal port (published on the host only when the database
// requested a public port), the named volume, the gotham.* labels and the
// container name.
//
// Credentials arrive sealed and are opened here: plaintext exists only in the
// returned options, which the containers service hands straight to the agent.
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
	// An internal database publishes nothing; a public one binds exactly one
	// host port so external clients can reach it.
	if db.PublicPort > 0 {
		spec := fmt.Sprintf("%d:%d", db.PublicPort, port.Internal)
		options.Ports = []string{spec}
		labels[portsLabel] = spec
	}
	return options, nil
}

// Healthcheck timing for a database container's native Docker healthcheck.
// The interval is 10s on purpose: it keeps running for the container's whole
// life, and a 2s cadence would exec mongosh/mysqladmin every couple of
// seconds forever (and could flip healthy→unhealthy on a loaded node under the
// 5s check timeout). The create-path readiness wait does its own 500ms polling
// and the start period is the engine's provisioning window, so the slower
// interval does not delay provisioning.
const (
	healthcheckInterval = 10 * time.Second
	healthcheckTimeout  = 5 * time.Second
	healthcheckRetries  = 3
)

// runHealthcheck builds the container healthcheck from the engine's canonical
// probe command (pg_isready, mysqladmin ping, ...). Credentials are rendered
// into the command here; the same values already reach the container through
// its environment, so this adds no new exposure. The probe runs inside the
// container, which is what lets the control plane evaluate a real engine
// readiness signal without an Exec RPC. A nil result disables the healthcheck.
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

// containerName derives a Docker-safe, unique container name:
// "gotham-db-<sanitized name>-<id prefix>". The ID suffix keeps a recreated
// database (same name, new row) from colliding with the container it replaces.
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
