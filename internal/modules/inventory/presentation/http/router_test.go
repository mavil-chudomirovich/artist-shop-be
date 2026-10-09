package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// T026: the four paths US1 owns resolve under /admin/inventory — the stock read
// and the three writes — and the group carries the administrator role guard.
//
// The /movements path is deliberately not asserted here: it belongs to US6 and
// this test must pass before the history endpoint exists.
func TestTheUS1InventoryPathsResolve(t *testing.T) {
	id := uuid.New()

	restock, _, _ := newStockRouter(t, 0, true, inventoryHooks(nil))
	if rec := performJSON(restock, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/restock", `{"quantity":1}`, "admin-token"); rec.Code != http.StatusOK {
		t.Fatalf("POST .../restock: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	damage, _, _ := newStockRouter(t, 3, true, inventoryHooks(nil))
	if rec := performJSON(damage, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/damage", `{"quantity":1}`, "admin-token"); rec.Code != http.StatusOK {
		t.Fatalf("POST .../damage: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	adjust, _, _ := newStockRouter(t, 3, true, inventoryHooks(nil))
	if rec := performJSON(adjust, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/adjustment", `{"quantity":2}`, "admin-token"); rec.Code != http.StatusOK {
		t.Fatalf("POST .../adjustment: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	read, _, _ := newStockRouter(t, 3, true, inventoryHooks(nil))
	if rec := performJSON(read, http.MethodGet, inventoryAdminPath+"/"+id.String(), "", "admin-token"); rec.Code != http.StatusOK {
		t.Fatalf("GET .../{productId}: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// The group is guarded: without a token every path answers 401 rather than
// reaching a handler, which is what proves the administrator role guard is
// installed on the group and not on the individual routes only.
func TestTheInventoryGroupCarriesTheAdministratorGuard(t *testing.T) {
	id := uuid.New()
	router, _, _ := newStockRouter(t, 3, true, inventoryHooks(nil))

	for _, route := range inventoryRoutes(id) {
		rec := performJSON(router, route.method, route.path, route.body, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401 without a token, got %d (%s)", route.method, route.path, rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
			t.Fatalf("%s %s: expected UNAUTHENTICATED, got %s", route.method, route.path, body.Error.Code)
		}
	}
}
