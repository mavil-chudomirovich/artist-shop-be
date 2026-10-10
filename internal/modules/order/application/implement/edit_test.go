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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of US3: EditMine replaces an editable order's
// lines (and optionally its address), re-snapshots each line's current price,
// recomputes the total, bumps the content version and writes an edit-history row,
// all in one transaction. Editing an order awaiting the artist leaves it awaiting
// the artist and touches no stock; editing an awaiting-payment order releases its
// hold exactly once and returns it to awaiting confirmation. Every refusal — not
// editable, empty, an invalid line, a foreign address — leaves the order exactly
// as it was (FR-012 to FR-017, SC-003). It runs the real edit use case over
// in-memory fakes of every contract, so the answers asserted are the service's own.

// editOrders is the in-memory OrderRepository the edit use case uses. It holds
// orders by identifier, answers the row lock, and records every accepted write —
// the replaced order and the edit-history row — so a test can prove what the edit
// persisted and that a refusal persisted nothing. Embedding the interface makes any
// method the use case drifts into panic loudly rather than silently pass.
type editOrders struct {
	appinterface.OrderRepository
	orders   map[uuid.UUID]*model.Order
	locked   []uuid.UUID
	replaced []*model.Order
	history  []model.EditHistory
}

func (r *editOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.orders[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	r.locked = append(r.locked, id)
	return order, nil
}

func (r *editOrders) ReplaceLines(_ context.Context, order *model.Order, _ time.Time) error {
	r.replaced = append(r.replaced, order)
	if stored, ok := r.orders[order.ID]; ok {
		stored.Lines = order.Lines
		stored.Total = order.Total
		stored.Address = order.Address
		stored.Status = order.Status
		stored.Version = order.Version
		stored.ConfirmedAt = order.ConfirmedAt
		stored.PaymentExpiresAt = order.PaymentExpiresAt
	}
	return nil
}

func (r *editOrders) InsertEditHistory(_ context.Context, record model.EditHistory) error {
	r.history = append(r.history, record)
	return nil
}

// editFixture is the real edit use case over in-memory fakes.
type editFixture struct {
	svc          *Service
	orders       *editOrders
	catalog      *fakeCatalog
	availability *fakeAvailability
	reservations *fakeReservation
	customers    *fakeCustomers
}

func newEditFixture(store *editOrders) *editFixture {
	catalog := &fakeCatalog{products: map[uuid.UUID]contracts.ProductSummary{}}
	availability := &fakeAvailability{available: map[uuid.UUID]int64{}}
	reservations := &fakeReservation{}
	customers := &fakeCustomers{}
	svc := New(Service{
		Orders:       store,
		Products:     catalog,
		Availability: availability,
		Reservations: reservations,
		Customers:    customers,
		Tx:           passthroughTx{},
		Clock:        memoryClock{},
		Mapper:       mapper.New(),
	})
	return &editFixture{
		svc:          svc,
		orders:       store,
		catalog:      catalog,
		availability: availability,
		reservations: reservations,
		customers:    customers,
	}
}

// editOrder builds an order awaiting the artist over the given lines, using the
// domain constructor so its identifier, line links and total are the same ones a
// checkout would produce.
func editOrder(owner uuid.UUID, lines ...model.OrderLine) *model.Order {
	return model.NewOrder(owner, model.Address{
		RecipientName: "Nguyễn Văn A",
		StreetAddress: "1 Đinh Tiên Hoàng",
	}, lines, fixedNow)
}

// confirmEditOrder marks an order as confirmed and awaiting payment, the state an
// edit returns to awaiting confirmation (research D7).
func confirmEditOrder(order *model.Order) {
	confirmed := fixedNow
	expires := fixedNow.Add(holdWindow)
	order.Status = constant.StatusPaymentPending
	order.Version = 2
	order.ConfirmedAt = &confirmed
	order.PaymentExpiresAt = &expires
}

// seedEditProduct puts one on-sale product in the fake catalogue with the given
// shelf and returns its identifier.
func seedEditProduct(f *editFixture, name, slug string, amount, available int64) uuid.UUID {
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

// FR-012, FR-013, FR-015, FR-017: editing an order awaiting the artist replaces
// its lines, re-snapshots each product's current name, slug and price, recomputes
// the total, bumps the content version and records an edit-history row — and it
// stays awaiting the artist, touching no stock.
func TestEditPendingReplacesLinesResnapshotsPricesAndStaysPending(t *testing.T) {
	owner := uuid.New()
	kept := uuid.New()
	added := uuid.New()
	order := editOrder(owner, line(kept, 1000, 2))
	f := newEditFixture(&editOrders{orders: map[uuid.UUID]*model.Order{order.ID: order}})

	// The catalogue now prices the kept product differently and carries a second
	// one — an edit re-takes the current price rather than refusing it.
	f.catalog.products[kept] = contracts.ProductSummary{
		ID: kept, Name: "Tranh mới", Slug: "tranh-moi", OnSale: true,
		Price: contracts.ProductPrice{Amount: 1500, Currency: "VND"},
	}
	f.availability.available[kept] = 10
	f.catalog.products[added] = contracts.ProductSummary{
		ID: added, Name: "Sản phẩm B", Slug: "san-pham-b", OnSale: true,
		Price: contracts.ProductPrice{Amount: 2000, Currency: "VND"},
	}
	f.availability.available[added] = 10

	view, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines: []appdto.EditLineInput{
			{ProductID: kept, Quantity: 3},
			{ProductID: added, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("EditMine: %v", err)
	}

	if view.Status != constant.StatusPending {
		t.Fatalf("editing an order awaiting the artist must leave it awaiting the artist, got %s", view.Status)
	}
	if view.Total.Amount != 3*1500+1*2000 || view.Total.Currency != "VND" {
		t.Fatalf("the total must be recomputed from the current prices, got %+v", view.Total)
	}
	if len(view.Lines) != 2 {
		t.Fatalf("expected two lines, got %+v", view.Lines)
	}
	if view.Lines[0].ProductID != kept || view.Lines[0].Name != "Tranh mới" ||
		view.Lines[0].Slug != "tranh-moi" || view.Lines[0].UnitPrice.Amount != 1500 ||
		view.Lines[0].Quantity != 3 {
		t.Fatalf("the kept line must be re-snapshotted at its current values, got %+v", view.Lines[0])
	}
	if view.Lines[1].ProductID != added || view.Lines[1].Name != "Sản phẩm B" || view.Lines[1].UnitPrice.Amount != 2000 {
		t.Fatalf("the added line must carry the product's snapshot, got %+v", view.Lines[1])
	}
	if view.Address.RecipientName != "Nguyễn Văn A" || view.Address.StreetAddress != "1 Đinh Tiên Hoàng" {
		t.Fatalf("an edit with no addressId must keep the current address, got %+v", view.Address)
	}
	if order.Version != 2 {
		t.Fatalf("an accepted edit must bump the content version, got %d", order.Version)
	}
	if len(f.reservations.releases) != 0 {
		t.Fatalf("editing an order awaiting the artist must not touch stock, got %+v", f.reservations.releases)
	}
	if len(f.orders.replaced) != 1 {
		t.Fatalf("the edit must persist through ReplaceLines once, got %d", len(f.orders.replaced))
	}
	if len(f.orders.history) != 1 {
		t.Fatalf("an accepted edit must record one edit-history row, got %d", len(f.orders.history))
	}
	history := f.orders.history[0]
	if history.OrderID != order.ID || history.Version != 2 || history.ActorID != owner {
		t.Fatalf("the edit-history row must name the order, the new version and the actor, got %+v", history)
	}
	if len(history.Before.Lines) != 1 || history.Before.Lines[0].UnitPrice.Amount != 1000 || len(history.After.Lines) != 2 {
		t.Fatalf("the edit history must record the content before and after, got before=%+v after=%+v", history.Before, history.After)
	}
}

// FR-012, FR-015: an edit that names a saved address replaces the delivery
// address; a foreign one is refused (asserted separately).
func TestEditAppliesTheNamedAddress(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 1))
	store.orders[order.ID] = order

	second := contracts.CustomerAddress{
		ID: uuid.New(), RecipientName: "Trần Thị B", RecipientPhone: "0987654321",
		ProvinceCode: "02", ProvinceName: "Hải Phòng", WardCode: "00002", WardName: "X",
		StreetAddress: "2 Lê Lợi",
	}
	f.customers.customer = contracts.Customer{ID: owner, Addresses: []contracts.CustomerAddress{second}}

	view, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID:   order.ID,
		AddressID: &second.ID,
		Lines:     []appdto.EditLineInput{{ProductID: product, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("EditMine: %v", err)
	}
	if view.Address.RecipientName != second.RecipientName || view.Address.StreetAddress != second.StreetAddress {
		t.Fatalf("the named address must be applied, got %+v want %+v", view.Address, second)
	}
}

// FR-014, SC-003, research D7: editing an awaiting-payment order releases its hold
// exactly once, clears its confirmation instant and deadline, bumps the version
// and returns it to awaiting confirmation, recording the edit.
func TestEditPaymentPendingReleasesHoldClearsDeadlineAndReturnsToPending(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 2))
	confirmEditOrder(order)
	store.orders[order.ID] = order

	view, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{ProductID: product, Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("EditMine: %v", err)
	}

	if view.Status != constant.StatusPending {
		t.Fatalf("editing an awaiting-payment order must return it to awaiting confirmation, got %s", view.Status)
	}
	if len(f.reservations.releases) != 1 {
		t.Fatalf("the old hold must be released exactly once, got %+v", f.reservations.releases)
	}
	if release := f.reservations.releases[0]; release.orderID != order.ID || release.productID != product {
		t.Fatalf("the release must name the order and its line, got %+v", release)
	}
	if order.ConfirmedAt != nil || order.PaymentExpiresAt != nil {
		t.Fatalf("the edit must clear the confirmation instant and deadline, got %v/%v", order.ConfirmedAt, order.PaymentExpiresAt)
	}
	if order.Version != 3 {
		t.Fatalf("the edit must bump the version from 2 to 3, got %d", order.Version)
	}
	if len(f.orders.history) != 1 || f.orders.history[0].Version != 3 {
		t.Fatalf("the edit must record a history row at the new version, got %+v", f.orders.history)
	}
	if view.Total.Amount != 4000 {
		t.Fatalf("the total must be recomputed, got %+v", view.Total)
	}
}

