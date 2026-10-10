package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file is the use-case contract of US1: a successful checkout snapshots each
// cart line — including the link segment — and the delivery address, creates the
// order awaiting the artist holding nothing, and clears the cart in one
// transaction, and every refusal — empty cart, an off-sale or removed product, a
// price that moved, a quantity above what is available, and a customer with no
// address — leaves nothing created and the cart untouched (FR-001 to FR-007,
// SC-002). It runs the real checkout over in-memory fakes of every contract, so
// the answers asserted are the service's own.

// fixedNow is the deterministic instant the fixture stamps orders with.
var fixedNow = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

// holdWindow is the fixture's fixed hold window; the order's expires_at is
// derived from it (FR-016).
const holdWindow = 15 * time.Minute

// fakeCart answers the CartCheckout contract from a fixed cart and records the
// clear, so a test can prove the cart was emptied (FR-007).
type fakeCart struct {
	lines    []contracts.CartLine
	linesErr error
	clearErr error
	cleared  bool
	clearFor uuid.UUID
}

func (f *fakeCart) CartLines(_ context.Context, _ uuid.UUID) ([]contracts.CartLine, error) {
	if f.linesErr != nil {
		return nil, f.linesErr
	}
	return f.lines, nil
}

func (f *fakeCart) ClearCart(_ context.Context, userID uuid.UUID) error {
	f.cleared = true
	f.clearFor = userID
	return f.clearErr
}

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

// fakeAvailability answers the InventoryAvailability contract from a mutable map,
// so a test can set a shelf and later lower it.
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

// reserveCall records one hold taken for an order.
type reserveCall struct {
	orderID   uuid.UUID
	productID uuid.UUID
	quantity  int64
}

// saleCall records one hold consumed into a sale, with the per-line source
// reference the payment event produced (FR-014, research D5).
type saleCall struct {
	orderID   uuid.UUID
	productID uuid.UUID
	reference string
}

// releaseCall records one hold returned to availability (FR-015).
type releaseCall struct {
	orderID   uuid.UUID
	productID uuid.UUID
}

// fakeReservation answers the InventoryReservation contract. It records every
// hold, sale and release so a test can prove which lines were touched and that
// the same line's effect is applied exactly once.
type fakeReservation struct {
	calls    []reserveCall
	sales    []saleCall
	releases []releaseCall
	window   time.Duration
}

func (f *fakeReservation) Reserve(_ context.Context, orderID, productID uuid.UUID, quantity int64) error {
	f.calls = append(f.calls, reserveCall{orderID: orderID, productID: productID, quantity: quantity})
	return nil
}

func (f *fakeReservation) Release(_ context.Context, orderID, productID uuid.UUID) error {
	f.releases = append(f.releases, releaseCall{orderID: orderID, productID: productID})
	return nil
}

func (f *fakeReservation) ApplySale(_ context.Context, orderID, productID uuid.UUID, reference string) error {
	f.sales = append(f.sales, saleCall{orderID: orderID, productID: productID, reference: reference})
	return nil
}

func (f *fakeReservation) HoldWindow() time.Duration {
	if f.window == 0 {
		return holdWindow
	}
	return f.window
}

// fakeCustomers answers the CustomerLookupService contract.
type fakeCustomers struct {
	customer contracts.Customer
	err      error
}

func (f *fakeCustomers) LookupCustomer(_ context.Context, _ uuid.UUID) (contracts.Customer, error) {
	if f.err != nil {
		return contracts.Customer{}, f.err
	}
	return f.customer, nil
}

// memoryOrders records every created order. It embeds the repository interface so
// only Create needs implementing; any other method panics loudly if the use case
// drifts.
type memoryOrders struct {
	appinterface.OrderRepository
	created []*model.Order
}

func (r *memoryOrders) Create(_ context.Context, order *model.Order) error {
	r.created = append(r.created, order)
	return nil
}

// memoryTx is the in-memory UnitOfWork. It runs the function and, on error,
// discards whatever orders the function recorded before it failed, so a test can
// observe the same "nothing is created when the checkout refuses" the real
// transaction produces — including when the order was written before a hold that
// could not be taken (FR-017).
type memoryTx struct{ orders *memoryOrders }

func (t memoryTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	before := len(t.orders.created)
	if err := fn(ctx); err != nil {
		t.orders.created = t.orders.created[:before]
		return err
	}
	return nil
}

