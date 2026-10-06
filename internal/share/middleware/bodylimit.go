package middleware

import (
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// BodyLimit rejects requests whose declared size exceeds limit and caps the
// readable body (FR-019).
//
// The refusal is the generic one. This middleware serves routes that have no size
// rule of their own, so it has no reason to prefer one limit over another and
// PAYLOAD_TOO_LARGE is the honest answer. A route that does know what its ceiling
// is for calls BodyLimitWithRefusal instead (FR-003).
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return BodyLimitWithRefusal(limit, httpx.New(httpx.CodePayloadTooLarge))
}

// BodyLimitWithRefusal is BodyLimit for a route that knows which input the
// ceiling is about. The route supplies the whole refusal — code, status, message
// and any field detail — because only it knows what the client has to change; the
// middleware still owns the size rule and still refuses before the body is read.
// The avatar upload route passes the very refusal its handler reports when its own
// read ceiling is reached, so a declared length, a chunked body and an understated
// length all answer identically (FR-001, FR-002, research D1).
//
// The refusal is built once, at wiring time, and never mutated afterwards, so one
// value serves every request.
func BodyLimitWithRefusal(limit int64, refusal *httpx.AppError) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				httpx.WriteError(w, r, refusal, nil)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
