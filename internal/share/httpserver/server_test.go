package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

func testConfig() *config.Config {
	return &config.Config{
		AppEnv:       config.EnvDevelopment,
		HTTP:         config.HTTPConfig{Addr: ":0"},
		CORS:         config.CORSConfig{AllowedOrigins: []string{"http://localhost:3000"}},
		RateLimit:    config.RateLimitConfig{RequestsPerSecond: 100, Burst: 100},
		MaxBodyBytes: 1 << 20,
		Log:          config.LogConfig{Level: "error"},
	}
}

func testRouter() http.Handler {
	return NewRouter(Dependencies{
		Config:  testConfig(),
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Version: func(context.Context) (int64, error) { return 0, nil },
	})
}

func TestLivenessEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNotFoundUsesStandardEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	testRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id header")
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND, got %q", body.Error.Code)
	}
}

func TestMethodNotAllowedUsesStandardEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
