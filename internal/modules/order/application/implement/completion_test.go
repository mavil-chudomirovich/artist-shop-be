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

// This file is the use-case contract of US3: the customer's confirmation of
// receipt. ConfirmReceived drives DELIVERED -> COMPLETED, stamps the completion
// instant and the acting customer, and records the act naming the order and the
// customer; a non-DELIVERED order is refused naming its current state, and
// another customer's order answers the same not-found an unknown one does
// (FR-011, FR-014, FR-018). It runs the real use case over in-memory fakes of the
// repository and the auditor, so the answers asserted are the service's own.

// completionOrders is the in-memory OrderRepository the completion use case uses.
// Its row lock returns a copy — the way the postgres adapter scans a fresh row —
// so a refused confirmation never mutates what the repository holds. It records
// the completed writes so a test can assert the persisted completion facts.
type completionOrders struct {
	appinterface.OrderRepository
	byID      map[uuid.UUID]*model.Order
	completed []*model.Order
}

func (r *completionOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	clone := *order
	return &clone, nil
}

func (r *completionOrders) SaveCompleted(_ context.Context, order *model.Order, _ time.Time) error {
	r.completed = append(r.completed, order)
	if stored, ok := r.byID[order.ID]; ok {
		stored.Status = order.Status
		stored.CompletedAt = order.CompletedAt
		stored.CompletedBy = order.CompletedBy
	}
	return nil
}

// completionFixture is the real completion use case over in-memory fakes.
type completionFixture struct {
	svc    *Service
	orders *completionOrders
	audit  *recordingAuditor
}

func newCompletionFixture(orders ...*model.Order) *completionFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	store := &completionOrders{byID: byID}
	recorder := &recordingAuditor{}
	svc := New(Service{
		Orders: store,
		Tx:     passthroughTx{},
		Clock:  memoryClock{},
		Mapper: mapper.New(),
		Audit:  recorder,
	})
	return &completionFixture{svc: svc, orders: store, audit: recorder}
}

// deliveredOrder builds a DELIVERED order owned by owner, ready for the customer's
// confirmation.
func deliveredOrder(owner uuid.UUID) *model.Order {
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusDelivered
	deliveredAt := fixedNow
	order.DeliveredAt = &deliveredAt
	return order
}

// assertCustomerAudit asserts one audit event names the action, a SUCCESS
// outcome, the acting customer and the order.
func assertCustomerAudit(t *testing.T, event recordedAudit, action string, customer uuid.UUID, orderID uuid.UUID) {
	t.Helper()
	if event.action != action {
		t.Fatalf("expected action %s, got %s", action, event.action)
	}
	if event.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, event.outcome)
	}
	if event.actorID == nil || *event.actorID != customer {
		t.Fatalf("%s: expected the customer actor %s, got %v", action, customer, event.actorID)
	}
	if event.actorRole != string(access.RoleCustomer) {
		t.Fatalf("%s: expected the CUSTOMER role, got %q", action, event.actorRole)
	}
	if event.targetType != "order" || event.targetID != orderID.String() {
		t.Fatalf("%s: expected the order as the target, got %q/%q", action, event.targetType, event.targetID)
	}
}

// FR-011, FR-014, FR-018: the owner confirms a delivered order, driving it to
// COMPLETED, stamping the completion instant and the acting customer, and
// recording the act.
func TestConfirmReceivedCompletesTheDeliveredOrderAndAuditsIt(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newCompletionFixture(order)

	view, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("ConfirmReceived: %v", err)
	}
	if view.Status != constant.StatusCompleted {
		t.Fatalf("the order must answer COMPLETED, got %s", view.Status)
	}
	if view.CompletedAt == nil || !view.CompletedAt.Equal(fixedNow) {
		t.Fatalf("the confirmation must stamp the completion instant, got %v", view.CompletedAt)
	}

	stored := f.orders.byID[order.ID]
	if stored.Status != constant.StatusCompleted {
		t.Fatalf("the persisted state must be COMPLETED, got %s", stored.Status)
	}
	if stored.CompletedAt == nil || !stored.CompletedAt.Equal(fixedNow) {
		t.Fatalf("the persisted completion instant must be stamped, got %v", stored.CompletedAt)
	}
	if stored.CompletedBy == nil || *stored.CompletedBy != owner {
		t.Fatalf("the persisted completion actor must be the customer %s, got %v", owner, stored.CompletedBy)
	}
	if len(f.orders.completed) != 1 {
		t.Fatalf("the completion must be persisted once, got %d", len(f.orders.completed))
	}

	if len(f.audit.events) != 1 {
		t.Fatalf("completing must record one audit entry, got %+v", f.audit.events)
	}
	assertCustomerAudit(t, f.audit.events[0], constant.AuditOrderCompleted, owner, order.ID)
}

