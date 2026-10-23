package server

import (
	"log/slog"
	"os"
	"strings"

	"github.com/justindeelux/gotham/internal/config"
)

func ParseLevel(level string) slog.Level {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return slog.LevelInfo
	}
	return parsed
}

func NewLogger(cfg config.Log) *slog.Logger {
	logger, _ := NewLoggerWithLevel(cfg)
	return logger
}

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
