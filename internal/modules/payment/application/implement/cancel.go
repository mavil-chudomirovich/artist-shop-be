package implement

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is US4: cancelling an unpaid order cancels its payment request. It is
// the use case module 07 drives through the contracts.PaymentCancellation port
// after the order's own cancellation has committed, so the provider link the
// customer could still pay is cancelled too (FR-015, FR-016, research D9).
//
// The call is deliberately best-effort and reports nothing: the order's
// cancellation is already final and must never be blocked or rolled back by a
// payment or provider failure. A provider that refuses the cancel leaves the
// payment live for the operator to reconcile, and the failure is logged rather
// than returned (research D9, contracts/error-codes.md).

// cancelReason is the short reason sent to the provider when a cancelled order's
// link is cancelled and recorded on the payment's own PAYMENT_CANCELLED event.
const cancelReason = "order cancelled"

// CancelForOrder cancels the live payment of a cancelled order. It reads the
// order's live payment; when the order has none it is a no-op, so cancelling an
// order whose payment is already terminal changes nothing. Otherwise it asks the
// provider to cancel the link and, only when the provider accepts, moves the
// payment to CANCELLED and records the PAYMENT_CANCELLED event in one
// transaction.
//
// A provider failure is logged and never returned: the payment is left live for
// reconciliation, exactly as the webhook leaves an unapplicable confirmation for
// the operator (FR-016, research D9). The order's cancellation has already
// committed, so nothing here can un-cancel the order or its released goods.
//
// The provider call sits outside any transaction and before the write, so a slow
// or failing provider never holds a database connection open and can never fail
// the order's cancellation (research D9). The locked reload re-checks the state,
// so a concurrent webhook that already applied the confirmation, or a concurrent
// cancel, leaves the payment untouched.
func (s *Service) CancelForOrder(ctx context.Context, orderID uuid.UUID) {
	live, err := s.Payments.FindLiveByOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, domainerr.ErrNotFound) {
			// The order has no live payment — it was never started, or it is
			// already terminal — so there is nothing to cancel (FR-015).
			return
		}
		// Reading the payment is not the order's concern and the port cannot
		// carry an error; log and stop rather than fail the cancellation.
		s.log().ErrorContext(ctx, "payment: reading the live payment of a cancelled order failed",
			slog.String("orderId", orderID.String()),
			slog.Any("error", err))
		return
	}

	if err := s.Provider.CancelLink(ctx, dto.CancelLinkInput{
		OrderCode:     live.OrderCode,
		PaymentLinkID: live.PaymentLinkID,
		Reason:        cancelReason,
	}); err != nil {
		// Best-effort: the payment is left live for the operator to reconcile,
		// and the failure is logged, never returned (FR-016).
		s.log().WarnContext(ctx, "payment: the provider refused to cancel a cancelled order's link; left live for reconciliation",
			slog.String("orderId", orderID.String()),
			slog.String("paymentId", live.ID.String()),
			slog.Int64("orderCode", live.OrderCode),
			slog.Any("error", err))
		return
	}

	var cancelled *model.Payment
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		locked, err := s.Payments.LockByID(txCtx, live.ID)
		if err != nil {
			return err
		}
		// Re-check under the row lock: a concurrent confirmation that already
		// applied leaves this a no-op, so the payment is moved once (FR-009).
		if !locked.IsLive() {
			return nil
		}
		if err := locked.Cancel(); err != nil {
			return err
		}
		if err := s.Payments.UpdateStatus(txCtx, locked.ID, locked.Status, s.now()); err != nil {
			return err
		}
		if err := s.Payments.CreateEvent(txCtx, model.NewPaymentEvent(
			locked.ID, constant.AuditPaymentCancelled, nil, cancelReason, s.now())); err != nil {
			return err
		}
		cancelled = locked
		return nil
	}); err != nil {
		s.log().ErrorContext(ctx, "payment: marking a cancelled order's payment failed",
			slog.String("orderId", orderID.String()),
			slog.String("paymentId", live.ID.String()),
			slog.Any("error", err))
		return
	}

	// The cancellation is a system act from the payment module's point of view —
	// the port carries no acting account — so the shared audit entry names no
	// actor, exactly as the provider's webhook does (data-model.md).
	if cancelled != nil {
		s.recordCancelled(ctx, cancelled.ID, cancelled.OrderID)
	}
}

// recordCancelled writes one audit entry for a cancellation. The acting account is
// nil: the port carries none, so the payment module cannot name a person. It is
// best-effort and never fails the operation (Constitution VI); a nil auditor is a
// no-op.
func (s *Service) recordCancelled(ctx context.Context, paymentID, orderID uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditPaymentCancelled, string(audit.OutcomeSuccess), nil, "",
		targetTypePayment, paymentID.String(), map[string]any{"orderId": orderID.String()})
}
