package middleware

import (
	"mime"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// JSONContentType rejects request bodies that are not JSON with
// UNSUPPORTED_MEDIA_TYPE (415), per contracts/http-conventions.md. Requests
// without a body are unaffected.
func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			httpx.WriteError(w, r, httpx.New(httpx.CodeUnsupportedMedia), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
