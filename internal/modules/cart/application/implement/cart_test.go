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

// This file is the use-case contract of US1 and US2. US1: adding creates one line
// carrying the captured price, adding again raises it, changing and removing act
// on the line, and the read answers every line with an exact subtotal. US2: an add
// and a change are refused for a variant that is not on sale or a quantity above
// what is available, and a read reports each line's buyable decision and, when it
// is short, the available quantity. It runs the real use cases over an in-memory
// repository and fakes of the two cross-module contracts, so the answers asserted
// are the ones the service produces (FR-002 to FR-009, FR-012). No inventory write
// is reached: adding to a cart holds no stock (FR-011).

// memoryRepo is an in-memory CartRepository for the use-case fixture. It mirrors
// the storage guarantees US1 rests on: one cart per owner and one line per
// variant, with the upsert summing a repeated variant's quantity.
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
		if r.lines[cartID][i].VariantID == line.VariantID {
			r.lines[cartID][i].Quantity += line.Quantity
			return nil
		}
	}
	r.lines[cartID] = append(r.lines[cartID], line)
	return nil
}

func (r *memoryRepo) SetQuantity(_ context.Context, cartID, variantID uuid.UUID, quantity int64, _ time.Time) error {
	for i := range r.lines[cartID] {
		if r.lines[cartID][i].VariantID == variantID {
			r.lines[cartID][i].Quantity = quantity
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *memoryRepo) DeleteLine(_ context.Context, cartID, variantID uuid.UUID) error {
	lines := r.lines[cartID]
	for i := range lines {
		if lines[i].VariantID == variantID {
			r.lines[cartID] = append(lines[:i], lines[i+1:]...)
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (r *memoryRepo) Lines(_ context.Context, cartID uuid.UUID) ([]model.CartLine, error) {
	return append([]model.CartLine(nil), r.lines[cartID]...), nil
}

// ClearLines empties the cart, mirroring the adapter's one-statement delete that
// checkout uses to empty the cart it just turned into an order (research D1).
func (r *memoryRepo) ClearLines(_ context.Context, cartID uuid.UUID) error {
	delete(r.lines, cartID)
	return nil
}

// DeleteLinesByVariants removes every line holding one of the given variants,
// from every cart, mirroring the adapter's one-statement delete the hide guards
// use (specs/016-hide-guard research D4).
func (r *memoryRepo) DeleteLinesByVariants(_ context.Context, variantIDs []uuid.UUID) (int64, error) {
	held := make(map[uuid.UUID]bool, len(variantIDs))
	for _, id := range variantIDs {
		held[id] = true
	}
	var removed int64
	for cartID, lines := range r.lines {
		kept := lines[:0]
		for _, line := range lines {
			if held[line.VariantID] {
				removed++
				continue
			}
			kept = append(kept, line)
		}
		r.lines[cartID] = kept
	}
	return removed, nil
}

var _ domainrepo.CartRepository = (*memoryRepo)(nil)

// fakeVariants answers the ProductVariants contract from a fixed set of variants.
type fakeVariants struct {
	variants map[uuid.UUID]contracts.VariantSummary
	err      error
}

func (f *fakeVariants) Variants(_ context.Context, variantIDs []uuid.UUID) ([]contracts.VariantSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]contracts.VariantSummary, 0, len(variantIDs))
	for _, id := range variantIDs {
		if variant, ok := f.variants[id]; ok {
			out = append(out, variant)
		}
	}
	return out, nil
}

// fakeAvailability answers the InventoryAvailability contract from a fixed set
// of shelves. US1 does not read it, but US2 reads it on add, on change and on
// every read, so a test seeds the shelf it needs (FR-007, FR-012).
type fakeAvailability struct {
	available map[uuid.UUID]int64
	err       error
}

func (f *fakeAvailability) AvailableQuantity(_ context.Context, variantIDs []uuid.UUID) ([]contracts.Availability, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]contracts.Availability, 0, len(variantIDs))
	for _, id := range variantIDs {
		out = append(out, contracts.Availability{VariantID: id, Available: f.available[id]})
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

// defaultAvailable is the quantity the fixture gives a seeded variant unless a
// test lowers it, so a US1 test that is not about the shelf is never refused by
// the US2 availability rule.
const defaultAvailable int64 = 1000

// newCartService builds the real use cases over the fixtures.
func newCartService(t *testing.T) (*Service, *memoryRepo, *fakeVariants, *fakeAvailability) {
	t.Helper()
	repo := newMemoryRepo()
	variants := &fakeVariants{variants: map[uuid.UUID]contracts.VariantSummary{}}
	availability := &fakeAvailability{available: map[uuid.UUID]int64{}}
	svc := New(Service{
		Carts:        repo,
		Variants:     variants,
		Availability: availability,
		Tx:           memoryTx{},
		Clock:        memoryClock{at: fixedNow},
		Mapper:       mapper.New(),
	})
	return svc, repo, variants, availability
}

// actorContext puts the acting customer in the context the way presentation does
// from the session. The owner is never part of a use-case input (FR-010).
func actorContext(userID uuid.UUID) context.Context {
	return appinterface.WithActor(context.Background(), appinterface.Actor{ID: userID, Role: access.RoleCustomer})
}

// seedVariant puts one on-sale variant in the fake catalogue with the fixture's
// default availability and returns its id.
func seedVariant(variants *fakeVariants, availability *fakeAvailability, productName, variantName string, amount int64) uuid.UUID {
	return seedVariantState(variants, availability, productName, variantName, amount, true)
}

// seedVariantState puts one variant in the fake catalogue with the fixture's
// default availability, on sale or not, and returns its id.
func seedVariantState(variants *fakeVariants, availability *fakeAvailability, productName, variantName string, amount int64, onSale bool) uuid.UUID {
	id := uuid.New()
	suffix := id.String()[:8]
	variants.variants[id] = contracts.VariantSummary{
		VariantID:   id,
		ProductID:   uuid.New(),
		ProductName: productName,
		ProductCode: "P-" + suffix,
		VariantName: variantName,
		SKU:         "SKU-" + suffix,
		OnSale:      onSale,
		Price:       contracts.ProductPrice{Amount: amount, Currency: "VND"},
	}
	availability.available[id] = defaultAvailable
	return id
}

// seedAvailable lowers a seeded variant's currently available quantity, so a test
// can exercise a short shelf or an over-available request.
func seedAvailable(availability *fakeAvailability, variantID uuid.UUID, quantity int64) {
	availability.available[variantID] = quantity
}

// markOffSale takes a seeded variant off sale without touching its price or name,
// so a test can exercise a variant that stops being sellable after it was added.
func markOffSale(variants *fakeVariants, variantID uuid.UUID) {
	variant := variants.variants[variantID]
	variant.OnSale = false
	variants.variants[variantID] = variant
}

// removeVariant takes a seeded variant out of the catalogue entirely, so a test
// can exercise a line whose variant was removed (FR-012).
func removeVariant(variants *fakeVariants, variantID uuid.UUID) {
	delete(variants.variants, variantID)
}

// FR-002, FR-018: adding a variant creates exactly one line carrying its captured
// price, and the subtotal is the line total.
func TestAddCreatesOneLineWithTheCapturedPrice(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh sơn dầu", "Khổ lớn", 120000)

	view, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("adding a variant must create exactly one line, got %d", len(view.Lines))
	}
	line := view.Lines[0]
	if line.VariantID != variant || line.Quantity != 2 {
		t.Fatalf("unexpected line identity/quantity: %+v", line)
	}
	if line.ProductName == nil || *line.ProductName != "Tranh sơn dầu" {
		t.Fatalf("the line must carry the product name, got %+v", line.ProductName)
	}
	if line.VariantName == nil || *line.VariantName != "Khổ lớn" {
		t.Fatalf("the line must carry the variant name, got %+v", line.VariantName)
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

// FR-002: adding a variant already in the cart raises its quantity rather than
// creating a second line.
func TestAddingTheSameVariantAgainRaisesTheLine(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	view, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 1})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("a variant appears at most once, got %d lines", len(view.Lines))
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
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	view, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{VariantID: variant, Quantity: 5})
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
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	first := seedVariant(variants, availability, "Một", "M", 100000)
	second := seedVariant(variants, availability, "Hai", "H", 250000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: first, Quantity: 1}); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: second, Quantity: 2}); err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if err := svc.Remove(actorContext(user), appdto.RemoveItemInput{VariantID: first}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.Lines) != 1 || view.Lines[0].VariantID != second {
		t.Fatalf("only the second line must remain, got %+v", view.Lines)
	}
	if view.Subtotal == nil || view.Subtotal.Amount != 500000 {
		t.Fatalf("subtotal after removal = %+v, want 500000", view.Subtotal)
	}
}

