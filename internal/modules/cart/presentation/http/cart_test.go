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

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	cartimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/implement"
	cartmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file is the transport contract of US1 and US2: the singleton /cart group
// answers an empty cart, the three writes answer the updated cart (or 204 for a
// removal), a malformed identifier or a fractional quantity is a field-level
// validation error, and US2's refusals answer the two 409 codes with the offending
// field named while the view carries each line's buyable flag and, when short, the
// available quantity. It mounts the routes exactly the way the composition root
// mounts them, over the real use cases, so the answers asserted are the service's
// own (contracts/openapi.yaml, FR-002 to FR-007, FR-012).

const cartPath = "/api/v1/cart"

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// testCustomerID is the subject the test hooks resolve, so a test can assert the
// identity the handler handed to the use case.
var testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")

// testCustomerTwoID is the second signed-in customer the ownership fixture
// resolves, so a test can prove two customers never share a cart (US3, FR-001).
var testCustomerTwoID = uuid.MustParse("33333333-3333-3333-3333-333333333333")

// fixedHTTPNow is the instant the fixture stamps rows with.
var fixedHTTPNow = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

// httpRepo is an in-memory CartRepository for the transport fixture.
type httpRepo struct {
	carts  map[uuid.UUID]*model.Cart
	owners map[uuid.UUID]uuid.UUID
	lines  map[uuid.UUID][]model.CartLine
}

func newHTTPRepo() *httpRepo {
	return &httpRepo{
		carts:  map[uuid.UUID]*model.Cart{},
		owners: map[uuid.UUID]uuid.UUID{},
		lines:  map[uuid.UUID][]model.CartLine{},
	}
}

var _ domainrepo.CartRepository = (*httpRepo)(nil)

func (r *httpRepo) FindByOwner(_ context.Context, userID uuid.UUID) (*model.Cart, bool, error) {
	cartID, ok := r.owners[userID]
	if !ok {
		return nil, false, nil
	}
	cart := *r.carts[cartID]
	cart.Lines = append([]model.CartLine(nil), r.lines[cartID]...)
	return &cart, true, nil
}

func (r *httpRepo) Create(_ context.Context, cart *model.Cart) error {
	if _, ok := r.owners[cart.UserID]; ok {
		return domainerr.ErrCartAlreadyExists
	}
	r.carts[cart.ID] = cart
	r.owners[cart.UserID] = cart.ID
	return nil
}

func (r *httpRepo) Lock(context.Context, uuid.UUID) error { return nil }

func (r *httpRepo) UpsertLine(_ context.Context, cartID uuid.UUID, line model.CartLine, _ time.Time) error {
	for i := range r.lines[cartID] {
		if r.lines[cartID][i].ProductID == line.ProductID {
			r.lines[cartID][i].Quantity += line.Quantity
			return nil
		}
	}
	r.lines[cartID] = append(r.lines[cartID], line)
	return nil
}