// FR-016: an order that is paid or beyond cannot be edited; the refusal leaves it
// exactly as it was and writes nothing.
func TestEditRefusesAnOrderThatIsNotEditable(t *testing.T) {
	owner := uuid.New()
	product := uuid.New()
	order := editOrder(owner, line(product, 1000, 1))
	order.Status = constant.StatusPaid
	order.Version = 2
	store := &editOrders{orders: map[uuid.UUID]*model.Order{order.ID: order}}
	f := newEditFixture(store)
	seedEditProduct(f, "Tranh", "tranh", 1000, 10)

	_, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{ProductID: product, Quantity: 2}},
	})
	if !errors.Is(err, domainerr.ErrNotEditable) {
		t.Fatalf("expected ErrNotEditable, got %v", err)
	}
	if order.Status != constant.StatusPaid || order.Version != 2 {
		t.Fatalf("a refused edit must leave the order untouched, got %s v%d", order.Status, order.Version)
	}
	if len(f.orders.replaced) != 0 || len(f.orders.history) != 0 || len(f.reservations.releases) != 0 {
		t.Fatalf("a refused edit must write nothing, got replaced=%d history=%d releases=%d",
			len(f.orders.replaced), len(f.orders.history), len(f.reservations.releases))
	}
}

// FR-015: an edit that would leave the order with no line is refused.
func TestEditRefusesAnEmptyLineSet(t *testing.T) {
	owner := uuid.New()
	product := uuid.New()
	order := editOrder(owner, line(product, 1000, 1))
	f := newEditFixture(&editOrders{orders: map[uuid.UUID]*model.Order{order.ID: order}})

	_, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{OrderID: order.ID, Lines: nil})
	if !errors.Is(err, domainerr.ErrEmptyOrder) {
		t.Fatalf("expected ErrEmptyOrder, got %v", err)
	}
	if order.Status != constant.StatusPending || order.Version != 1 {
		t.Fatalf("a refused empty edit must leave the order untouched, got %s v%d", order.Status, order.Version)
	}
	if len(f.orders.replaced) != 0 || len(f.orders.history) != 0 {
		t.Fatalf("a refused empty edit must write nothing, got replaced=%d history=%d",
			len(f.orders.replaced), len(f.orders.history))
	}
}

