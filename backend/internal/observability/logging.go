// Package observability is how a process says how it is doing: logs, metrics
// on their own port, and traces when a collector is configured.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns the process logger: JSON in production so it can be
// ingested, and human readable text elsewhere.
func NewLogger(level string, production bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if production {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
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
