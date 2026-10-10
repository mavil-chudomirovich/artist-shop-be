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

// This file is the use-case contract of US5: both sides are notified. The artist
// is emailed whenever an order needs confirmation — a fresh checkout and any edit
// that leaves the order awaiting confirmation — and the customer is emailed on
// every status change. Sending is best-effort and runs after the transaction
// commits, so a failing notifier never fails the order operation (FR-019 to
// FR-021, research D9). It runs the real use cases over a fake notifier and the
// in-memory fakes of every contract, so the answers asserted are the service's
// own.

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
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
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

	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1"); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 1 {
		t.Fatalf("paying must email the customer once, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusPaid))
}

// FR-020: shipping then completing a paid order emails the customer each new
// state.
func TestShipAndCompleteEmailTheCustomer(t *testing.T) {
	owner := uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	f := newAdminFixture(order)
	notifier := &fakeNotifier{}
	enableNotifications(f.svc, notifier, owner)

	if _, err := f.svc.ShipByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("ShipByAdmin: %v", err)
	}
	if _, err := f.svc.CompleteByAdmin(adminContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("CompleteByAdmin: %v", err)
	}

	toCustomer := messagesTo(notifier, customerEmail)
	if len(toCustomer) != 2 {
		t.Fatalf("ship then complete must email the customer twice, got %+v", notifier.sent)
	}
	assertEmailNames(t, toCustomer[0], order.ID.String(), string(constant.StatusShipped))
	assertEmailNames(t, toCustomer[1], order.ID.String(), string(constant.StatusCompleted))
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
		Lines:   []appdto.EditLineInput{{ProductID: product, Quantity: 2}},
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
		Lines:   []appdto.EditLineInput{{ProductID: product, Quantity: 3}},
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
	f.cart.lines = []contracts.CartLine{{ProductID: product, Quantity: 1, UnitPriceAmount: 100000, Currency: "VND"}}
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
}