// FR-003, FR-011: confirming an order that is not DELIVERED is refused naming its
// current state, leaving the order and the audit trail untouched.
func TestConfirmReceivedRefusesANonDeliveredOrder(t *testing.T) {
	for _, status := range []constant.Status{
		constant.StatusPending,
		constant.StatusPaymentPending,
		constant.StatusPaid,
		constant.StatusShipping,
		constant.StatusCompleted,
		constant.StatusCancelled,
	} {
		owner := uuid.New()
		order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
		order.Status = status
		f := newCompletionFixture(order)

		_, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})

		var refusal *domainerr.StateTransitionError
		if !errors.As(err, &refusal) {
			t.Fatalf("status %s: expected a state transition refusal, got %v", status, err)
		}
		if refusal.From != status || refusal.To != constant.StatusCompleted {
			t.Fatalf("status %s: the refusal must name the current state, got %+v", status, refusal)
		}
		if stored := f.orders.byID[order.ID]; stored.Status != status {
			t.Fatalf("status %s: a refused confirmation must leave the order untouched, got %s", status, stored.Status)
		}
		if len(f.orders.completed) != 0 {
			t.Fatalf("status %s: a refused confirmation must persist nothing, got %+v", status, f.orders.completed)
		}
		if len(f.audit.events) != 0 {
			t.Fatalf("status %s: a refused confirmation must audit nothing, got %+v", status, f.audit.events)
		}
	}
}

// FR-018: another customer's order answers the same not-found an unknown order
// does, and is left exactly as it was.
func TestConfirmReceivedAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	order := deliveredOrder(owner)
	f := newCompletionFixture(order)

	_, err := f.svc.ConfirmReceived(actorContext(other), appdto.OrderRefInput{OrderID: order.ID})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("another customer's order must answer not-found, got %v", err)
	}
	if stored := f.orders.byID[order.ID]; stored.Status != constant.StatusDelivered || stored.CompletedAt != nil {
		t.Fatalf("a refused confirmation must leave the order untouched, got %+v", stored)
	}
	if stored := f.orders.byID[order.ID]; stored.CompletedBy != nil {
		t.Fatalf("a refused confirmation must not record a completer, got %v", stored.CompletedBy)
	}
	if len(f.orders.completed) != 0 {
		t.Fatalf("a refused confirmation must persist nothing, got %+v", f.orders.completed)
	}
	if len(f.audit.events) != 0 {
		t.Fatalf("a refused confirmation must audit nothing, got %+v", f.audit.events)
	}

	// An unknown order answers the same not-found.
	if _, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: uuid.New()}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("an unknown order must answer not-found, got %v", err)
	}
}

// FR-018: a confirmation reached without a session is a programming error, not a
// client-facing refusal — the session guard at the transport answers
// UNAUTHENTICATED.
func TestConfirmReceivedRequiresASession(t *testing.T) {
	order := deliveredOrder(uuid.New())
	f := newCompletionFixture(order)

	if _, err := f.svc.ConfirmReceived(context.Background(), appdto.OrderRefInput{OrderID: order.ID}); !errors.Is(err, errNoActor) {
		t.Fatalf("ConfirmReceived without a session must fail with errNoActor, got %v", err)
	}
}