// FR-004, FR-009: reading returns every line with its variant, quantity, captured
// price and line total, and the subtotal is the exact sum with no rounding.
func TestReadReturnsLinesAndTheExactSubtotal(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	first := seedVariant(variants, availability, "Một", "M", 199999)
	second := seedVariant(variants, availability, "Hai", "H", 1234567)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: first, Quantity: 2}); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: second, Quantity: 7}); err != nil {
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
	svc, _, _, _ := newCartService(t)

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

// FR-002, error-codes.md: a variant absent from the catalogue is the shared
// PRODUCT_NOT_FOUND, so a removed variant is distinguishable from a removed line.
func TestAddUnknownVariantIsNotFound(t *testing.T) {
	svc, _, _, _ := newCartService(t)

	_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{VariantID: uuid.New(), Quantity: 1})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// specs/016-hide-guard FR-006 edge case: a hidden variant's ACTIVE facts are
// absent from the variant-facts read exactly like a removed variant's, so adding
// it after the hide is refused with the shared PRODUCT_NOT_FOUND and no line is
// written.
func TestAddHiddenVariantAbsentIsNotFound(t *testing.T) {
	svc, repo, _, _ := newCartService(t)
	hidden := uuid.New()

	_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{VariantID: hidden, Quantity: 1})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("adding a hidden variant must be refused as not-found, got %v", err)
	}
	if len(repo.lines) != 0 {
		t.Fatalf("a refused add must write no line, got %+v", repo.lines)
	}
}

