package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file is US1's transport contract: the role guard on every route, the
// status each manual operation answers with, the stock shape, the identifier and
// quantity validation, and the oversell refusal. The routes are mounted exactly
// the way the composition root mounts them, over the real use cases, so the
// answers asserted here are the answers the service produces (FR-005 to FR-007,
// FR-012).

const inventoryAdminPath = "/api/v1/admin/inventory"

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// The tokens the test hooks accept, carrying a fixed subject so a test can
// assert the identity the handler handed to the use case.
var (
	testAdminID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// httpRepository is an in-memory InventoryRepository for the transport fixture.
// It embeds the interface so only the operations US1 reaches are implemented.
// It counts the ledger appends so a test can prove a refused operation writes no
// history entry (FR-009).
type httpRepository struct {
	domainrepo.InventoryRepository
	physical  int64
	movements int
}

func (r *httpRepository) Level(context.Context, uuid.UUID) (int64, error) { return r.physical, nil }

func (r *httpRepository) Increase(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	r.physical += amount
	return r.physical, nil
}

func (r *httpRepository) Decrease(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	if r.physical < amount {
		return 0, domainerr.InsufficientStock(model.FieldQuantity, r.physical, amount)
	}
	r.physical -= amount
	return r.physical, nil
}

func (r *httpRepository) LockLevel(context.Context, uuid.UUID) error { return nil }

func (r *httpRepository) InsertMovement(context.Context, *model.Movement) error {
	r.movements++
	return nil
}

func (r *httpRepository) ActiveHeld(context.Context, uuid.UUID, time.Time) (int64, error) {
	return 0, nil
}

// httpLookup answers the ProductLookup contract with a fixed existence.
type httpLookup struct {
	exists bool
}

func (l *httpLookup) ProductExists(context.Context, uuid.UUID) (bool, error) {
	return l.exists, nil
}

// httpTx is the in-memory UnitOfWork: it runs the function with the same context.
type httpTx struct{}

func (httpTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// httpClock is the injected Clock; the manual paths use it only to stamp rows.
type httpClock struct{}

func (httpClock) Now() time.Time { return time.Now().UTC() }

// inventoryHooks supplies the foundation authentication hooks with a fixed
// administrator and customer identity, recording a denial the way the
// composition root does.
func inventoryHooks(denied *bool) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer admin-token":
				return &middleware.Identity{Subject: testAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			default:
				return nil, nil
			}
		},
		OnDenied: func(context.Context, middleware.Identity, *http.Request) {
			if denied != nil {
				*denied = true
			}
		},
	}
}

