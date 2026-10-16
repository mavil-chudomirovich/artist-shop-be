package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrOrderNotFound is returned by OrderPayment when no order carries the
// identifier. The providing module (order, 07) translates its own not-found into
// this sentinel, exactly as AccountLookup does with ErrAccountNotFound, so the
// payment module can classify a refusal without importing the order module's
// internals (docs/system-design/contract-purity.md).
var ErrOrderNotFound = errors.New("order not found")

// ErrOrderNotPayable is returned by OrderPayment.MarkPaid when the order is not
// awaiting payment — it is still awaiting the artist, was cancelled, or expired —
// so the confirmed payment must never be applied to it. The failing module is 07;
// the sentinel lives here so module 08 can classify the refusal without importing
// another module's error vocabulary.
var ErrOrderNotPayable = errors.New("order is not awaiting payment")

// ErrOrderStale is returned by OrderPayment.MarkPaid when the order's content
// version differs from the version the payment was created for — the order was
// edited and re-confirmed after the payment link was made — so the confirmation
// belongs to superseded content and must not be applied (research D5).
var ErrOrderStale = errors.New("order content is stale")

// OrderPayment exposes an order's payment-relevant facts and its version-aware
// paid transition to the payment module (08), so the payment can read the order
// and drive the paid edge without ever reading the order's tables (Constitution I,
// research D5).
//
// The provider module (order, 07) supplies an adapter at the composition root.
// The contract carries only contract-local types — no module's entity or enum —
// exactly as VariantSummary carries OnSale rather than module 04's state type
// (docs/system-design/contract-purity.md).
type OrderPayment interface {
	// PaymentTarget returns the facts a payment needs about an order: its owner,
	// its total, its content version, its payment deadline and whether it is
	// awaiting payment. A missing order answers ErrOrderNotFound.
	PaymentTarget(ctx context.Context, orderID uuid.UUID) (OrderPaymentTarget, error)

	// MarkPaid drives the order's paid transition. An order already paid (or
	// beyond) is a no-op success, exactly as module 07's delivered transition
	// already behaves, so a retried provider callback cannot double-apply.
	// An order awaiting the artist, cancelled or expired — or one whose version
	// is not expectedVersion — is refused with ErrOrderNotPayable or
	// ErrOrderStale, so a confirmation for superseded content is never applied
	// (FR-009, FR-012, research D5).
	MarkPaid(ctx context.Context, orderID uuid.UUID, sourceReference string, expectedVersion int64) error
}

// OrderPaymentTarget carries only what a payment needs, in contract-local types:
// the package imports no module, exactly as contracts.VariantSummary carries
// OnSale rather than module 04's state type (docs/system-design/contract-purity.md).
type OrderPaymentTarget struct {
	// UserID is the account that owns the order.
	UserID uuid.UUID
	// Amount is the order's committed total, in the currency's minor unit, with
	// no shipping fee (ADR 015 §5).
	Amount int64
	// Currency is the currency the total is denominated in.
	Currency string
	// Version is the order's content version at the instant of the read. A
	// confirmation whose stored version differs from it must not be applied.
	Version int64
	// PaymentExpiresAt is when an awaiting-payment order cancels itself. It is the
	// zero time for an order that is not awaiting payment.
	PaymentExpiresAt time.Time
	// AwaitingPayment reports whether the order is awaiting payment. It is a flag,
	// not module 07's status value, so no enum type crosses the boundary.
	AwaitingPayment bool
}
