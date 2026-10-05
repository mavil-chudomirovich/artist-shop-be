package logging

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// RedactedValue is substituted for any attribute whose key is sensitive.
const RedactedValue = "[REDACTED]"

// sensitiveKeys is the redaction allowlist-by-denial. Matching is
// case-insensitive and by exact key name.
var sensitiveKeys = map[string]struct{}{
	"password":       {},
	"passwd":         {},
	"secret":         {},
	"jwt_secret":     {},
	"client_secret":  {},
	"redis_password": {},
	"db_password":    {},
	"smtp_password":  {},
	"admin_password": {},
	"token":          {},
	"access_token":   {},
	"refresh_token":  {},
	"reset_token":    {},
	"authorization":  {},
	"api_key":        {},
	"apikey":         {},
	"cookie":         {},
	"set-cookie":     {},
	"dsn":            {},
	"database_url":   {},
	"db_url":         {},
	"otp":            {},
	"body":           {},
}

// IsSensitiveKey reports whether the given attribute key must be redacted.
func IsSensitiveKey(key string) bool {
	_, ok := sensitiveKeys[strings.ToLower(key)]
	return ok
}

// WithCorrelation returns a logger enriched with the correlation ID found in
// ctx, or the original logger when none is present.
func WithCorrelation(ctx context.Context, logger *slog.Logger) *slog.Logger {
	id := reqctx.CorrelationID(ctx)
	if id == "" {
		return logger
	}
	return logger.With(slog.String("correlation_id", id))
}

func redactAttr(a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, RedactedValue)
	}
	switch a.Value.Kind() {
	case slog.KindGroup:
		group := a.Value.Group()
		redacted := make([]slog.Attr, len(group))
		for i, child := range group {
			redacted[i] = redactAttr(child)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	default:
		return a
	}
}

func redactRecord(r slog.Record) slog.Record {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return out
}

type redactingHandler struct {
	inner slog.Handler
}

func newRedactingHandler(inner slog.Handler) *redactingHandler {
	return &redactingHandler{inner: inner}
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, redactRecord(r))
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return newRedactingHandler(h.inner.WithAttrs(redacted))
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return newRedactingHandler(h.inner.WithGroup(name))
}
