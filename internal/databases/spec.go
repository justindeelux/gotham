package databases

import (
	"fmt"
	"regexp"
	"strings"

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
	volume := engine.VolumeSpec()
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
		Image:   engine.Image(db.Version),
		Name:    containerName(db),
		Env:     engine.EnvSpec(credentials),
		Labels:  labels,
		Volumes: []string{db.StoragePath + ":" + volume.MountPath},
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
