package implement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is US2: the order's selling life. Every move runs in one UnitOfWork
// transaction, locking the order (LockByID) so the domain transition reads the
// current state under the row lock, driving the transition, and calling module 05
// in the same transaction: a confirmed payment consumes each line's hold into a
// sale, and a cancel returns it. A move the current state does not allow is
// refused by the domain with the current state named and leaves the order — and
// the stock — exactly as it was (FR-009 to FR-015, research D5, D6, D13).

// MarkPaid turns an awaiting-payment order into a paid one and consumes its hold
// into a sale, exactly once. It is the edge module 08's confirmed payment drives;
// it has no HTTP surface (FR-014, research D13).
//
// The payment event's identity is combined with each line's product identifier
// into a per-line source reference, because module 05's source_reference is
// globally unique and one order turns N lines into N movements: one reference for
// the whole order would collide on the second line (FR-014, research D5).
//
// An event whose effect is already applied is a no-op success, mirroring module
// 05, whose Reserve, Release and ApplySale are all no-op successes when their
// effect is already applied: an order already past the awaiting-payment edge —
// paid, shipped or completed — returns nil without a second transition or a
// second sale, so replaying the same payment sells once (FR-014). A payment
// arriving after cancellation is a real anomaly, not a replay, so it is refused
// by the state machine naming the cancelled state (FR-009, FR-011).
func (s *Service) MarkPaid(ctx context.Context, orderID uuid.UUID, sourceReference string) error {
	var paid *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, orderID)
		if err != nil {
			return err
		}
		switch order.Status {
		case constant.StatusPaid, constant.StatusShipped, constant.StatusCompleted:
			// The sale is already made; the repeat is a success, not an error,
			// so a retrying caller stops retrying (FR-014).
			return nil
		}
		// Only PAYMENT_PENDING reaches the edge and transitions; a cancelled
		// order is refused by the state machine naming the current state.
		if err := order.MarkPaid(); err != nil {
			return err
		}
		for _, line := range order.Lines {
			reference := lineSourceReference(sourceReference, line.ProductID)
			if err := s.Reservations.ApplySale(txCtx, order.ID, line.ProductID, reference); err != nil {
				return err
			}
		}
		if err := s.Orders.UpdateStatus(txCtx, order.ID, order.Status, s.now()); err != nil {
			return err
		}
		paid = order
		return nil
	}); err != nil {
		return err
	}
	// The customer is told the order is paid. Sent after commit, best-effort
	// (FR-020, FR-021, research D9). A replayed payment is a no-op above and
	// sends nothing.
	if paid != nil {
		s.notifyCustomerStatusChange(ctx, paid)
	}
	return nil
}

// Cancel cancels an awaiting-payment order and returns the hold of every line to
// availability, exactly once (FR-015, FR-019). It is driven by the customer and
// by the expiry sweep. A paid order is not cancellable: the state machine refuses
// the move and names the current state, because a paid order is transferred
// instead (FR-009, FR-024).
func (s *Service) Cancel(ctx context.Context, orderID uuid.UUID) error {
	var cancelled *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, orderID)
		if err != nil {
			return err
		}
		if err := order.Cancel(); err != nil {
			return err
		}
		for _, line := range order.Lines {
			if err := s.Reservations.Release(txCtx, order.ID, line.ProductID); err != nil {
				return err
			}
		}
		if err := s.Orders.UpdateStatus(txCtx, order.ID, order.Status, s.now()); err != nil {
			return err
		}
		cancelled = order
		return nil
	}); err != nil {
		return err
	}
	// The customer is told the order was cancelled. Sent after commit,
	// best-effort (FR-020, FR-021, research D9). This covers both the customer's
	// own cancel and the expiry sweep, which calls this use case.
	s.notifyCustomerStatusChange(ctx, cancelled)
	return nil
}

