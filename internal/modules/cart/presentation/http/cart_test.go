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

// This file is US1's transport contract: the singleton /cart group answers an
// empty cart, the three writes answer the updated cart (or 204 for a removal), and
// a malformed identifier or a fractional quantity is a field-level validation
// error. It mounts the routes exactly the way the composition root mounts them,
// over the real use cases, so the answers asserted are the service's own
// (contracts/openapi.yaml, FR-002 to FR-004, FR-006).

const cartPath = "/api/v1/cart"

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// testCustomerID is the subject the test hooks resolve, so a test can assert the
// identity the handler handed to the use case.
var testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")

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

// httpAvailability satisfies the availability port; US1 never reaches it.
type httpAvailability struct{}

func (httpAvailability) AvailableQuantity(_ context.Context, productIDs []uuid.UUID) ([]contracts.Availability, error) {
	out := make([]contracts.Availability, 0, len(productIDs))
	for _, id := range productIDs {
		out = append(out, contracts.Availability{ProductID: id, Available: 0})
	}
	return out, nil
}

// httpTx is the in-memory UnitOfWork.
type httpTx struct{}

func (httpTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// httpClock is the injected Clock.
type httpClock struct{}

func (httpClock) Now() time.Time { return fixedHTTPNow }

// cartHooks supplies the foundation authentication hooks with a fixed customer
// identity, so a test can exercise the session guard.
func cartHooks(denied *bool) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			if r.Header.Get("Authorization") == "Bearer customer-token" {
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			}
			return nil, nil
		},
		OnDenied: func(context.Context, middleware.Identity, *http.Request) {
			if denied != nil {
				*denied = true
			}
		},
	}
}

// newCartRouter builds the real use cases over the in-memory repository and mounts
// the group the way the composition root mounts it.
func newCartRouter(t *testing.T, hooks middleware.AuthHooks) (http.Handler, *httpRepo, *httpCatalog) {
	t.Helper()
	repo := newHTTPRepo()
	catalog := &httpCatalog{products: map[uuid.UUID]contracts.ProductSummary{}}
	svc := cartimplement.New(cartimplement.Service{
		Carts:        repo,
		Products:     catalog,
		Availability: httpAvailability{},
		Tx:           httpTx{},
		Clock:        httpClock{},
		Mapper:       cartmapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(cartPath, handler.Router(hooks))
	return root, repo, catalog
}

// seedHTTPProduct puts one on-sale product in the fake catalogue.
func seedHTTPProduct(catalog *httpCatalog, amount int64) uuid.UUID {
	id := uuid.New()
	catalog.products[id] = contracts.ProductSummary{
		ID: id, Name: "Tranh", Slug: "tranh", OnSale: true,
		Price: contracts.ProductPrice{Amount: amount, Currency: "VND"},
	}
	return id
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
			ProductID uuid.UUID `json:"productId"`
			Name      *string   `json:"name"`
			Slug      *string   `json:"slug"`
			Quantity  int64     `json:"quantity"`
			UnitPrice moneyBody `json:"unitPrice"`
			LineTotal moneyBody `json:"lineTotal"`
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
	router, repo, _ := newCartRouter(t, cartHooks(nil))

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
	router, _, catalog := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, 120000)

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
	router, _, catalog := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, 100000)

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
	router, _, _ := newCartRouter(t, cartHooks(nil))

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
	router, _, catalog := newCartRouter(t, cartHooks(nil))
	product := seedHTTPProduct(catalog, 100000)

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
	router, _, _ := newCartRouter(t, cartHooks(nil))

	rec := performJSON(router, http.MethodPost, cartPath+"/items",
		`{"productId":"`+uuid.New().String()+`","quantity":1}`, "customer-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}
}