// FR-006: a non-positive quantity is refused before anything is written, naming
// the offending member.
func TestAddRejectsNonPositiveQuantity(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	for _, quantity := range []int64{0, -1} {
		_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{VariantID: variant, Quantity: quantity})
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
// PRODUCT_NOT_FOUND an unknown variant answers, so the route never confirms what
// is in another customer's cart.
func TestChangeOrRemoveAMissingLineIsNotFound(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	held := seedVariant(variants, availability, "Có", "C", 100000)
	missing := uuid.New()

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: held, Quantity: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{VariantID: missing, Quantity: 2}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("changing a missing line: expected ErrProductNotFound, got %v", err)
	}
	if err := svc.Remove(actorContext(user), appdto.RemoveItemInput{VariantID: missing}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("removing a missing line: expected ErrProductNotFound, got %v", err)
	}
}

// US2, FR-005: adding a variant that is not on sale is refused with
// CART_PRODUCT_NOT_PURCHASABLE, naming the variant, and no line is written.
func TestAddRefusesANotOnSaleVariant(t *testing.T) {
	svc, repo, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariantState(variants, availability, "Sắp ra mắt", "Sắp ra mắt", 100000, false)

	_, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 1})
	if !errors.Is(err, domainerr.ErrProductNotPurchasable) {
		t.Fatalf("expected ErrProductNotPurchasable, got %v", err)
	}
	var notPurchasable *domainerr.NotPurchasableError
	if !errors.As(err, &notPurchasable) {
		t.Fatalf("expected the typed refusal, got %v", err)
	}
	if notPurchasable.VariantID != variant {
		t.Fatalf("the refusal named %s, want %s", notPurchasable.VariantID, variant)
	}
	if len(repo.lines) != 0 {
		t.Fatalf("a refused add must write no line, got %+v", repo.lines)
	}
}

