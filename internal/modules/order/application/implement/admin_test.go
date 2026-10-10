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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is the use-case contract of US4: the operator's order desk. An
// administrator lists every order (newest first, with its owner), reads any order
// in full, and advances fulfilment through the US2 transitions — refusing an
// illegal move naming the current state — while each successful move writes an
// audit entry naming the order and the administrator (FR-021 to FR-023). It runs
// the real use cases over in-memory fakes of the repository and the auditor, so
// the answers asserted are the service's own.

// adminOrders is the in-memory OrderRepository the admin use cases use. Unlike
// the customer store it applies no owner filter: FindByID answers any order and
// ListAll answers every order, newest first. Embedding the interface makes any
// method the use cases drift into panic loudly rather than silently pass.
type adminOrders struct {
	appinterface.OrderRepository
	byID    map[uuid.UUID]*model.Order
	ordered []model.OrderSummary
	updates []statusWrite
}

func (r *adminOrders) FindByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	return order, nil
}

func (r *adminOrders) ListAll(_ context.Context, page, size int, _ *constant.Status, _ constant.OrderListSort) ([]model.OrderSummary, int64, error) {
	total := int64(len(r.ordered))
	start := (page - 1) * size
	if start > len(r.ordered) {
		start = len(r.ordered)
	}
	end := start + size
	if end > len(r.ordered) {
		end = len(r.ordered)
	}
	return r.ordered[start:end], total, nil
}

func (r *adminOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	return order, nil
}

func (r *adminOrders) UpdateStatus(_ context.Context, id uuid.UUID, status constant.Status, _ time.Time) error {
	r.updates = append(r.updates, statusWrite{id: id, status: status})
	if order, ok := r.byID[id]; ok {
		order.Status = status
	}
	return nil
}

// recordedAudit is one audit event the admin use cases emitted.
type recordedAudit struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
}

// recordingAuditor is the in-memory Auditor. It records every event so a test can
// assert each administrator action names the order and the administrator
// (FR-023).
type recordingAuditor struct{ events []recordedAudit }

func (a *recordingAuditor) Record(_ context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, _ map[string]any) {
	a.events = append(a.events, recordedAudit{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
	})
}

var _ appinterface.Auditor = (*recordingAuditor)(nil)

// adminFixture is the real admin use cases over in-memory fakes.
type adminFixture struct {
	svc    *Service
	orders *adminOrders
	audit  *recordingAuditor
}

// newAdminFixture seeds the repository with the orders in the order supplied —
// the newest-first order the real ListAll returns — and builds the use cases over
// the shared in-memory UnitOfWork, clock and a recording auditor.
func newAdminFixture(orders ...*model.Order) *adminFixture {
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
	store := &adminOrders{byID: byID, ordered: summaries}
	recorder := &recordingAuditor{}
	svc := New(Service{
		Orders: store,
		Tx:     passthroughTx{},
		Clock:  memoryClock{},
		Mapper: mapper.New(),
		Audit:  recorder,
	})
	return &adminFixture{svc: svc, orders: store, audit: recorder}
}

// adminContext puts the acting administrator in the context the way presentation
// does from the session. The actor is never part of a use-case input (FR-020,
// FR-023).
func adminContext(userID uuid.UUID) context.Context {
	return appinterface.WithActor(context.Background(), appinterface.Actor{ID: userID, Role: access.RoleAdmin})
}

