package implement

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is US3 and US4: the two routes into COMPLETED. The customer confirms
// receipt (ConfirmReceived), and a delivered order nobody confirms completes
// itself after five days (AutoCompleteDelivered). Completion is deliberately no
// longer an operator action: only the customer or the sweep completes an order,
// and both drive the same single DELIVERED -> COMPLETED domain edge under the
// order's row lock (FR-011, FR-012). The owner is always the session's, never an
// input (FR-018).

// ConfirmReceived completes one of the caller's own delivered orders. In one
// UnitOfWork it locks the order, checks that the caller owns it, drives the
// domain Complete transition, stamps the completion instant and the acting
// customer, persists the order and records the act (FR-011, FR-014, FR-018).
//
// The ownership check and the transition both run under the row lock, so another
// customer's order answers the same not-found an unknown one does — the route
// never confirms that the order exists (FR-018). The lock also serialises the
// customer's confirmation against the completion sweep: whichever commits second
// finds the order no longer DELIVERED, so the order completes exactly once
// (FR-012).
//
// The acting customer is the session's, never an input; a call with no session
// behind it is a programming error rather than a client-facing refusal, because
// the transport's session guard answers UNAUTHENTICATED first (FR-018, FR-020).
func (s *Service) ConfirmReceived(ctx context.Context, in dto.OrderRefInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}

	var view dto.OrderView
	var completed *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		// Another customer's order is the same not-found an unknown one is: the
		// route must never confirm the order exists (FR-018).
		if order.UserID != actor.ID {
			return domainerr.ErrNotFound
		}
		// The domain is the only writer of the state, so an order that is not
		// DELIVERED is refused naming its current state and stays exactly as it
		// was (FR-003, FR-011). The acting customer is recorded as the completer.
		view, err = s.completeLocked(txCtx, order, &actor.ID)
		if err != nil {
			return err
		}
		// The completion and its audit entry are produced by the same unit of
		// work; the shared writer queues it and never fails the business
		// operation (FR-014, Constitution VI). The actor is the customer.
		s.record(txCtx, constant.AuditOrderCompleted, actor, order.ID)
		completed = order
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	// The customer is told the order completed. Sent after commit, best-effort
	// (FR-021, research D12).
	s.notifyCustomerStatusChange(ctx, completed)
	return view, nil
}

// AutoCompleteDelivered completes every delivered order whose five-day window has
// passed, exactly once, with no acting account. It is the use case the completion
// sweep calls, and it is US4's whole behaviour (FR-012, FR-013, FR-014).
//
// It asks the repository for the DELIVERED orders whose delivered_at is at or
// before now − DeliveryCompletionWindow — oldest delivered first, so a backlog
// drains in the order it accumulated — and completes each through the same domain
// Complete edge the customer's confirmation drives. The cutoff is derived from the
// injected clock and passed to the query, never computed by the database, so the
// window is owned by the domain and a test can move it with the clock
// (research D6).
//
// Completion is idempotent: whichever of the customer and the sweep commits second
// finds the order no longer DELIVERED under the row lock, and the sweep skips such
// an order rather than failing the run, exactly as the expiry sweep skips an order
// a concurrent cancel already moved (FR-012). An order that is not DELIVERED is
// never selected by the query in the first place (FR-013).
func (s *Service) AutoCompleteDelivered(ctx context.Context) error {
	cutoff := s.now().Add(-model.DeliveryCompletionWindow)
	ids, err := s.Orders.ListAwaitingCompletion(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, id := range ids {
		completed, err := s.completeDelivered(ctx, id)
		if err != nil {
			// An order a concurrent confirmation already completed (or whose row
			// is gone) is skipped rather than failing the sweep, so the operation
			// is idempotent (FR-012).
			if errors.Is(err, domainerr.ErrStateTransitionInvalid) || errors.Is(err, domainerr.ErrNotFound) {
				continue
			}
			return err
		}
		// The customer is told the order completed. Sent after commit, best-effort
		// (FR-021, research D12).
		s.notifyCustomerStatusChange(ctx, completed)
	}
	return nil
}

// completeDelivered completes one order selected by the sweep: in one UnitOfWork
// it locks the order, drives the domain Complete transition with no actor, stamps
// the completion instant, persists the order and records the act with no acting
// account. A refusal — the order is no longer DELIVERED because the customer
// confirmed it first — is returned to the caller, which skips it (FR-012, FR-014).
func (s *Service) completeDelivered(ctx context.Context, orderID uuid.UUID) (*model.Order, error) {
	var completed *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, orderID)
		if err != nil {
			return err
		}
		if _, err := s.completeLocked(txCtx, order, nil); err != nil {
			return err
		}
		// The automatic completion is recorded too, with no acting account: the
		// shared writer queues it inside the same unit of work and never fails the
		// business operation (FR-014, Constitution VI, research D10).
		s.recordAutoCompletion(txCtx, order.ID)
		completed = order
		return nil
	}); err != nil {
		return nil, err
	}
	return completed, nil
}

// completeLocked applies the single DELIVERED -> COMPLETED domain edge to an
// already-locked order and persists the completion facts: the instant it happened
// and, for the customer's confirmation, who confirmed; the sweep passes nil, so a
// sweep completion records no actor (FR-011, FR-012, FR-014). A move from any
// other state is refused by the domain naming the current state, which is what
// makes the order complete exactly once however the two routes interleave.
func (s *Service) completeLocked(ctx context.Context, order *model.Order, actor *uuid.UUID) (dto.OrderView, error) {
	if err := order.Complete(); err != nil {
		return dto.OrderView{}, err
	}
	now := s.now()
	order.CompletedAt = &now
	order.CompletedBy = actor
	if err := s.Orders.SaveCompleted(ctx, order, now); err != nil {
		return dto.OrderView{}, err
	}
	return s.Mapper.Order(*order), nil
}

// recordAutoCompletion emits the completion sweep's audit event, naming the order
// but no acting account, because the sweep is the system rather than a customer
// (FR-014, Constitution VI, research D10). An absent auditor is tolerated so a
// read-only construction cannot panic.
func (s *Service) recordAutoCompletion(ctx context.Context, orderID uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditOrderCompleted, string(audit.OutcomeSuccess),
		nil, "", targetTypeOrder, orderID.String(), nil)
}
