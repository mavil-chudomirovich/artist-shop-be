package implement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is US2: the shop trusts the gateway's confirmation exactly once. It
// verifies the signature first, matches the payment by the provider's order code,
// compares the amount, and applies the result in one transaction with the order's
// paid transition — or records it UNMATCHED for the operator and never applies it
// (FR-007 to FR-011, research D7, D8).

// HandleWebhook verifies a raw provider callback and applies it exactly once
// (FR-007 to FR-011).
//
// The signature is verified first, over the exact received bytes, through the
// provider port; a body that does not verify is refused and nothing is applied
// (FR-007, research D3). The payment is found by the provider's order code, never
// by anything the request could choose; an order code no payment carries is a
// verified event that cannot be applied, so it is acknowledged with success — the
// provider stops retrying — while nothing is recorded, because there is no row to
// attach a record to, and a warning is logged for the operator to reconcile out
// of band (research D7).
//
// A payment that is already PAID is a no-op success, so a replayed callback
// applies nothing (FR-009). Otherwise the confirmation is matched against the
// stored amount and, when it matches, the order's version-aware paid transition
// is driven with the payment's own stable reference and the order version it was
// created for. The payment's PAID write and its PAYMENT_APPLIED event commit in
// the same UnitOfWork as the order's paid transition and the stock sale, so they
// agree or none happens (FR-010, Constitution II). A mismatched amount is an
// anomaly: recorded UNMATCHED with a reason and never applied (FR-008, D8), and
// the callback still answers success so the provider stops retrying. A
// version/state refusal from the order's paid transition is the same anomaly: a
// confirmation for superseded content, or for an order no longer awaiting
// payment, is recorded UNMATCHED with a reason, never applied, and the order is
// left untouched — no refund is attempted (FR-012, FR-013, D8). A re-delivery on
// an already-UNMATCHED payment is a no-op success, so no duplicate is recorded
// (FR-014).
func (s *Service) HandleWebhook(ctx context.Context, body []byte) error {
	confirmed, err := s.Provider.VerifyWebhook(ctx, body)
	if err != nil {
		if errors.Is(err, domainerr.ErrWebhookInvalidSignature) {
			// The event is not from the provider; nothing is trusted (FR-007).
			return err
		}
		// The body could not be read as the provider's envelope at all, so the
		// signature check never ran. Presentation answers the shared
		// MALFORMED_REQUEST (contracts/error-codes.md).
		return fmt.Errorf("%w: %v", domainerr.ErrMalformedWebhook, err)
	}

	payment, err := s.Payments.FindByOrderCode(ctx, confirmed.OrderCode)
	if err != nil {
		if errors.Is(err, domainerr.ErrNotFound) {
			// A verified event that cannot be applied: nothing is recorded,
			// because there is no row to attach a record to. It is acknowledged
			// with success so the provider stops retrying, and the orphaned link
			// is logged as a warning for the operator to reconcile out of band
			// (research D7).
			s.log().WarnContext(ctx, "payment webhook for an unknown order code",
				slog.Int64("orderCode", confirmed.OrderCode))
			return nil
		}
		return err
	}
	if payment.Status == constant.StatusPaid {
		// The confirmation is already applied; the replay is a no-op success so
		// PayOS stops retrying (FR-009).
		return nil
	}

	var recordedAction string
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		locked, err := s.Payments.LockByID(txCtx, payment.ID)
		if err != nil {
			return err
		}
		// Re-check under the row lock: a concurrent confirmation that already
		// applied leaves this one a no-op, so the sale is made once (FR-009,
		// Constitution II).
		if locked.Status == constant.StatusPaid || locked.Status == constant.StatusUnmatched {
			return nil
		}

		// A confirmation whose amount differs from the stored amount is an
		// anomaly: recorded for the operator and never applied (FR-008, D8).
		if confirmed.Amount != locked.Amount {
			reason := unmatchedReason(locked.Amount, confirmed.Amount)
			if err := locked.RecordUnmatched(reason); err != nil {
				return err
			}
			if err := s.Payments.RecordUnmatched(txCtx, locked.ID, locked.UnmatchedReason, s.now()); err != nil {
				return err
			}
			if err := s.Payments.CreateEvent(txCtx, model.NewPaymentEvent(
				locked.ID, constant.AuditPaymentUnmatched, nil, locked.UnmatchedReason, s.now())); err != nil {
				return err
			}
			recordedAction = constant.AuditPaymentUnmatched
			return nil
		}

		// The order's paid transition is driven inside the same transaction, with
		// the payment's own stable reference and the version it was created for,
		// so a superseded link is never applied (FR-009, FR-010, D5, D7).
		if err := s.Orders.MarkPaid(txCtx, locked.OrderID, sourceReference(locked.OrderCode), locked.OrderVersion); err != nil {
			stale, refusal := staleConfirmation(err)
			if !stale {
				// An unexpected failure rolls the whole transaction back.
				return err
			}
			// The confirmation belongs to superseded content, or the order is no
			// longer awaiting payment: it is recorded for the operator and never
			// applied. The order is left untouched and no refund is attempted
			// (FR-012, FR-013, research D8).
			if err := locked.RecordUnmatched(refusal.Error()); err != nil {
				return err
			}
			if err := s.Payments.RecordUnmatched(txCtx, locked.ID, locked.UnmatchedReason, s.now()); err != nil {
				return err
			}
			if err := s.Payments.CreateEvent(txCtx, model.NewPaymentEvent(
				locked.ID, constant.AuditPaymentUnmatched, nil, locked.UnmatchedReason, s.now())); err != nil {
				return err
			}
			recordedAction = constant.AuditPaymentUnmatched
			return nil
		}
		if err := locked.Apply(confirmed.Reference); err != nil {
			return err
		}
		if err := s.Payments.ApplyPaid(txCtx, locked.ID, locked.ProviderReference, s.now()); err != nil {
			return err
		}
		if err := s.Payments.CreateEvent(txCtx, model.NewPaymentEvent(
			locked.ID, constant.AuditPaymentApplied, nil, locked.ProviderReference, s.now())); err != nil {
			return err
		}
		recordedAction = constant.AuditPaymentApplied
		return nil
	}); err != nil {
		return err
	}

	if recordedAction != "" {
		s.recordWebhook(ctx, recordedAction, payment.ID, payment.OrderID)
	}
	return nil
}

