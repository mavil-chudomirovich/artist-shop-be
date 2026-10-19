package implement

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of the live SSE fan-out
// (specs/020-order-sse-stream): admins learn an order needs confirmation or
// carries feedback, the owner learns its status changed. Publishing runs after
// the commit and never fails the operation, so these tests run the real use
// cases over a recording publisher and prove the topic, the type and the
// payload — and that a nil publisher changes nothing.

// publishedEvent is one event the fake publisher recorded.
type publishedEvent struct {
	topic     string
	eventType string
	payload   map[string]any
}

type fakePublisher struct {
	events []publishedEvent
}

func (f *fakePublisher) Publish(_ context.Context, topic, eventType string, payload []byte) {
	var decoded map[string]any
	_ = json.Unmarshal(payload, &decoded)
	f.events = append(f.events, publishedEvent{topic: topic, eventType: eventType, payload: decoded})
}

func eventsOf(f *fakePublisher, eventType string) []publishedEvent {
	out := make([]publishedEvent, 0, len(f.events))
	for _, e := range f.events {
		if e.eventType == eventType {
			out = append(out, e)
		}
	}
	return out
}

func enableEvents(svc *Service, publisher *fakePublisher) {
	svc.Events = publisher
}

// US1: checkout publishes needs_confirmation to admins.
func TestCheckoutPublishesNeedsConfirmation(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	f.customers.customer.Email = customerEmail
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{VariantID: product, Quantity: 1, UnitPrice: contracts.ProductPrice{Amount: 100000, Currency: "VND"}}}
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	view, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	got := eventsOf(publisher, appinterface.EventNeedsConfirmation)
	if len(got) != 1 {
		t.Fatalf("checkout must publish needs_confirmation once, got %+v", publisher.events)
	}
	if got[0].topic != appinterface.TopicAdmins {
		t.Fatalf("needs_confirmation must go to %q, got %q", appinterface.TopicAdmins, got[0].topic)
	}
	if got[0].payload["orderId"] != view.ID.String() {
		t.Fatalf("payload must name the order, got %+v", got[0].payload)
	}
	if len(eventsOf(publisher, appinterface.EventStatusChanged)) != 0 {
		t.Fatalf("checkout must not publish status_changed, got %+v", publisher.events)
	}
}

// US2: confirming publishes status_changed to the owner's topic only.
func TestConfirmPublishesStatusChangedToOwner(t *testing.T) {
	owner := uuid.New()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyen Van A"},
		[]model.OrderLine{line(uuid.New(), 1000, 1)}, fixedNow)
	f := newLifecycleFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if _, err := f.svc.ConfirmByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("ConfirmByAdmin: %v", err)
	}

	got := eventsOf(publisher, appinterface.EventStatusChanged)
	if len(got) != 1 {
		t.Fatalf("confirm must publish status_changed once, got %+v", publisher.events)
	}
	if want := appinterface.TopicForUser(owner); got[0].topic != want {
		t.Fatalf("status_changed must go to %q, got %q", want, got[0].topic)
	}
	if got[0].payload["from"] != string(constant.StatusPending) || got[0].payload["to"] != string(constant.StatusPaymentPending) {
		t.Fatalf("payload must carry PENDING -> PAYMENT_PENDING, got %+v", got[0].payload)
	}
}

// US2: cancelling publishes from the pre-cancel state, so a PAYMENT_PENDING
// cancel names its true origin.
func TestCancelPublishesFromPreCancelState(t *testing.T) {
	owner := uuid.New()
	order := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if err := f.svc.Cancel(context.Background(), order.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got := eventsOf(publisher, appinterface.EventStatusChanged)
	if len(got) != 1 {
		t.Fatalf("cancel must publish status_changed once, got %+v", publisher.events)
	}
	if got[0].payload["from"] != string(constant.StatusPaymentPending) || got[0].payload["to"] != string(constant.StatusCancelled) {
		t.Fatalf("payload must carry PAYMENT_PENDING -> CANCELLED, got %+v", got[0].payload)
	}
}

// US2: ship and deliver publish their own edges.
func TestShipAndDeliverPublishTheirEdges(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	f := newDeliveryFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if _, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID}); err != nil {
		t.Fatalf("StartShippingByAdmin: %v", err)
	}
	if _, err := f.svc.DeliverByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID}); err != nil {
		t.Fatalf("DeliverByAdmin: %v", err)
	}

	got := eventsOf(publisher, appinterface.EventStatusChanged)
	if len(got) != 2 {
		t.Fatalf("ship+deliver must publish twice, got %+v", publisher.events)
	}
	if got[0].payload["to"] != string(constant.StatusShipping) || got[1].payload["to"] != string(constant.StatusDelivered) {
		t.Fatalf("payloads must walk PAID -> SHIPPING -> DELIVERED, got %+v", got)
	}
	for _, e := range got {
		if e.topic != appinterface.TopicForUser(owner) {
			t.Fatalf("delivery events must reach the owner topic, got %q", e.topic)
		}
	}
}

