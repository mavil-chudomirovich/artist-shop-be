package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
)

// This file is the domain contract of the payment's four-state machine
// (Constitution III, IV). Every edge is covered both ways, a repeat on a PAID
// payment is a no-op success, a record as UNMATCHED requires a reason and no
// other state may carry one, the order code is a random positive 53-bit integer,
// and a fresh payment is for the order total with no shipping fee (research D4,
// D6, D8, D13).

// testNow is the deterministic instant the fixture stamps payments with.
var testNow = time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)

// newTestPayment builds a fresh PENDING payment for one order.
func newTestPayment() *Payment {
	return NewPayment(uuid.New(), uuid.New(), 123, 3000, "VND", "VQRIO123", 1, testNow)
}

// FR-011: the only edge into PAID is PENDING→PAID; a repeat on a PAID payment is
// a no-op success; a move from CANCELLED or UNMATCHED is refused naming the state.
func TestApplyIsTheOnlyEdgeToPaidAndIsIdempotent(t *testing.T) {
	t.Run("pending to paid", func(t *testing.T) {
		p := newTestPayment()
		if err := p.Apply("ref-1"); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if p.Status != constant.StatusPaid {
			t.Fatalf("status = %s, want PAID", p.Status)
		}
		if p.ProviderReference != "ref-1" {
			t.Fatalf("provider reference = %q, want ref-1", p.ProviderReference)
		}
	})

	t.Run("repeat on paid is a no-op success", func(t *testing.T) {
		p := newTestPayment()
		if err := p.Apply("ref-1"); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if err := p.Apply("ref-2"); err != nil {
			t.Fatalf("a repeat on PAID must be a no-op success, got %v", err)
		}
		if p.ProviderReference != "ref-1" {
			t.Fatalf("a repeat must not overwrite the reference, got %q", p.ProviderReference)
		}
	})

	for _, status := range []constant.Status{constant.StatusCancelled, constant.StatusUnmatched} {
		t.Run("refuses "+string(status), func(t *testing.T) {
			p := newTestPayment()
			p.Status = status
			var refusal *domainerr.StateTransitionError
			err := p.Apply("ref-1")
			if !errors.As(err, &refusal) {
				t.Fatalf("Apply from %s must be refused, got %v", status, err)
			}
			if refusal.From != status || refusal.To != constant.StatusPaid {
				t.Fatalf("the refusal must name %s→PAID, got %+v", status, refusal)
			}
			if p.Status != status {
				t.Fatalf("a refused move must leave the state, got %s", p.Status)
			}
		})
	}
}

// FR-011: the only edge into CANCELLED is PENDING→CANCELLED; a move from any
// other state is refused because the payment is not live.
func TestCancelIsTheOnlyEdgeToCancelled(t *testing.T) {
	t.Run("pending to cancelled", func(t *testing.T) {
		p := newTestPayment()
		if err := p.Cancel(); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		if p.Status != constant.StatusCancelled {
			t.Fatalf("status = %s, want CANCELLED", p.Status)
		}
	})

	for _, status := range []constant.Status{constant.StatusPaid, constant.StatusCancelled, constant.StatusUnmatched} {
		t.Run("refuses "+string(status), func(t *testing.T) {
			p := newTestPayment()
			p.Status = status
			err := p.Cancel()
			if !errors.Is(err, domainerr.ErrStateTransitionInvalid) {
				t.Fatalf("Cancel from %s must be refused, got %v", status, err)
			}
			if p.Status != status {
				t.Fatalf("a refused cancel must leave the state, got %s", p.Status)
			}
		})
	}
}

// FR-012, research D8: UNMATCHED is reachable from every state, but only with a
// reason; a re-delivered confirmation on an already-UNMATCHED payment is a no-op.
func TestRecordUnmatchedFromAnyStateAndIsIdempotent(t *testing.T) {
	seeds := []struct {
		name string
		seed func(*Payment)
	}{
		{name: "from pending", seed: func(*Payment) {}},
		{name: "from paid", seed: func(p *Payment) { _ = p.Apply("ref-1") }},
		{name: "from cancelled", seed: func(p *Payment) { _ = p.Cancel() }},
	}
	for _, tc := range seeds {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPayment()
			tc.seed(p)
			if err := p.RecordUnmatched("amount mismatch"); err != nil {
				t.Fatalf("RecordUnmatched: %v", err)
			}
			if p.Status != constant.StatusUnmatched || p.UnmatchedReason != "amount mismatch" {
				t.Fatalf("got %s / %q, want UNMATCHED / amount mismatch", p.Status, p.UnmatchedReason)
			}
		})
	}

	t.Run("repeat is a no-op", func(t *testing.T) {
		p := newTestPayment()
		if err := p.RecordUnmatched("first"); err != nil {
			t.Fatalf("RecordUnmatched: %v", err)
		}
		if err := p.RecordUnmatched("second"); err != nil {
			t.Fatalf("a re-delivered UNMATCHED record must be a no-op, got %v", err)
		}
		if p.UnmatchedReason != "first" {
			t.Fatalf("a re-delivery must not overwrite the reason, got %q", p.UnmatchedReason)
		}
	})
}

