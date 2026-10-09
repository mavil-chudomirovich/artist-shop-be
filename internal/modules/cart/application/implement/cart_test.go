package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file is US1's use-case contract: adding creates one line carrying the
// captured price, adding again raises it, changing and removing act on the line,
// and the read answers every line with an exact subtotal. It runs the real use
// cases over an in-memory repository and fakes of the two cross-module contracts,
// so the answers asserted are the ones the service produces (FR-002 to FR-004,
// FR-008, FR-009). No inventory operation is reached: adding to a cart holds no
// stock (FR-011).

// memoryRepo is an in-memory CartRepository for the use-case fixture. It mirrors
// the storage guarantees US1 rests on: one cart per owner and one line per
// product, with the upsert summing a repeated product's quantity.
type memoryRepo struct {
	carts  map[uuid.UUID]*model.Cart
	owners map[uuid.UUID]uuid.UUID
	lines  map[uuid.UUID][]model.CartLine
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		carts:  map[uuid.UUID]*model.Cart{},
		owners: map[uuid.UUID]uuid.UUID{},
		lines:  map[uuid.UUID][]model.CartLine{},
	}
}

func (r *memoryRepo) FindByOwner(_ context.Context, userID uuid.UUID) (*model.Cart, bool, error) {
	cartID, ok := r.owners[userID]
	if !ok {
		return nil, false, nil
	}
	cart := *r.carts[cartID]
	cart.Lines = append([]model.CartLine(nil), r.lines[cartID]...)
	return &cart, true, nil
}

func (r *memoryRepo) Create(_ context.Context, cart *model.Cart) error {
	if _, ok := r.owners[cart.UserID]; ok {
		return domainerr.ErrCartAlreadyExists
	}
	r.carts[cart.ID] = cart
	r.owners[cart.UserID] = cart.ID
	return nil
}

func (r *memoryRepo) Lock(context.Context, uuid.UUID) error { return nil }

func (r *memoryRepo) UpsertLine(_ context.Context, cartID uuid.UUID, line model.CartLine, _ time.Time) error {
	for i := range r.lines[cartID] {
		if r.lines[cartID][i].ProductID == line.ProductID {
			r.lines[cartID][i].Quantity += line.Quantity
			return nil
		}
	}
	r.lines[cartID] = append(r.lines[cartID], line)
	return nil
}

func (r *memoryRepo) SetQuantity(_ context.Context, cartID, productID uuid.UUID, quantity int64, _ time.Time) error {
	for i := range r.lines[cartID] {
		if r.lines[cartID][i].ProductID == productID {
			r.lines[cartID][i].Quantity = quantity
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *memoryRepo) DeleteLine(_ context.Context, cartID, productID uuid.UUID) error {
	lines := r.lines[cartID]
	for i := range lines {
		if lines[i].ProductID == productID {
			r.lines[cartID] = append(lines[:i], lines[i+1:]...)
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *memoryRepo) Lines(_ context.Context, cartID uuid.UUID) ([]model.CartLine, error) {
	return append([]model.CartLine(nil), r.lines[cartID]...), nil
}

var _ domainrepo.CartRepository = (*memoryRepo)(nil)

// fakeCatalog answers the ProductCatalog contract from a fixed set of products.
type fakeCatalog struct {
	products map[uuid.UUID]contracts.ProductSummary
	err      error
}

func (f *fakeCatalog) Products(_ context.Context, productIDs []uuid.UUID) ([]contracts.ProductSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]contracts.ProductSummary, 0, len(productIDs))
	for _, id := range productIDs {
		if product, ok := f.products[id]; ok {
			out = append(out, product)
		}
	}
	return out, nil
}

// fakeAvailability answers the InventoryAvailability contract. US1 reaches no
// inventory read, so this fake is present to satisfy the wiring and to fail
// loudly if a US1 path ever calls it.
type fakeAvailability struct {
	available map[uuid.UUID]int64
	err       error
}

func (f *fakeAvailability) AvailableQuantity(_ context.Context, productIDs []uuid.UUID) ([]contracts.Availability, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]contracts.Availability, 0, len(productIDs))
	for _, id := range productIDs {
		out = append(out, contracts.Availability{ProductID: id, Available: f.available[id]})
	}
	return out, nil
}

