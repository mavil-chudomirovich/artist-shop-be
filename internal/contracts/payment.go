package contracts

import (
	"context"

	"github.com/google/uuid"
)

// PaymentCancellation lets the order module (07) tell the payment module (08) that
// an order was cancelled, so the provider payment request it created is cancelled
// too — the mirror direction of OrderPayment (research D9).
//
// The payment module supplies an adapter at the composition root. The call is
// best-effort: a nil adapter is a no-op, and a provider failure is logged by the
// payment module and never fails the order's cancellation. A consumer must
// therefore never depend on this method's outcome, and must call it after its own
// transaction has committed, never from inside it.
type PaymentCancellation interface {
	// CancelForOrder cancels the live payment of a cancelled order. It is
	// best-effort and reports nothing: a live payment the provider refuses to
	// cancel is logged for the operator, and an order with no live payment is a
	// no-op.
	CancelForOrder(ctx context.Context, orderID uuid.UUID)
}
