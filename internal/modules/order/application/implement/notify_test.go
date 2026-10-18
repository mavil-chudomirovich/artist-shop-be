package implement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of the notification behaviour: both sides are
// notified. The artist is emailed whenever an order needs confirmation — a fresh
// checkout and any edit that leaves the order awaiting confirmation — and when the
// customer submits order feedback; the customer is emailed on every status change,
// including the delivery lifecycle's new SHIPPING, DELIVERED and COMPLETED states
// (FR-021, FR-022). Sending is best-effort and runs after the transaction commits,
// so a failing notifier never fails the order operation (FR-023, research D12). It
// runs the real use cases over a fake notifier and the in-memory fakes of every
// contract, so the answers asserted are the service's own.

// The two mailbox addresses the fixture uses: the shop operator's configured
// address and the customer's account address.
const (
	artistEmail   = "shop@example.com"
	customerEmail = "customer@example.com"
)

// sentEmail is one message the fake notifier recorded.
type sentEmail struct {
	to      string
	subject string
	body    string
}

// fakeNotifier is the in-memory Notifier. It records every message so a test can
// prove who was emailed and what the note named; err makes Send fail so a test
// can prove a failing notifier never fails the order operation (FR-021).
type fakeNotifier struct {
	sent []sentEmail
	err  error
}

func (f *fakeNotifier) Send(_ context.Context, to, subject, body string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentEmail{to: to, subject: subject, body: body})
	return nil
}

// messagesTo returns every recorded message addressed to one mailbox.
func messagesTo(n *fakeNotifier, to string) []sentEmail {
	out := make([]sentEmail, 0, len(n.sent))
	for _, message := range n.sent {
		if message.to == to {
			out = append(out, message)
		}
	}
	return out
}

// enableNotifications wires a fake notifier, the shop's address and a customer
// lookup returning the given account into an already-built service, the way the
// composition root does (FR-019, FR-020, research D9).
func enableNotifications(svc *Service, notifier *fakeNotifier, customerID uuid.UUID) {
	svc.Notifier = notifier
	svc.ArtistEmail = artistEmail
	svc.Customers = &fakeCustomers{customer: contracts.Customer{ID: customerID, Email: customerEmail}}
}

// assertEmailNames checks one message names the order and carries the status in
// its body, so the note is factual rather than a bare ping.
func assertEmailNames(t *testing.T, message sentEmail, orderID, status string) {
	t.Helper()
	if !strings.Contains(message.subject, orderID) || !strings.Contains(message.body, orderID) {
		t.Fatalf("the email must name the order %s, got subject %q body %q", orderID, message.subject, message.body)
	}
	if !strings.Contains(message.body, status) {
		t.Fatalf("the email must name the status %s, got body %q", status, message.body)
	}
}

// FR-019: checkout notifies the artist that a fresh order needs confirmation, and
// does not email the customer, because nothing about the customer's order has
// changed yet.
func TestCheckoutNotifiesTheArtistButNotTheCustomer(t *testing.T) {
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	f.customers.customer.Email = customerEmail
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{VariantID: product, Quantity: 1, UnitPrice: contracts.ProductPrice{Amount: 100000, Currency: "VND"}}}
	notifier := &fakeNotifier{}
	// The checkout fixture already owns a customer lookup carrying the seeded
	// address; only the notifier and the artist address are added here.
	f.svc.Notifier = notifier
	f.svc.ArtistEmail = artistEmail

	view, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	toArtist := messagesTo(notifier, artistEmail)
	if len(toArtist) != 1 {
		t.Fatalf("checkout must email the artist once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toArtist[0], view.ID.String(), string(constant.StatusPending))
	if got := messagesTo(notifier, customerEmail); len(got) != 0 {
		t.Fatalf("checkout must not email the customer, got %+v", got)
	}
}

// FR-020: confirming an order emails the customer its new state.
func TestConfirmEmailsTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(uuid.New(), 1000, 1)}, fixedNow)
	f := newLifecycleFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.ConfirmByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("ConfirmByAdmin: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("confirming must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusPaymentPending))
	if got := messagesTo(notifier, artistEmail); len(got) != 0 {
		t.Fatalf("a confirmation must not email the artist, got %+v", got)
	}
}

// FR-018, FR-020: declining an order emails the customer its cancellation.
func TestRejectEmailsTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	f := newAdminFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.RejectByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("RejectByAdmin: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("declining must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusCancelled))
}

