package databases

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// sampleCredentials is the credential set the engine table-driven tests feed
// into EnvSpec and Healthcheck.CommandFor.
var sampleCredentials = Credentials{
	Username:     "app",
	Password:     "s3cret-pw",
	Database:     "appdb",
	RootPassword: "root-pw",
}

// TestEngineSpecs pins every engine's container shape: default and explicit
// image tag, standard environment, internal port, data directory and
// readiness probe. A change here is a change to what gets deployed.
func TestEngineSpecs(t *testing.T) {
	tests := []struct {
		name         string
		engine       string
		image        string
		explicitTag  string
		explicit     string
		envKeys      []string
		internalPort int32
		mount        string
		probe        string
	}{
		{
			name:         "postgres",
			engine:       EnginePostgres,
			image:        "postgres:16-alpine",
			explicitTag:  "15",
			explicit:     "postgres:15",
			envKeys:      []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"},
			internalPort: 5432,
			mount:        "/var/lib/postgresql/data",
			probe:        "pg_isready",
		},
		{
			name:         "mysql",
			engine:       EngineMySQL,
			image:        "mysql:8.4",
			explicitTag:  "8.0",
			explicit:     "mysql:8.0",
			envKeys:      []string{"MYSQL_ROOT_PASSWORD", "MYSQL_USER", "MYSQL_PASSWORD", "MYSQL_DATABASE"},
			internalPort: 3306,
			mount:        "/var/lib/mysql",
			probe:        "mysqladmin",
		},
		{
			name:         "mariadb",
			engine:       EngineMariaDB,
			image:        "mariadb:11.4",
			explicitTag:  "10.11",
			explicit:     "mariadb:10.11",
			envKeys:      []string{"MYSQL_ROOT_PASSWORD", "MYSQL_USER", "MYSQL_PASSWORD", "MYSQL_DATABASE"},
			internalPort: 3306,
			mount:        "/var/lib/mysql",
			probe:        "mariadb-admin",
		},
		{
			name:         "mongodb",
			engine:       EngineMongoDB,
			image:        "mongo:7.0",
			explicitTag:  "6.0",
			explicit:     "mongo:6.0",
			envKeys:      []string{"MONGO_INITDB_ROOT_USERNAME", "MONGO_INITDB_ROOT_PASSWORD", "MONGO_INITDB_DATABASE"},
			internalPort: 27017,
			mount:        "/data/db",
			probe:        "mongosh",
		},
		{
			name:         "redis",
			engine:       EngineRedis,
			image:        "redis:7.2-alpine",
			explicitTag:  "7.4",
			explicit:     "redis:7.4",
			envKeys:      []string{"REDIS_PASSWORD"},
			internalPort: 6379,
			mount:        "/data",
			probe:        "redis-cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, ok := LookupEngine(tt.engine)
			if !ok {
				t.Fatalf("LookupEngine(%q) not registered", tt.engine)
			}

			if got := engine.Image(""); got != tt.image {
				t.Errorf("Image(\"\") = %q, want %q", got, tt.image)
			}
			if got := engine.Image(tt.explicitTag); got != tt.explicit {
				t.Errorf("Image(%q) = %q, want %q", tt.explicitTag, got, tt.explicit)
			}

			env := engine.EnvSpec(sampleCredentials)
			if keys := envKeys(env); !equalStrings(keys, tt.envKeys) {
				t.Errorf("EnvSpec keys = %v, want %v", keys, tt.envKeys)
			}
			for _, entry := range env {
				if !strings.Contains(entry, "=") {
					t.Errorf("EnvSpec entry %q is not KEY=VALUE", entry)
				}
				if strings.TrimSpace(entry) == "" {
					t.Error("EnvSpec produced an empty entry")
				}
			}

			port := engine.PortSpec()
			if port.Internal != tt.internalPort || port.Protocol != "tcp" {
				t.Errorf("PortSpec = %+v, want {Internal:%d Protocol:tcp}", port, tt.internalPort)
			}
			if port.Internal <= 0 || port.Internal > 65535 {
				t.Errorf("PortSpec.Internal = %d, out of range", port.Internal)
			}

			volume := engine.VolumeSpec()
			if volume.MountPath != tt.mount {
				t.Errorf("VolumeSpec = %+v, want mount %q", volume, tt.mount)
			}
			if !strings.HasPrefix(volume.MountPath, "/") {
				t.Errorf("VolumeSpec.MountPath %q must be absolute", volume.MountPath)
			}

			health := engine.Healthcheck()
			if health.Probe != ProbeState {
				t.Errorf("Healthcheck.Probe = %q, want %q", health.Probe, ProbeState)
			}
			if health.Timeout <= 0 {
				t.Errorf("Healthcheck.Timeout = %s, want > 0", health.Timeout)
			}
			if len(health.Command) == 0 || health.Command[0] != tt.probe {
				t.Errorf("Healthcheck.Command = %v, want it to start with %q", health.Command, tt.probe)
			}
		})
	}
}

// TestRegistryCoversEveryEngine pins the registry contents and their order —
// the API lists and the UI picker read it.
func TestRegistryCoversEveryEngine(t *testing.T) {
	want := []string{EnginePostgres, EngineMySQL, EngineMariaDB, EngineMongoDB, EngineRedis}
	got := EngineNames()
	if !equalStrings(got, want) {
		t.Fatalf("EngineNames() = %v, want %v", got, want)
	}
	for _, name := range want {
		if _, ok := LookupEngine(name); !ok {
			t.Errorf("engine %q is not registered", name)
		}
	}
	if _, ok := LookupEngine("  POSTGRES  "); !ok {
		t.Error("LookupEngine must trim and lower-case the name")
	}
	if _, ok := LookupEngine("oracle"); ok {
		t.Error("LookupEngine accepted an unknown engine")
	}
}

