package httpx_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

func contractRouter() http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Use(middleware.Correlation)
	r.Use(middleware.Recovery(logger))
	r.Get("/ok", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteSuccess(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.New(httpx.CodeNotFound), logger)
	})
	return r
}

func TestSuccessContract(t *testing.T) {
	rec := httptest.NewRecorder()
	contractRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	requestID := rec.Header().Get(middleware.CorrelationHeader)
	if requestID == "" {
		t.Fatal("expected X-Request-Id header")
	}

	var body struct {
		Data map[string]string `json:"data"`
		Meta struct {
			RequestID string `json:"requestId"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Data["status"] != "ok" || body.Meta.RequestID != requestID {
		t.Fatalf("contract mismatch: data=%v meta=%v header=%s", body.Data, body.Meta, requestID)
	}
}

func TestErrorContract(t *testing.T) {
	rec := httptest.NewRecorder()
	contractRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))

	requestID := rec.Header().Get(middleware.CorrelationHeader)
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" || body.Error.Message == "" {
		t.Fatalf("unexpected error body: %#v", body.Error)
	}
	if body.Error.RequestID != requestID {
		t.Fatalf("error requestId %q != header %q", body.Error.RequestID, requestID)
	}
}