// memoryTx is the in-memory UnitOfWork: it runs the function with the same
// context, so the use case sees one transaction boundary without a database.
type memoryTx struct{}

func (memoryTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// memoryClock is the injected Clock.
type memoryClock struct{ at time.Time }

func (c memoryClock) Now() time.Time { return c.at }

// fixedNow is the deterministic instant the fixture stamps rows with.
var fixedNow = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

// newCartService builds the real use cases over the fixtures.
func newCartService(t *testing.T) (*Service, *memoryRepo, *fakeCatalog) {
	t.Helper()
	repo := newMemoryRepo()
	catalog := &fakeCatalog{products: map[uuid.UUID]contracts.ProductSummary{}}
	svc := New(Service{
		Carts:        repo,
		Products:     catalog,
		Availability: &fakeAvailability{available: map[uuid.UUID]int64{}},
		Tx:           memoryTx{},
		Clock:        memoryClock{at: fixedNow},
		Mapper:       mapper.New(),
	})
	return svc, repo, catalog
}

// actorContext puts the acting customer in the context the way presentation does
// from the session. The owner is never part of a use-case input (FR-010).
func actorContext(userID uuid.UUID) context.Context {
	return appinterface.WithActor(context.Background(), appinterface.Actor{ID: userID, Role: access.RoleCustomer})
}

// seedProduct puts one on-sale product in the fake catalogue and returns its id.
func seedProduct(catalog *fakeCatalog, name, slug string, amount int64) uuid.UUID {
	id := uuid.New()
	catalog.products[id] = contracts.ProductSummary{
		ID:     id,
		Name:   name,
		Slug:   slug,
		OnSale: true,
		Price:  contracts.ProductPrice{Amount: amount, Currency: "VND"},
	}
	return id
}

// FR-002, FR-008: adding a product creates exactly one line carrying its name and
// the price captured at add time, and the subtotal is the line total.
func TestAddCreatesOneLineWithTheCapturedPrice(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	product := seedProduct(catalog, "Tranh sơn dầu", "tranh-son-dau", 120000)

	view, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: product, Quantity: 2})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("adding a product must create exactly one line, got %d", len(view.Lines))
	}
	line := view.Lines[0]
	if line.ProductID != product || line.Quantity != 2 {
		t.Fatalf("unexpected line identity/quantity: %+v", line)
	}
	if line.Name == nil || *line.Name != "Tranh sơn dầu" {
		t.Fatalf("the line must carry the product name, got %+v", line.Name)
	}
	if line.Slug == nil || *line.Slug != "tranh-son-dau" {
		t.Fatalf("the line must carry the product slug, got %+v", line.Slug)
	}
	if line.UnitPrice.Amount != 120000 || line.UnitPrice.Currency != "VND" {
		t.Fatalf("unexpected captured price: %+v", line.UnitPrice)
	}
	if line.LineTotal.Amount != 240000 {
		t.Fatalf("line total = %d, want 240000", line.LineTotal.Amount)
	}
	if view.Subtotal == nil || view.Subtotal.Amount != 240000 {
		t.Fatalf("the subtotal must equal the line total, got %+v", view.Subtotal)
	}
}

// FR-002: adding a product already in the cart raises its quantity rather than
// creating a second line.
func TestAddingTheSameProductAgainRaisesTheLine(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	product := seedProduct(catalog, "Tranh", "tranh", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: product, Quantity: 2}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	view, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: product, Quantity: 1})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("a product appears at most once, got %d lines", len(view.Lines))
	}
	if view.Lines[0].Quantity != 3 {
		t.Fatalf("raising an existing line: quantity = %d, want 3", view.Lines[0].Quantity)
	}
	if view.Subtotal == nil || view.Subtotal.Amount != 300000 {
		t.Fatalf("subtotal after raising = %+v, want 300000", view.Subtotal)
	}
}

// FR-003: changing a line's quantity updates the line and the subtotal.
func TestChangeQuantityUpdatesTheLine(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	product := seedProduct(catalog, "Tranh", "tranh", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: product, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	view, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{ProductID: product, Quantity: 5})
	if err != nil {
		t.Fatalf("ChangeQuantity: %v", err)
	}
	if len(view.Lines) != 1 || view.Lines[0].Quantity != 5 {
		t.Fatalf("the line must show quantity 5, got %+v", view.Lines)
	}
	if view.Subtotal == nil || view.Subtotal.Amount != 500000 {
		t.Fatalf("subtotal after change = %+v, want 500000", view.Subtotal)
	}
}

