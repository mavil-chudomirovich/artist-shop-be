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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// This file is US5: the customer's one order-level feedback to the artist. It is
// text only — no rating and no moderation, which belong to module 13 — and it is
// accepted only once the order is delivered or completed. It may carry up to five
// optional photos, validated and stored exactly like delivery evidence. The author
// is always the session's, never an input, so a customer can only write about an
// order that is theirs (FR-015 to FR-018, FR-025, research D8, D16).
//
// The one-per-order rule has two layers, deliberately: the use case refuses an
// order that already carries feedback, and the unique index on order_feedback
// (order_id) refuses the row a concurrent submission would add. Whichever layer
// catches it, the client sees ORDER_FEEDBACK_ALREADY_SENT (FR-016, research D8).
//
// The photos take the delivery shape: every payload is decided on its own bytes
// before anything is sent anywhere, uploaded through the shared media store
// OUTSIDE any transaction, and their rows written inside a second transaction that
// re-checks the ceiling under the order's row lock — so no database lock is held
// across the network (FR-025, research D5, D16).
//
// Once the feedback has committed, the artist is emailed that the order carries it
// through the existing Notifier port — best-effort and after the commit, so a
// delivery failure never fails the feedback (FR-022, FR-023, research D12).

// SubmitFeedback records the caller's order-level feedback, with any attached
// photos, and answers the order with it attached (FR-015 to FR-017, FR-025).
//
// The order is the specification:
//
//  1. The content (1–2000 characters after trimming) and every uploaded payload
//     (at most five, each of an accepted format within the size ceiling) are
//     decided before anything is stored, so a refused submission changes nothing
//     and leaves no orphaned asset (FR-015, FR-025).
//  2. A first transaction locks the order, checks that the caller owns it and that
//     its state is DELIVERED or COMPLETED, and refuses one that already carries
//     feedback — all before a byte is uploaded (FR-015, FR-016, FR-018).
//  3. The bytes go to the media provider outside any transaction (research D5).
//  4. A second transaction re-locks the order, re-checks ownership, state and the
//     absence of feedback, then inserts the feedback row and its photo rows, so
//     the feedback and its images commit together or not at all. A failure in
//     this transaction releases the just-stored assets; the worst case of a
//     failed release is an orphaned asset, never a feedback that references a
//     missing one (FR-025, research D5, D16).
//
// Ownership runs first and answers the same not-found an unknown order does, so
// the route never confirms another customer's order exists (FR-018). Two
// concurrent submissions both pass the "no feedback yet" read, but only one can
// insert: the storage unique index refuses the other and the repository surfaces
// it as ErrFeedbackAlreadySent (FR-016, research D8).
func (s *Service) SubmitFeedback(ctx context.Context, in dto.FeedbackInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}
	if err := s.validateFeedback(in.Content, in.Photos); err != nil {
		return dto.OrderView{}, err
	}

	// First transaction: refuse a wrong-state, already-answered or foreign request
	// before a byte is uploaded.
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		return s.checkFeedbackAllowed(txCtx, order, actor.ID)
	}); err != nil {
		return dto.OrderView{}, err
	}

	// The provider call sits outside the transaction (research D5).
	feedbackID := uuid.New()
	photos, err := s.uploadFeedbackPhotos(ctx, feedbackID, in.Photos)
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
		if err := s.checkFeedbackAllowed(txCtx, order, actor.ID); err != nil {
			return err
		}
		if err := model.EnsureFeedbackPhotoCapacity(len(photos)); err != nil {
			return err
		}
		now := s.now()
		feedback := model.Feedback{
			ID:        feedbackID,
			OrderID:   order.ID,
			AuthorID:  actor.ID,
			Content:   strings.TrimSpace(in.Content),
			CreatedAt: now,
		}
		if err := s.Orders.InsertFeedback(txCtx, feedback); err != nil {
			return err
		}
		if len(photos) > 0 {
			for i := range photos {
				photos[i].Position = i
			}
			if err := s.Orders.InsertFeedbackPhotos(txCtx, feedback.ID, photos, now); err != nil {
				return err
			}
		}
		feedback.Photos = photos
		order.Feedback = &feedback
		submitted = order
		view = s.Mapper.Order(*order)
		return nil
	}); err != nil {
		// The rows never went in, so the just-stored assets are ours to release.
		// Releasing is best-effort: the worst case is an orphaned asset
		// (research D5).
		s.releaseFeedbackPhotos(ctx, photos)
		return dto.OrderView{}, err
	}
	// The artist is told the order carries feedback. Sent after the transaction
	// committed and best-effort, so a delivery failure never fails the feedback
	// (FR-022, FR-023, research D12).
	s.notifyArtistFeedbackReceived(ctx, submitted)
	s.publishFeedbackReceived(ctx, submitted)
	return view, nil
}