// memoryClock is the injected Clock.
type memoryClock struct{}

func (memoryClock) Now() time.Time { return fixedNow }

// checkoutFixture is the real checkout over in-memory fakes.
type checkoutFixture struct {
	svc          *Service
	cart         *fakeCart
	catalog      *fakeCatalog
	availability *fakeAvailability
	reservations *fakeReservation
	customers    *fakeCustomers
	orders       *memoryOrders
}

func newCheckoutFixture(t *testing.T) *checkoutFixture {
	t.Helper()
	cart := &fakeCart{}
	catalog := &fakeCatalog{products: map[uuid.UUID]contracts.ProductSummary{}}
	availability := &fakeAvailability{available: map[uuid.UUID]int64{}}
	reservations := &fakeReservation{}
	customers := &fakeCustomers{}
	orders := &memoryOrders{}
	svc := New(Service{
		Orders:       orders,
		Carts:        cart,
		Products:     catalog,
		Availability: availability,
		Reservations: reservations,
		Customers:    customers,
		Tx:           memoryTx{orders: orders},
		Clock:        memoryClock{},
		Mapper:       mapper.New(),
	})
	return &checkoutFixture{
		svc:          svc,
		cart:         cart,
		catalog:      catalog,
		availability: availability,
		reservations: reservations,
		customers:    customers,
		orders:       orders,
	}
}

// actorContext puts the acting customer in the context the way presentation does
// from the session. The owner is never part of a use-case input (FR-020).
func actorContext(userID uuid.UUID) context.Context {
	return appinterface.WithActor(context.Background(), appinterface.Actor{ID: userID, Role: access.RoleCustomer})
}

// seedProduct puts one on-sale product in the fake catalogue with the given shelf
// and returns its identifier.
func seedProduct(f *checkoutFixture, name, slug string, amount, available int64) uuid.UUID {
	id := uuid.New()
	f.catalog.products[id] = contracts.ProductSummary{
		ID:     id,
		Name:   name,
		Slug:   slug,
		OnSale: true,
		Price:  contracts.ProductPrice{Amount: amount, Currency: "VND"},
	}
	f.availability.available[id] = available
	return id
}

// seedAddress gives the customer one address and returns it.
func seedAddress(f *checkoutFixture, userID uuid.UUID) contracts.CustomerAddress {
	address := contracts.CustomerAddress{
		ID:             uuid.New(),
		RecipientName:  "Nguyễn Văn A",
		RecipientPhone: "0912345678",
		ProvinceCode:   "01",
		ProvinceName:   "Hà Nội",
		WardCode:       "00001",
		WardName:       "Phúc Xá",
		StreetAddress:  "1 Đinh Tiên Hoàng",
		IsDefault:      true,
	}
	f.customers.customer = contracts.Customer{ID: userID, Addresses: []contracts.CustomerAddress{address}}
	return address
}

// assertCartUntouched proves a refused checkout left the cart exactly as the
// customer left it: it was not cleared and still holds every one of its lines
// (SC-002). It compares against a copy taken before the attempt, so a regression
// that emptied or rewrote the cart on refusal is caught.
func assertCartUntouched(t *testing.T, f *checkoutFixture, want []contracts.CartLine) {
	t.Helper()
	if f.cart.cleared {
		t.Fatal("a refused checkout must leave the cart unchanged: it was cleared")
	}
	if len(f.cart.lines) != len(want) {
		t.Fatalf("a refused checkout must leave the cart's lines unchanged: got %d lines, want %d", len(f.cart.lines), len(want))
	}
	for i := range want {
		if f.cart.lines[i] != want[i] {
			t.Fatalf("a refused checkout must leave line %d unchanged: got %+v, want %+v", i, f.cart.lines[i], want[i])
		}
	}
}

