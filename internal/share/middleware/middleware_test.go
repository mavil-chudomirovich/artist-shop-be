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