// newStockRouter builds the real use cases over the in-memory repository and
// mounts the administrator group the way the composition root mounts it.
func newStockRouter(t *testing.T, physical int64, exists bool, hooks middleware.AuthHooks) (http.Handler, *httpRepository, *httpLookup) {
	t.Helper()
	repo := &httpRepository{physical: physical}
	lookup := &httpLookup{exists: exists}
	svc := inventoryimplement.New(inventoryimplement.Service{
		Inventory: repo,
		Lookup:    lookup,
		Tx:        httpTx{},
		Clock:     httpClock{},
		Audit:     nil,
		Mapper:    mapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(inventoryAdminPath, handler.AdminRouter(hooks))
	return root, repo, lookup
}

// performJSON sends a request with an optional JSON body and bearer token.
func performJSON(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type stockBody struct {
	Data struct {
		PhysicalQuantity  int64 `json:"physicalQuantity"`
		HeldQuantity      int64 `json:"heldQuantity"`
		AvailableQuantity int64 `json:"availableQuantity"`
	} `json:"data"`
}

func decodeStock(t *testing.T, rec *httptest.ResponseRecorder) stockBody {
	t.Helper()
	var body stockBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the stock body %q: %v", rec.Body.String(), err)
	}
	return body
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// inventoryRoute is one administrator route the guard must protect.
type inventoryRoute struct {
	method string
	path   string
	body   string
}

func inventoryRoutes(id uuid.UUID) []inventoryRoute {
	return []inventoryRoute{
		{http.MethodGet, inventoryAdminPath + "/" + id.String(), ""},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/restock", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/damage", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/adjustment", `{"quantity":1}`},
	}
}

// FR-005: every route answers 401 with no token and 403 with a customer token,
// and the denial is recorded through the foundation's OnDenied hook.
func TestEveryInventoryRouteRefusesNoTokenAndCustomerToken(t *testing.T) {
	id := uuid.New()
	for _, route := range inventoryRoutes(id) {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			denied := false
			router, _, _ := newStockRouter(t, 5, true, inventoryHooks(&denied))

			noToken := performJSON(router, route.method, route.path, route.body, "")
			if noToken.Code != http.StatusUnauthorized {
				t.Fatalf("no token: expected 401, got %d (%s)", noToken.Code, noToken.Body.String())
			}
			if body := decodeError(t, noToken); body.Error.Code != string("UNAUTHENTICATED") {
				t.Fatalf("no token: expected UNAUTHENTICATED, got %s", body.Error.Code)
			}

			customer := performJSON(router, route.method, route.path, route.body, "customer-token")
			if customer.Code != http.StatusForbidden {
				t.Fatalf("customer token: expected 403, got %d (%s)", customer.Code, customer.Body.String())
			}
			if body := decodeError(t, customer); body.Error.Code != string("FORBIDDEN") {
				t.Fatalf("customer token: expected FORBIDDEN, got %s", body.Error.Code)
			}
			if !denied {
				t.Fatal("the role denial must be recorded through the foundation's OnDenied hook")
			}
		})
	}
}

// FR-001 to FR-003, FR-007: each write answers 200 with the three quantity
// members, and the read answers the same shape.
func TestEachManualOperationAnswers200WithTheStockShape(t *testing.T) {
	id := uuid.New()

	restock, _, _ := newStockRouter(t, 0, true, inventoryHooks(nil))
	rec := performJSON(restock, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/restock", `{"quantity":10}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 10 || stock.Data.AvailableQuantity != 10 || stock.Data.HeldQuantity != 0 {
		t.Fatalf("restock: unexpected stock answer: %+v", stock.Data)
	}

	damage, _, _ := newStockRouter(t, 10, true, inventoryHooks(nil))
	rec = performJSON(damage, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/damage", `{"quantity":4}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("damage: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 6 {
		t.Fatalf("damage: unexpected stock answer: %+v", stock.Data)
	}

	adjust, _, _ := newStockRouter(t, 10, true, inventoryHooks(nil))
	rec = performJSON(adjust, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/adjustment", `{"quantity":7}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("adjustment: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 7 {
		t.Fatalf("adjustment: unexpected stock answer: %+v", stock.Data)
	}

	read, _, _ := newStockRouter(t, 7, true, inventoryHooks(nil))
	rec = performJSON(read, http.MethodGet, inventoryAdminPath+"/"+id.String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("read: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 7 || stock.Data.HeldQuantity != 0 || stock.Data.AvailableQuantity != 7 {
		t.Fatalf("read: unexpected stock answer: %+v", stock.Data)
	}
}

// FR-007, research D12, quickstart 1b: an unknown identifier is 404
// PRODUCT_NOT_FOUND on the read and on every write.
func TestAnUnknownProductAnswers404(t *testing.T) {
	id := uuid.New()
	router, _, _ := newStockRouter(t, 0, false, inventoryHooks(nil))

	requests := []inventoryRoute{
		{http.MethodGet, inventoryAdminPath + "/" + id.String(), ""},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/restock", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/damage", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/" + id.String() + "/adjustment", `{"quantity":1}`},
	}
	for _, request := range requests {
		t.Run(request.method, func(t *testing.T) {
			rec := performJSON(router, request.method, request.path, request.body, "admin-token")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
				t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
			}
		})
	}
}

// A malformed identifier is 400 VALIDATION_ERROR naming productId
// (contracts/error-codes.md).
func TestAMalformedIdentifierIs400NamingTheField(t *testing.T) {
	router, _, _ := newStockRouter(t, 0, true, inventoryHooks(nil))
	id := uuid.New()

	requests := []inventoryRoute{
		{http.MethodGet, inventoryAdminPath + "/not-a-uuid", ""},
		{http.MethodPost, inventoryAdminPath + "/not-a-uuid/restock", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/not-a-uuid/damage", `{"quantity":1}`},
		{http.MethodPost, inventoryAdminPath + "/not-a-uuid/adjustment", `{"quantity":1}`},
	}
	for _, request := range requests {
		t.Run(request.method, func(t *testing.T) {
			rec := performJSON(router, request.method, request.path, request.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "productId" {
				t.Fatalf("expected the detail to name productId, got %+v", body.Error.Details)
			}
		})
	}
	_ = id
}

// FR-012, quickstart 3d-3f, 4d: a missing, fractional, zero or negative quantity
// is 400 VALIDATION_ERROR naming `quantity`.
func TestABadQuantityIs400NamingQuantity(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name string
		path string
		body string
	}{
		{"missing on restock", "/restock", `{}`},
		{"fractional on restock", "/restock", `{"quantity":1.5}`},
		{"zero on restock", "/restock", `{"quantity":0}`},
		{"negative on restock", "/restock", `{"quantity":-1}`},
		{"missing on damage", "/damage", `{}`},
		{"fractional on damage", "/damage", `{"quantity":1.5}`},
		{"negative on damage", "/damage", `{"quantity":-1}`},
		{"negative on adjustment", "/adjustment", `{"quantity":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, _, _ := newStockRouter(t, 5, true, inventoryHooks(nil))
			rec := performJSON(router, http.MethodPost, inventoryAdminPath+"/"+id.String()+tc.path, tc.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
				t.Fatalf("expected the detail to name quantity, got %+v", body.Error.Details)
			}
		})
	}
}

// FR-002, FR-009, quickstart 3b-3c: damage over the shelf is 409
// INVENTORY_INSUFFICIENT_STOCK, the shelf is unchanged on a follow-up read, and
// the refusal leaves no history entry — a real refusal, not a negative number
// that then exists (US2).
func TestDamageOverTheShelfIs409AndLeavesTheStockUnchanged(t *testing.T) {
	id := uuid.New()
	router, repo, _ := newStockRouter(t, 3, true, inventoryHooks(nil))

	rec := performJSON(router, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/damage", `{"quantity":5}`, "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "INVENTORY_INSUFFICIENT_STOCK" {
		t.Fatalf("expected INVENTORY_INSUFFICIENT_STOCK, got %s", body.Error.Code)
	}
	if repo.physical != 3 {
		t.Fatalf("a refused damage changed the shelf: %d", repo.physical)
	}
	if repo.movements != 0 {
		t.Fatalf("a refused damage must write no history entry, got %d", repo.movements)
	}

	read := performJSON(router, http.MethodGet, inventoryAdminPath+"/"+id.String(), "", "admin-token")
	if stock := decodeStock(t, read); stock.Data.PhysicalQuantity != 3 {
		t.Fatalf("the shelf must be unchanged after a refusal, got %+v", stock.Data)
	}
}

// FR-005, Constitution V: the acting account comes from the session, never from
// the body. A request that tries to name one is refused as an unknown member.
func TestTheActorComesFromTheSessionNotTheRequestBody(t *testing.T) {
	id := uuid.New()
	router, _, _ := newStockRouter(t, 5, true, inventoryHooks(nil))

	rec := performJSON(router, http.MethodPost, inventoryAdminPath+"/"+id.String()+"/restock",
		`{"quantity":1,"actorId":"`+testCustomerID.String()+`"}`, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a body naming an actor must be refused, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "MALFORMED_REQUEST" {
		t.Fatalf("expected MALFORMED_REQUEST, got %s", body.Error.Code)
	}
}