// sourceReference builds the payment's own stable reference that module 07
// records per line, so a retried confirmation sells once (research D7). It is
// derived from the payment's stored order code, which is stable across replays.
func sourceReference(orderCode int64) string {
	return fmt.Sprintf("%s:%d", constant.ProviderPayOS, orderCode)
}

// unmatchedReason names the amount mismatch for the operator's reconciliation
// queue, without leaking anything beyond the two amounts (FR-008, D8).
func unmatchedReason(stored, confirmed int64) string {
	return fmt.Sprintf("amount mismatch: the payment asked for %d, the provider confirmed %d", stored, confirmed)
}

// staleConfirmation maps the order contract's refusal onto the payment module's
// own ErrStaleConfirmation sentinel, so the webhook branches on a payment-module
// error without importing module 07 (research D5, D8). Module 07's adapter already
// translated its version/state refusal onto contracts.ErrOrderStale — the order's
// content changed after the link was made — and contracts.ErrOrderNotPayable —
// the order is no longer awaiting payment. A refusal that is neither is returned
// unchanged, so an unexpected failure still propagates and rolls the transaction
// back. The contract refusal's own message (the two versions, the current state)
// is kept, so the unmatched reason names the mismatch for the operator (FR-012,
// FR-013).
func staleConfirmation(err error) (bool, error) {
	switch {
	case errors.Is(err, contracts.ErrOrderStale), errors.Is(err, contracts.ErrOrderNotPayable):
		return true, fmt.Errorf("%w: %v", domainerr.ErrStaleConfirmation, err)
	default:
		return false, err
	}
}

// recordWebhook writes one audit entry for a provider act. The acting account is
// nil: the provider is not a person (data-model.md, FR-019). It is best-effort
// and never fails the callback (Constitution VI); a nil auditor is a no-op.
func (s *Service) recordWebhook(ctx context.Context, action string, paymentID, orderID uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess), nil, "",
		targetTypePayment, paymentID.String(), map[string]any{"orderId": orderID.String()})
}
