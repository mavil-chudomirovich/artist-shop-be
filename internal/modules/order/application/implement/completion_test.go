package implement

import (
	"context"
	"errors"
	"sort"
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
	// lastCutoff is the instant the completion sweep handed the repository, so a
	// test can prove the window is derived from the injected clock and passed to
	// the query rather than computed by the database (research D6).
	lastCutoff time.Time
	// stale is reported on top of the live selection, the way a concurrent sweep
	// that read before another committed still carries identifiers it can no
	// longer move. It lets a test prove the row lock and the state check — not the
	// selection alone — make completion happen once (FR-012).
	stale []uuid.UUID
}

func (r *completionOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	clone := *order
	return &clone, nil
}

// ListAwaitingCompletion mirrors the adapter's selection: only DELIVERED orders
// whose delivered_at is at or before the cutoff, oldest delivered first, plus the
// deliberately stale identifiers a test injects (FR-012, FR-013).
func (r *completionOrders) ListAwaitingCompletion(_ context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	r.lastCutoff = cutoff
	out := make([]uuid.UUID, 0)
	for _, order := range r.byID {
		if order.Status == constant.StatusDelivered && order.DeliveredAt != nil && !order.DeliveredAt.After(cutoff) {
			out = append(out, order.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := r.byID[out[i]], r.byID[out[j]]
		if !left.DeliveredAt.Equal(*right.DeliveredAt) {
			return left.DeliveredAt.Before(*right.DeliveredAt)
		}
		return left.ID.String() < right.ID.String()
	})
	return append(out, r.stale...), nil
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

// expiredDeliveryOrder builds a DELIVERED order whose delivery instant lies at the
// given instant, so a test can place it before or after the completion window.
func expiredDeliveryOrder(owner uuid.UUID, deliveredAt time.Time) *model.Order {
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusDelivered
	stamp := deliveredAt
	order.DeliveredAt = &stamp
	return order
}

// assertSystemAudit asserts one audit event names the action, a SUCCESS outcome,
// the order, and no acting account — the shape the completion sweep writes when it
// completes an order on its own (FR-012, FR-014, research D10).
func assertSystemAudit(t *testing.T, event recordedAudit, action string, orderID uuid.UUID) {
	t.Helper()
	if event.action != action {
		t.Fatalf("expected action %s, got %s", action, event.action)
	}
	if event.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, event.outcome)
	}
	if event.actorID != nil {
		t.Fatalf("%s: the sweep completes with no actor, got %v", action, event.actorID)
	}
	if event.actorRole != "" {
		t.Fatalf("%s: the sweep passes no role, got %q", action, event.actorRole)
	}
	if event.targetType != "order" || event.targetID != orderID.String() {
		t.Fatalf("%s: expected the order as the target, got %q/%q", action, event.targetType, event.targetID)
	}
}

