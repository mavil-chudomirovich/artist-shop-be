package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file exercises the order entity's six-state machine and its total: every
// allowed edge succeeds, every forbidden move is refused naming the current state
// and leaves the order untouched, editing is allowed only before payment, and the
// total is the exact sum of the line snapshots with no shipping fee and no
// rounding (FR-001, FR-004, FR-008, FR-010, FR-012, FR-018).

// testOrder builds an order in the given state over one snapshot line, with an
// identifier and timestamps so the assertions can observe them.
func testOrder(status constant.Status) *Order {
	line := OrderLine{
		ProductID: uuid.New(),
		Name:      "Ink wash",
		Slug:      "ink-wash",
		UnitPrice: Price{Amount: 120000, Currency: "VND"},
		Quantity:  2,
	}
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	order := NewOrder(uuid.New(), testAddress(), []OrderLine{line}, now)
	order.Status = status
	return order
}

// testAddress is the delivery address every test order carries.
func testAddress() Address {
	return Address{
		RecipientName:  "Nguyễn Văn A",
		RecipientPhone: "0912345678",
		ProvinceCode:   "01",
		ProvinceName:   "Hà Nội",
		WardCode:       "00001",
		WardName:       "Phúc Xá",
		StreetAddress:  "1 Đinh Tiên Hoàng",
	}
}

// allStates is the six-state set, used to drive the forbidden-move table.
var allStates = []constant.Status{
	constant.StatusPending,
	constant.StatusPaymentPending,
	constant.StatusPaid,
	constant.StatusShipped,
	constant.StatusCompleted,
	constant.StatusCancelled,
}

// move is one allowed state transition the table tests drive.
type move struct {
	name string
	fn   func(*Order) error
	from constant.Status
	to   constant.Status
}

// FR-001, FR-004, FR-010: every allowed edge succeeds and lands on the expected
// state, including Cancel from both pre-paid states.
func TestEveryAllowedMoveSucceeds(t *testing.T) {
	moves := []move{
		{name: "confirm", fn: (*Order).Confirm, from: constant.StatusPending, to: constant.StatusPaymentPending},
		{name: "reject", fn: (*Order).Reject, from: constant.StatusPending, to: constant.StatusCancelled},
		{name: "cancel from PENDING", fn: (*Order).Cancel, from: constant.StatusPending, to: constant.StatusCancelled},
		{name: "cancel from PAYMENT_PENDING", fn: (*Order).Cancel, from: constant.StatusPaymentPending, to: constant.StatusCancelled},
		{name: "pay", fn: (*Order).MarkPaid, from: constant.StatusPaymentPending, to: constant.StatusPaid},
		{name: "ship", fn: (*Order).Ship, from: constant.StatusPaid, to: constant.StatusShipped},
		{name: "complete", fn: (*Order).Complete, from: constant.StatusShipped, to: constant.StatusCompleted},
	}

	for _, m := range moves {
		t.Run(m.name, func(t *testing.T) {
			order := testOrder(m.from)
			if err := m.fn(order); err != nil {
				t.Fatalf("%s from %s: %v", m.name, m.from, err)
			}
			if order.Status != m.to {
				t.Fatalf("%s moved to %s, want %s", m.name, order.Status, m.to)
			}
		})
	}
}

