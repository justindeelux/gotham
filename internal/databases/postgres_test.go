package databases

import (
	"testing"
)

// TestPostgresVolumeSpecForVersion pins the PostgreSQL mount matrix. The
// official image moved the declared VOLUME from /var/lib/postgresql/data to
// /var/lib/postgresql in major 18, and the 18+ entrypoint refuses to start
// when the old path is mounted — so a database pinned to 18 would never boot
// with the pre-18 path. Regression for D1-7.
func TestPostgresVolumeSpecForVersion(t *testing.T) {
	engine := NewPostgresEngine()
	tests := []struct {
		version string
		want    string
	}{
		{"", postgresLegacyMountPath}, // default tag is 16-alpine
		{"16", postgresLegacyMountPath},
		{"16-alpine", postgresLegacyMountPath},
		{"16.4-alpine", postgresLegacyMountPath},
		{"17", postgresLegacyMountPath},
		{"18", postgres18MountPath},
		{"18-alpine", postgres18MountPath},
		{"18.1", postgres18MountPath},
		{"19", postgres18MountPath},
		// A tag with no leading major resolves to the current layout: mounting
		// the pre-18 path on current "latest"/"alpine" images is exactly the
		// failure this fixes.
		{"latest", postgres18MountPath},
		{"alpine", postgres18MountPath},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := engine.VolumeSpecFor(tt.version).MountPath; got != tt.want {
				t.Errorf("VolumeSpecFor(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}

	// The declarative default path stays the pre-18 one: only the versioned
	// resolver knows about the move.
	if got := engine.VolumeSpec().MountPath; got != postgresLegacyMountPath {
		t.Errorf("VolumeSpec() = %q, want %q", got, postgresLegacyMountPath)
	}
}

// TestBuildRunOptionsPostgres18Mount proves the versioned path reaches the run
// payload, not just the engine helper.
func TestBuildRunOptionsPostgres18Mount(t *testing.T) {
	database, secrets := sealedSample(t, "orders", EnginePostgres, 0)
	database.Version = "18-alpine"
	engine, _ := LookupEngine(EnginePostgres)

	options, err := buildRunOptions(database, engine, secrets, testSecret)
	if err != nil {
		t.Fatalf("buildRunOptions: %v", err)
	}
	if options.Image != "postgres:18-alpine" {
		t.Errorf("image = %q, want postgres:18-alpine", options.Image)
	}
	want := database.StoragePath + ":" + postgres18MountPath
	if len(options.Volumes) != 1 || options.Volumes[0] != want {
		t.Errorf("volumes = %v, want [%s]", options.Volumes, want)
	}
}