// US2, FR-007: a requested quantity above what is available is refused with
// CART_QUANTITY_EXCEEDS_AVAILABLE, naming quantity and the available amount; a
// quantity exactly equal to what is available is accepted. The refusal counts the
// quantity an add would produce on an existing line, not the added amount alone.
func TestAddRefusesAQuantityAboveAvailable(t *testing.T) {
	svc, repo, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)
	seedAvailable(availability, variant, 3)

	_, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 5})
	if !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("expected ErrQuantityExceedsAvailable, got %v", err)
	}
	var exceeds *domainerr.QuantityExceedsAvailableError
	if !errors.As(err, &exceeds) {
		t.Fatalf("expected the typed refusal, got %v", err)
	}
	if exceeds.Available != 3 || exceeds.Requested != 5 {
		t.Fatalf("the refusal must state available=3 requested=5, got %+v", exceeds)
	}
	if len(repo.lines) != 0 {
		t.Fatalf("a refused add must write no line, got %+v", repo.lines)
	}

	// The whole available amount is reachable.
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 3}); err != nil {
		t.Fatalf("adding exactly the available amount must succeed, got %v", err)
	}

	// Raising an existing line past availability is refused on the sum.
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 1}); !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("raising past availability: expected ErrQuantityExceedsAvailable, got %v", err)
	}
}

// US2, FR-005: changing a line's quantity is refused for a variant that has gone
// off sale since it was added, and the previous quantity is kept.
func TestChangeRefusesAnOffSaleVariant(t *testing.T) {
	svc, repo, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	markOffSale(variants, variant)

	_, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{VariantID: variant, Quantity: 5})
	if !errors.Is(err, domainerr.ErrProductNotPurchasable) {
		t.Fatalf("expected ErrProductNotPurchasable, got %v", err)
	}
	lines := repo.lines[repo.owners[user]]
	if len(lines) != 1 || lines[0].Quantity != 2 {
		t.Fatalf("a refused change must keep the previous quantity, got %+v", lines)
	}
}

// US2, FR-007: changing a line's quantity above what is now available is refused
// with CART_QUANTITY_EXCEEDS_AVAILABLE and the previous quantity is kept.
func TestChangeRefusesAQuantityAboveAvailable(t *testing.T) {
	svc, repo, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	seedAvailable(availability, variant, 2)

	_, err := svc.ChangeQuantity(actorContext(user), appdto.ChangeQuantityInput{VariantID: variant, Quantity: 5})
	if !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("expected ErrQuantityExceedsAvailable, got %v", err)
	}
	lines := repo.lines[repo.owners[user]]
	if len(lines) != 1 || lines[0].Quantity != 1 {
		t.Fatalf("a refused change must keep the previous quantity, got %+v", lines)
	}
}

// US2, FR-012: a read reports buyable=true for a line that is on sale with at
// least its quantity available, and reports no available quantity — there is no
// limit to show.
func TestReadReportsABuyableLine(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)
	seedAvailable(availability, variant, 3)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 3}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	line := view.Lines[0]
	if !line.Buyable {
		t.Fatalf("a fully available on-sale line must be buyable, got %+v", line)
	}
	if line.AvailableQuantity != nil {
		t.Fatalf("a buyable line must report no available quantity, got %d", *line.AvailableQuantity)
	}
}

// US2, FR-012, research D10: a line short of what is available is not buyable and
// reports the currently available quantity, so the customer can reduce it.
func TestReadReportsTheAvailableQuantityForAShortLine(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)
	seedAvailable(availability, variant, 3)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	seedAvailable(availability, variant, 1)

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	line := view.Lines[0]
	if line.Buyable {
		t.Fatalf("a short line must not be buyable, got %+v", line)
	}
	if line.AvailableQuantity == nil || *line.AvailableQuantity != 1 {
		t.Fatalf("a short line must report available=1, got %+v", line.AvailableQuantity)
	}
	if line.Quantity != 2 || line.UnitPrice.Amount != 100000 {
		t.Fatalf("a read must not change the line's quantity or captured price, got %+v", line)
	}
}