// FR-010, FR-011: every forbidden move is refused with the current state named,
// and the order is left exactly as it was. Cancel is allowed from either pre-paid
// state and from nowhere else; MarkPaid only from PAYMENT_PENDING.
func TestEveryForbiddenMoveIsRefusedNamingTheCurrentState(t *testing.T) {
	moves := []struct {
		name    string
		fn      func(*Order) error
		allowed []constant.Status
	}{
		{name: "confirm", fn: (*Order).Confirm, allowed: []constant.Status{constant.StatusPending}},
		{name: "reject", fn: (*Order).Reject, allowed: []constant.Status{constant.StatusPending}},
		{name: "pay", fn: (*Order).MarkPaid, allowed: []constant.Status{constant.StatusPaymentPending}},
		{name: "ship", fn: (*Order).Ship, allowed: []constant.Status{constant.StatusPaid}},
		{name: "complete", fn: (*Order).Complete, allowed: []constant.Status{constant.StatusShipped}},
		{name: "cancel", fn: (*Order).Cancel, allowed: []constant.Status{constant.StatusPending, constant.StatusPaymentPending}},
	}

	for _, m := range moves {
		for _, from := range allStates {
			if contains(m.allowed, from) {
				continue
			}
			t.Run(m.name+" from "+string(from), func(t *testing.T) {
				order := testOrder(from)
				err := m.fn(order)
				if err == nil {
					t.Fatalf("%s from %s must be refused", m.name, from)
				}
				if !errors.Is(err, domainerr.ErrStateTransitionInvalid) {
					t.Fatalf("%s from %s: expected ErrStateTransitionInvalid, got %v", m.name, from, err)
				}
				var transition *domainerr.StateTransitionError
				if !errors.As(err, &transition) {
					t.Fatalf("the refusal must carry the current state, got %T", err)
				}
				if transition.From != from {
					t.Fatalf("the refusal names %s, want the current state %s", transition.From, from)
				}
				if order.Status != from {
					t.Fatalf("a refused move changed the state to %s", order.Status)
				}
			})
		}
	}
}

// FR-018: a completed or cancelled order is terminal — no move out of either
// succeeds.
func TestCompletedAndCancelledAreTerminal(t *testing.T) {
	for _, terminal := range []constant.Status{constant.StatusCompleted, constant.StatusCancelled} {
		order := testOrder(terminal)
		for _, fn := range []func(*Order) error{(*Order).Confirm, (*Order).Reject, (*Order).MarkPaid, (*Order).Ship, (*Order).Complete, (*Order).Cancel} {
			if err := fn(order); !errors.Is(err, domainerr.ErrStateTransitionInvalid) {
				t.Fatalf("a move out of %s must be refused, got %v", terminal, err)
			}
		}
		if order.Status != terminal {
			t.Fatalf("a refused move changed the terminal state to %s", order.Status)
		}
		if !order.IsTerminal() {
			t.Fatalf("%s must report terminal", terminal)
		}
	}
}

// FR-012: editing is allowed while the order awaits the artist or payment; an
// awaiting-payment edit releases the deadline and returns the order to
// awaiting the artist; a paid order or beyond is refused with ErrNotEditable and
// left untouched. Every accepted edit bumps the content version.
func TestEditIsAllowedOnlyBeforePaymentAndBumpsTheVersion(t *testing.T) {
	// Editing an awaiting-confirmation order keeps it awaiting the artist and
	// bumps the version.
	pending := testOrder(constant.StatusPending)
	if err := pending.Edit(); err != nil {
		t.Fatalf("editing an awaiting-confirmation order must be allowed, got %v", err)
	}
	if pending.Status != constant.StatusPending || pending.Version != 2 {
		t.Fatalf("a PENDING edit must stay PENDING at version 2, got %s/%d", pending.Status, pending.Version)
	}

	// Editing an awaiting-payment order returns it to awaiting the artist and
	// clears the confirmation instant and the payment deadline.
	confirmedAt := time.Now().UTC()
	expiresAt := confirmedAt.Add(time.Hour)
	awaitingPayment := testOrder(constant.StatusPaymentPending)
	awaitingPayment.ConfirmedAt = &confirmedAt
	awaitingPayment.PaymentExpiresAt = &expiresAt
	if err := awaitingPayment.Edit(); err != nil {
		t.Fatalf("editing an awaiting-payment order must be allowed, got %v", err)
	}
	if awaitingPayment.Status != constant.StatusPending {
		t.Fatalf("a PAYMENT_PENDING edit must return the order to PENDING, got %s", awaitingPayment.Status)
	}
	if awaitingPayment.Version != 2 {
		t.Fatalf("a PAYMENT_PENDING edit must bump the version, got %d", awaitingPayment.Version)
	}
	if awaitingPayment.ConfirmedAt != nil || awaitingPayment.PaymentExpiresAt != nil {
		t.Fatalf("a PAYMENT_PENDING edit must clear confirmed_at and payment_expires_at, got %v/%v",
			awaitingPayment.ConfirmedAt, awaitingPayment.PaymentExpiresAt)
	}

	// A paid order or beyond is frozen.
	for _, frozen := range []constant.Status{
		constant.StatusPaid,
		constant.StatusShipped,
		constant.StatusCompleted,
		constant.StatusCancelled,
	} {
		order := testOrder(frozen)
		if err := order.Edit(); !errors.Is(err, domainerr.ErrNotEditable) {
			t.Fatalf("editing a %s order must be refused with ErrNotEditable, got %v", frozen, err)
		}
		if order.Status != frozen || order.Version != 1 {
			t.Fatalf("a refused edit must leave %s untouched at version 1, got %s/%d", frozen, order.Status, order.Version)
		}
	}
}

