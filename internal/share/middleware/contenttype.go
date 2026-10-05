package middleware

import (
	"mime"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// Media types the request pipeline accepts as a request body. JSON is what every
// body-carrying endpoint takes; multipart/form-data is what the upload endpoints
// take. Nothing else is a body this service accepts.
const (
	// MediaTypeJSON is the content type of a JSON request body.
	MediaTypeJSON = "application/json"
	// MediaTypeMultipart is the content type of a multipart/form-data upload.
	MediaTypeMultipart = "multipart/form-data"
)

// JSONContentType rejects request bodies that are not JSON with
// UNSUPPORTED_MEDIA_TYPE (415), per contracts/http-conventions.md. Requests
// without a body are unaffected.
//
// It is a per-route middleware rather than a pipeline-wide one, and it has to be:
// a multipart/form-data upload is not JSON, so enforcing this check on the whole
// mux answers 415 to every upload before the route that would handle it runs. A
// route that takes no body and a route that takes a body this service does not
// parse are both better served by a route that says what it expects.
func JSONContentType(next http.Handler) http.Handler {
	return AllowedContentTypes(MediaTypeJSON)(next)
}

// AllowedContentTypes rejects a request body whose media type is not one of the
// given types, with UNSUPPORTED_MEDIA_TYPE (415). Requests without a body are
// unaffected, so a GET or a bodyless POST is never refused for a header it did not
// send.
//
// This is the pipeline-level guard, applied where the whole request surface is
// known. It lists what this API accepts as a body rather than what one route
// accepts: the pipeline cannot know which of the mounted routes wants JSON, and a
// body of any other kind — form-encoded, XML, plain text — is a client mistake on
// every route in the service. The per-route JSONContentType above then narrows it
// further on the routes that actually decode JSON, so a multipart body sent to a
// JSON route is still a 415 there rather than a parse error.
//
// Parameters are ignored: `application/json; charset=utf-8` and a multipart body
// with its boundary are both the type they claim.
func AllowedContentTypes(mediaTypes ...string) func(http.Handler) http.Handler {
	accepted := make(map[string]bool, len(mediaTypes))
	for _, mediaType := range mediaTypes {
		accepted[mediaType] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil || r.ContentLength == 0 {
				next.ServeHTTP(w, r)
				return
			}
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !accepted[mediaType] {
				httpx.WriteError(w, r, httpx.New(httpx.CodeUnsupportedMedia), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
