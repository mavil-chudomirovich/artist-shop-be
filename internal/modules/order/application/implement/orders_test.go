package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of US3: a customer lists and reads only
// their own orders, reads another customer's order as not-found, and cancels an
// order that is still awaiting payment — which returns its goods — while a paid
// order is refused. It runs the real use cases over in-memory fakes of the
// repository and the inventory reservation contract, so the answers asserted are
// the service's own (FR-018 to FR-020).

// ordersStore is the in-memory OrderRepository the customer-order use cases use.
// FindByOwner answers only when the row belongs to the owner, and ListOwner
// filters the seeded, newest-first rows by owner, so a cross-account read can
// only answer the same not-found the real adapter answers. It embeds the
// repository interface so any method the use cases drift into panics loudly
// rather than silently passing.
type ordersStore struct {
	appinterface.OrderRepository
	byID    map[uuid.UUID]*model.Order
	ordered []model.OrderSummary
}

func (r *ordersStore) FindByOwner(_ context.Context, ownerID, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok || order.UserID != ownerID {
		return nil, domainerr.ErrNotFound
	}
	return order, nil
}

func (r *ordersStore) ListOwner(_ context.Context, ownerID uuid.UUID, page, size int) ([]model.OrderSummary, int64, error) {
	mine := make([]model.OrderSummary, 0, len(r.ordered))
	for _, summary := range r.ordered {
		if summary.UserID == ownerID {
			mine = append(mine, summary)
		}
	}
	total := int64(len(mine))
	start := (page - 1) * size
	if start > len(mine) {
		start = len(mine)
	}
	end := start + size
	if end > len(mine) {
		end = len(mine)
	}
	return mine[start:end], total, nil
}

func (r *ordersStore) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	return order, nil
}

func (r *ordersStore) UpdateStatus(_ context.Context, id uuid.UUID, status constant.Status, _ time.Time) error {
	if order, ok := r.byID[id]; ok {
		order.Status = status
	}
	return nil
}

// ordersFixture is the real customer-order use cases over in-memory fakes.
type ordersFixture struct {
	svc          *Service
	store        *ordersStore
	reservations *fakeReservation
}

// newOrdersFixture seeds the repository with the orders in the order supplied —
// the newest-first order the real ListOwner returns — and builds the use cases
// over the shared in-memory UnitOfWork, clock and reservation fake.
func newOrdersFixture(orders ...*model.Order) *ordersFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	summaries := make([]model.OrderSummary, 0, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
		summaries = append(summaries, model.OrderSummary{
			ID:        order.ID,
			UserID:    order.UserID,
			Status:    order.Status,
			Total:     order.Total,
			ItemCount: int64(len(order.Lines)),
			CreatedAt: order.CreatedAt,
		})
	}
	store := &ordersStore{byID: byID, ordered: summaries}
	reservations := &fakeReservation{}
	svc := New(Service{
		Orders:       store,
		Reservations: reservations,
		Tx:           passthroughTx{},
		Clock:        memoryClock{},
		Mapper:       mapper.New(),
	})
	return &ordersFixture{svc: svc, store: store, reservations: reservations}
}

// orderAt builds an awaiting-payment order placed at the given instant, using
// the domain constructor so its identifier, line links and total are the ones a
// checkout would produce.
func orderAt(owner uuid.UUID, createdAt time.Time, lines ...model.OrderLine) *model.Order {
	return model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"}, lines, createdAt.Add(holdWindow), createdAt)
}

