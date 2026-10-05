// Package middleware provides the shared request pipeline: correlation,
// recovery, body limits, CORS, rate limiting, and authentication hooks.
package middleware

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// CorrelationHeader is the header carrying the request correlation ID.
const CorrelationHeader = "X-Request-Id"

// Correlation assigns or propagates a correlation ID for every request and
// echoes it in the response header (FR-009).
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(CorrelationHeader)
		if !isUUID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(CorrelationHeader, id)
		next.ServeHTTP(w, r.WithContext(reqctx.WithCorrelation(r.Context(), id)))
	})
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}