// TestParseEngine exercises the alias and error paths of parseEngine.
func TestParseEngine(t *testing.T) {
	tests := []struct {
		input     string
		canonical string
		wantErr   bool
	}{
		{input: "postgres", canonical: EnginePostgres},
		{input: " PostgreSQL ", canonical: EnginePostgres},
		{input: "pg", canonical: EnginePostgres},
		{input: "mongo", canonical: EngineMongoDB},
		{input: "MongoDB", canonical: EngineMongoDB},
		{input: "MariaDB", canonical: EngineMariaDB},
		{input: "redis", canonical: EngineRedis},
		{input: "oracle", wantErr: true},
		{input: "", wantErr: true},
		{input: "postgres; DROP TABLE", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			engine, canonical, err := parseEngine(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("parseEngine(%q) error = %v, want ErrValidation", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEngine(%q): %v", tt.input, err)
			}
			if engine == nil {
				t.Fatal("parseEngine returned a nil engine")
			}
			if canonical != tt.canonical {
				t.Errorf("canonical = %q, want %q", canonical, tt.canonical)
			}
		})
	}
}

// TestValidateVersion pins the image-tag alphabet: no colons, slashes or
// whitespace may reach the image reference.
func TestValidateVersion(t *testing.T) {
	// Surrounding whitespace is tolerated (the service trims before storing);
	// the tag alphabet itself stays strict.
	valid := []string{"", "16", "16-alpine", "8.0.36", "7.4.0-beta.1", "LATEST", " 16", "16\n"}
	for _, version := range valid {
		if err := ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q) = %v, want nil", version, err)
		}
	}
	invalid := []string{"16:tag", "repo/16", "16 tag", "-16", "16/../../etc"}
	for _, version := range invalid {
		if err := ValidateVersion(version); !errors.Is(err, ErrValidation) {
			t.Errorf("ValidateVersion(%q) = %v, want ErrValidation", version, err)
		}
	}
}

// TestHealthcheckCommandFor checks the credential placeholders render per
// engine, including the separate MySQL/MariaDB root login.
func TestHealthcheckCommandFor(t *testing.T) {
	engine, _ := LookupEngine(EnginePostgres)
	got := engine.Healthcheck().CommandFor(sampleCredentials)
	want := []string{"pg_isready", "-U", "app", "-d", "appdb"}
	if !equalStrings(got, want) {
		t.Errorf("postgres probe = %v, want %v", got, want)
	}

	engine, _ = LookupEngine(EngineMySQL)
	line := strings.Join(engine.Healthcheck().CommandFor(sampleCredentials), " ")
	if !strings.Contains(line, "-proot-pw") {
		t.Errorf("mysql probe = %q, want the root password substituted", line)
	}
	if strings.Contains(line, placeholderRootPassword) {
		t.Errorf("mysql probe = %q, still contains the placeholder", line)
	}

	credentials := sampleCredentials
	credentials.RootPassword = ""
	engine, _ = LookupEngine(EngineMariaDB)
	line = strings.Join(engine.Healthcheck().CommandFor(credentials), " ")
	if !strings.Contains(line, "-ps3cret-pw") {
		t.Errorf("mariadb probe without a root password = %q, want the fallback", line)
	}

	engine, _ = LookupEngine(EngineRedis)
	line = strings.Join(engine.Healthcheck().CommandFor(sampleCredentials), " ")
	if !strings.Contains(line, "s3cret-pw") {
		t.Errorf("redis probe = %q, want the password substituted", line)
	}
}

// TestRedisNeedsCommandSpec pins the one engine whose official image requires
// an explicit run command to enable authentication and persistence.
func TestRedisNeedsCommandSpec(t *testing.T) {
	engine, ok := LookupEngine(EngineRedis)
	if !ok {
		t.Fatal("redis is not registered")
	}
	commander, ok := engine.(CommandSpec)
	if !ok {
		t.Fatal("redis must implement CommandSpec: the image has no password environment variable")
	}
	command := strings.Join(commander.Command(sampleCredentials), " ")
	if !strings.Contains(command, "redis-server") || !strings.Contains(command, "--requirepass") {
		t.Errorf("redis command = %q, want redis-server --requirepass", command)
	}
	if !strings.Contains(command, "REDIS_PASSWORD") {
		t.Errorf("redis command = %q, want it to read the sealed password from the environment", command)
	}
	if _, isCommander := mustEngine(t, EnginePostgres).(CommandSpec); isCommander {
		t.Error("postgres should run the image entrypoint, not declare CommandSpec")
	}
}

// TestHealthcheckWindows pins the per-engine provisioning windows so a typo
// cannot shorten them to a flaky wait.
func TestHealthcheckWindows(t *testing.T) {
	for _, name := range EngineNames() {
		engine, _ := LookupEngine(name)
		timeout := engine.Healthcheck().Timeout
		if timeout < 10*time.Second {
			t.Errorf("%s healthcheck timeout = %s, want at least 10s", name, timeout)
		}
	}
}

// mustEngine resolves an engine or fails the test.
func mustEngine(t *testing.T, name string) DatabaseEngine {
	t.Helper()
	engine, ok := LookupEngine(name)
	if !ok {
		t.Fatalf("engine %q is not registered", name)
	}
	return engine
}

// envKeys extracts the sorted KEY list of a KEY=VALUE environment.
func envKeys(env []string) []string {
	keys := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		keys = append(keys, key)
	}
	return keys
}

// equalStrings compares two string slices order-sensitively.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
