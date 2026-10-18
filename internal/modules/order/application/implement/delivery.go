package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// This file is US1: dispatching a paid order, with photographic evidence. The
// operator marks a PAID order in transit (SHIPPING) and may attach up to five
// dispatch photos. The evidence is validated on its own bytes before anything is
// sent anywhere, uploaded through the shared media store OUTSIDE any
// transaction, and its rows written inside a second transaction that re-checks
// the per-stage ceiling under the order's row lock — so no database lock is held
// across the network and two concurrent uploads cannot exceed five (FR-005,
// FR-006, FR-009; research D5).

// StartShippingByAdmin moves a paid order in transit (dispatches it), attaches up
// to five optional dispatch photos and records the act (FR-005, FR-006, FR-019).
//
// The order is the specification:
//
//  1. Every uploaded payload is decided on its own bytes — the format sniffed
//     from the leading signature and the size ceiling — before anything is sent
//     anywhere, and a request carrying more than five photos is refused before
//     the provider is called, so a refused upload leaves no orphaned asset
//     (FR-006, FR-009).
//  2. A first transaction locks the order, drives the domain PAID -> SHIPPING
//     transition (which refuses a non-PAID order naming its current state) and
//     checks the per-stage ceiling, so a wrong-state or over-ceiling request is
//     refused before a byte is uploaded (FR-005, research D5).
//  3. The bytes go to the media provider outside any transaction. Holding a
//     database lock across a third-party call is how a slow provider becomes a
//     database problem (research D5).
//  4. A second transaction re-locks the order, re-checks the ceiling and applies
//     the transition, inserts the photo rows, touches updated_at and audits, so
//     two concurrent uploads cannot both observe room for five (FR-010,
//     research D5). A failure in this transaction releases the just-stored
//     assets; the worst case of a failed release is an orphaned asset, never an
//     order that references a missing one (research D5).
//
// The acting administrator is the session's, never an input (FR-019, FR-020).
func (s *Service) StartShippingByAdmin(ctx context.Context, in dto.EvidenceInput) (dto.AdminOrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.AdminOrderView{}, err
	}
	if err := s.validateEvidence(model.StageShipping, in.Photos); err != nil {
		return dto.AdminOrderView{}, err
	}

	// First transaction: refuse a wrong-state or over-ceiling request before a
	// byte is uploaded. The domain transition is the state check, so the refusal
	// names the current state exactly as the domain does.
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		if err := order.StartShipping(); err != nil {
			return err
		}
		if _, err := s.ensureEvidenceCapacity(txCtx, in.OrderID, model.StageShipping, len(in.Photos)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return dto.AdminOrderView{}, err
	}

	// The provider call sits outside the transaction (research D5).
	photos, err := s.uploadEvidence(ctx, in.OrderID, model.StageShipping, in.Photos)
	if err != nil {
		return dto.AdminOrderView{}, err
	}

	var view dto.AdminOrderView
	var moved *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		if err := order.StartShipping(); err != nil {
			return err
		}
		count, err := s.ensureEvidenceCapacity(txCtx, in.OrderID, model.StageShipping, len(photos))
		if err != nil {
			return err
		}
		now := s.now()
		if err := s.Orders.StartShipping(txCtx, order, now); err != nil {
			return err
		}
		if len(photos) > 0 {
			for i := range photos {
				photos[i].Position = count + i
			}
			if err := s.Orders.AddDeliveryPhotos(txCtx, order.ID, photos, now); err != nil {
				return err
			}
		}
		order.DeliveryPhotos = photos
		s.record(txCtx, constant.AuditOrderShipped, actor, order.ID)
		moved = order
		view = s.Mapper.AdminOrder(*order)
		return nil
	}); err != nil {
		// The rows never went in, so the just-stored assets are ours to release.
		// Releasing is best-effort: the worst case is an orphaned asset
		// (research D5).
		s.releaseEvidence(ctx, photos)
		return dto.AdminOrderView{}, err
	}
	// The customer is told the order is on its way. Sent after commit,
	// best-effort (FR-021, research D12).
	s.notifyCustomerStatusChange(ctx, moved)
	return view, nil
}

// validateEvidence applies the request-shape rules of a dispatch or delivery
// step to every uploaded payload before anything is stored: at most five photos,
// and each of an accepted format within the size ceiling (FR-006, FR-009). It
// runs before the transaction and before the provider, so a refused upload
// changes nothing and leaves no orphaned asset.
func (s *Service) validateEvidence(stage model.Stage, photos []dto.UploadedPhoto) error {
	if len(photos) > model.MaxDeliveryPhotos {
		return domainerr.DeliveryPhotoLimit(string(stage))
	}
	for _, photo := range photos {
		if err := model.ValidateDeliveryPhoto(photo.Content, model.MaxDeliveryPhotoBytes); err != nil {
			return err
		}
	}
	return nil
}

// ensureEvidenceCapacity refuses a step that has no room for the incoming photos,
// counting what the order already carries at the stage under the caller's row
// lock. It returns how many photos the stage already holds when it admits, so the
// caller can position the new ones (FR-006, FR-008; research D3, D5).
func (s *Service) ensureEvidenceCapacity(ctx context.Context, orderID uuid.UUID, stage model.Stage, incoming int) (int, error) {
	existing, err := s.Orders.CountDeliveryPhotos(ctx, orderID, stage)
	if err != nil {
		return 0, err
	}
	if existing+incoming > model.MaxDeliveryPhotos {
		return 0, domainerr.DeliveryPhotoLimit(string(stage))
	}
	return existing, nil
}

// uploadEvidence sends each validated payload to the media provider and builds
// the photo rows to persist, stamping the stage and the order on each. It runs
// outside any transaction (research D5). A payload the provider refuses releases
// whatever this call already stored, so a partial upload leaves nothing behind.
func (s *Service) uploadEvidence(ctx context.Context, orderID uuid.UUID, stage model.Stage, inputs []dto.UploadedPhoto) ([]model.DeliveryPhoto, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if s.Media == nil {
		// A composition without a media store cannot accept evidence; it is an
		// internal misconfiguration, hidden behind INTERNAL_ERROR rather than
		// described in order terms (contracts/error-codes.md).
		return nil, media.ErrUnavailable
	}
	photos := make([]model.DeliveryPhoto, 0, len(inputs))
	for _, input := range inputs {
		stored, err := s.Media.Upload(ctx, input.Content, 0)
		if err != nil {
			s.releaseEvidence(ctx, photos)
			return nil, media.ErrUnavailable
		}
		photos = append(photos, model.DeliveryPhoto{
			ID:       uuid.New(),
			OrderID:  orderID,
			Stage:    stage,
			PublicID: stored.PublicID,
			URL:      stored.URL,
			Width:    stored.Width,
			Height:   stored.Height,
		})
	}
	return photos, nil
}

// releaseEvidence releases the stored assets of the given photos, best-effort:
// a release that fails leaves an orphaned asset rather than failing the request,
// which is the accepted cost of uploading outside the transaction (research D5).
func (s *Service) releaseEvidence(ctx context.Context, photos []model.DeliveryPhoto) {
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