// ConfirmByAdmin accepts an order awaiting the artist: in one transaction it locks
// the order, drives the PENDING → PAYMENT_PENDING transition, holds every line
// through module 05, opens the payment window from the hold window, bumps the
// content version, persists the confirmation and records the act. Holding every
// line in this one transaction is what makes the hold all-or-nothing: a line that
// cannot be held returns the shortage and rolls the whole hold back, leaving the
// order PENDING with no line held (FR-004, FR-005, FR-006, FR-010, FR-024,
// research D3).
func (s *Service) ConfirmByAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return appdto.AdminOrderView{}, err
	}

	var view appdto.AdminOrderView
	var confirmed *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		if err := order.Confirm(); err != nil {
			return err
		}
		// Hold every line in this same transaction; a line that cannot be held
		// rolls the whole hold back, so the order stays PENDING (FR-005, FR-024).
		for _, line := range order.Lines {
			if err := s.Reservations.Reserve(txCtx, order.ID, line.ProductID, line.Quantity); err != nil {
				var available int64
				if fresh, readErr := s.available(txCtx, line.ProductID); readErr == nil {
					available = fresh
				}
				return domainerr.QuantityExceedsAvailable(line.ProductID, available, line.Quantity)
			}
		}
		now := s.now()
		order.ConfirmedAt = &now
		expires := now.Add(s.Reservations.HoldWindow())
		order.PaymentExpiresAt = &expires
		// Confirmation marks the exact content that was accepted and opened for
		// payment, so it bumps the version (research D10).
		order.Version++
		if err := s.Orders.SaveConfirm(txCtx, order, now); err != nil {
			return err
		}
		s.record(txCtx, constant.AuditOrderConfirmed, actor, order.ID)
		confirmed = order
		view = s.Mapper.AdminOrder(*order)
		return nil
	}); err != nil {
		return appdto.AdminOrderView{}, err
	}
	// The customer is told the order was confirmed. Sent after commit,
	// best-effort (FR-020, FR-021, research D9).
	s.notifyCustomerStatusChange(ctx, confirmed)
	return view, nil
}

// Ship moves a paid order to shipped (FR-022). It is driven by an administrator;
// the role guard and the audit entry live on the operator use case that wraps it.
func (s *Service) Ship(ctx context.Context, orderID uuid.UUID) error {
	return s.transition(ctx, orderID, (*model.Order).Ship)
}

// Complete moves a shipped order to completed (FR-022). It is driven by an
// administrator; the role guard and the audit entry live on the operator use case
// that wraps it.
func (s *Service) Complete(ctx context.Context, orderID uuid.UUID) error {
	return s.transition(ctx, orderID, (*model.Order).Complete)
}

// transition locks one order, applies one allowed edge and persists the new
// state, all in one UnitOfWork transaction. The domain transition is the only
// writer of the state, so an illegal move is refused with the current state named
// and never reaches storage (FR-009, FR-010, FR-011).
func (s *Service) transition(ctx context.Context, orderID uuid.UUID, move func(*model.Order) error) error {
	var moved *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, orderID)
		if err != nil {
			return err
		}
		if err := move(order); err != nil {
			return err
		}
		if err := s.Orders.UpdateStatus(txCtx, order.ID, order.Status, s.now()); err != nil {
			return err
		}
		moved = order
		return nil
	}); err != nil {
		return err
	}
	// The customer is told the order advanced to its new state. Sent after
	// commit, best-effort (FR-020, FR-021, research D9).
	s.notifyCustomerStatusChange(ctx, moved)
	return nil
}

// ExpireOrders cancels every awaiting-payment order (PAYMENT_PENDING) whose
// payment deadline has passed and returns its goods, exactly once. It is the use
// case the expiry sweeper calls. The repository selects only orders awaiting
// payment past payment_expires_at, so an order that is still awaiting the artist
// — which has no deadline — is never selected and never expires by time
// (FR-008, FR-009, research D5).
//
// The selection is made with the instant the injected clock produced, so the
// order's sweep and module 05's agree on what has expired. An order a concurrent
// customer cancel already moved, or one that is gone, is skipped rather than
// failing the sweep, so the operation is idempotent; module 05's own release is
// idempotent too, so the two sweeps cannot free the same goods twice (FR-008).
func (s *Service) ExpireOrders(ctx context.Context) error {
	ids, err := s.Orders.ListExpiredPending(ctx, s.now())
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Cancel(ctx, id); err != nil {
			if errors.Is(err, domainerr.ErrStateTransitionInvalid) || errors.Is(err, domainerr.ErrNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

// lineSourceReference derives the per-line source reference module 05 records
// for one line's sale: the payment event's identity combined with the product
// identifier, so every line of one order carries a distinct reference and the
// globally-unique source_reference cannot collide (FR-014, research D5).
func lineSourceReference(sourceReference string, productID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", sourceReference, productID)
}
