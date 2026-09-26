package server

import (
	"log/slog"
	"os"
	"strings"

	"github.com/justindeelux/gotham/internal/config"
)

// ParseLevel converts a textual log level into a slog.Level using slog's own
// UnmarshalText rules (case-insensitive: DEBUG, INFO, WARN, ERROR) and falls
// back to Info for empty or unknown values.
func ParseLevel(level string) slog.Level {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return slog.LevelInfo
	}
	return parsed
}

// NewLogger builds a slog.Logger from the configured level and format. It is a
// convenience wrapper around NewLoggerWithLevel for callers that do not need
// runtime level changes.
func NewLogger(cfg config.Log) *slog.Logger {
	logger, _ := NewLoggerWithLevel(cfg)
	return logger
}

// NewLoggerWithLevel builds a slog.Logger and returns the LevelVar backing it,
// so a hot reload can change the level at runtime. JSON is the default handler;
// any format other than "text" falls back to JSON.
func NewLoggerWithLevel(cfg config.Log) (*slog.Logger, *slog.LevelVar) {
	level := new(slog.LevelVar)
	level.Set(ParseLevel(cfg.Level))

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(cfg.Format), config.FormatText) {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler), level
}
