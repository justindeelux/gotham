package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDockerConfigUsesStateDirWhenHomeHidden(t *testing.T) {
	stateDir := t.TempDir()
	hiddenHome := filepath.Join(t.TempDir(), "missing")
	t.Setenv("HOME", hiddenHome)
	t.Setenv("DOCKER_CONFIG", "")

	dir, err := defaultDockerConfig(stateDir)
	if err != nil {
		t.Fatalf("defaultDockerConfig: %v", err)
	}
	want := filepath.Join(stateDir, ".docker")
	if dir != want || os.Getenv("DOCKER_CONFIG") != want {
		t.Fatalf("DOCKER_CONFIG = %q (returned %q); want %q", os.Getenv("DOCKER_CONFIG"), dir, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat %s: %v", want, err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("mode = %o; want 700", got)
	}
}

func TestDefaultDockerConfigKeepsReadableHome(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".docker"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_CONFIG", "")

	dir, err := defaultDockerConfig(stateDir)
	if err != nil {
		t.Fatalf("defaultDockerConfig: %v", err)
	}
	if dir != "" || os.Getenv("DOCKER_CONFIG") != "" {
		t.Fatalf("DOCKER_CONFIG = %q (returned %q); want it untouched", os.Getenv("DOCKER_CONFIG"), dir)
	}
	if _, err := os.Stat(filepath.Join(stateDir, ".docker")); !os.IsNotExist(err) {
		t.Fatalf("state dir .docker created although ~/.docker is readable: %v", err)
	}
}

func TestDefaultDockerConfigHonoursExplicitValue(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("DOCKER_CONFIG", "/tmp/explicit-docker-config")

	dir, err := defaultDockerConfig(stateDir)
	if err != nil {
		t.Fatalf("defaultDockerConfig: %v", err)
	}
	if dir != "" || os.Getenv("DOCKER_CONFIG") != "/tmp/explicit-docker-config" {
		t.Fatalf("explicit DOCKER_CONFIG was overwritten: returned %q, env %q", dir, os.Getenv("DOCKER_CONFIG"))
	}
}
