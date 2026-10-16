package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// T022: the four paths US1 owns resolve under /cart — the cart read, the add, and
// the change and removal of a line — and the group requires a session, so no path
// reaches a handler without one (research D7, FR-010).

func TestTheUS1CartPathsResolve(t *testing.T) {
	router, _, variants, availability := newCartRouter(t, cartHooks(nil))
	variant := seedHTTPVariant(variants, availability, 100000)

	if rec := performJSON(router, http.MethodGet, cartPath, "", "customer-token"); rec.Code != http.StatusOK {
		t.Fatalf("GET /cart: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"variantId":"`+variant.String()+`","quantity":1}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cart/items: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = performJSON(router, http.MethodPatch, cartPath+"/items/"+variant.String(), `{"quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /cart/items/{variantId}: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = performJSON(router, http.MethodDelete, cartPath+"/items/"+variant.String(), "", "customer-token")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /cart/items/{variantId}: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// The group is guarded: without a token every path answers 401 rather than
// reaching a handler, which is what proves the session guard is installed on the
// group and not on the individual routes only.
func TestTheCartGroupRequiresASession(t *testing.T) {
	router, _, _, _ := newCartRouter(t, cartHooks(nil))
	variant := uuid.New()

	for _, route := range cartRoutes(variant) {
		rec := performJSON(router, route.method, route.path, route.body, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401 without a token, got %d (%s)", route.method, route.path, rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
			t.Fatalf("%s %s: expected UNAUTHENTICATED, got %s", route.method, route.path, body.Error.Code)
		}
	}
}