// FR-012, FR-014: a DELIVERED order past delivered_at + 5 days is completed by the
// sweep with no acting account, stamping the completion instant, and the cutoff the
// sweep hands the repository is the injected clock minus the domain window. A
// repeat — even one that is still handed the identifier — finds the order no longer
// DELIVERED and completes nothing a second time, so the sweep is idempotent.
func TestAutoCompleteDeliveredCompletesAnOrderPastTheWindowExactlyOnce(t *testing.T) {
	owner := uuid.New()
	order := expiredDeliveryOrder(owner, fixedNow.Add(-model.DeliveryCompletionWindow-time.Minute))
	f := newCompletionFixture(order)
	// A concurrent sweep that read before this one committed still reports the
	// identifier; the row lock and the state check make the second attempt skip.
	f.orders.stale = []uuid.UUID{order.ID}

	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}

	wantCutoff := fixedNow.Add(-model.DeliveryCompletionWindow)
	if !f.orders.lastCutoff.Equal(wantCutoff) {
		t.Fatalf("the sweep must pass now - DeliveryCompletionWindow as the cutoff, got %v want %v",
			f.orders.lastCutoff, wantCutoff)
	}

	stored := f.orders.byID[order.ID]
	if stored.Status != constant.StatusCompleted {
		t.Fatalf("a delivered order past its window must reach COMPLETED, got %s", stored.Status)
	}
	if stored.CompletedAt == nil || !stored.CompletedAt.Equal(fixedNow) {
		t.Fatalf("the sweep must stamp the completion instant, got %v", stored.CompletedAt)
	}
	if stored.CompletedBy != nil {
		t.Fatalf("the sweep completes with no acting customer, got %v", stored.CompletedBy)
	}
	if len(f.orders.completed) != 1 {
		t.Fatalf("the completion must be persisted once, got %d", len(f.orders.completed))
	}
	if len(f.audit.events) != 1 {
		t.Fatalf("the sweep must record one audit entry, got %+v", f.audit.events)
	}
	assertSystemAudit(t, f.audit.events[0], constant.AuditOrderCompleted, order.ID)

	// A repeated sweep — with the same stale selection — changes nothing and
	// audits nothing further (FR-012: exactly once, even across repeats).
	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("a repeated AutoCompleteDelivered must not fail: %v", err)
	}
	if len(f.orders.completed) != 1 {
		t.Fatalf("a repeated sweep must not complete again, got %d completions", len(f.orders.completed))
	}
	if len(f.audit.events) != 1 {
		t.Fatalf("a repeated sweep must not audit again, got %+v", f.audit.events)
	}
	if stored.Status != constant.StatusCompleted {
		t.Fatalf("a repeated sweep must leave the order COMPLETED, got %s", stored.Status)
	}
}

// FR-013: only a DELIVERED order is selected. An order in any other state — even
// one that somehow carries a delivery instant — is never completed by time, and
// an order delivered inside the window is not selected either.
func TestAutoCompleteDeliveredNeverSelectsANonDeliveredOrder(t *testing.T) {
	oldDelivery := fixedNow.Add(-3 * model.DeliveryCompletionWindow)
	besidesDelivered := []constant.Status{
		constant.StatusPending,
		constant.StatusPaymentPending,
		constant.StatusPaid,
		constant.StatusShipping,
		constant.StatusCompleted,
		constant.StatusCancelled,
	}
	orders := make([]*model.Order, 0, len(besidesDelivered)+2)
	for _, status := range besidesDelivered {
		order := expiredDeliveryOrder(uuid.New(), oldDelivery)
		order.Status = status
		orders = append(orders, order)
	}
	// Delivered, but not yet past the window: the customer still has time to
	// confirm, so the sweep must leave it alone (FR-012).
	insideWindow := expiredDeliveryOrder(uuid.New(), fixedNow.Add(-model.DeliveryCompletionWindow+time.Minute))
	orders = append(orders, insideWindow)
	expired := expiredDeliveryOrder(uuid.New(), oldDelivery)
	orders = append(orders, expired)

	f := newCompletionFixture(orders...)

	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}

	for _, order := range orders {
		if order.ID == expired.ID {
			continue
		}
		if stored := f.orders.byID[order.ID]; stored.Status != order.Status {
			t.Fatalf("status %s must never be completed by time, got %s", order.Status, stored.Status)
		}
	}
	if stored := f.orders.byID[insideWindow.ID]; stored.CompletedAt != nil {
		t.Fatalf("an order inside the window must not complete, got %v", stored.CompletedAt)
	}
	if stored := f.orders.byID[expired.ID]; stored.Status != constant.StatusCompleted {
		t.Fatalf("the expired delivered order must complete, got %s", stored.Status)
	}
	if len(f.orders.completed) != 1 {
		t.Fatalf("only the expired delivered order may complete, got %d completions", len(f.orders.completed))
	}
	if len(f.audit.events) != 1 {
		t.Fatalf("only the expired delivered order may audit, got %+v", f.audit.events)
	}
}