// FR-001, FR-002, FR-003, FR-007: a successful checkout snapshots every line —
// including the link segment, which the fixtures deliberately set apart from the
// name so a dropped slug cannot pass — and the address, creates the order
// awaiting the artist holding nothing, clears the cart and returns the order in
// PENDING (FR-001, FR-002, FR-003, FR-007).
func TestCheckoutSnapshotsLinesAndAddressAndHoldsNothing(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	address := seedAddress(f, user)
	const firstSlug = "tranh-son-dau"
	const secondSlug = "silk-scroll"
	first := seedProduct(f, "Tranh sơn dầu", firstSlug, 120000, 5)
	second := seedProduct(f, "Silk scroll", secondSlug, 33333, 5)
	f.cart.lines = []contracts.CartLine{
		{ProductID: first, Quantity: 2, UnitPriceAmount: 120000, Currency: "VND"},
		{ProductID: second, Quantity: 3, UnitPriceAmount: 33333, Currency: "VND"},
	}

	view, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	if view.ID == uuid.Nil || view.Status != "PENDING" {
		t.Fatalf("expected an order awaiting the artist's confirmation, got %+v", view)
	}
	if view.Total.Amount != 2*120000+3*33333 || view.Total.Currency != "VND" {
		t.Fatalf("total = %+v, want the exact sum 339999 VND", view.Total)
	}
	if view.ItemCount != 2 || len(view.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %+v", view.Lines)
	}
	if view.Lines[0].Name != "Tranh sơn dầu" || view.Lines[0].Slug != firstSlug ||
		view.Lines[0].UnitPrice.Amount != 120000 ||
		view.Lines[0].LineTotal.Amount != 240000 || view.Lines[0].Quantity != 2 {
		t.Fatalf("first line snapshot = %+v", view.Lines[0])
	}
	if view.Lines[1].Name != "Silk scroll" || view.Lines[1].Slug != secondSlug ||
		view.Lines[1].UnitPrice.Amount != 33333 ||
		view.Lines[1].LineTotal.Amount != 99999 {
		t.Fatalf("second line snapshot = %+v", view.Lines[1])
	}
	if view.Address.RecipientName != address.RecipientName || view.Address.StreetAddress != address.StreetAddress ||
		view.Address.ProvinceCode != address.ProvinceCode {
		t.Fatalf("address snapshot = %+v, want %+v", view.Address, address)
	}

	if len(f.orders.created) != 1 {
		t.Fatalf("expected one order created, got %d", len(f.orders.created))
	}
	created := f.orders.created[0]
	if created.UserID != user {
		t.Fatalf("owner = %s, want the session's %s", created.UserID, user)
	}
	if created.Version != 1 {
		t.Fatalf("a fresh order must carry version 1, got %d", created.Version)
	}
	if created.ConfirmedAt != nil || created.PaymentExpiresAt != nil {
		t.Fatalf("a fresh order must carry no confirmation instant and no deadline, got %v/%v", created.ConfirmedAt, created.PaymentExpiresAt)
	}
	for i, line := range created.Lines {
		if line.OrderID != created.ID {
			t.Errorf("line %d is not linked to the order", i)
		}
	}

	// FR-001, FR-002: checkout holds nothing — the goods are set aside only when
	// the artist confirms, so no line may reach module 05 here.
	if len(f.reservations.calls) != 0 {
		t.Fatalf("checkout must hold nothing, got %+v", f.reservations.calls)
	}

	if !f.cart.cleared || f.cart.clearFor != user {
		t.Fatalf("the cart must be emptied for the owner, got cleared=%v for=%s", f.cart.cleared, f.cart.clearFor)
	}
}

// FR-003: the address the customer names is the one snapshotted.
func TestCheckoutUsesTheNamedAddress(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	defaultAddress := seedAddress(f, user)
	named := contracts.CustomerAddress{
		ID: uuid.New(), RecipientName: "Trần Thị B", RecipientPhone: "0987654321",
		ProvinceCode: "02", ProvinceName: "Hải Phòng", WardCode: "00002", WardName: "X",
		StreetAddress: "2 Lê Lợi",
	}
	f.customers.customer.Addresses = append(f.customers.customer.Addresses, named)
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}

	view, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{AddressID: &named.ID})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if view.Address.RecipientName != named.RecipientName {
		t.Fatalf("the named address must be snapshotted, got %+v want %+v", view.Address, named)
	}
	if view.Address.RecipientName == defaultAddress.RecipientName {
		t.Fatal("the default address must not be used when one is named")
	}
}

// FR-006: an empty cart is refused and nothing is created.
func TestCheckoutRefusesAnEmptyCart(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if !errors.Is(err, domainerr.ErrCartEmpty) {
		t.Fatalf("expected ErrCartEmpty, got %v", err)
	}
	if len(f.orders.created) != 0 || f.cart.cleared {
		t.Fatalf("a refused checkout must create nothing and clear nothing: %d orders, cleared=%v", len(f.orders.created), f.cart.cleared)
	}
}

