package runtimewatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSummarizeExposesDesiredState(t *testing.T) {
	settings := DefaultSettings()
	settings.DesiredState = "stopped"
	got := summarize(settings, nil, uuid.Nil, time.Now())
	if got.DesiredState != "stopped" {
		t.Fatalf("DesiredState = %q, want stopped", got.DesiredState)
	}
}
