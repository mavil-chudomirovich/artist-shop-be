package implement

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is US5: the customer's one order-level feedback to the artist. It is
// text only — no rating and no moderation, which belong to module 13 — and it is
// accepted only once the order is delivered or completed. The author is always the
// session's, never an input, so a customer can only write about an order that is
// theirs (FR-015 to FR-018, research D8).
//
// The one-per-order rule has two layers, deliberately: the use case refuses an
// order that already carries feedback, and the unique index on order_feedback
// (order_id) refuses the row a concurrent submission would add. Whichever layer
// catches it, the client sees ORDER_FEEDBACK_ALREADY_SENT (FR-016, research D8).
//
// Once the feedback has committed, the artist is emailed that the order carries it
// through the existing Notifier port — best-effort and after the commit, so a
// delivery failure never fails the feedback (FR-022, FR-023, research D12).

// SubmitFeedback records the caller's order-level feedback and answers the order
// with it attached (FR-015 to FR-017).
//
// In one UnitOfWork it locks the order, checks that the caller owns it, requires
// the order to be DELIVERED or COMPLETED, refuses one that already carries
// feedback, validates the content and inserts the row. The row and the answer
// commit together, so a refused request leaves neither a feedback nor a changed
// order.
//
// Ownership runs first and answers the same not-found an unknown order does, so
// the route never confirms another customer's order exists (FR-018). A state that
// is not delivered/completed answers ORDER_FEEDBACK_NOT_ALLOWED naming the state,
// and a second submission answers ORDER_FEEDBACK_ALREADY_SENT (FR-015, FR-016).
//
// Two concurrent submissions both pass the "no feedback yet" read, but only one
// can insert: the storage unique index refuses the other and the repository
// surfaces it as ErrFeedbackAlreadySent, so the refusal is still the documented
// code rather than an internal error (FR-016, research D8).
func (s *Service) SubmitFeedback(ctx context.Context, in dto.FeedbackInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}

	var view dto.OrderView
	var submitted *model.Order
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
		// Feedback needs a receipt: only a delivered or completed order accepts it
		// (FR-015). The refusal names the current state, as FR-003 requires of any
		// state the request does not allow.
		if order.Status != constant.StatusDelivered && order.Status != constant.StatusCompleted {
			return domainerr.FeedbackNotAllowed(order.Status)
		}
		// The use case's own one-per-order check gives the clear refusal; the
		// storage unique index is the guarantee behind it (FR-016).
		existing, err := s.Orders.FindFeedback(txCtx, order.ID)
		switch {
		case err == nil && existing != nil:
			return domainerr.ErrFeedbackAlreadySent
		case err != nil && !errors.Is(err, domainerr.ErrNotFound):
			return err
		}
		// The message must be 1 to 2000 characters after trimming (FR-015). It is
		// validated here as well as at the transport so no other caller can store
		// an empty or over-long feedback.
		if err := model.ValidateFeedbackContent(in.Content); err != nil {
			return err
		}

		feedback := model.Feedback{
			ID:        uuid.New(),
			OrderID:   order.ID,
			AuthorID:  actor.ID,
			Content:   strings.TrimSpace(in.Content),
			CreatedAt: s.now(),
		}
		if err := s.Orders.InsertFeedback(txCtx, feedback); err != nil {
			return err
		}
		order.Feedback = &feedback
		submitted = order
		view = s.Mapper.Order(*order)
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	// The artist is told the order carries feedback. Sent after the transaction
	// committed and best-effort, so a delivery failure never fails the feedback
	// (FR-022, FR-023, research D12).
	s.notifyArtistFeedbackReceived(ctx, submitted)
	return view, nil
}
