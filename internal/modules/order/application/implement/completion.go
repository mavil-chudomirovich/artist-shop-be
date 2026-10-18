package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is US3: the customer's confirmation of receipt. Once the operator has
// marked an order delivered, its owner confirms it arrived and the order reaches
// COMPLETED — the terminal state — recording when and who confirmed (FR-011,
// FR-014). Completion is deliberately no longer an operator action: only the
// customer (here) or the five-day sweep (a later use case) completes an order,
// and both drive the same single DELIVERED -> COMPLETED domain edge (FR-011,
// FR-012). The owner is always the session's, never an input (FR-018).

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
		// was (FR-003, FR-011).
		if err := order.Complete(); err != nil {
			return err
		}
		now := s.now()
		order.CompletedAt = &now
		completedBy := actor.ID
		order.CompletedBy = &completedBy
		if err := s.Orders.SaveCompleted(txCtx, order, now); err != nil {
			return err
		}
		// The completion and its audit entry are produced by the same unit of
		// work; the shared writer queues it and never fails the business
		// operation (FR-014, Constitution VI). The actor is the customer.
		s.record(txCtx, constant.AuditOrderCompleted, actor, order.ID)
		completed = order
		view = s.Mapper.Order(*order)
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	// The customer is told the order completed. Sent after commit, best-effort
	// (FR-021, research D12).
	s.notifyCustomerStatusChange(ctx, completed)
	return view, nil
}
