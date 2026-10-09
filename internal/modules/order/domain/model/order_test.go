package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file exercises the order entity's state machine and total: every allowed
// edge succeeds, every forbidden move is refused naming the current state and
// leaves the order untouched, and the total is the exact sum of the line
// snapshots with no shipping fee and no rounding (FR-008, FR-009, FR-010,
// FR-011).

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
	order := NewOrder(uuid.New(), testAddress(), []OrderLine{line}, now.Add(15*time.Minute), now)
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

// move is one state transition the table tests drive.
type move struct {
	name string
	fn   func(*Order) error
	from constant.Status
	to   constant.Status
}

// FR-009: every allowed edge succeeds and lands on the expected state.
func TestEveryAllowedMoveSucceeds(t *testing.T) {
	moves := []move{
		{name: "pay", fn: (*Order).MarkPaid, from: constant.StatusPendingPayment, to: constant.StatusPaid},
		{name: "ship", fn: (*Order).Ship, from: constant.StatusPaid, to: constant.StatusShipped},
		{name: "complete", fn: (*Order).Complete, from: constant.StatusShipped, to: constant.StatusCompleted},
		{name: "cancel", fn: (*Order).Cancel, from: constant.StatusPendingPayment, to: constant.StatusCancelled},
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

// FR-009, FR-011: every forbidden move is refused with the current state named,
// and the order is left exactly as it was.
func TestEveryForbiddenMoveIsRefusedNamingTheCurrentState(t *testing.T) {
	all := []constant.Status{
		constant.StatusPendingPayment,
		constant.StatusPaid,
		constant.StatusShipped,
		constant.StatusCompleted,
		constant.StatusCancelled,
	}
	moves := []struct {
		name  string
		fn    func(*Order) error
		valid constant.Status
	}{
		{name: "pay", fn: (*Order).MarkPaid, valid: constant.StatusPendingPayment},
		{name: "ship", fn: (*Order).Ship, valid: constant.StatusPaid},
		{name: "complete", fn: (*Order).Complete, valid: constant.StatusShipped},
		{name: "cancel", fn: (*Order).Cancel, valid: constant.StatusPendingPayment},
	}

	for _, m := range moves {
		for _, from := range all {
			if from == m.valid {
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

// FR-009: a completed or cancelled order is terminal — no move out of either
// succeeds.
func TestCompletedAndCancelledAreTerminal(t *testing.T) {
	for _, terminal := range []constant.Status{constant.StatusCompleted, constant.StatusCancelled} {
		order := testOrder(terminal)
		for _, fn := range []func(*Order) error{(*Order).MarkPaid, (*Order).Ship, (*Order).Complete, (*Order).Cancel} {
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

// FR-008: the order total is the exact sum of quantity times the snapshot unit
// price, with no shipping fee and no rounding.
func TestOrderTotalIsTheSumOfLineSnapshots(t *testing.T) {
	now := time.Now().UTC()
	lines := []OrderLine{
		{ProductID: uuid.New(), Name: "A", Slug: "a", UnitPrice: Price{Amount: 120000, Currency: "VND"}, Quantity: 2},
		{ProductID: uuid.New(), Name: "B", Slug: "b", UnitPrice: Price{Amount: 33333, Currency: "VND"}, Quantity: 3},
	}
	order := NewOrder(uuid.New(), testAddress(), lines, now.Add(15*time.Minute), now)

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

// FR-001, FR-002: a fresh order starts awaiting payment, carries its address and
// timestamps snapshot, and every line is linked back to the order in position
// order.
func TestNewOrderStartsAwaitingPaymentWithItsLinesLinked(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	expires := now.Add(15 * time.Minute)
	owner := uuid.New()
	lines := []OrderLine{
		{ProductID: uuid.New(), Name: "A", Slug: "a", UnitPrice: Price{Amount: 1000, Currency: "VND"}, Quantity: 1},
		{ProductID: uuid.New(), Name: "B", Slug: "b", UnitPrice: Price{Amount: 2000, Currency: "VND"}, Quantity: 1},
	}

	order := NewOrder(owner, testAddress(), lines, expires, now)
	if order.ID == uuid.Nil {
		t.Fatal("a new order must carry an identifier")
	}
	if order.Status != constant.StatusPendingPayment {
		t.Fatalf("a new order must start awaiting payment, got %s", order.Status)
	}
	if order.UserID != owner {
		t.Fatalf("owner = %s, want %s", order.UserID, owner)
	}
	if !order.ExpiresAt.Equal(expires) || !order.CreatedAt.Equal(now) || !order.UpdatedAt.Equal(now) {
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
