package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
)

// This file is the module's leak contract: what an administrator's response and
// the module's audit entries must NOT carry. The stock response is the three
// quantities and nothing else — no internal hold row, no ledger row, no actor —
// and no audit entry carries a customer's personal data (FR-006, Constitution V
// and VI). The product identifier is the resource address in the path, so the
// body does not repeat it and must not invent a second copy.
//
// It lives at the transport layer on purpose: these are the shapes a client
// receives and the metadata the audit writer persists, so the assertion is made
// on the serialised answer rather than on an intermediate struct.

// leakEvent is one audit entry captured for inspection.
type leakEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	role       string
	targetType string
	targetID   string
	meta       map[string]any
}

// leakAuditor records every audit entry the use cases emit, copying the metadata
// so a later mutation cannot hide a leak behind a shared map.
type leakAuditor struct {
	mu     sync.Mutex
	events []leakEvent
}

// Record satisfies the application Auditor port.
func (a *leakAuditor) Record(_ context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any) {
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	a.mu.Lock()
	a.events = append(a.events, leakEvent{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		role:       actorRole,
		targetType: targetType,
		targetID:   targetID,
		meta:       copied,
	})
	a.mu.Unlock()
}

// recorded returns a copy of every captured entry.
func (a *leakAuditor) recorded() []leakEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]leakEvent(nil), a.events...)
}

// newLeakService builds the real use cases over the in-memory repository with the
// capturing auditor. The lookup always reports the product present; existence is
// US1's contract and not what this file probes.
func newLeakService(repo *httpRepository, audit *leakAuditor) *inventoryimplement.Service {
	return inventoryimplement.New(inventoryimplement.Service{
		Inventory: repo,
		Lookup:    &httpLookup{exists: true},
		Tx:        httpTx{},
		Clock:     httpClock{},
		Audit:     audit,
		Mapper:    mapper.New(),
	})
}

// newLeakRouter mounts the administrator group the way the composition root
// mounts it.
func newLeakRouter(svc *inventoryimplement.Service) http.Handler {
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(inventoryAdminPath, handler.AdminRouter(inventoryHooks(nil)))
	return root
}

// A hold is placed so heldQuantity is non-zero, then the response is inspected:
// it carries exactly the three quantities, and neither an internal hold/ledger
// detail nor a second copy of the product identifier leaks into the body
// (FR-007, Constitution V).
func TestStockResponseCarriesOnlyTheThreeQuantities(t *testing.T) {
	id := uuid.New()
	repo := &httpRepository{physical: 5}
	svc := newLeakService(repo, &leakAuditor{})
	if err := svc.Reserve(context.Background(), appdto.ReserveInput{OrderID: uuid.New(), ProductID: id, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	root := newLeakRouter(svc)

	rec := performJSON(root, http.MethodGet, inventoryAdminPath+"/"+id.String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the stock body %q: %v", rec.Body.String(), err)
	}

	want := map[string]struct{}{
		"physicalQuantity":  {},
		"heldQuantity":      {},
		"availableQuantity": {},
	}
	if len(body.Data) != len(want) {
		t.Fatalf("the stock response must carry exactly %d members, got %d: %v", len(want), len(body.Data), keysOf(body.Data))
	}
	for key := range want {
		if _, ok := body.Data[key]; !ok {
			t.Fatalf("the stock response is missing %q: %v", key, keysOf(body.Data))
		}
	}

	// The raw body must not name an internal hold or ledger concept, nor repeat
	// the product identifier the path already carries.
	raw := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"holdid", "orderid", "sourcereference", "actorid", "movements", "note", "productid"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the stock response leaks %q: %s", forbidden, rec.Body.String())
		}
	}
}

// Every manual operation writes one audit entry naming the product and the
// acting administrator, and the metadata carries only the change (kind and
// delta) — never the operator's free-text note and never a customer's personal
// data (FR-006, Constitution VI).
func TestManualAuditEntriesCarryOnlyTheProductAndChange(t *testing.T) {
	id := uuid.New()
	audit := &leakAuditor{}
	svc := newLeakService(&httpRepository{physical: 10}, audit)
	root := newLeakRouter(svc)

	requests := []struct {
		name string
		body string
	}{
		{"restock", `{"quantity":2,"note":"hàng về kho"}`},
		{"damage", `{"quantity":1,"note":"một món vỡ"}`},
		{"adjustment", `{"quantity":7,"note":"kiểm kê tháng"}`},
	}
	for _, request := range requests {
		path := inventoryAdminPath + "/" + id.String() + "/" + request.name
		rec := performJSON(root, http.MethodPost, path, request.body, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d (%s)", request.name, rec.Code, rec.Body.String())
		}
	}

	events := audit.recorded()
	if len(events) != len(requests) {
		t.Fatalf("expected %d audit entries, got %d", len(requests), len(events))
	}
	for _, event := range events {
		if event.targetType != "product" || event.targetID != id.String() {
			t.Fatalf("an audit entry must name the product, got type=%q target=%q", event.targetType, event.targetID)
		}
		if event.actorID == nil || *event.actorID != testAdminID {
			t.Fatalf("an audit entry must name the acting administrator, got %v", event.actorID)
		}
		if len(event.meta) != 2 {
			t.Fatalf("an audit entry must carry exactly kind and delta, got %v", event.meta)
		}
		for key := range event.meta {
			if key != "kind" && key != "delta" {
				t.Fatalf("an audit entry carries an unexpected member %q", key)
			}
		}
		if _, ok := event.meta["kind"]; !ok {
			t.Fatalf("an audit entry is missing kind: %v", event.meta)
		}
		if _, ok := event.meta["delta"]; !ok {
			t.Fatalf("an audit entry is missing delta: %v", event.meta)
		}
		assertAuditHasNoPersonalData(t, event.meta)
	}
}

// assertAuditHasNoPersonalData fails when an audit metadata member or value
// names customer personal data, or names the customer account of the test
// session. No inventory audit entry may carry either (FR-006, Constitution VI).
func assertAuditHasNoPersonalData(t *testing.T, meta map[string]any) {
	t.Helper()
	forbidden := map[string]struct{}{
		"email": {}, "name": {}, "fullname": {}, "full_name": {},
		"phone": {}, "address": {}, "customer": {}, "customerid": {}, "customer_id": {},
	}
	for key, value := range meta {
		lower := strings.ToLower(key)
		if _, bad := forbidden[lower]; bad {
			t.Fatalf("the audit entry carries customer personal data under %q", key)
		}
		if strings.Contains(strings.ToLower(fmt.Sprint(value)), testCustomerID.String()) {
			t.Fatalf("the audit entry names the customer account under %q", key)
		}
	}
}

// keysOf lists a decoded object's member names for a failure message.
func keysOf(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}
