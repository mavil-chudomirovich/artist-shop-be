package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
)

// PaymentRepository persists the payment and its events. The concrete
// implementation (infrastructure/implement/postgres) satisfies this interface.
//
// No method opens a transaction: the application owns the boundary through the
// UnitOfWork port, and every method here joins the transaction the context
// already carries (Constitution I, Constitution II). The write methods are narrow
// so a transition can never overwrite a concurrent one, and each ties a status
// change to the column the storage check requires — a paid write stores its
// reference, an unmatched write stores its reason — so the two can never disagree.
type PaymentRepository interface {
	// Create inserts a payment. The unique index on order_code is what refuses a
	// collision; the caller retries with a fresh code (research D4). It joins the
	// caller's transaction.
	Create(ctx context.Context, payment *model.Payment) error

	// FindByID returns one payment, or domainerr.ErrNotFound. It joins the
	// caller's transaction.
	FindByID(ctx context.Context, id uuid.UUID) (*model.Payment, error)

	// FindByOrder returns every payment of one order, newest first (created_at
	// then id). It joins the caller's transaction.
	FindByOrder(ctx context.Context, orderID uuid.UUID) ([]model.Payment, error)

	// FindLiveByOrder returns the live (PENDING) payment of one order, or
	// domainerr.ErrNotFound when the order has none. A cancelled order whose
	// payment is already terminal answers not-found, so cancelling a payment is a
	// no-op on the second pass. It joins the caller's transaction.
	FindLiveByOrder(ctx context.Context, orderID uuid.UUID) (*model.Payment, error)

	// FindByOrderCode returns the payment carrying the provider's order code, or
	// domainerr.ErrNotFound. It is the webhook's lookup, and it never trusts
	// anything else from the client. It joins the caller's transaction.
	FindByOrderCode(ctx context.Context, orderCode int64) (*model.Payment, error)

	// ListAll returns one page of every payment, newest first (created_at then
	// id), optionally narrowed to one state, and the total number of them.
	// status=UNMATCHED is the reconciliation queue (FR-018, research D15). It
	// joins the caller's transaction.
	ListAll(ctx context.Context, page, size int, status *constant.Status) ([]model.Payment, int64, error)

	// LockByID returns one payment under its row lock, for a transition. The lock
	// serialises two concurrent confirmations of the same webhook so the apply
	// happens once (FR-009, Constitution II). It joins the caller's transaction.
	LockByID(ctx context.Context, id uuid.UUID) (*model.Payment, error)

	// UpdateStatus writes one payment's status and touches its updated_at. It is
	// used by the cancel path, where no provider reference or reason is written. It
	// joins the caller's transaction.
	UpdateStatus(ctx context.Context, id uuid.UUID, status constant.Status, now time.Time) error

	// ApplyPaid marks a payment PAID and stores the provider's transaction
	// reference in one write, so the status and the reference it belongs to commit
	// together (FR-009, research D7). It joins the caller's transaction.
	ApplyPaid(ctx context.Context, id uuid.UUID, providerReference string, now time.Time) error

	// RecordUnmatched marks a payment UNMATCHED and stores the reason in one
	// write, satisfying the (status = 'UNMATCHED') = (reason IS NOT NULL) check
	// (FR-012, FR-013). It joins the caller's transaction.
	RecordUnmatched(ctx context.Context, id uuid.UUID, reason string, now time.Time) error
}