// FR-012, research D6: the sweep walks the awaiting orders oldest-delivered first,
// so a backlog drains in the order it accumulated.
func TestAutoCompleteDeliveredCompletesOldestFirst(t *testing.T) {
	oldest := expiredDeliveryOrder(uuid.New(), fixedNow.Add(-4*model.DeliveryCompletionWindow))
	middle := expiredDeliveryOrder(uuid.New(), fixedNow.Add(-3*model.DeliveryCompletionWindow))
	newest := expiredDeliveryOrder(uuid.New(), fixedNow.Add(-2*model.DeliveryCompletionWindow))
	f := newCompletionFixture(newest, oldest, middle)

	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}

	got := make([]uuid.UUID, 0, len(f.orders.completed))
	for _, order := range f.orders.completed {
		got = append(got, order.ID)
	}
	want := []uuid.UUID{oldest.ID, middle.ID, newest.ID}
	if len(got) != len(want) {
		t.Fatalf("expected %d completions, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the sweep must complete oldest delivered first, got %v want %v", got, want)
		}
	}
}

// FR-012, edge case: the customer's confirmation and the sweep race. Whichever
// runs second finds the order no longer DELIVERED and does nothing, so the order
// completes exactly once and only one audit entry is written. Both interleavings
// are asserted, because either side can win the row lock.
func TestAutoCompleteDeliveredAndCustomerConfirmationCompleteOnce(t *testing.T) {
	t.Run("the sweep completes first", func(t *testing.T) {
		owner := uuid.New()
		order := expiredDeliveryOrder(owner, fixedNow.Add(-model.DeliveryCompletionWindow-time.Hour))
		f := newCompletionFixture(order)

		if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
			t.Fatalf("AutoCompleteDelivered: %v", err)
		}
		_, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})
		var refusal *domainerr.StateTransitionError
		if !errors.As(err, &refusal) {
			t.Fatalf("a confirmation after the sweep must be refused, got %v", err)
		}
		if refusal.From != constant.StatusCompleted || refusal.To != constant.StatusCompleted {
			t.Fatalf("the refusal must name the current state COMPLETED, got %+v", refusal)
		}
		if len(f.orders.completed) != 1 {
			t.Fatalf("the order must complete once, got %d completions", len(f.orders.completed))
		}
		if len(f.audit.events) != 1 {
			t.Fatalf("the order must audit one completion, got %+v", f.audit.events)
		}
		assertSystemAudit(t, f.audit.events[0], constant.AuditOrderCompleted, order.ID)
		if stored := f.orders.byID[order.ID]; stored.CompletedBy != nil {
			t.Fatalf("the sweep's completion records no actor, got %v", stored.CompletedBy)
		}
	})

	t.Run("the customer confirms first", func(t *testing.T) {
		owner := uuid.New()
		order := expiredDeliveryOrder(owner, fixedNow.Add(-model.DeliveryCompletionWindow-time.Hour))
		f := newCompletionFixture(order)

		if _, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
			t.Fatalf("ConfirmReceived: %v", err)
		}
		// The sweep still carries the identifier the way a concurrent selection
		// would, and skips it rather than failing or writing a second completion.
		f.orders.stale = []uuid.UUID{order.ID}
		if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
			t.Fatalf("a sweep racing a confirmation must not fail: %v", err)
		}

		if len(f.orders.completed) != 1 {
			t.Fatalf("the order must complete once, got %d completions", len(f.orders.completed))
		}
		if len(f.audit.events) != 1 {
			t.Fatalf("the order must audit one completion, got %+v", f.audit.events)
		}
		assertCustomerAudit(t, f.audit.events[0], constant.AuditOrderCompleted, owner, order.ID)
		if stored := f.orders.byID[order.ID]; stored.CompletedBy == nil || *stored.CompletedBy != owner {
			t.Fatalf("the customer's completion must keep the acting customer, got %v", stored.CompletedBy)
		}
	})
}
