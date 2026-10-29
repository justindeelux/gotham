package databases

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

func testDatabase(engine, version string) Database {
	id := uuid.New()
	now := time.Now().UTC()
	return Database{
		ID:          id,
		UserID:      uuid.New(),
		ServerID:    uuid.New(),
		Name:        "test-" + engine,
		Engine:      engine,
		Version:     version,
		Status:      StatusRunning,
		ContainerID: "db-container",
		StoragePath: VolumeName(id),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func testCredentials() Credentials {
	return Credentials{
		Username:     "gotham_app",
		Password:     "s3cret-password",
		Database:     "gotham_app",
		RootPassword: "root-password",
	}
}

func containsEnv(env []string, prefix string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}
