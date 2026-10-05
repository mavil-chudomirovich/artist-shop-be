// Package health exposes liveness and readiness signals.
package health

import (
	"context"
	"encoding/json"
	"net/http"
)

// Checker combines readiness state and connectivity.
type Checker interface {
	Ready() bool
	Ping(ctx context.Context) error
}

// VersionFunc reports the current schema version.
type VersionFunc func(ctx context.Context) (int64, error)

// Pinger reports connectivity to a required dependency (e.g. Redis).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler serves the operational endpoints.
type Handler struct {
	db      Checker
	version VersionFunc
	cache   Pinger
}

// New creates a health handler. version may be nil when schema checks are not
// available; readiness then relies on the database check alone.
func New(db Checker, version VersionFunc) *Handler {
	return &Handler{db: db, version: version}
}

// WithCache registers a required cache dependency for the readiness check.
func (h *Handler) WithCache(cache Pinger) *Handler {
	h.cache = cache
	return h
}

type readinessBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Liveness always reports alive while the process is running (FR-013).
func (h *Handler) Liveness(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, map[string]string{"status": "alive"})
}

// Readiness reports ready only when the database and schema are usable (FR-013).
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"database": "ok", "migrations": "ok"}
	if h.cache != nil {
		checks["cache"] = "ok"
	}

	if !h.databaseReady(r.Context()) {
		checks["database"] = "failed"
		checks["migrations"] = "failed"
		write(w, http.StatusServiceUnavailable, readinessBody{Status: "not_ready", Checks: checks})
		return
	}

	if !h.schemaReady(r.Context()) {
		checks["migrations"] = "failed"
		write(w, http.StatusServiceUnavailable, readinessBody{Status: "not_ready", Checks: checks})
		return
	}

	if h.cache != nil && h.cache.Ping(r.Context()) != nil {
		checks["cache"] = "failed"
		write(w, http.StatusServiceUnavailable, readinessBody{Status: "not_ready", Checks: checks})
		return
	}

	write(w, http.StatusOK, readinessBody{Status: "ready", Checks: checks})
}

func (h *Handler) databaseReady(ctx context.Context) bool {
	return h.db != nil && h.db.Ready() && h.db.Ping(ctx) == nil
}

func (h *Handler) schemaReady(ctx context.Context) bool {
	if h.version == nil {
		return true
	}
	version, err := h.version(ctx)
	return err == nil && version > 0
}

func write(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
