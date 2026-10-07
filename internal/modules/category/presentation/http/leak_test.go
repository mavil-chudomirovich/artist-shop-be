package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// The two folding keys exist only to be indexed and compared. The adapter derives
// them on read and on write, the mapper omits them, and the audit metadata is
// built from the operator's own name and slug. This file proves the three
// surfaces that could carry them — the rendered response body, the captured audit
// metadata and a log line — because a struct-level assertion cannot catch a JSON
// tag added later.

// leakPublicPath is the public group's mount point, repeated here because the
// integration fixture that names it carries the integration build tag and this
// test must run under the ordinary unit gate too.
const leakPublicPath = "/api/v1/categories"

// leakAuditor captures the metadata map the use case hands the audit port, so a
// test can assert what an audit entry would actually carry.
type leakAuditor struct {
	mu       sync.Mutex
	metadata []map[string]any
}

func (a *leakAuditor) Record(_ context.Context, _, _ string, _ *uuid.UUID, _, _, _ string, metadata map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.metadata = append(a.metadata, metadata)
}

var _ appinterface.Auditor = (*leakAuditor)(nil)

func (a *leakAuditor) entries() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]map[string]any, len(a.metadata))
	copy(out, a.metadata)
	return out
}

// failingRepo makes one write fail the way a storage outage would, so the handler
// takes its only logging branch (INTERNAL_ERROR) while the request body carried a
// category name. That is the log line to prove carries no folded value.
type failingRepo struct {
	*collisionRepo
}

func (r *failingRepo) Create(context.Context, *model.Category) error {
	return errors.New("storage is unavailable")
}

// assertNoFoldingKey asserts a rendered body, serialized metadata or log line
// carries neither the folding-key member names nor the folded value itself.
func assertNoFoldingKey(t *testing.T, what, got, folded string) {
	t.Helper()
	for _, member := range []string{"normalizedName", "normalizedSlug"} {
		if strings.Contains(got, member) {
			t.Fatalf("%s carries the folding-key member %q: %s", what, member, got)
		}
	}
	if strings.Contains(got, folded) {
		t.Fatalf("%s carries the folded value %q: %s", what, folded, got)
	}
}

// FR-004, FR-007: the folding keys appear in no response and no audit entry. The
// fixture folds to a different string from the operator's name, so an absence
// assertion is meaningful rather than vacuous, and every body is also checked to
// still carry the name, so a response that had swallowed everything would fail
// instead of passing silently.
func TestTheFoldingKeysNeverReachAResponseOrAnAuditEntry(t *testing.T) {
	const (
		name = "Điêu khắc"
		slug = "diau-khac"
	)
	foldedName := model.FoldKey(name)
	if foldedName == name {
		t.Fatalf("the fixture must fold to a different string, both are %q", name)
	}

	repo := newCollisionRepo()
	auditor := &leakAuditor{}
	service := implement.New(implement.Service{Categories: repo, Audit: auditor, Mapper: mapper.New()})
	handler := New(service, testLogger)

	router := chi.NewRouter()
	router.Mount(leakPublicPath, handler.Router(middleware.AuthHooks{}))
	router.Mount(adminCategoriesPath, handler.AdminRouter(maintenanceHooks(nil)))

	created := performJSON(router, http.MethodPost, adminCategoriesPath,
		`{"name":"`+name+`","slug":"`+slug+`","description":"một mô tả","position":1}`, "admin-token")
	if created.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", created.Code, created.Body.String())
	}
	id := decodeAdminCategory(t, created).ID

	rendered := map[string]string{
		"the create answer":        created.Body.String(),
		"the administrator list":   performJSON(router, http.MethodGet, adminCategoriesPath, "", "admin-token").Body.String(),
		"the administrator detail": performJSON(router, http.MethodGet, adminCategoriesPath+"/"+id.String(), "", "admin-token").Body.String(),
		"the public list":          perform(router, http.MethodGet, leakPublicPath, "").Body.String(),
		"the public detail":        perform(router, http.MethodGet, leakPublicPath+"/"+slug, "").Body.String(),
	}
	for what, body := range rendered {
		assertNoFoldingKey(t, what, body, foldedName)
		if !strings.Contains(body, name) {
			t.Fatalf("%s must still carry the operator's name, got %s", what, body)
		}
	}

	// The captured audit metadata is what an audit entry would persist. It may
	// carry the operator's name and slug; it must carry nothing derived from them.
	entries := auditor.entries()
	if len(entries) != 1 {
		t.Fatalf("expected exactly one audit entry from the create, got %d", len(entries))
	}
	metadata := entries[0]
	serialized, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("serialize the captured audit metadata: %v", err)
	}
	assertNoFoldingKey(t, "the audit entry", string(serialized), foldedName)
	if len(metadata) != 2 || metadata["name"] != name || metadata["slug"] != slug {
		t.Fatalf("the audit entry must carry only the operator's name and slug, got %v", metadata)
	}
}

// The module's HTTP layer logs on no success path; its one log line is the
// INTERNAL_ERROR branch. This drives that branch with the name in the request
// body and proves the line carries no category value — the folded key included.
func TestAStorageFailureLogsNoCategoryValue(t *testing.T) {
	name := "Điêu khắc"
	foldedName := model.FoldKey(name)
	if foldedName == name {
		t.Fatalf("the fixture must fold to a different string, both are %q", name)
	}

	service := implement.New(implement.Service{
		Categories: &failingRepo{collisionRepo: newCollisionRepo()},
		Audit:      &leakAuditor{},
		Mapper:     mapper.New(),
	})
	logs := &bytes.Buffer{}
	handler := New(service, slog.New(slog.NewJSONHandler(logs, nil)))

	router := chi.NewRouter()
	router.Mount(adminCategoriesPath, handler.AdminRouter(maintenanceHooks(nil)))

	rec := performJSON(router, http.MethodPost, adminCategoriesPath,
		`{"name":"`+name+`","slug":"diau-khac"}`, "admin-token")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected the storage failure to be 500, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "request failed") {
		t.Fatalf("expected the internal-error log line, got %q", logs.String())
	}
	assertNoFoldingKey(t, "the log line", logs.String(), foldedName)
}