// FR-001, FR-002: a fresh order starts awaiting the artist's confirmation, holds
// no deadline, carries version 1, and links every line back to the order in
// position order.
func TestNewOrderStartsPendingWithItsLinesLinked(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	owner := uuid.New()
	lines := []OrderLine{
		{ProductID: uuid.New(), Name: "A", Slug: "a", UnitPrice: Price{Amount: 1000, Currency: "VND"}, Quantity: 1},
		{ProductID: uuid.New(), Name: "B", Slug: "b", UnitPrice: Price{Amount: 2000, Currency: "VND"}, Quantity: 1},
	}

	order := NewOrder(owner, testAddress(), lines, now)
	if order.ID == uuid.Nil {
		t.Fatal("a new order must carry an identifier")
	}
	if order.Status != constant.StatusPending {
		t.Fatalf("a new order must start PENDING, got %s", order.Status)
	}
	if order.Version != 1 {
		t.Fatalf("a new order must start at version 1, got %d", order.Version)
	}
	if order.ConfirmedAt != nil || order.PaymentExpiresAt != nil {
		t.Fatalf("a new order must carry no confirmation instant and no deadline, got %v/%v", order.ConfirmedAt, order.PaymentExpiresAt)
	}
	if order.UserID != owner {
		t.Fatalf("owner = %s, want %s", order.UserID, owner)
	}
	if !order.CreatedAt.Equal(now) || !order.UpdatedAt.Equal(now) {
		t.Fatalf("the order did not carry its timestamps: %+v", order)
	}
	if len(order.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(order.Lines))
	}
	for i, line := range order.Lines {
		if line.OrderID != order.ID {
			t.Errorf("line %d is not linked to the order", i)
		}
		if line.Position != i {
			t.Errorf("line %d has position %d, want %d", i, line.Position, i)
		}
		if line.ID == uuid.Nil {
			t.Errorf("line %d has no identifier", i)
		}
	}
}

// FR-008: the order total is the exact sum of quantity times the snapshot unit
// price, with no shipping fee and no rounding.
func TestOrderTotalIsTheSumOfLineSnapshots(t *testing.T) {
	now := time.Now().UTC()
	lines := []OrderLine{
		{ProductID: uuid.New(), Name: "A", Slug: "a", UnitPrice: Price{Amount: 120000, Currency: "VND"}, Quantity: 2},
		{ProductID: uuid.New(), Name: "B", Slug: "b", UnitPrice: Price{Amount: 33333, Currency: "VND"}, Quantity: 3},
	}
	order := NewOrder(uuid.New(), testAddress(), lines, now)

	// 2*120000 + 3*33333 = 240000 + 99999 = 339999, exactly.
	const want int64 = 339999
	if order.Total.Amount != want {
		t.Fatalf("total = %d, want the exact sum %d (no shipping, no rounding)", order.Total.Amount, want)
	}
	if order.Total.Currency != "VND" {
		t.Fatalf("total currency = %q, want VND", order.Total.Currency)
	}

	var sum int64
	for _, line := range order.Lines {
		sum += line.Quantity * line.UnitPrice.Amount
	}
	if order.Total.Amount != sum {
		t.Fatalf("total = %d, want the sum of the lines %d", order.Total.Amount, sum)
	}
}

// contains reports whether values holds target.
func contains(values []constant.Status, target constant.Status) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
