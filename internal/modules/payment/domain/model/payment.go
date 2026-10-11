package model

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
)

// Payment is module 08's own record of what the customer agreed to pay, what the
// provider said, and where that leaves both (data-model.md). It carries its own
// four-state life, separate from the order's single status axis.
type Payment struct {
	// ID identifies the payment.
	ID uuid.UUID
	// OrderID is the order the payment belongs to. A loose reference, with no
	// foreign key: the order lives in module 07 (research D5).
	OrderID uuid.UUID
	// UserID is the account that started the payment, copied at creation so the
	// operator can see who paid without a join. A loose reference.
	UserID uuid.UUID
	// OrderCode is PayOS's orderCode: a random positive 53-bit integer, unique by
	// an index (research D4).
	OrderCode int64
	// Amount is the amount requested, in the currency's minor unit. It equals the
	// order total with no shipping fee (FR-004, ADR 015 §5).
	Amount int64
	// Currency is the currency code: three uppercase letters, VND in MVP.
	Currency string
	// Description is the short description sent to PayOS (the payer sees it).
	Description string
	// OrderVersion is the order's content version at the time the payment was
	// created. A confirmation is applied only when this still matches (research
	// D5).
	OrderVersion int64
	// Status is where the payment is in its life. Changed only through the
	// transition methods below (FR-011).
	Status constant.Status
	// Provider is the provider name; payos.
	Provider string
	// ProviderReference is PayOS's own transaction reference once it confirms.
	// Empty until confirmed (research D7).
	ProviderReference string
	// PaymentLinkID is PayOS's paymentLinkId, the handle used to read and cancel
	// the link.
	PaymentLinkID string
	// CheckoutURL is the PayOS checkout URL the customer opens.
	CheckoutURL string
	// QRCode is the VietQR payload PayOS returns, for QR rendering.
	QRCode string
	// UnmatchedReason is why a verified confirmation could not be applied. Empty
	// unless the status is UNMATCHED (FR-012, FR-013).
	UnmatchedReason string
	// CreatedAt is when the payment was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is touched on every write.
	UpdatedAt time.Time
}

// NewPayment builds a fresh PENDING payment for one order. The amount is the
// order total the caller read through the OrderPayment contract, stored verbatim:
// there is no shipping fee and no re-computation here (FR-004, ADR 015 §5). The
// order code is supplied by the caller so a unique-index collision can be retried.
func NewPayment(orderID, userID uuid.UUID, orderCode, amount int64, currency, description string, orderVersion int64, now time.Time) *Payment {
	return &Payment{
		ID:           uuid.New(),
		OrderID:      orderID,
		UserID:       userID,
		OrderCode:    orderCode,
		Amount:       amount,
		Currency:     currency,
		Description:  description,
		OrderVersion: orderVersion,
		Status:       constant.StatusPending,
		Provider:     constant.ProviderPayOS,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// Apply moves a live payment to PAID and stores the provider's transaction
// reference. A repeat on an already-PAID payment is a no-op success, so a retried
// provider callback cannot double-apply (FR-009, research D7). A move from any
// other state is refused naming the current state (FR-011).
func (p *Payment) Apply(providerReference string) error {
	switch p.Status {
	case constant.StatusPaid:
		// The confirmation is already applied; the repeat is a success, not an
		// error, exactly as the order's own paid transition behaves (FR-009).
		return nil
	case constant.StatusPending:
		p.Status = constant.StatusPaid
		p.ProviderReference = providerReference
		return nil
	default:
		return domainerr.StateTransitionInvalid(p.Status, constant.StatusPaid)
	}
}

// Cancel moves a live payment to CANCELLED. It is the edge a cancelled order
// drives through the PaymentCancellation contract. A payment that is not live is
// refused naming the current state; the caller's own "no live payment" case is a
// no-op before this is reached (FR-015, research D9).
func (p *Payment) Cancel() error {
	if p.Status != constant.StatusPending {
		return domainerr.StateTransitionInvalid(p.Status, constant.StatusCancelled)
	}
	p.Status = constant.StatusCancelled
	return nil
}

// RecordUnmatched records a verified confirmation that cannot be applied as
// UNMATCHED with the reason, from any state. A reason is required — the storage
// check ties UNMATCHED to a non-null reason and the operator's queue needs it —
// and a re-delivered confirmation on an already-UNMATCHED payment is a no-op so it
// records no duplicate (FR-012, FR-013, research D8).
func (p *Payment) RecordUnmatched(reason string) error {
	if reason == "" {
		return domainerr.UnmatchedReasonRequired()
	}
	if p.Status == constant.StatusUnmatched {
		return nil
	}
	p.Status = constant.StatusUnmatched
	p.UnmatchedReason = reason
	return nil
}

// IsLive reports whether the payment is still payable: only PENDING is live.
func (p Payment) IsLive() bool { return p.Status == constant.StatusPending }

// IsTerminal reports whether the payment has reached a state no move leaves:
// PAID, CANCELLED and UNMATCHED are all terminal.
func (p Payment) IsTerminal() bool { return p.Status != constant.StatusPending }

// NewOrderCode returns a random positive 53-bit integer for PayOS's orderCode. The
// caller stores it beside the order id and retries on the unique-index collision;
// a random code avoids leaking order volume and invites no guessing (research D4).
func NewOrderCode() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(constant.MaxOrderCode))
	if err != nil {
		return 0, err
	}
	// n is in [0, MaxOrderCode-1]; +1 yields [1, MaxOrderCode].
	return n.Int64() + 1, nil
}