// FR-010, FR-020: a confirmed payment emails the customer its paid state.
func TestMarkPaidEmailsTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1", order.Version); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("paying must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusPaid))
}

// FR-021: dispatching then confirming receipt of a delivered order emails the
// customer each new state.
func TestShipThenConfirmEmailTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	f := newAdminFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID}); err != nil {
		t.Fatalf("StartShippingByAdmin: %v", err)
	}
	// The delivery step (US2) moves the order to DELIVERED; here the fixture sets
	// it directly so the customer's confirmation can be exercised.
	order.Status = constant.StatusDelivered
	if _, err := f.svc.ConfirmReceived(actorContext(owner), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("ConfirmReceived: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 2 {
		t.Fatalf("dispatch then complete must email the customer twice, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusShipping))
	assertEmailNames(t, toCustomer[1], order.ID.String(), string(constant.StatusCompleted))
}

// FR-021: delivering an in-transit order emails the customer the new DELIVERED
// state the delivery lifecycle introduces, and does not reach the artist.
func TestDeliverEmailsTheCustomerDelivered(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusShipping
	f := newDeliveryFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.DeliverByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID}); err != nil {
		t.Fatalf("DeliverByAdmin: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("delivering must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusDelivered))
	if got := messagesTo(notifier, artistEmail); len(got) != 0 {
		t.Fatalf("a delivery must not email the artist, got %+v", got)
	}
}

// FR-021: the completion sweep's automatic completion emails the customer
// COMPLETED, just as the customer's own confirmation does, so the order nobody
// confirms still reaches the customer.
func TestAutoCompleteEmailsTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := expiredDeliveryOrder(owner, fixedNow.Add(-model.DeliveryCompletionWindow-time.Hour))
	f := newCompletionFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("the automatic completion must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusCompleted))
	if got := messagesTo(notifier, artistEmail); len(got) != 0 {
		t.Fatalf("an automatic completion must not email the artist, got %+v", got)
	}
}

// FR-022: when the customer submits feedback, the artist is emailed that the
// order carries it, and the customer is not — the message is the artist's.
func TestFeedbackEmailsTheArtist(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	const content = "Giao hàng cẩn thận, tranh nguyên vẹn"
	if _, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{OrderID: order.ID, Content: content}); err != nil {
		t.Fatalf("SubmitFeedback: %v", err)
	}

	toArtist := messagesTo(notifier, artistEmail)
	if len(toArtist) != 1 {
		t.Fatalf("a feedback must email the artist once, got %+v", notifier.sent)
	}
	if !strings.Contains(toArtist[0].subject, order.ID.String()) || !strings.Contains(toArtist[0].body, order.ID.String()) {
		t.Fatalf("the artist's note must name the order %s, got subject %q body %q", order.ID, toArtist[0].subject, toArtist[0].body)
	}
	if !strings.Contains(toArtist[0].body, content) {
		t.Fatalf("the artist's note must carry the feedback, got body %q", toArtist[0].body)
	}
	if got := messagesTo(notifier, customerEmail); len(got) != 0 {
		t.Fatalf("a feedback must not email the customer, got %+v", got)
	}
}

// FR-022, FR-023: a feedback reached over a failing notifier is still stored and
// answered — email never fails the feedback operation, and no note is recorded.
func TestAFailingNotifierDoesNotFailFeedback(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)
	enableNotifications(f.svc, &fakeNotifier{err: errors.New("smtp unavailable")}, owner)

	view, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{OrderID: order.ID, Content: "vẫn gửi được"})
	if err != nil {
		t.Fatalf("a failing notifier must not fail feedback, got %v", err)
	}
	if view.Feedback == nil || view.Feedback.Content != "vẫn gửi được" {
		t.Fatalf("the feedback must still be recorded, got %+v", view.Feedback)
	}
	if len(f.orders.inserts) != 1 {
		t.Fatalf("the feedback row must still be written, got %d", len(f.orders.inserts))
	}
}

// FR-011, FR-020: a customer cancel emails the customer its cancellation.
func TestCancelEmailsTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if err := f.svc.Cancel(context.Background(), order.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("cancelling must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusCancelled))
}

// FR-008, FR-020: an expired unpaid order emails the customer its cancellation
// once; a retried sweep neither releases nor emails again.
func TestExpireEmailsTheCustomerOnce(t *testing.T) {
	owner := uuid.New()
	expired := paymentPendingOrder(owner, line(uuid.New(), 1000, 1))
	past := fixedNow.Add(-time.Minute)
	expired.PaymentExpiresAt = &past
	f := newLifecycleFixture(expired)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("ExpireOrders: %v", err)
	}
	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("a retried ExpireOrders must not fail: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("expiry must email the customer exactly once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], expired.ID.String(), string(constant.StatusCancelled))
}

