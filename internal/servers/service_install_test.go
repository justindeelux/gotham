package servers

import (
	"testing"

	"github.com/google/uuid"
)

func TestInstallLock(t *testing.T) {
	s := &ServerService{}
	id := uuid.New()

	if !s.lockInstall(id) {
		t.Fatal("first install lock was refused")
	}
	if s.lockInstall(id) {
		t.Fatal("second install lock was granted while one was running")
	}
	if !s.lockInstall(uuid.New()) {
		t.Fatal("lock for another server was refused")
	}

	s.unlockInstall(id)
	if !s.lockInstall(id) {
		t.Fatal("install lock was not released")
	}
}