// checkFeedbackAllowed applies the submission's per-order rules under the caller's
// row lock: the caller must own the order (another customer's is the same
// not-found an unknown one is), the state must be DELIVERED or COMPLETED, and the
// order must not already carry feedback (FR-015, FR-016, FR-018).
func (s *Service) checkFeedbackAllowed(ctx context.Context, order *model.Order, ownerID uuid.UUID) error {
	if order.UserID != ownerID {
		return domainerr.ErrNotFound
	}
	if order.Status != constant.StatusDelivered && order.Status != constant.StatusCompleted {
		return domainerr.FeedbackNotAllowed(order.Status)
	}
	existing, err := s.Orders.FindFeedback(ctx, order.ID)
	switch {
	case err == nil && existing != nil:
		return domainerr.ErrFeedbackAlreadySent
	case err != nil && !errors.Is(err, domainerr.ErrNotFound):
		return err
	}
	return nil
}

// validateFeedback applies the submission's request-shape rules before anything is
// stored: the content is 1 to 2000 characters after trimming, at most five photos
// are attached, and each photo is of an accepted format within the size ceiling
// (FR-015, FR-025). It runs before the transaction and before the provider, so a
// refused submission changes nothing and leaves no orphaned asset.
func (s *Service) validateFeedback(content string, photos []dto.UploadedPhoto) error {
	if err := model.ValidateFeedbackContent(content); err != nil {
		return err
	}
	if err := model.EnsureFeedbackPhotoCapacity(len(photos)); err != nil {
		return err
	}
	for _, photo := range photos {
		if err := model.ValidateFeedbackPhoto(photo.Content); err != nil {
			return err
		}
	}
	return nil
}

// uploadFeedbackPhotos sends each validated payload to the media provider and
// builds the photo rows to persist, stamping the feedback on each. It runs outside
// any transaction (research D5). A payload the provider refuses releases whatever
// this call already stored, so a partial upload leaves nothing behind.
func (s *Service) uploadFeedbackPhotos(ctx context.Context, feedbackID uuid.UUID, inputs []dto.UploadedPhoto) ([]model.FeedbackPhoto, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if s.Media == nil {
		// A composition without a media store cannot accept photos; it is an
		// internal misconfiguration, hidden behind INTERNAL_ERROR rather than
		// described in order terms (contracts/error-codes.md).
		return nil, media.ErrUnavailable
	}
	photos := make([]model.FeedbackPhoto, 0, len(inputs))
	for _, input := range inputs {
		stored, err := s.Media.Upload(ctx, input.Content, 0)
		if err != nil {
			s.releaseFeedbackPhotos(ctx, photos)
			return nil, media.ErrUnavailable
		}
		photos = append(photos, model.FeedbackPhoto{
			ID:         uuid.New(),
			FeedbackID: feedbackID,
			PublicID:   stored.PublicID,
			URL:        stored.URL,
			Width:      stored.Width,
			Height:     stored.Height,
		})
	}
	return photos, nil
}

// releaseFeedbackPhotos releases the stored assets of the given photos, best-effort:
// a release that fails leaves an orphaned asset rather than failing the request,
// which is the accepted cost of uploading outside the transaction (research D5).
func (s *Service) releaseFeedbackPhotos(ctx context.Context, photos []model.FeedbackPhoto) {
	if s.Media == nil {
		return
	}
	for _, photo := range photos {
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: photo.PublicID,
			URL:      photo.URL,
			Width:    photo.Width,
			Height:   photo.Height,
		})
	}
}