// US2, FR-012, research D10: an off-sale line is not buyable and reports no
// available quantity — there is no number to give, only the truth that it cannot
// be bought. Its variant still exists, so its product name is still shown.
func TestReadReportsNoQuantityForAnOffSaleLine(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	markOffSale(variants, variant)

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	line := view.Lines[0]
	if line.Buyable {
		t.Fatalf("an off-sale line must not be buyable, got %+v", line)
	}
	if line.AvailableQuantity != nil {
		t.Fatalf("an off-sale line must report no available quantity, got %d", *line.AvailableQuantity)
	}
	if line.ProductName == nil || *line.ProductName != "Tranh" {
		t.Fatalf("an off-sale variant still exists, so its product name must be shown, got %+v", line.ProductName)
	}
}

// US2, FR-012: a line whose variant was removed is not buyable, reports no
// product name, no variant name and no available quantity, and is never silently
// dropped.
func TestReadReportsNoNameOrQuantityForAGoneLine(t *testing.T) {
	svc, _, variants, availability := newCartService(t)
	user := uuid.New()
	variant := seedVariant(variants, availability, "Tranh", "Khổ lớn", 100000)

	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: variant, Quantity: 2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	removeVariant(variants, variant)

	view, err := svc.Get(actorContext(user), appdto.GetCartInput{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("a read must never silently drop a line, got %d lines", len(view.Lines))
	}
	line := view.Lines[0]
	if line.Buyable {
		t.Fatalf("a gone line must not be buyable, got %+v", line)
	}
	if line.ProductName != nil || line.VariantName != nil {
		t.Fatalf("a gone line must report no product name and no variant name, got product=%v variant=%v", line.ProductName, line.VariantName)
	}
	if line.AvailableQuantity != nil {
		t.Fatalf("a gone line must report no available quantity, got %d", *line.AvailableQuantity)
	}
}

// US1, FR-006: adding without choosing a variant is refused. The zero identifier
// names no variant, so it is the shared not-found rather than an accepted line,
// and nothing is written.
func TestAddWithoutAVariantIsRefused(t *testing.T) {
	svc, repo, _, _ := newCartService(t)

	_, err := svc.Add(actorContext(uuid.New()), appdto.AddItemInput{VariantID: uuid.Nil, Quantity: 1})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("adding without a variant must be refused as not-found, got %v", err)
	}
	if len(repo.lines) != 0 {
		t.Fatalf("a refused add must write no line, got %+v", repo.lines)
	}
}

// US1, FR-007: adding a variant that is entirely out of stock (nothing available)
// is refused with CART_QUANTITY_EXCEEDS_AVAILABLE and writes no line. A sibling
// variant that is on sale is unaffected.
func TestAddRefusesAnOutOfStockVariant(t *testing.T) {
	svc, repo, variants, availability := newCartService(t)
	user := uuid.New()
	soldOut := seedVariant(variants, availability, "Tranh", "Hết hàng", 100000)
	seedAvailable(availability, soldOut, 0)
	onSale := seedVariant(variants, availability, "Tranh", "Còn hàng", 100000)

	_, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: soldOut, Quantity: 1})
	if !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("adding an out-of-stock variant must be refused, got %v", err)
	}
	if len(repo.lines) != 0 {
		t.Fatalf("a refused add must write no line, got %+v", repo.lines)
	}

	// The sibling variant still adds: the refusal is per variant (FR-007).
	if _, err := svc.Add(actorContext(user), appdto.AddItemInput{VariantID: onSale, Quantity: 1}); err != nil {
		t.Fatalf("an on-sale sibling must still add, got %v", err)
	}
}
