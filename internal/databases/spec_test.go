package databases

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// sealedSample returns a database row plus the sealed credentials of a fresh
// provision, which is the input buildRunOptions sees in production.
func sealedSample(t *testing.T, name, engineName string, publicPort int32) (Database, []Secret) {
	t.Helper()
	credentials, err := generateCredentials(engineName, name)
	if err != nil {
		t.Fatalf("generateCredentials: %v", err)
	}
	database := Database{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		ServerID:   uuid.New(),
		Name:       name,
		Engine:     engineName,
		Status:     StatusCreating,
		PublicPort: publicPort,
	}
	database.StoragePath = VolumeName(database.ID)
	secrets, err := sealCredentials(testSecret, database.ID, credentials)
	if err != nil {
		t.Fatalf("sealCredentials: %v", err)
	}
	return database, secrets
}

// TestBuildRunOptionsPublicPostgres asserts the full payload of a public
// PostgreSQL: image, decrypted standard environment, single published port,
// named volume bind and the gotham.* labels.
func TestBuildRunOptionsPublicPostgres(t *testing.T) {
	database, secrets := sealedSample(t, "orders", EnginePostgres, 55432)
	engine, _ := LookupEngine(EnginePostgres)

	options, err := buildRunOptions(database, engine, secrets, testSecret)
	if err != nil {
		t.Fatalf("buildRunOptions: %v", err)
	}

	if options.Image != "postgres:16-alpine" {
		t.Errorf("image = %q", options.Image)
	}
	if !strings.HasPrefix(options.Name, "gotham-db-orders-") {
		t.Errorf("container name = %q, want gotham-db-orders-<id>", options.Name)
	}
	if strings.Contains(options.Name, " ") || len(options.Name) > 64 {
		t.Errorf("container name %q is not Docker-safe", options.Name)
	}

	env := map[string]string{}
	for _, entry := range options.Env {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	if env["POSTGRES_USER"] != "orders" {
		t.Errorf("POSTGRES_USER = %q, want orders", env["POSTGRES_USER"])
	}
	if env["POSTGRES_DB"] != "orders" {
		t.Errorf("POSTGRES_DB = %q, want orders", env["POSTGRES_DB"])
	}
	if env["POSTGRES_PASSWORD"] == "" {
		t.Error("POSTGRES_PASSWORD is empty: credentials were not opened")
	}
	credentials, err := openCredentials(testSecret, secrets)
	if err != nil {
		t.Fatalf("openCredentials: %v", err)
	}
	if env["POSTGRES_PASSWORD"] != credentials.Password {
		t.Error("POSTGRES_PASSWORD does not match the stored credential")
	}

	if len(options.Volumes) != 1 || options.Volumes[0] != database.StoragePath+":/var/lib/postgresql/data" {
		t.Errorf("volumes = %v, want [%s:/var/lib/postgresql/data]", options.Volumes, database.StoragePath)
	}
	if !strings.HasPrefix(database.StoragePath, "gotham-db-") {
		t.Errorf("volume %q must follow the gotham-db- convention", database.StoragePath)
	}

	if len(options.Ports) != 1 || options.Ports[0] != "55432:5432" {
		t.Errorf("ports = %v, want [55432:5432]", options.Ports)
	}
	if options.Labels["gotham.ports"] != "55432:5432" {
		t.Errorf("ports label = %q", options.Labels["gotham.ports"])
	}
	if options.Labels[labelManaged] != "true" ||
		options.Labels[labelDatabaseID] != database.ID.String() ||
		options.Labels[labelEngine] != EnginePostgres {
		t.Errorf("labels = %v, want the gotham.db_id/engine/managed set", options.Labels)
	}
	if len(options.Command) != 0 {
		t.Errorf("command = %v, postgres runs the image entrypoint", options.Command)
	}
}

// TestBuildRunOptionsInternalOnly: without a public port nothing is published,
// so the database stays reachable only from containers on the node.
func TestBuildRunOptionsInternalOnly(t *testing.T) {
	database, secrets := sealedSample(t, "cache", EngineRedis, 0)
	engine, _ := LookupEngine(EngineRedis)

	options, err := buildRunOptions(database, engine, secrets, testSecret)
	if err != nil {
		t.Fatalf("buildRunOptions: %v", err)
	}
	if len(options.Ports) != 0 {
		t.Errorf("ports = %v, want none for an internal database", options.Ports)
	}
	if _, published := options.Labels[portsLabel]; published {
		t.Errorf("labels = %v, want no ports label", options.Labels)
	}
	if len(options.Command) == 0 || !strings.Contains(strings.Join(options.Command, " "), "--requirepass") {
		t.Errorf("command = %v, want redis-server --requirepass", options.Command)
	}
	if len(options.Env) != 1 || !strings.HasPrefix(options.Env[0], "REDIS_PASSWORD=") {
		t.Errorf("env = %v, want the REDIS_PASSWORD entry", options.Env)
	}
}

// TestBuildRunOptionsRejectsForeignVolume: a corrupted row must not mount an
// arbitrary path on a node.
func TestBuildRunOptionsRejectsForeignVolume(t *testing.T) {
	database, secrets := sealedSample(t, "orders", EnginePostgres, 0)
	database.StoragePath = "/etc"
	engine, _ := LookupEngine(EnginePostgres)

	if _, err := buildRunOptions(database, engine, secrets, testSecret); !errors.Is(err, ErrValidation) {
		t.Fatalf("buildRunOptions error = %v, want ErrValidation", err)
	}
}

// TestBuildRunOptionsBadSecret: credentials that cannot be opened never reach
// the agent.
func TestBuildRunOptionsBadSecret(t *testing.T) {
	database, secrets := sealedSample(t, "orders", EnginePostgres, 0)
	engine, _ := LookupEngine(EnginePostgres)

	if _, err := buildRunOptions(database, engine, secrets, "wrong-key"); err == nil {
		t.Fatal("buildRunOptions accepted credentials sealed with another key")
	}
}

// TestVolumeName pins the volume convention shared with backups and restore.
func TestVolumeName(t *testing.T) {
	id := uuid.New()
	want := "gotham-db-" + id.String()
	if got := VolumeName(id); got != want {
		t.Errorf("VolumeName = %q, want %q", got, want)
	}
}

// TestContainerName covers sanitisation, uniqueness and the empty-name
// fallback.
func TestContainerName(t *testing.T) {
	tests := []struct {
		name   string
		dbName string
		want   string
	}{
		{name: "orders", dbName: "orders", want: "gotham-db-orders-"},
		{name: "spaces", dbName: "My Orders!", want: "gotham-db-my-orders-"},
		{name: "long", dbName: strings.Repeat("a", 80), want: "gotham-db-" + strings.Repeat("a", 32) + "-"},
		{name: "symbols", dbName: "!!!", want: "gotham-db-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := Database{ID: uuid.New(), Name: tt.dbName}
			got := containerName(database)
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("containerName = %q, want prefix %q", got, tt.want)
			}
			// Two databases with the same name must not share a container name.
			other := Database{ID: uuid.New(), Name: tt.dbName}
			if containerName(other) == got {
				t.Errorf("containerName %q collides across databases", got)
			}
		})
	}
}