// FR-003: removing a line drops it and the subtotal falls by its contribution.
func TestRemoveDropsTheLine(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	first := seedProduct(catalog, "Một", "mot", 100000)
	second := seedProduct(catalog, "Hai", "hai", 250000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: first, Quantity: 1}); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: second, Quantity: 2}); err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if err := svc.Remove(actorContext(user), appdto.RemoveItemInput{ProductID: first}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.Lines) != 1 || view.Lines[0].ProductID != second {
		t.Fatalf("only the second line must remain, got %+v", view.Lines)
	}
	if view.Subtotal == nil || view.Subtotal.Amount != 500000 {
		t.Fatalf("subtotal after removal = %+v, want 500000", view.Subtotal)
	}
}

// FR-004, FR-009: reading returns every line with product, quantity, captured
// price and line total, and the subtotal is the exact sum with no rounding.
func TestReadReturnsLinesAndTheExactSubtotal(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	first := seedProduct(catalog, "Một", "mot", 199999)
	second := seedProduct(catalog, "Hai", "hai", 1234567)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: first, Quantity: 2}); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: second, Quantity: 7}); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.Lines) != 2 {
		t.Fatalf("expected two lines, got %d", len(view.Lines))
	}
	const want = 2*199999 + 7*1234567
	if view.Subtotal == nil || view.Subtotal.Amount != want {
		t.Fatalf("subtotal = %+v, want %d", view.Subtotal, want)
	}
	for _, line := range view.Lines {
		if line.LineTotal.Amount != line.Quantity*line.UnitPrice.Amount {
			t.Fatalf("line total must equal quantity times captured price: %+v", line)
		}
	}
}

// FR-004: a customer with no cart reads an empty cart whose subtotal is absent,
// not an error and not a zero in an invented currency.
func TestEmptyCartReadsEmptyWithNoSubtotal(t *testing.T) {
	svc, _, _ := newCartService(t)

	view, err := svc.Get(actorContext(uuid.New()), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.Lines) != 0 {
		t.Fatalf("a customer with no cart must read no lines, got %d", len(view.Lines))
	}
	if view.Subtotal != nil {
		t.Fatalf("an empty cart must carry no subtotal, got %+v", view.Subtotal)
	}
}

// FR-002, error-codes.md: a product absent from the catalogue is the shared
// PRODUCT_NOT_FOUND, so a removed product is distinguishable from a removed line.
func TestAddUnknownProductIsNotFound(t *testing.T) {
	svc, _, _ := newCartService(t)

	_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{ProductID: uuid.New(), Quantity: 1})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// FR-006: a non-positive quantity is refused before anything is written, naming
// the offending member.
func TestAddRejectsNonPositiveQuantity(t *testing.T) {
	svc, _, catalog := newCartService(t)
	product := seedProduct(catalog, "Tranh", "tranh", 100000)

	for _, quantity := range []int64{0, -1} {
		_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{ProductID: product, Quantity: quantity})
		var invalid *domainerr.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Fatalf("quantity %d: expected InvalidValueError, got %v", quantity, err)
		}
		if invalid.Field != model.FieldQuantity {
			t.Fatalf("quantity %d: rejection named %q, want %q", quantity, invalid.Field, model.FieldQuantity)
		}
	}
}

// error-codes.md: changing or removing a line this cart does not hold is the same
// PRODUCT_NOT_FOUND an unknown product answers, so the route never confirms what
// is in another customer's cart.
func TestChangeOrRemoveAMissingLineIsNotFound(t *testing.T) {
	svc, _, catalog := newCartService(t)
	user := uuid.New()
	held := seedProduct(catalog, "Có", "co", 100000)
	missing := uuid.New()

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{ProductID: held, Quantity: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{ProductID: missing, Quantity: 2}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("changing a missing line: expected ErrProductNotFound, got %v", err)
	}
	if err := svc.Remove(actorContext(user), appdto.RemoveItemInput{ProductID: missing}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("removing a missing line: expected ErrProductNotFound, got %v", err)
	}
}