// FR-012, FR-013: an UNMATCHED payment must carry a reason (the operator's queue
// needs it), and no other state may carry one — the only writer is RecordUnmatched.
func TestUnmatchedRequiresAReasonAndNoOtherStateCarriesOne(t *testing.T) {
	p := newTestPayment()
	if err := p.RecordUnmatched(""); !errors.Is(err, domainerr.ErrUnmatchedReasonRequired) {
		t.Fatalf("UNMATCHED without a reason must be refused, got %v", err)
	}
	if p.Status != constant.StatusPending || p.UnmatchedReason != "" {
		t.Fatalf("a refused record must leave the payment untouched, got %s / %q", p.Status, p.UnmatchedReason)
	}

	for _, seed := range []func(*Payment){
		func(*Payment) {},
		func(p *Payment) { _ = p.Apply("ref-1") },
		func(p *Payment) { _ = p.Cancel() },
	} {
		fresh := newTestPayment()
		seed(fresh)
		if fresh.UnmatchedReason != "" {
			t.Fatalf("state %s must not carry an unmatched reason, got %q", fresh.Status, fresh.UnmatchedReason)
		}
	}
}

// FR-011, research D6: PENDING is live and not terminal; PAID, CANCELLED and
// UNMATCHED are terminal and not live.
func TestIsLiveAndIsTerminal(t *testing.T) {
	live := newTestPayment()
	if !live.IsLive() {
		t.Error("a PENDING payment must be live")
	}
	if live.IsTerminal() {
		t.Error("a PENDING payment must not be terminal")
	}

	for _, status := range []constant.Status{constant.StatusPaid, constant.StatusCancelled, constant.StatusUnmatched} {
		p := newTestPayment()
		p.Status = status
		if p.IsLive() {
			t.Errorf("a %s payment must not be live", status)
		}
		if !p.IsTerminal() {
			t.Errorf("a %s payment must be terminal", status)
		}
	}
}

// research D4: the order code is a random positive 53-bit integer, distinct across
// many draws so the unique index collision is rare enough to retry.
func TestNewOrderCodeIsARandomPositive53BitInteger(t *testing.T) {
	seen := make(map[int64]bool, 4096)
	for i := 0; i < 4096; i++ {
		code, err := NewOrderCode()
		if err != nil {
			t.Fatalf("NewOrderCode: %v", err)
		}
		if code <= 0 || code > constant.MaxOrderCode {
			t.Fatalf("code %d is outside (0, %d]", code, constant.MaxOrderCode)
		}
		if seen[code] {
			t.Fatalf("code %d repeated; the generator must be distinct enough", code)
		}
		seen[code] = true
	}
}

// FR-004, research D13: a fresh payment is for the order total exactly, in its
// currency, with no shipping fee added.
func TestNewPaymentIsForTheOrderTotalWithNoShippingFee(t *testing.T) {
	orderID, userID := uuid.New(), uuid.New()
	p := NewPayment(orderID, userID, 555, 120000, "VND", "VQRIO123", 2, testNow)

	if p.OrderID != orderID || p.UserID != userID {
		t.Fatalf("payment names order %s / user %s, want %s / %s", p.OrderID, p.UserID, orderID, userID)
	}
	if p.Amount != 120000 || p.Currency != "VND" {
		t.Fatalf("amount = %d %s, want the order total 120000 VND unchanged", p.Amount, p.Currency)
	}
	if p.Status != constant.StatusPending {
		t.Fatalf("a fresh payment must be PENDING, got %s", p.Status)
	}
	if p.Provider != constant.ProviderPayOS {
		t.Fatalf("provider = %q, want %q", p.Provider, constant.ProviderPayOS)
	}
	if p.OrderVersion != 2 {
		t.Fatalf("order version = %d, want the order's 2", p.OrderVersion)
	}
	if p.ID == uuid.Nil || !p.CreatedAt.Equal(testNow) {
		t.Fatalf("a fresh payment must carry an id and the creation instant, got %s / %v", p.ID, p.CreatedAt)
	}
}