// FR-004: an off-sale product refuses the checkout and names the item.
func TestCheckoutRefusesAnOffSaleProduct(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	product := seedProduct(f, "Sắp ra mắt", "sap-ra-mat", 100000, 5)
	f.catalog.products[product] = contracts.ProductSummary{ID: product, Name: "Sắp ra mắt", Slug: "sap-ra-mat", OnSale: false}
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
	want := append([]contracts.CartLine(nil), f.cart.lines...)

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if !errors.Is(err, domainerr.ErrItemNotPurchasable) {
		t.Fatalf("expected ErrItemNotPurchasable, got %v", err)
	}
	var refusal *domainerr.ItemNotPurchasableError
	if !errors.As(err, &refusal) || refusal.ProductID != product {
		t.Fatalf("the refusal must name the product, got %v", err)
	}
	if len(f.orders.created) != 0 {
		t.Fatal("a refused checkout must create nothing")
	}
	assertCartUntouched(t, f, want)
}

// FR-004, edge case: a product removed from the catalogue refuses the checkout.
func TestCheckoutRefusesARemovedProduct(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	delete(f.catalog.products, product)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
	want := append([]contracts.CartLine(nil), f.cart.lines...)

	if _, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{}); !errors.Is(err, domainerr.ErrItemNotPurchasable) {
		t.Fatalf("expected ErrItemNotPurchasable for a removed product, got %v", err)
	}
	assertCartUntouched(t, f, want)
}

// FR-004: a price that moved since the customer saw it refuses the whole
// checkout and names the line; nothing is silently re-priced.
func TestCheckoutRefusesAChangedPrice(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	product := seedProduct(f, "Tranh", "tranh", 150000, 5)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
	want := append([]contracts.CartLine(nil), f.cart.lines...)

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if !errors.Is(err, domainerr.ErrItemPriceChanged) {
		t.Fatalf("expected ErrItemPriceChanged, got %v", err)
	}
	var refusal *domainerr.ItemPriceChangedError
	if !errors.As(err, &refusal) || refusal.ProductID != product {
		t.Fatalf("the refusal must name the product, got %v", err)
	}
	if len(f.orders.created) != 0 {
		t.Fatal("a refused checkout must create nothing")
	}
	assertCartUntouched(t, f, want)
}

// FR-005: a quantity above what is available refuses the checkout and names the
// available amount.
func TestCheckoutRefusesAQuantityAboveAvailable(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	product := seedProduct(f, "Tranh", "tranh", 100000, 2)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 5, UnitPriceAmount: 100000, Currency: "VND"}}
	want := append([]contracts.CartLine(nil), f.cart.lines...)

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("expected ErrQuantityExceedsAvailable, got %v", err)
	}
	var refusal *domainerr.QuantityExceedsAvailableError
	if !errors.As(err, &refusal) {
		t.Fatalf("expected the typed refusal, got %v", err)
	}
	if refusal.ProductID != product || refusal.Available != 2 || refusal.Requested != 5 {
		t.Fatalf("the refusal must name the item and the amounts, got %+v", refusal)
	}
	if len(f.orders.created) != 0 {
		t.Fatal("a refused checkout must create nothing")
	}
	assertCartUntouched(t, f, want)
}

// FR-003: a customer with no delivery address is refused.
func TestCheckoutRefusesACustomerWithNoAddress(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	f.customers.customer = contracts.Customer{ID: user}
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
	want := append([]contracts.CartLine(nil), f.cart.lines...)

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if !errors.Is(err, domainerr.ErrNoAddress) {
		t.Fatalf("expected ErrNoAddress, got %v", err)
	}
	if len(f.orders.created) != 0 {
		t.Fatal("a refused checkout must create nothing")
	}
	assertCartUntouched(t, f, want)
}

// error-codes.md: an `addressId` that is not one of the customer's addresses is a
// request-shape refusal naming the member.
func TestCheckoutRefusesAForeignAddressID(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
	foreign := uuid.New()

	_, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{AddressID: &foreign})
	var invalid *domainerr.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidValueError, got %v", err)
	}
	if invalid.Field != model.FieldAddressID {
		t.Fatalf("the refusal named %q, want %q", invalid.Field, model.FieldAddressID)
	}
	if len(f.orders.created) != 0 {
		t.Fatal("a refused checkout must create nothing")
	}
}