// FR-015: an edited line whose product is off sale or removed is refused naming
// the item, and one above what is available is refused naming the item and the
// remaining amount; nothing is written either way.
func TestEditRefusesAnInvalidLine(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	kept := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	offSale := uuid.New()
	short := seedEditProduct(f, "Ít hàng", "it-hang", 1000, 2)
	order := editOrder(owner, line(kept, 1000, 1))
	store.orders[order.ID] = order
	f.catalog.products[offSale] = contracts.ProductSummary{ID: offSale, Name: "Ngừng bán", Slug: "ngung-ban", OnSale: false}
	f.availability.available[offSale] = 10

	_, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{ProductID: offSale, Quantity: 1}},
	})
	var notPurchasable *domainerr.ItemNotPurchasableError
	if !errors.As(err, &notPurchasable) || notPurchasable.ProductID != offSale {
		t.Fatalf("an off-sale line must refuse the edit naming the item, got %v", err)
	}

	_, err = f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{ProductID: short, Quantity: 5}},
	})
	var exceeds *domainerr.QuantityExceedsAvailableError
	if !errors.As(err, &exceeds) || exceeds.ProductID != short || exceeds.Available != 2 || exceeds.Requested != 5 {
		t.Fatalf("a line above available must refuse the edit naming the item and amounts, got %v", err)
	}

	if order.Status != constant.StatusPending || order.Version != 1 {
		t.Fatalf("a refused edit must leave the order untouched, got %s v%d", order.Status, order.Version)
	}
	if len(f.orders.replaced) != 0 || len(f.orders.history) != 0 {
		t.Fatalf("a refused edit must write nothing, got replaced=%d history=%d",
			len(f.orders.replaced), len(f.orders.history))
	}
}

