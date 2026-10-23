package databases

import (
	"testing"
)

func TestPostgresVolumeSpecForVersion(t *testing.T) {
	engine := NewPostgresEngine()
	tests := []struct {
		version string
		want    string
	}{
		{"", postgresLegacyMountPath},
		{"16", postgresLegacyMountPath},
		{"16-alpine", postgresLegacyMountPath},
		{"16.4-alpine", postgresLegacyMountPath},
		{"17", postgresLegacyMountPath},
		{"18", postgres18MountPath},
		{"18-alpine", postgres18MountPath},
		{"18.1", postgres18MountPath},
		{"19", postgres18MountPath},
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

	if got := engine.VolumeSpec().MountPath; got != postgresLegacyMountPath {
		t.Errorf("VolumeSpec() = %q, want %q", got, postgresLegacyMountPath)
	}
}

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
