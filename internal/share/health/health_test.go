package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeChecker struct {
	ready bool
	ping  error
}

func (f fakeChecker) Ready() bool                { return f.ready }
func (f fakeChecker) Ping(context.Context) error { return f.ping }

func okVersion(context.Context) (int64, error)  { return 1, nil }
func badVersion(context.Context) (int64, error) { return 0, errors.New("no schema") }

func TestLivenessAlwaysAlive(t *testing.T) {
	rec := httptest.NewRecorder()
	New(fakeChecker{}, nil).Liveness(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestReadinessReady(t *testing.T) {
	rec := httptest.NewRecorder()
	New(fakeChecker{ready: true}, okVersion).Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body readinessBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Status != "ready" || body.Checks["database"] != "ok" || body.Checks["migrations"] != "ok" {
		t.Fatalf("unexpected readiness: %#v", body)
	}
}

func TestReadinessNotReadyWhenNotMarked(t *testing.T) {
	rec := httptest.NewRecorder()
	New(fakeChecker{ready: false}, okVersion).Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestReadinessNotReadyWhenPingFails(t *testing.T) {
	rec := httptest.NewRecorder()
	New(fakeChecker{ready: true, ping: errors.New("down")}, okVersion).Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestReadinessNotReadyWhenSchemaMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	New(fakeChecker{ready: true}, badVersion).Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var body readinessBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Checks["database"] != "ok" || body.Checks["migrations"] != "failed" {
		t.Fatalf("expected database ok and migrations failed, got %#v", body.Checks)
	}
}