// FR-012, error-codes.md: an `addressId` that is not one of the customer's
// addresses is a request-shape refusal naming the member, leaving the order
// unchanged.
func TestEditRefusesAForeignAddressID(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 1))
	store.orders[order.ID] = order
	f.customers.customer = contracts.Customer{ID: owner, Addresses: []contracts.CustomerAddress{{ID: uuid.New(), RecipientName: "A"}}}
	foreign := uuid.New()

	_, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID:   order.ID,
		AddressID: &foreign,
		Lines:     []appdto.EditLineInput{{ProductID: product, Quantity: 1}},
	})
	var invalid *domainerr.InvalidValueError
	if !errors.As(err, &invalid) || invalid.Field != model.FieldAddressID {
		t.Fatalf("a foreign addressId must be refused naming addressId, got %v", err)
	}
	if len(f.orders.replaced) != 0 || len(f.orders.history) != 0 {
		t.Fatalf("a refused edit must write nothing, got replaced=%d history=%d",
			len(f.orders.replaced), len(f.orders.history))
	}
}

// FR-022: another customer's order answers the same not-found an unknown one does,
// so the route never confirms whose order it is.
func TestEditAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	owner := uuid.New()
	intruder := uuid.New()
	product := uuid.New()
	order := editOrder(owner, line(product, 1000, 1))
	store := &editOrders{orders: map[uuid.UUID]*model.Order{order.ID: order}}
	f := newEditFixture(store)
	seedEditProduct(f, "Tranh", "tranh", 1000, 10)

	_, err := f.svc.EditMine(actorContext(intruder), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{ProductID: product, Quantity: 1}},
	})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("another customer's order must answer not-found, got %v", err)
	}
	if len(f.orders.replaced) != 0 || len(f.orders.history) != 0 {
		t.Fatalf("a refused edit must write nothing, got replaced=%d history=%d",
			len(f.orders.replaced), len(f.orders.history))
	}
}
