package attractor

import (
	"log/slog"
	"os"
	"strings"
)

func newFactoryLogger() *slog.Logger {
	level := parseLogLevel(os.Getenv("FACTORY_LOG_LEVEL"))
	format := strings.ToLower(strings.TrimSpace(os.Getenv("FACTORY_LOG_FORMAT")))
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
