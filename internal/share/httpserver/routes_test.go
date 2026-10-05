package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file covers the request pipeline's body handling, which is a foundation
// concern every mounted module inherits.
//
// The two failures it guards against were found in the avatar phase and both are
// invisible from a module's own tests, because a module's router is built without
// this pipeline:
//
//   - a global JSON-only content-type check answered 415 to every multipart
//     upload, so an upload route could never be reached;
//   - a global MaxBytesReader wrapped the body before routing, so a route that
//     declares a larger limit for its own handler could never lift it.

// avatarImageCeiling mirrors the 2 MB ceiling of FR-014, and uploadOverhead the
// room a router leaves for the multipart envelope. They are repeated here rather
// than imported because the pipeline has to be exercised against the shape a
// module builds, not against the module's own configuration.
const (
	avatarImageCeiling = int64(2 << 20)
	uploadOverhead     = int64(64 << 10)
	// pipelineCeiling is the value MAX_BODY_BYTES ships as. The config package's own
	// test pins the default; what matters here is that this ceiling is above the
	// avatar route's, or a legitimate upload could never fit.
	pipelineCeiling = int64(4 << 20)
)

// mountedRouter is a stand-in for a module router, built the way a module builds
// one: a JSON route carrying the JSON-only content-type check, and an upload route
// carrying a body limit above the image ceiling.
func mountedRouter(seen *[]string) http.Handler {
	r := chi.NewRouter()
	r.With(middleware.JSONContentType).Patch("/things", func(w http.ResponseWriter, _ *http.Request) {
		*seen = append(*seen, "json route")
		w.WriteHeader(http.StatusOK)
	})
	r.With(middleware.BodyLimit(avatarImageCeiling+uploadOverhead)).
		Post("/things/photo", func(w http.ResponseWriter, req *http.Request) {
			*seen = append(*seen, "upload route")
			// Read the part the way a real upload route does: the body has to be
			// readable, not merely routed.
			if _, err := io.ReadAll(io.LimitReader(req.Body, avatarImageCeiling)); err != nil {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	return r
}

func pipeline(maxBodyBytes int64, seen *[]string) http.Handler {
	cfg := testConfig()
	cfg.MaxBodyBytes = maxBodyBytes
	return NewRouter(Dependencies{
		Config:  cfg,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Version: func(context.Context) (int64, error) { return 0, nil },
		// Mount is handed the router that already sits under /api/v1, exactly as a
		// composition root registers module groups.
		Mount: func(r chi.Router) { r.Mount("/demo", mountedRouter(seen)) },
	})
}

// multipartBody builds a multipart/form-data request with one file part.
func multipartBody(content []byte) (string, []byte) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(content); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return writer.FormDataContentType(), body.Bytes()
}

// A multipart upload must reach the route that handles it. Before the content-type
// check was scoped to the routes that expect JSON, this request was answered 415 by
// the pipeline and no upload could ever be stored.
func TestAMultipartUploadReachesItsRoute(t *testing.T) {
	var seen []string
	contentType, body := multipartBody(bytes.Repeat([]byte("a"), 64))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/things/photo", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	pipeline(pipelineCeiling, &seen).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(seen) != 1 || seen[0] != "upload route" {
		t.Fatalf("expected the upload route to have run, got %v", seen)
	}
}

// A legitimate 2 MB photo must fit the pipeline at the shipped default, which is
// what raising the global ceiling was for. This is the request that used to be
// refused by the 1 MiB default before the avatar route could apply its own rule.
func TestALegitimateAvatarSizedUploadFitsTheShippedDefault(t *testing.T) {
	var seen []string
	contentType, body := multipartBody(make([]byte, avatarImageCeiling))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/things/photo", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	pipeline(pipelineCeiling, &seen).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a %d byte upload must fit the default ceiling of %d, got %d (%s)",
			avatarImageCeiling, pipelineCeiling, rec.Code, rec.Body.String())
	}
	if len(seen) != 1 || seen[0] != "upload route" {
		t.Fatalf("expected the upload route to have run, got %v", seen)
	}
}

// The global ceiling still refuses an oversized body early, before it is buffered
// whole and before any route runs. Scoping the content-type check must not have
// turned the pipeline into something that accepts any size.
func TestTheGlobalCeilingStillRefusesAnOversizedBody(t *testing.T) {
	var seen []string
	contentType, body := multipartBody(bytes.Repeat([]byte("a"), 4096))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/things/photo", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	pipeline(1024, &seen).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("expected PAYLOAD_TOO_LARGE, got %s", got)
	}
	if len(seen) != 0 {
		t.Fatalf("no route may run for an oversized body, got %v", seen)
	}
}

// A JSON route is still refused with 415 when it is sent another media type. The
// check moved to the routes that expect JSON; it did not disappear.
func TestAJSONRouteIsStillRefusedAnotherMediaType(t *testing.T) {
	for _, contentType := range []string{"text/plain", "application/xml", "application/x-www-form-urlencoded"} {
		t.Run(contentType, func(t *testing.T) {
			var seen []string
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/demo/things", strings.NewReader(`{"a":1}`))
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()

			pipeline(pipelineCeiling, &seen).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("expected 415, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != "UNSUPPORTED_MEDIA_TYPE" {
				t.Fatalf("expected UNSUPPORTED_MEDIA_TYPE, got %s", got)
			}
			if len(seen) != 0 {
				t.Fatalf("a JSON route must not run for the wrong media type, got %v", seen)
			}
		})
	}
}

// The other half of the same rule: a multipart body sent to a route that decodes
// JSON is a client mistake, not an upload, so it is a 415 there too â€” even though
// the pipeline itself admits multipart for the upload routes.
func TestAMultipartBodyOnAJSONRouteIsRefused(t *testing.T) {
	var seen []string
	contentType, body := multipartBody([]byte("payload"))
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/demo/things", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	pipeline(pipelineCeiling, &seen).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(seen) != 0 {
		t.Fatalf("a JSON route must not run for a multipart body, got %v", seen)
	}
}

// A JSON body still reaches its route, with and without the charset parameter.
func TestAJSONBodyStillReachesItsRoute(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		t.Run(contentType, func(t *testing.T) {
			var seen []string
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/demo/things", strings.NewReader(`{"a":1}`))
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()

			pipeline(pipelineCeiling, &seen).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
			}
			if len(seen) != 1 || seen[0] != "json route" {
				t.Fatalf("expected the JSON route to have run, got %v", seen)
			}
		})
	}
}

// A bodyless request is unaffected by the content-type check, so a POST that sends
// no body is never refused for a header it did not send.
func TestABodylessRequestIsUnaffected(t *testing.T) {
	var seen []string
	rec := httptest.NewRecorder()

	pipeline(pipelineCeiling, &seen).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/api/v1/demo/things/photo", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error body %q: %v", rec.Body.String(), err)
	}
	return body.Error.Code
}
