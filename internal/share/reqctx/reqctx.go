// Package reqctx carries per-request values (notably the correlation ID)
// through context.Context without coupling callers to a transport package.
package reqctx

import "context"

type ctxKey int

const correlationKey ctxKey = iota

// WithCorrelation returns a context carrying the given correlation ID.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID returns the correlation ID from the context, or "" if absent.
func CorrelationID(ctx context.Context) string {
	if v, ok := ctx.Value(correlationKey).(string); ok {
		return v
	}
	return ""
}
