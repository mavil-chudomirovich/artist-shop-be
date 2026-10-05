// Package logging provides structured JSON logging with secret redaction and
// correlation-ID injection.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New builds a structured JSON logger at the given level, wrapped so that
// sensitive attributes are redacted (FR-010, FR-011).
func New(level string, w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(newRedactingHandler(handler))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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