func (r *httpRepo) SetQuantity(_ context.Context, cartID, productID uuid.UUID, quantity int64, _ time.Time) error {
	for i := range r.lines[cartID] {
		if r.lines[cartID][i].ProductID == productID {
			r.lines[cartID][i].Quantity = quantity
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *httpRepo) DeleteLine(_ context.Context, cartID, productID uuid.UUID) error {
	lines := r.lines[cartID]
	for i := range lines {
		if lines[i].ProductID == productID {
			r.lines[cartID] = append(lines[:i], lines[i+1:]...)
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *httpRepo) Lines(_ context.Context, cartID uuid.UUID) ([]model.CartLine, error) {
	return append([]model.CartLine(nil), r.lines[cartID]...), nil
}

// httpCatalog answers the ProductCatalog contract from a fixed set.
type httpCatalog struct {
	products map[uuid.UUID]contracts.ProductSummary
}

func (f *httpCatalog) Products(_ context.Context, productIDs []uuid.UUID) ([]contracts.ProductSummary, error) {
	out := make([]contracts.ProductSummary, 0, len(productIDs))
	for _, id := range productIDs {
		if product, ok := f.products[id]; ok {
			out = append(out, product)
		}
	}
	return out, nil
}

// httpAvailability answers the availability port from a mutable map, so a test
// can set a product's shelf and later lower it to exercise a short line.
type httpAvailability struct {
	available map[uuid.UUID]int64
}

func (f *httpAvailability) AvailableQuantity(_ context.Context, productIDs []uuid.UUID) ([]contracts.Availability, error) {
	out := make([]contracts.Availability, 0, len(productIDs))
	for _, id := range productIDs {
		out = append(out, contracts.Availability{ProductID: id, Available: f.available[id]})
	}
	return out, nil
}

// httpTx is the in-memory UnitOfWork.
type httpTx struct{}

func (httpTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// httpClock is the injected Clock.
type httpClock struct{}

func (httpClock) Now() time.Time { return fixedHTTPNow }

// cartHooks supplies the foundation authentication hooks with two fixed customer
// identities, so a test can exercise the session guard and prove two signed-in
// customers never reach each other's cart.
func cartHooks(denied *bool) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			case "Bearer customer2-token":
				return &middleware.Identity{Subject: testCustomerTwoID.String(), Role: string(access.RoleCustomer), TokenID: "customer2"}, nil
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

// defaultHTTPAvailable is the shelf the fixture gives a seeded product unless a
// test lowers it, so a test that is not about availability is not refused by it.
const defaultHTTPAvailable int64 = 1000

// newCartRouter builds the real use cases over the in-memory repository and mounts
// the group the way the composition root mounts it.
func newCartRouter(t *testing.T, hooks middleware.AuthHooks) (http.Handler, *httpRepo, *httpCatalog, *httpAvailability) {
	t.Helper()
	repo := newHTTPRepo()
	catalog := &httpCatalog{products: map[uuid.UUID]contracts.ProductSummary{}}
	availability := &httpAvailability{available: map[uuid.UUID]int64{}}
	svc := cartimplement.New(cartimplement.Service{
		Carts:        repo,
		Products:     catalog,
		Availability: availability,
		Tx:           httpTx{},
		Clock:        httpClock{},
		Mapper:       cartmapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(cartPath, handler.Router(hooks))
	return root, repo, catalog, availability
}

// seedHTTPProduct puts one on-sale product in the fake catalogue with the
// fixture's default availability.
func seedHTTPProduct(catalog *httpCatalog, availability *httpAvailability, amount int64) uuid.UUID {
	return seedHTTPProductState(catalog, availability, amount, true)
}

// seedHTTPProductState puts one product in the fake catalogue with the fixture's
// default availability, on sale or not.
func seedHTTPProductState(catalog *httpCatalog, availability *httpAvailability, amount int64, onSale bool) uuid.UUID {
	id := uuid.New()
	catalog.products[id] = contracts.ProductSummary{
		ID: id, Name: "Tranh", Slug: "tranh", OnSale: onSale,
		Price: contracts.ProductPrice{Amount: amount, Currency: "VND"},
	}
	availability.available[id] = defaultHTTPAvailable
	return id
}

// seedHTTPAvailable lowers a seeded product's shelf for the fixture.
func seedHTTPAvailable(availability *httpAvailability, productID uuid.UUID, quantity int64) {
	availability.available[productID] = quantity
}

// markHTTPProductOffSale takes a seeded product off sale without touching its name
// or price.
func markHTTPProductOffSale(catalog *httpCatalog, productID uuid.UUID) {
	summary := catalog.products[productID]
	summary.OnSale = false
	catalog.products[productID] = summary
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

type moneyBody struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type cartBody struct {
	Data struct {
		Lines []struct {
			ProductID         uuid.UUID `json:"productId"`
			Name              *string   `json:"name"`
			Slug              *string   `json:"slug"`
			Quantity          int64     `json:"quantity"`
			UnitPrice         moneyBody `json:"unitPrice"`
			LineTotal         moneyBody `json:"lineTotal"`
			Buyable           bool      `json:"buyable"`
			AvailableQuantity *int64    `json:"availableQuantity"`
		} `json:"lines"`
		Subtotal *moneyBody `json:"subtotal"`
	} `json:"data"`
}

func decodeCart(t *testing.T, rec *httptest.ResponseRecorder) cartBody {
	t.Helper()
	var body cartBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the cart body %q: %v", rec.Body.String(), err)
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

// decodeError is shared with the integration file; the inventory module's package
// defines its own, so a module never imports another's test helpers.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// cartRoutes lists the four routes the /cart group owns (research D7).
func cartRoutes(productID uuid.UUID) []struct {
	method string
	path   string
	body   string
} {
	return []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, cartPath, ""},
		{http.MethodPost, cartPath + "/items", `{"productId":"` + productID.String() + `","quantity":1}`},
		{http.MethodPatch, cartPath + "/items/" + productID.String(), `{"quantity":1}`},
		{http.MethodDelete, cartPath + "/items/" + productID.String(), ""},
	}
}

// quickstart scenario 1a: an empty cart answers 200 with `lines: []` and
// `subtotal: null`, not an error and not a created row.
func TestGetEmptyCartAnswersEmptyWithNullSubtotal(t *testing.T) {
	router, repo, _, _ := newCartRouter(t, cartHooks(nil))

	rec := performJSON(router, http.MethodGet, cartPath, "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cart: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCart(t, rec)
	if len(body.Data.Lines) != 0 {
		t.Fatalf("an empty cart must have no lines, got %+v", body.Data.Lines)
	}
	if body.Data.Subtotal != nil {
		t.Fatalf("an empty cart must answer subtotal null, got %+v", body.Data.Subtotal)
	}
	if !strings.Contains(rec.Body.String(), `"lines":[]`) || !strings.Contains(rec.Body.String(), `"subtotal":null`) {
		t.Fatalf("an empty cart must serialise as lines:[] and subtotal:null, got %s", rec.Body.String())
	}
	if len(repo.carts) != 0 {
		t.Fatalf("reading a cart must not create a row, got %d", len(repo.carts))
	}
}

// quickstart scenario 2: adding a product answers the updated cart with the line
// and the subtotal.
func TestAddAnswersTheUpdatedCart(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 120000)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cart/items: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCart(t, rec)
	if len(body.Data.Lines) != 1 {
		t.Fatalf("expected one line, got %+v", body.Data.Lines)
	}
	line := body.Data.Lines[0]
	if line.ProductID != product || line.Quantity != 2 {
		t.Fatalf("unexpected line: %+v", line)
	}
	if line.UnitPrice.Amount != 120000 || line.LineTotal.Amount != 240000 {
		t.Fatalf("unexpected money: unit=%+v total=%+v", line.UnitPrice, line.LineTotal)
	}
	if body.Data.Subtotal == nil || body.Data.Subtotal.Amount != 240000 {
		t.Fatalf("unexpected subtotal: %+v", body.Data.Subtotal)
	}
}

// quickstart scenario 4: changing a line's quantity answers the updated cart and
// removing it answers 204 with the line gone.
func TestChangeAndRemoveBehaveAsTheContractDeclares(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = performJSON(router, http.MethodPatch, cartPath+"/items/"+product.String(), `{"quantity":5}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeCart(t, rec); len(body.Data.Lines) != 1 || body.Data.Lines[0].Quantity != 5 {
		t.Fatalf("the line must show quantity 5, got %+v", body.Data.Lines)
	}

	rec = performJSON(router, http.MethodDelete, cartPath+"/items/"+product.String(), "", "customer-token")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = performJSON(router, http.MethodGet, cartPath, "", "customer-token")
	if body := decodeCart(t, rec); len(body.Data.Lines) != 0 {
		t.Fatalf("the cart must be empty after a removal, got %+v", body.Data.Lines)
	}
}

// contracts/openapi.yaml: a product identifier that is not a UUID is 400
// VALIDATION_ERROR naming `productId`, in the body and in the path alike.
func TestMalformedProductIdentifierIs400NamingTheField(t *testing.T) {
	router, _, _, _ := newCartRouter(t, cartHooks(nil))

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"body", http.MethodPost, cartPath + "/items", `{"productId":"not-a-uuid","quantity":1}`},
		{"path patch", http.MethodPatch, cartPath + "/items/not-a-uuid", `{"quantity":1}`},
		{"path delete", http.MethodDelete, cartPath + "/items/not-a-uuid", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performJSON(router, tc.method, tc.path, tc.body, "customer-token")
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
}

// contracts/openapi.yaml: a fractional quantity is a field error, not a decode
// failure. The request DTO decodes `quantity` as a json.Number, as module 05 does.
func TestFractionalQuantityIs400NamingQuantity(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)

	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":1.5}`, "customer-token"); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST fractional: expected 400, got %d (%s)", rec.Code, rec.Body.String())
	} else if body := decodeError(t, rec); body.Error.Code != "VALIDATION_ERROR" ||
		len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
		t.Fatalf("POST fractional: expected VALIDATION_ERROR naming quantity, got %+v", body.Error)
	}

	if rec := performJSON(router, http.MethodPatch, cartPath+"/items/"+product.String(),
		`{"quantity":1.5}`, "customer-token"); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH fractional: expected 400, got %d (%s)", rec.Code, rec.Body.String())
	} else if body := decodeError(t, rec); body.Error.Code != "VALIDATION_ERROR" ||
		len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
		t.Fatalf("PATCH fractional: expected VALIDATION_ERROR naming quantity, got %+v", body.Error)
	}
}

// A product absent from the catalogue is the shared PRODUCT_NOT_FOUND (404).
func TestAddUnknownProductIs404(t *testing.T) {
	router, _, _, _ := newCartRouter(t, cartHooks(nil))

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+uuid.New().String()+`","quantity":1}`, "customer-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}
}

// US2, FR-005, error-codes.md: adding a product that is not on sale is 409
// CART_PRODUCT_NOT_PURCHASABLE and its detail names `productId`.
func TestAddOffSaleProductIs409NamingProduct(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProductState(catalog, availability, 100000, false)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":1}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "CART_PRODUCT_NOT_PURCHASABLE" {
		t.Fatalf("expected CART_PRODUCT_NOT_PURCHASABLE, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "productId" {
		t.Fatalf("expected the detail to name productId, got %+v", body.Error.Details)
	}
}

// US2, FR-007, error-codes.md: adding more than is available is 409
// CART_QUANTITY_EXCEEDS_AVAILABLE and its detail names `quantity` with the
// available amount in the issue.
func TestAddOverAvailableIs409NamingQuantity(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)
	seedHTTPAvailable(availability, product, 3)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":5}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "CART_QUANTITY_EXCEEDS_AVAILABLE" {
		t.Fatalf("expected CART_QUANTITY_EXCEEDS_AVAILABLE, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
		t.Fatalf("expected the detail to name quantity, got %+v", body.Error.Details)
	}
	if !strings.Contains(body.Error.Details[0].Issue, "3") {
		t.Fatalf("the issue must state the available amount, got %q", body.Error.Details[0].Issue)
	}

	// The whole available amount is reachable.
	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":3}`, "customer-token"); rec.Code != http.StatusOK {
		t.Fatalf("adding exactly the available amount: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// US2, FR-005: a PATCH onto a line whose product has gone off sale is the same
// 409 CART_PRODUCT_NOT_PURCHASABLE, naming productId, and the previous quantity is
// kept.
func TestPatchOffSaleIs409NamingProduct(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	markHTTPProductOffSale(catalog, product)

	rec = performJSON(router, http.MethodPatch, cartPath+"/items/"+product.String(), `{"quantity":5}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "CART_PRODUCT_NOT_PURCHASABLE" {
		t.Fatalf("expected CART_PRODUCT_NOT_PURCHASABLE, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "productId" {
		t.Fatalf("expected the detail to name productId, got %+v", body.Error.Details)
	}

	// The previous quantity is untouched.
	rec = performJSON(router, http.MethodGet, cartPath, "", "customer-token")
	if cart := decodeCart(t, rec); len(cart.Data.Lines) != 1 || cart.Data.Lines[0].Quantity != 2 {
		t.Fatalf("a refused change must keep the previous quantity, got %+v", cart.Data.Lines)
	}
}

// US2, FR-007: a PATCH above what is now available is 409
// CART_QUANTITY_EXCEEDS_AVAILABLE, naming quantity, and the previous quantity is
// kept; reducing to the available amount succeeds and reads buyable again.
func TestPatchOverAvailableIs409NamingQuantity(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":1}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	seedHTTPAvailable(availability, product, 2)

	rec = performJSON(router, http.MethodPatch, cartPath+"/items/"+product.String(), `{"quantity":5}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "CART_QUANTITY_EXCEEDS_AVAILABLE" {
		t.Fatalf("expected CART_QUANTITY_EXCEEDS_AVAILABLE, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
		t.Fatalf("expected the detail to name quantity, got %+v", body.Error.Details)
	}

	rec = performJSON(router, http.MethodPatch, cartPath+"/items/"+product.String(), `{"quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("reducing to the available amount: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	cart := decodeCart(t, rec)
	if len(cart.Data.Lines) != 1 || cart.Data.Lines[0].Quantity != 2 || !cart.Data.Lines[0].Buyable {
		t.Fatalf("the reduced line must be buyable at quantity 2, got %+v", cart.Data.Lines)
	}
}

// US2, FR-012, research D10: the view always carries `buyable`, and carries
// `availableQuantity` only when the line is on sale but short of its quantity.
func TestViewCarriesBuyableAndAvailableQuantityOnlyWhenShort(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)
	seedHTTPAvailable(availability, product, 3)

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+product.String()+`","quantity":3}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCart(t, rec)
	if len(body.Data.Lines) != 1 {
		t.Fatalf("expected one line, got %+v", body.Data.Lines)
	}
	if !body.Data.Lines[0].Buyable {
		t.Fatalf("a fully available line must be buyable, got %+v", body.Data.Lines[0])
	}
	if body.Data.Lines[0].AvailableQuantity != nil {
		t.Fatalf("a buyable line must not report an available quantity, got %d", *body.Data.Lines[0].AvailableQuantity)
	}
	if strings.Contains(rec.Body.String(), "availableQuantity") {
		t.Fatalf("a fully available line must not carry availableQuantity, got %s", rec.Body.String())
	}

	// Damage the shelf so the line is now short.
	seedHTTPAvailable(availability, product, 1)
	rec = performJSON(router, http.MethodGet, cartPath, "", "customer-token")
	body = decodeCart(t, rec)
	if len(body.Data.Lines) != 1 {
		t.Fatalf("expected one line, got %+v", body.Data.Lines)
	}
	line := body.Data.Lines[0]
	if line.Buyable {
		t.Fatalf("a short line must not be buyable, got %+v", line)
	}
	if line.AvailableQuantity == nil || *line.AvailableQuantity != 1 {
		t.Fatalf("a short line must report available=1, got %+v", line.AvailableQuantity)
	}
	if line.Quantity != 3 || line.UnitPrice.Amount != 100000 {
		t.Fatalf("a read must not change the quantity or captured price, got %+v", line)
	}
}

// US3, FR-010, quickstart scenario 8a: every route of the /cart group refuses a
// request with no token, so no cart is reachable without a signed-in session. The
// guard is on the group, so the read and each write alike answer UNAUTHENTICATED.
func TestEveryCartRouteRefusesWithoutASession(t *testing.T) {
	router, _, _, _ := newCartRouter(t, cartHooks(nil))
	product := uuid.New()

	for _, route := range cartRoutes(product) {
		rec := performJSON(router, route.method, route.path, route.body, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401, got %d (%s)", route.method, route.path, rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
			t.Fatalf("%s %s: expected UNAUTHENTICATED, got %s", route.method, route.path, body.Error.Code)
		}
	}
}

// US3, FR-001, FR-010, quickstart scenario 8b: two signed-in customers each build
// a cart and neither sees the other's lines. The single repository is shared, so
// the separation is the owner taken from the session, not a second store.
func TestTwoCustomersEachGetTheirOwnCart(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	firstProduct := seedHTTPProduct(catalog, availability, 100000)
	secondProduct := seedHTTPProduct(catalog, availability, 200000)

	// The first customer adds one product.
	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+firstProduct.String()+`","quantity":1}`, "customer-token"); rec.Code != http.StatusOK {
		t.Fatalf("first customer add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The second customer has no cart yet: a read answers an empty cart, not the
	// first customer's.
	rec := performJSON(router, http.MethodGet, cartPath, "", "customer2-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("second customer read: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeCart(t, rec); len(body.Data.Lines) != 0 || body.Data.Subtotal != nil {
		t.Fatalf("the second customer must start with an empty cart, got %+v", body.Data)
	}

	// The second customer adds a different product.
	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+secondProduct.String()+`","quantity":2}`, "customer2-token"); rec.Code != http.StatusOK {
		t.Fatalf("second customer add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Each read now shows only its own line.
	first := decodeCart(t, performJSON(router, http.MethodGet, cartPath, "", "customer-token"))
	if len(first.Data.Lines) != 1 || first.Data.Lines[0].ProductID != firstProduct {
		t.Fatalf("the first customer must see only their line, got %+v", first.Data.Lines)
	}
	second := decodeCart(t, performJSON(router, http.MethodGet, cartPath, "", "customer2-token"))
	if len(second.Data.Lines) != 1 || second.Data.Lines[0].ProductID != secondProduct {
		t.Fatalf("the second customer must see only their line, got %+v", second.Data.Lines)
	}
}

// US3, FR-010, quickstart scenarios 8c and 8d: changing or removing a product
// that only another customer holds answers 404 PRODUCT_NOT_FOUND, so the route
// never confirms what is in another customer's cart. The second customer holds a
// cart of their own, so the 404 is the missing line and not a missing cart.
func TestChangingOrRemovingAnotherCustomersLineIs404(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	heldByFirst := seedHTTPProduct(catalog, availability, 100000)
	heldBySecond := seedHTTPProduct(catalog, availability, 200000)

	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+heldByFirst.String()+`","quantity":1}`, "customer-token"); rec.Code != http.StatusOK {
		t.Fatalf("first customer add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+heldBySecond.String()+`","quantity":1}`, "customer2-token"); rec.Code != http.StatusOK {
		t.Fatalf("second customer add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec := performJSON(router, http.MethodPatch, cartPath+"/items/"+heldByFirst.String(), `{"quantity":2}`, "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("change another customer's line: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("change another customer's line: expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}

	rec = performJSON(router, http.MethodDelete, cartPath+"/items/"+heldByFirst.String(), "", "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("remove another customer's line: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("remove another customer's line: expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}

	// The first customer's line is untouched by the second customer's attempts.
	first := decodeCart(t, performJSON(router, http.MethodGet, cartPath, "", "customer-token"))
	if len(first.Data.Lines) != 1 || first.Data.Lines[0].ProductID != heldByFirst || first.Data.Lines[0].Quantity != 1 {
		t.Fatalf("the first customer's line must be untouched, got %+v", first.Data.Lines)
	}
}

// US3, FR-010, research D13: no request may name an owner. The decoder refuses an
// unknown member, so a body carrying an owner is MALFORMED_REQUEST rather than an
// accepted caller-chosen identity.
func TestABodyThatNamesAnOwnerIs400MalformedRequest(t *testing.T) {
	router, _, catalog, availability := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, availability, 100000)
	other := uuid.New().String()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"add names userId", http.MethodPost, cartPath + "/items",
			`{"productId":"` + product.String() + `","quantity":1,"userId":"` + other + `"}`},
		{"add names ownerId", http.MethodPost, cartPath + "/items",
			`{"productId":"` + product.String() + `","quantity":1,"ownerId":"` + other + `"}`},
		{"change names userId", http.MethodPatch, cartPath + "/items/" + product.String(),
			`{"quantity":1,"userId":"` + other + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performJSON(router, tc.method, tc.path, tc.body, "customer-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != "MALFORMED_REQUEST" {
				t.Fatalf("expected MALFORMED_REQUEST, got %s", body.Error.Code)
			}
		})
	}
}
