package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

func withCorrelation(r *http.Request, id string) *http.Request {
	return r.WithContext(reqctx.WithCorrelation(r.Context(), id))
}

func TestWriteSuccessEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/", nil), "corr-1")

	WriteSuccess(rec, req, http.StatusOK, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Data map[string]string `json:"data"`
		Meta struct {
			RequestID string `json:"requestId"`
			Timestamp string `json:"timestamp"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Data["hello"] != "world" {
		t.Fatalf("unexpected data: %#v", body.Data)
	}
	if body.Meta.RequestID != "corr-1" || body.Meta.Timestamp == "" {
		t.Fatalf("missing meta: %#v", body.Meta)
	}
}

func TestWriteSuccessNullData(t *testing.T) {
	rec := httptest.NewRecorder()
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/", nil), "corr-1")

	WriteSuccess(rec, req, http.StatusNoContent, nil)

	if got := rec.Body.String(); got == "" {
		t.Fatal("expected body to include explicit data field")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if string(raw["data"]) != "null" {
		t.Fatalf("expected data:null, got %s", raw["data"])
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/", nil), "corr-2")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	WriteError(rec, req, New(CodeNotFound), logger)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error.Code != string(CodeNotFound) || body.Error.RequestID != "corr-2" {
		t.Fatalf("unexpected error body: %#v", body.Error)
	}
}
