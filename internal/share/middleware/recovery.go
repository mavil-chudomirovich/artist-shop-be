package middleware

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// Recovery converts panics into the standard internal error without leaking
// details (FR-008, FR-018).
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					logger.ErrorContext(r.Context(), "recovered panic", slog.Any("panic", p))
					httpx.WriteError(w, r, httpx.Wrap(fmt.Errorf("panic: %v", p), httpx.CodeInternal), logger)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