// FR-018, FR-020: the list is the caller's only, newest first and paginated —
// another customer's order never appears, and the owner is the session's.
func TestListMineReturnsOnlyTheCallersOrdersNewestFirst(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	first := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	second := orderAt(owner, fixedNow.Add(time.Hour), line(uuid.New(), 2000, 1))
	third := orderAt(owner, fixedNow.Add(2*time.Hour), line(uuid.New(), 3000, 1))
	foreign := orderAt(other, fixedNow.Add(30*time.Minute), line(uuid.New(), 9999, 1))

	// Seeded in the newest-first order the real repository returns.
	f := newOrdersFixture(third, second, first, foreign)

	page, err := f.svc.ListMine(actorContext(owner), appdto.ListInput{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if page.Page != 1 || page.PageSize != 2 {
		t.Fatalf("the page must echo the requested window, got %d/%d", page.Page, page.PageSize)
	}
	if page.Total != 3 {
		t.Fatalf("the total must count the caller's orders only, got %d", page.Total)
	}
	if len(page.Orders) != 2 || page.Orders[0].ID != third.ID || page.Orders[1].ID != second.ID {
		t.Fatalf("the first page must be the newest two of the caller's, got %+v", page.Orders)
	}
	for _, summary := range page.Orders {
		if summary.ID == foreign.ID {
			t.Fatal("another customer's order must never appear in the list")
		}
	}

	// The second page carries the last of the caller's orders.
	next, err := f.svc.ListMine(actorContext(owner), appdto.ListInput{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("ListMine page 2: %v", err)
	}
	if len(next.Orders) != 1 || next.Orders[0].ID != first.ID {
		t.Fatalf("the second page must carry the remaining order, got %+v", next.Orders)
	}

	// Another customer's list does not contain the caller's orders either.
	otherPage, err := f.svc.ListMine(actorContext(other), appdto.ListInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListMine for the other customer: %v", err)
	}
	if otherPage.Total != 1 || len(otherPage.Orders) != 1 || otherPage.Orders[0].ID != foreign.ID {
		t.Fatalf("the other customer must see only their own order, got %+v", otherPage.Orders)
	}
}

// FR-018, FR-020: reading one's own order returns it in full; an order that
// belongs to another customer answers the same not-found an unknown one does, so
// the route never confirms it exists.
func TestGetMineReturnsTheCallersOrderAndHidesAnotherOwners(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	product := uuid.New()
	mine := orderAt(owner, fixedNow, line(product, 120000, 2))
	theirs := orderAt(other, fixedNow, line(uuid.New(), 500, 1))
	f := newOrdersFixture(mine, theirs)

	view, err := f.svc.GetMine(actorContext(owner), appdto.OrderRefInput{OrderID: mine.ID})
	if err != nil {
		t.Fatalf("GetMine: %v", err)
	}
	if view.ID != mine.ID || len(view.Lines) != 1 || view.Lines[0].ProductID != product {
		t.Fatalf("the caller's own order must be returned in full, got %+v", view)
	}

	if _, err := f.svc.GetMine(actorContext(owner), appdto.OrderRefInput{OrderID: theirs.ID}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("another customer's order must answer not-found, got %v", err)
	}
	if _, err := f.svc.GetMine(actorContext(other), appdto.OrderRefInput{OrderID: mine.ID}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("reading the caller's order as another customer must answer not-found, got %v", err)
	}
	if _, err := f.svc.GetMine(actorContext(owner), appdto.OrderRefInput{OrderID: uuid.New()}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("an unknown order must answer not-found, got %v", err)
	}
}

// FR-019, FR-020: cancelling an unpaid order the caller owns returns its goods
// exactly once and answers the order now cancelled.
func TestCancelMineCancelsAnUnpaidOrderAndReturnsTheGoods(t *testing.T) {
	owner := uuid.New()
	product := uuid.New()
	order := orderAt(owner, fixedNow, line(product, 120000, 2))
	f := newOrdersFixture(order)

	view, err := f.svc.CancelMine(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("CancelMine: %v", err)
	}
	if view.Status != constant.StatusCancelled {
		t.Fatalf("the order must answer CANCELLED, got %s", view.Status)
	}
	if order.Status != constant.StatusCancelled {
		t.Fatalf("the persisted state must be CANCELLED, got %s", order.Status)
	}
	if len(f.reservations.releases) != 1 {
		t.Fatalf("cancelling must return the hold of every line once, got %+v", f.reservations.releases)
	}
	if release := f.reservations.releases[0]; release.orderID != order.ID || release.productID != product {
		t.Fatalf("the release must name the order and the product, got %+v", release)
	}
	if len(f.reservations.sales) != 0 {
		t.Fatalf("cancelling must not sell anything, got %+v", f.reservations.sales)
	}
}

// FR-019: a paid order cannot be cancelled — it is transferred instead — so the
// move is refused naming the current state and nothing is released.
func TestCancelMineRefusesAPaidOrder(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 120000, 1))
	order.Status = constant.StatusPaid
	f := newOrdersFixture(order)

	_, err := f.svc.CancelMine(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("cancelling a paid order must be refused, got %v", err)
	}
	if refusal.From != constant.StatusPaid {
		t.Fatalf("the refusal must name PAID, got %+v", refusal)
	}
	if order.Status != constant.StatusPaid {
		t.Fatalf("a refused cancel must leave the order untouched, got %s", order.Status)
	}
	if len(f.reservations.releases) != 0 {
		t.Fatalf("a refused cancel must release nothing, got %+v", f.reservations.releases)
	}
}

// FR-020: cancelling an order that belongs to another customer answers not-found
// and changes nothing, so a customer can never cancel across accounts.
func TestCancelMineRefusesAnotherOwnersOrder(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 120000, 1))
	f := newOrdersFixture(order)

	_, err := f.svc.CancelMine(actorContext(other), appdto.OrderRefInput{OrderID: order.ID})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("cancelling another customer's order must answer not-found, got %v", err)
	}
	if order.Status != constant.StatusPendingPayment {
		t.Fatalf("a foreign cancel must leave the order untouched, got %s", order.Status)
	}
	if len(f.reservations.releases) != 0 {
		t.Fatalf("a foreign cancel must release nothing, got %+v", f.reservations.releases)
	}
}

// FR-020: a use case reached without a session is a programming error, not a
// client-facing refusal.
func TestOrderUseCasesRequireASession(t *testing.T) {
	order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
	f := newOrdersFixture(order)
	ctx := context.Background()

	if _, err := f.svc.ListMine(ctx, appdto.ListInput{Page: 1, PageSize: 20}); !errors.Is(err, errNoActor) {
		t.Fatalf("ListMine without a session must fail with errNoActor, got %v", err)
	}
	if _, err := f.svc.GetMine(ctx, appdto.OrderRefInput{OrderID: order.ID}); !errors.Is(err, errNoActor) {
		t.Fatalf("GetMine without a session must fail with errNoActor, got %v", err)
	}
	if _, err := f.svc.CancelMine(ctx, appdto.OrderRefInput{OrderID: order.ID}); !errors.Is(err, errNoActor) {
		t.Fatalf("CancelMine without a session must fail with errNoActor, got %v", err)
	}
}
