package middleware

import (
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// BodyLimit rejects requests whose declared size exceeds limit and caps the
// readable body (FR-019).
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				httpx.WriteError(w, r, httpx.New(httpx.CodePayloadTooLarge), nil)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
