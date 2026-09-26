package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/config"
)

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"error":   slog.LevelError,
		"":        slog.LevelInfo,
		"verbose": slog.LevelInfo,
	}

	for input, want := range tests {
		if got := ParseLevel(input); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNewLoggerJSONDefault(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: ParseLevel("info")})
	logger := slog.New(handler)

	logger.Info("hello", "key", "value")

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entry); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if entry["msg"] != "hello" || entry["key"] != "value" {
		t.Errorf("log entry = %v", entry)
	}
}

func TestNewLoggerWithLevelRespectsLevel(t *testing.T) {
	logger, level := NewLoggerWithLevel(config.Log{Level: "error", Format: config.FormatText})
	if level.Level() != slog.LevelError {
		t.Fatalf("level = %v, want error", level.Level())
	}
	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("logger enabled for info, want disabled at error level")
	}
	if !logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("logger disabled for error, want enabled")
	}
}

func TestNewLoggerWithLevelInvalidFallsBackToInfo(t *testing.T) {
	_, level := NewLoggerWithLevel(config.Log{Level: "nonsense", Format: config.FormatJSON})
	if level.Level() != slog.LevelInfo {
		t.Errorf("level = %v, want info fallback", level.Level())
	}
}
