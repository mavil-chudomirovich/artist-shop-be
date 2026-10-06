package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func statusOf(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Code
}

func messageOf(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Message
}

func fieldOf(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error struct {
			Details []struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Error.Details) != 1 {
		return ""
	}
	return body.Error.Details[0].Field
}

func TestBodyLimitRejectsOversized(t *testing.T) {
	handler := BodyLimit(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.ContentLength = 100
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
	if statusOf(rec) != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("expected PAYLOAD_TOO_LARGE, got %q", statusOf(rec))
	}
}

// The shared ceiling is used by every route that has no size rule of its own, so
// its refusal has to stay exactly the generic one: the same code, the same status
// and the same catalogue message. This pins the default, because a default that
// drifts silently changes every route that never asked for a reason of its own
// (FR-003).
func TestBodyLimitDefaultRefusalStaysTheGenericRequestTooLarge(t *testing.T) {
	handler := BodyLimit(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/addresses", strings.NewReader(`{"a":1}`))
	req.ContentLength = 100
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
	if got := statusOf(rec); got != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("expected PAYLOAD_TOO_LARGE, got %q", got)
	}
	if got := messageOf(rec); got != "Request body is too large" {
		t.Fatalf("expected the catalogue message, got %q", got)
	}
}

// A route that knows what its ceiling is for supplies the refusal it wants, and
// the middleware reports it verbatim: code, status and the field detail that tells
// the client which input to change. Only the route knows the input, so the
// middleware never invents one (FR-001, FR-020).
func TestBodyLimitReportsTheCallersRefusal(t *testing.T) {
	refusal := &httpx.AppError{
		Code:    "USER_AVATAR_TOO_LARGE",
		Status:  http.StatusRequestEntityTooLarge,
		Message: "Image exceeds the size limit",
		Details: []httpx.Detail{{Field: "file", Issue: "compress the image before uploading"}},
	}
	called := false
	handler := BodyLimitWithRefusal(10, refusal)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", nil)
	req.ContentLength = 100
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
	if got := statusOf(rec); got != "USER_AVATAR_TOO_LARGE" {
		t.Fatalf("expected the caller's code, got %q", got)
	}
	if got := messageOf(rec); got != "Image exceeds the size limit" {
		t.Fatalf("expected the caller's message, got %q", got)
	}
	if got := fieldOf(rec); got != "file" {
		t.Fatalf("expected the caller's field detail, got %q", got)
	}
	if called {
		t.Fatal("a refused request must not reach the route")
	}
}

// Supplying a reason must not weaken the rule: the ceiling and the cap on the
// readable body are unchanged, so a body that declares nothing or lies about its
// size is still cut at the limit.
func TestBodyLimitWithRefusalKeepsTheCeilingAndTheCap(t *testing.T) {
	refusal := &httpx.AppError{
		Code:    "USER_AVATAR_TOO_LARGE",
		Status:  http.StatusRequestEntityTooLarge,
		Message: "Image exceeds the size limit",
	}
	var read int64
	handler := BodyLimitWithRefusal(10, refusal)(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			n, err := io.Copy(io.Discard, r.Body)
			if err == nil {
				read = n
			}
		}))

	for name, declared := range map[string]int64{"undeclared": -1, "understated": 5} {
		t.Run(name, func(t *testing.T) {
			read = 0
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 100)))
			req.ContentLength = declared
			handler.ServeHTTP(httptest.NewRecorder(), req)
			if read > 10 {
				t.Fatalf("expected the body to be capped at 10 bytes, read %d", read)
			}
		})
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small"))
	req.ContentLength = 5
	handler.ServeHTTP(rec, req)
	if read != 5 {
		t.Fatalf("a body within the ceiling must be readable in full, read %d", read)
	}
}