// FR-019: editing an order that stays awaiting confirmation re-notifies the
// artist, because it needs confirming afresh; the customer is not emailed, since
// the order's status did not change (FR-020).
func TestEditStayingPendingReNotifiesTheArtistOnly(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 1))
	store.orders[order.ID] = order
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{VariantID: product, Quantity: 2}},
	}); err != nil {
		t.Fatalf("EditMine: %v", err)
	}

	toArtist := messagesTo(notifier, artistEmail)
	if len(toArtist) != 1 {
		t.Fatalf("editing a pending order must re-notify the artist once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toArtist[0], order.ID.String(), string(constant.StatusPending))
	if got := messagesTo(notifier, customerEmail); len(got) != 0 {
		t.Fatalf("an edit that keeps the status must not email the customer, got %+v", got)
	}
}

// FR-014, FR-019, FR-020: editing an awaiting-payment order returns it to
// awaiting confirmation, so the artist is re-notified and the customer is emailed
// the changed status.
func TestEditReturningToPendingNotifiesBothSides(t *testing.T) {
	owner := uuid.New()
	store := &editOrders{orders: map[uuid.UUID]*model.Order{}}
	f := newEditFixture(store)
	product := seedEditProduct(f, "Tranh", "tranh", 1000, 10)
	order := editOrder(owner, line(product, 1000, 2))
	confirmEditOrder(order)
	store.orders[order.ID] = order
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.EditMine(actorContext(owner), appdto.EditInput{
		OrderID: order.ID,
		Lines:   []appdto.EditLineInput{{VariantID: product, Quantity: 3}},
	}); err != nil {
		t.Fatalf("EditMine: %v", err)
	}

	toArtist := messagesTo(notifier, artistEmail)
	if len(toArtist) != 1 {
		t.Fatalf("editing back to pending must re-notify the artist once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toArtist[0], order.ID.String(), string(constant.StatusPending))

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("editing back to pending must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusPending))
}

// FR-021: a notifier that fails never fails the order operation — checkout and a
// status change both succeed and persist their effect regardless.
func TestAFailingNotifierDoesNotFailTheOrderOperation(t *testing.T) {
	// Checkout over a failing notifier still creates the order.
	f := newCheckoutFixture(t)
	user := uuid.New()
	seedAddress(f, user)
	f.customers.customer.Email = customerEmail
	product := seedProduct(f, "Tranh", "tranh", 100000, 5)
	f.cart.lines = []contracts.CartLine{{VariantID: product, Quantity: 1, UnitPrice: contracts.ProductPrice{Amount: 100000, Currency: "VND"}}}
	f.svc.Notifier = &fakeNotifier{err: errors.New("smtp unavailable")}
	f.svc.ArtistEmail = artistEmail

	view, err := f.svc.Checkout(actorContext(user), appdto.CheckoutInput{})
	if err != nil {
		t.Fatalf("a failing notifier must not fail checkout, got %v", err)
	}
	if view.Status != constant.StatusPending || len(f.orders.created) != 1 {
		t.Fatalf("the order must still be created, got %+v", view)
	}

	// A status change over a failing notifier still changes the state.
	owner := uuid.New()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(uuid.New(), 1000, 1)}, fixedNow)
	lifecycle := newLifecycleFixture(order)
	enableNotifications(lifecycle.svc, &fakeNotifier{err: errors.New("smtp unavailable")}, owner)

	if _, err := lifecycle.svc.ConfirmByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("a failing notifier must not fail confirmation, got %v", err)
	}
	if order.Status != constant.StatusPaymentPending {
		t.Fatalf("the confirmation must still take effect, got %s", order.Status)
	}

	// A dispatch over a failing notifier still moves the order SHIPPING.
	paid := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
	paid.Status = constant.StatusPaid
	delivery := newDeliveryFixture(paid)
	enableNotifications(delivery.svc, &fakeNotifier{err: errors.New("smtp unavailable")}, paid.UserID)

	if _, err := delivery.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: paid.ID}); err != nil {
		t.Fatalf("a failing notifier must not fail dispatch, got %v", err)
	}
	if paid.Status != constant.StatusShipping {
		t.Fatalf("the dispatch must still take effect, got %s", paid.Status)
	}
}