// US1+US2: editing back to PENDING re-publishes needs_confirmation and the
// status change; an edit that keeps PENDING publishes no status change.
func TestEditReturningToPendingPublishesBoth(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 2))
	confirmEditOrder(order)
	store.orders[order.ID] = order
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if _, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{VariantID: product, Quantity: 3}},
	}); err != nil {
		t.Fatalf("EditMine: %v", err)
	}

	if len(eventsOf(publisher, appinterface.EventNeedsConfirmation)) != 1 {
		t.Fatalf("edit back to pending must publish needs_confirmation, got %+v", publisher.events)
	}
	changed := eventsOf(publisher, appinterface.EventStatusChanged)
	if len(changed) != 1 {
		t.Fatalf("edit back to pending must publish status_changed once, got %+v", publisher.events)
	}
	if changed[0].payload["from"] != string(constant.StatusPaymentPending) || changed[0].payload["to"] != string(constant.StatusPending) {
		t.Fatalf("payload must carry PAYMENT_PENDING -> PENDING, got %+v", changed[0].payload)
	}
}

// US3: feedback publishes feedback_received to admins and nothing to customers.
func TestFeedbackPublishesFeedbackReceived(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if _, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{OrderID: order.ID, Content: "tot"}); err != nil {
		t.Fatalf("SubmitFeedback: %v", err)
	}

	got := eventsOf(publisher, appinterface.EventFeedbackReceived)
	if len(got) != 1 {
		t.Fatalf("feedback must publish feedback_received once, got %+v", publisher.events)
	}
	if got[0].topic != appinterface.TopicAdmins || got[0].payload["orderId"] != order.ID.String() {
		t.Fatalf("feedback event must name the order on admins, got %+v", got[0])
	}
	if len(eventsOf(publisher, appinterface.EventStatusChanged)) != 0 {
		t.Fatalf("feedback must not publish status_changed, got %+v", publisher.events)
	}
}

// A refused feedback publishes nothing.
func TestRefusedFeedbackPublishesNothing(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	f := newFeedbackFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	if _, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{OrderID: order.ID, Content: "som"}); err == nil {
		t.Fatal("feedback on an undelivered order must be refused")
	}
	if len(publisher.events) != 0 {
		t.Fatalf("a refused feedback must publish nothing, got %+v", publisher.events)
	}
}

// A nil publisher never fails the operation and never panics.
func TestNilPublisherNeverFailsTheOperation(t *testing.T) {
	owner := uuid.New()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyen Van A"},
		[]model.OrderLine{line(uuid.New(), 1000, 1)}, fixedNow)
	f := newLifecycleFixture(order)
	f.svc.Events = nil

	if _, err := f.svc.ConfirmByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("a nil publisher must not fail confirmation, got %v", err)
	}
	if order.Status != constant.StatusPaymentPending {
		t.Fatalf("the confirmation must still take effect, got %s", order.Status)
	}
}

// US3: cancelling one's own order publishes status_changed to the owner's topic
// once, after the ownership check; a customer cancelling another customer's
// order answers not-found and publishes nothing.
func TestCancelMinePublishesAfterOwnershipCheck(t *testing.T) {
	owner := uuid.New()
	order := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	f := newOrdersFixture(order)
	publisher := &fakePublisher{}
	enableEvents(f.svc, publisher)

	view, err := f.svc.CancelMine(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("CancelMine: %v", err)
	}
	if view.Status != constant.StatusCancelled {
		t.Fatalf("the order must answer CANCELLED, got %s", view.Status)
	}

	got := eventsOf(publisher, appinterface.EventStatusChanged)
	if len(got) != 1 {
		t.Fatalf("cancel must publish status_changed once, got %+v", publisher.events)
	}
	if want := appinterface.TopicForUser(owner); got[0].topic != want {
		t.Fatalf("status_changed must go to %q, got %q", want, got[0].topic)
	}
	if got[0].payload["from"] != string(constant.StatusPaymentPending) || got[0].payload["to"] != string(constant.StatusCancelled) {
		t.Fatalf("payload must carry PAYMENT_PENDING -> CANCELLED, got %+v", got[0].payload)
	}

	// Another customer's cancel answers not-found and publishes nothing.
	other := uuid.New()
	foreign := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	f2 := newOrdersFixture(foreign)
	strangerPublisher := &fakePublisher{}
	enableEvents(f2.svc, strangerPublisher)

	if _, err := f2.svc.CancelMine(actorContext(other), appdto.OrderRefInput{OrderID: foreign.ID}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("cancelling another customer's order must answer not-found, got %v", err)
	}
	if len(strangerPublisher.events) != 0 {
		t.Fatalf("a foreign cancel must publish nothing, got %+v", strangerPublisher.events)
	}
}