// FR-021: the operator's list is every order, newest first, with its owner,
// state and total, paginated.
func TestListAllReturnsEveryOrderNewestFirstWithItsOwner(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	first := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	second := orderAt(other, fixedNow.Add(time.Hour), line(uuid.New(), 2000, 1))
	third := orderAt(owner, fixedNow.Add(2*time.Hour), line(uuid.New(), 3000, 1))

	// Seeded in the newest-first order the real repository returns.
	f := newAdminFixture(third, second, first)

	page, err := f.svc.ListAll(adminContext(uuid.New()), appdto.ListInput{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if page.Page != 1 || page.PageSize != 2 {
		t.Fatalf("the page must echo the requested window, got %d/%d", page.Page, page.PageSize)
	}
	if page.Total != 3 {
		t.Fatalf("the total must count every order, got %d", page.Total)
	}
	if len(page.Orders) != 2 || page.Orders[0].ID != third.ID || page.Orders[1].ID != second.ID {
		t.Fatalf("the first page must be the newest two orders, got %+v", page.Orders)
	}
	if page.Orders[0].UserID != owner || page.Orders[1].UserID != other {
		t.Fatalf("each row must carry its owner, got %+v", page.Orders)
	}
	if page.Orders[0].Status != constant.StatusPending || page.Orders[0].Total.Amount != 3000 {
		t.Fatalf("each row must carry its state and total, got %+v", page.Orders[0])
	}
}

// FR-021: the operator reads any order in full, including its owner; an unknown
// identifier is the module's not-found.
func TestGetByIDAdminReturnsAnyOrderInFull(t *testing.T) {
	owner, admin := uuid.New(), uuid.New()
	product := uuid.New()
	order := orderAt(owner, fixedNow, line(product, 120000, 2))
	f := newAdminFixture(order)

	view, err := f.svc.GetByIDAdmin(adminContext(admin), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("GetByIDAdmin: %v", err)
	}
	if view.ID != order.ID || view.UserID != owner {
		t.Fatalf("the order must be returned with its owner, got %+v", view)
	}
	if len(view.Lines) != 1 || view.Lines[0].ProductID != product || view.Lines[0].Quantity != 2 {
		t.Fatalf("the order must be returned in full, got %+v", view.Lines)
	}

	if _, err := f.svc.GetByIDAdmin(adminContext(admin), appdto.OrderRefInput{OrderID: uuid.New()}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("an unknown order must answer not-found, got %v", err)
	}
}

// FR-022, FR-023: shipping a paid order then completing it advances the state and
// records each act, naming the order and the administrator.
func TestShipAndCompleteByAdminAdvanceTheOrderAndAuditEach(t *testing.T) {
	admin := uuid.New()
	order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 120000, 1))
	order.Status = constant.StatusPaid
	f := newAdminFixture(order)

	shipped, err := f.svc.ShipByAdmin(adminContext(admin), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("ShipByAdmin: %v", err)
	}
	if shipped.Status != constant.StatusShipped {
		t.Fatalf("the order must answer SHIPPED, got %s", shipped.Status)
	}
	if order.Status != constant.StatusShipped {
		t.Fatalf("the persisted state must be SHIPPED, got %s", order.Status)
	}
	if len(f.audit.events) != 1 {
		t.Fatalf("shipping must record one audit entry, got %+v", f.audit.events)
	}
	assertAudit(t, f.audit.events[0], constant.AuditOrderShipped, admin, order.ID)

	completed, err := f.svc.CompleteByAdmin(adminContext(admin), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("CompleteByAdmin: %v", err)
	}
	if completed.Status != constant.StatusCompleted {
		t.Fatalf("the order must answer COMPLETED, got %s", completed.Status)
	}
	if len(f.audit.events) != 2 {
		t.Fatalf("completing must record a second audit entry, got %+v", f.audit.events)
	}
	assertAudit(t, f.audit.events[1], constant.AuditOrderCompleted, admin, order.ID)

	if len(f.orders.updates) != 2 || f.orders.updates[0].status != constant.StatusShipped ||
		f.orders.updates[1].status != constant.StatusCompleted {
		t.Fatalf("each move must persist its new state, got %+v", f.orders.updates)
	}
}

// FR-011, FR-022: a move the current state does not allow is refused naming that
// state, and neither the order nor the audit trail changes.
func TestAdminMoveRefusesAnIllegalMoveAndRecordsNothing(t *testing.T) {
	admin := uuid.New()
	order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
	f := newAdminFixture(order)

	_, err := f.svc.ShipByAdmin(adminContext(admin), appdto.OrderRefInput{OrderID: order.ID})
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("shipping an unpaid order must be refused with a state transition error, got %v", err)
	}
	if refusal.From != constant.StatusPending || refusal.To != constant.StatusShipped {
		t.Fatalf("the refusal must name PENDING, got %+v", refusal)
	}
	if order.Status != constant.StatusPending {
		t.Fatalf("a refused move must leave the order untouched, got %s", order.Status)
	}
	if len(f.orders.updates) != 0 {
		t.Fatalf("a refused move must persist nothing, got %+v", f.orders.updates)
	}
	if len(f.audit.events) != 0 {
		t.Fatalf("a refused move must audit nothing, got %+v", f.audit.events)
	}
}

// FR-020, FR-023: an administrator move reached without a session is a
// programming error, not a client-facing refusal — the role guard at the
// transport is what answers FORBIDDEN.
func TestAdminMoveRequiresASession(t *testing.T) {
	order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	f := newAdminFixture(order)

	if _, err := f.svc.ShipByAdmin(context.Background(), appdto.OrderRefInput{OrderID: order.ID}); !errors.Is(err, errNoActor) {
		t.Fatalf("ShipByAdmin without a session must fail with errNoActor, got %v", err)
	}
}

// assertAudit asserts one audit event names the action, a SUCCESS outcome, the
// acting administrator and the order.
func assertAudit(t *testing.T, event recordedAudit, action string, admin uuid.UUID, orderID uuid.UUID) {
	t.Helper()
	if event.action != action {
		t.Fatalf("expected action %s, got %s", action, event.action)
	}
	if event.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, event.outcome)
	}
	if event.actorID == nil || *event.actorID != admin {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, admin, event.actorID)
	}
	if event.actorRole != string(access.RoleAdmin) {
		t.Fatalf("%s: expected the ADMIN role, got %q", action, event.actorRole)
	}
	if event.targetType != "order" || event.targetID != orderID.String() {
		t.Fatalf("%s: expected the order as the target, got %q/%q", action, event.targetType, event.targetID)
	}
}
