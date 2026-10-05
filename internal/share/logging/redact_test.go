package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

func TestRedactsSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("login attempt", "email", "user@example.com", "password", "hunter2", "token", "abc.def")

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked into logs: %s", out)
	}
	if strings.Contains(out, "abc.def") {
		t.Fatalf("token leaked into logs: %s", out)
	}
	if !strings.Contains(out, RedactedValue) {
		t.Fatalf("expected redaction marker, got %s", out)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Fatalf("non-sensitive attribute should be preserved, got %s", out)
	}
}

func TestRedactsNestedGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("nested", slog.Group("db", slog.String("database_url", "postgres://u:p@h/db"), slog.String("host", "h")))

	out := buf.String()
	if strings.Contains(out, "postgres://u:p@h/db") {
		t.Fatalf("nested secret leaked: %s", out)
	}
	if !strings.Contains(out, "\"h\"") {
		t.Fatalf("non-sensitive nested value lost: %s", out)
	}
}

func TestWithCorrelation(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	ctx := reqctx.WithCorrelation(context.Background(), "corr-123")
	WithCorrelation(ctx, logger).Info("hello")

	if !strings.Contains(buf.String(), "corr-123") {
		t.Fatalf("expected correlation id in logs, got %s", buf.String())
	}
}

func TestJSONStructured(t *testing.T) {
	var buf bytes.Buffer
	New("info", &buf).Info("structured", "key", "value")

	if !strings.Contains(buf.String(), "\"msg\"") || !strings.Contains(buf.String(), "\"key\":\"value\"") {
		t.Fatalf("expected JSON structure, got %s", buf.String())
	}
}