func TestBodyLimitAllowsWithinLimit(t *testing.T) {
	called := false
	handler := BodyLimit(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.ContentLength = 5
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestRateLimitRejectsAfterBurst(t *testing.T) {
	handler := RateLimit(1, 1, discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first request allowed, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRecoveryConvertsPanic(t *testing.T) {
	handler := Recovery(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if statusOf(rec) != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", statusOf(rec))
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	handler := CORS([]string{"http://localhost:3000"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("expected allow-origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRateLimitKeysByRouteClass(t *testing.T) {
	handler := RateLimit(1, 1, discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	read := httptest.NewRecorder()
	handler.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))
	if read.Code != http.StatusOK {
		t.Fatalf("expected read allowed, got %d", read.Code)
	}

	write := httptest.NewRecorder()
	handler.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/api/v1/items", nil))
	if write.Code != http.StatusOK {
		t.Fatalf("expected write class to have its own bucket, got %d", write.Code)
	}

	readAgain := httptest.NewRecorder()
	handler.ServeHTTP(readAgain, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))
	if readAgain.Code != http.StatusTooManyRequests {
		t.Fatalf("expected read bucket exhausted, got %d", readAgain.Code)
	}
}

func TestRequireAuthenticationRejectsAnonymous(t *testing.T) {
	hooks := AuthHooks{Authenticate: func(context.Context, *http.Request) (*Identity, error) { return nil, nil }}
	handler := RequireAuthentication(hooks)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireRoleEnforcesRole(t *testing.T) {
	const role = "ADMIN"

	admin := AuthHooks{Authenticate: func(context.Context, *http.Request) (*Identity, error) {
		return &Identity{Subject: "a", Role: role}, nil
	}}
	customer := AuthHooks{Authenticate: func(context.Context, *http.Request) (*Identity, error) {
		return &Identity{Subject: "c", Role: "CUSTOMER"}, nil
	}}

	adminRec := httptest.NewRecorder()
	RequireRole(admin, role)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(adminRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin allowed, got %d", adminRec.Code)
	}

	custRec := httptest.NewRecorder()
	RequireRole(customer, role)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(custRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if custRec.Code != http.StatusForbidden {
		t.Fatalf("expected customer forbidden, got %d", custRec.Code)
	}
}

func TestRequireRoleNotifiesOnDenial(t *testing.T) {
	var denied []Identity
	hooks := AuthHooks{
		Authenticate: func(context.Context, *http.Request) (*Identity, error) {
			return &Identity{Subject: "c", Role: "CUSTOMER"}, nil
		},
		OnDenied: func(_ context.Context, id Identity, _ *http.Request) {
			denied = append(denied, id)
		},
	}

	rec := httptest.NewRecorder()
	RequireRole(hooks, "ADMIN")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if len(denied) != 1 || denied[0].Role != "CUSTOMER" {
		t.Fatalf("expected the denied identity to be reported once, got %+v", denied)
	}
}

func TestRequireAuthenticationKeepsModuleErrorCodes(t *testing.T) {
	hooks := AuthHooks{Authenticate: func(context.Context, *http.Request) (*Identity, error) {
		return nil, httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: "token", Issue: "expired"})
	}}

	rec := httptest.NewRecorder()
	RequireAuthentication(hooks)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected the module code to be preserved (400), got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("expected VALIDATION_ERROR in the body, got %s", rec.Body.String())
	}
}

func TestRequestLoggerEmitsCorrelationAndStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New("info", &buf)

	handler := Correlation(RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))

	out := buf.String()
	if !strings.Contains(out, "request handled") {
		t.Fatalf("expected request log entry, got %s", out)
	}
	if !strings.Contains(out, "\"status\":204") {
		t.Fatalf("expected status in log, got %s", out)
	}
	if id := rec.Header().Get(CorrelationHeader); id == "" || !strings.Contains(out, id) {
		t.Fatalf("expected correlation id %q in logs, got %s", id, out)
	}
}

func TestJSONContentTypeRejectsNonJSON(t *testing.T) {
	handler := JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
	if statusOf(rec) != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("expected UNSUPPORTED_MEDIA_TYPE, got %q", statusOf(rec))
	}
}

func TestJSONContentTypeAllowsJSON(t *testing.T) {
	handler := JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestJSONContentTypeAllowsBodylessRequests(t *testing.T) {
	handler := JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// The pipeline-level guard exists because a global JSON-only check answered 415 to
// every multipart upload, so an upload route could never be reached.
func TestAllowedContentTypesAdmitsMultipartAndStillRefusesTheRest(t *testing.T) {
	called := 0
	handler := AllowedContentTypes(MediaTypeJSON, MediaTypeMultipart)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}))

	accepted := map[string]string{
		"json":                "application/json",
		"json with a charset": "application/json; charset=utf-8",
		"multipart":           `multipart/form-data; boundary=abc`,
	}
	for name, contentType := range accepted {
		t.Run(name, func(t *testing.T) {
			called = 0
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
			req.Header.Set("Content-Type", contentType)
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %q, got %d", contentType, rec.Code)
			}
			if called != 1 {
				t.Fatalf("expected the handler to run for %q", contentType)
			}
		})
	}

	refused := map[string]string{
		"text":       "text/plain",
		"xml":        "application/xml",
		"form":       "application/x-www-form-urlencoded",
		"unparsable": "not a media type at all",
	}
	for name, contentType := range refused {
		t.Run(name, func(t *testing.T) {
			called = 0
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
			req.Header.Set("Content-Type", contentType)
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("expected 415 for %q, got %d", contentType, rec.Code)
			}
			if statusOf(rec) != "UNSUPPORTED_MEDIA_TYPE" {
				t.Fatalf("expected UNSUPPORTED_MEDIA_TYPE, got %q", statusOf(rec))
			}
			if called != 0 {
				t.Fatalf("the handler must not run for %q", contentType)
			}
		})
	}
}

// The per-route check is JSON-only on purpose: a multipart body sent to a route
// that decodes JSON is a client mistake, not an upload.
func TestJSONContentTypeStillRefusesMultipart(t *testing.T) {
	called := false
	handler := JSONContentType(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
	req.Header.Set("Content-Type", `multipart/form-data; boundary=abc`)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
	if called {
		t.Fatal("a JSON route must not run for a multipart body")
	}
}

func TestCORSRejectsUnconfiguredOrigin(t *testing.T) {
	handler := CORS([]string{"http://localhost:3000"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow-origin header, got %q", got)
	}
}
