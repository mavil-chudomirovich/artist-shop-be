package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// The whole use-case surface is satisfied now that every use case exists, so the
// compile-time assertion lands here rather than earlier. It is the one declaration
// of the surface: presentation consumes this interface directly rather than
// declaring a second subset that could drift away from it.
var _ appinterface.ProductService = (*Service)(nil)

// The variant use cases (FR-011, FR-012, FR-015, FR-016, FR-014). A variant is the
// unit of sale: it carries its own name, SKU, optional price, image, order and sell
// state. Every mutation validates through the entity, persists through the variant
// repository and emits an audit event naming the variant.

// AddVariant adds one variant to a product and audits the change (FR-014).
//
// A variant starts in COMING_SOON; the sell state is moved afterwards through the
// dedicated transition endpoint or the automatic stock signal. A SKU another
// variant already holds is reported by the repository as the SKU sentinel, and a
// product identifier no row carries as the module's not-found.
func (s *Service) AddVariant(ctx context.Context, in dto.AddVariantInput) (dto.AdminProductDetailOutput, error) {
	variant, err := model.NewVariant(in.ProductID, model.VariantDraft{
		Name:        in.Name,
		SKU:         in.SKU,
		PriceAmount: in.PriceAmount,
		Position:    in.Position,
	}, now())
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Variants.Create(ctx, variant); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordVariant(ctx, constant.AuditProductVariantAdded, variant.ID, map[string]any{"productId": in.ProductID.String()})
	return s.adminDetail(ctx, in.ProductID)
}

// UpdateVariant applies a partial variant edit and audits the change (FR-014).
//
// It reads the variant, confirms it belongs to the addressed product, applies the
// whole change through the entity and writes it. A variant that does not belong to
// the product answers the module's not-found, so the two are indistinguishable. A
// SKU another variant already holds is reported as the SKU sentinel; re-sending
// the variant's own SKU succeeds because the unique index excludes the row being
// updated (FR-011).
func (s *Service) UpdateVariant(ctx context.Context, in dto.UpdateVariantInput) (dto.AdminProductDetailOutput, error) {
	variant, err := s.ownedVariant(ctx, in.ProductID, in.VariantID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := variant.Apply(model.VariantEdit{
		Name:           in.Name,
		SKU:            in.SKU,
		PriceAmount:    in.PriceAmount,
		PriceAmountSet: in.PriceAmountSet,
		Position:       in.Position,
	}, now()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Variants.Update(ctx, variant); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordVariant(ctx, constant.AuditProductVariantUpdated, variant.ID, map[string]any{"productId": in.ProductID.String()})
	return s.adminDetail(ctx, in.ProductID)
}

// RemoveVariant removes one variant and audits the removal (FR-012, FR-014).
//
// Removing a product's last variant is refused with ErrVariantLastRemoval, so a
// product always keeps at least one variant (FR-012). A variant that does not
// belong to the product answers the module's not-found. Removal is a hard delete;
// the audit entry is what survives it.
func (s *Service) RemoveVariant(ctx context.Context, in dto.VariantRefInput) error {
	if _, err := s.ownedVariant(ctx, in.ProductID, in.VariantID); err != nil {
		return err
	}
	count, err := s.Variants.CountByProduct(ctx, in.ProductID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return domainerr.ErrVariantLastRemoval
	}
	if err := s.Variants.Delete(ctx, in.VariantID); err != nil {
		return err
	}
	s.recordVariant(ctx, constant.AuditProductVariantRemoved, in.VariantID, map[string]any{"productId": in.ProductID.String()})
	return nil
}

// ChangeVariantState moves a variant through its selling life and audits the change
// (FR-015, Constitution III).
//
// Every move is routed through the entity's transition functions: the use case
// loads the variant, asks it to transition and persists the entity it gets back, so
// a move the transition table does not list cannot reach the row. A refused move is
// reported by the domain with the state it started from and the row is left
// untouched. A target that is not one of the four states is a request error naming
// `to` before anything is read.
func (s *Service) ChangeVariantState(ctx context.Context, in dto.ChangeVariantStateInput) (dto.AdminProductDetailOutput, error) {
	if !constant.IsValidSellState(in.To) {
		return dto.AdminProductDetailOutput{}, domainerr.InvalidProductField(
			model.FieldTo, "must be one of COMING_SOON, ACTIVE, OUT_OF_STOCK, DISCONTINUED")
	}
	variant, err := s.ownedVariant(ctx, in.ProductID, in.VariantID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	from := variant.SellState
	if err := variant.ChangeSellState(in.To, now()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Variants.Update(ctx, variant); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordVariant(ctx, constant.AuditProductVariantUpdated, variant.ID, map[string]any{
		"productId": in.ProductID.String(),
		"from":      string(from),
		"to":        string(variant.SellState),
	})
	return s.adminDetail(ctx, in.ProductID)
}

// AddVariantImage validates the upload, stores it through the media port, attaches
// it to the variant and audits the change (FR-016).
//
// The bytes are decided on their own signature and ceiling before anything is sent
// anywhere. Uploading again replaces the variant's single image: the whole model
// keeps a reference, never the bytes. A failed row write releases the just-stored
// asset.
func (s *Service) AddVariantImage(ctx context.Context, in dto.AddVariantImageInput) (dto.AdminProductDetailOutput, error) {
	if err := model.ValidatePictureUpload(in.Content, s.Config.PictureMaxBytesOrDefault()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	variant, err := s.ownedVariant(ctx, in.ProductID, in.VariantID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	stored, err := s.Media.Upload(ctx, in.Content, s.Config.PictureTargetWidthOrDefault())
	if err != nil {
		// Whatever the provider said is dropped here: presentation logs whatever
		// error it is handed, and no provider text or credential belongs in a log
		// line (Constitution V, VI).
		return dto.AdminProductDetailOutput{}, domainerr.ErrProductMediaUnavailable
	}

	previous := variant.Image
	variant.Image = &model.VariantImage{
		PublicID: stored.PublicID,
		URL:      stored.URL,
		Width:    stored.Width,
		Height:   stored.Height,
	}
	if err := s.Variants.Update(ctx, variant); err != nil {
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: stored.PublicID,
			URL:      stored.URL,
			Width:    stored.Width,
			Height:   stored.Height,
		})
		return dto.AdminProductDetailOutput{}, err
	}
	// The replaced image is released after the row is committed, so the worst case
	// of a failed release is an orphaned asset rather than a variant rendering a
	// picture that no longer exists.
	if previous != nil && previous.PublicID != stored.PublicID {
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: previous.PublicID,
			URL:      previous.URL,
			Width:    previous.Width,
			Height:   previous.Height,
		})
	}

	s.recordVariant(ctx, constant.AuditProductVariantUpdated, variant.ID, map[string]any{
		"productId": in.ProductID.String(),
		"image":     "set",
	})
	return s.adminDetail(ctx, in.ProductID)
}

// RemoveVariantImage clears the variant's image, releases the stored asset and
// audits the change (FR-016). A variant with no image is a no-op on the asset: the
// row is left as it was and the call still answers the product detail.
func (s *Service) RemoveVariantImage(ctx context.Context, in dto.VariantRefInput) (dto.AdminProductDetailOutput, error) {
	variant, err := s.ownedVariant(ctx, in.ProductID, in.VariantID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	previous := variant.Image
	if previous != nil {
		variant.Image = nil
		if err := s.Variants.Update(ctx, variant); err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: previous.PublicID,
			URL:      previous.URL,
			Width:    previous.Width,
			Height:   previous.Height,
		})
		s.recordVariant(ctx, constant.AuditProductVariantUpdated, variant.ID, map[string]any{
			"productId": in.ProductID.String(),
			"image":     "removed",
		})
	}
	return s.adminDetail(ctx, in.ProductID)
}

// MarkVariantOutOfStock moves an on-sale variant to out of stock as the inventory
// module signals it (FR-015).
func (s *Service) MarkVariantOutOfStock(ctx context.Context, variantID uuid.UUID) error {
	return s.signalAvailability(ctx, variantID, constant.SellStateOutOfStock)
}

// MarkVariantOnSale moves an out-of-stock variant back on sale as the inventory
// module signals it (FR-015).
func (s *Service) MarkVariantOnSale(ctx context.Context, variantID uuid.UUID) error {
	return s.signalAvailability(ctx, variantID, constant.SellStateActive)
}

// signalAvailability applies one availability signal. It is a tolerant no-op
// unless the variant is in the state the signal leaves (ACTIVE for out-of-stock,
// OUT_OF_STOCK for on sale): an announced or retired variant, or one already in the
// target state, must not move, so a correct stock operation is never failed by a
// signal that changes nothing. An unknown variant is the module's not-found.
func (s *Service) signalAvailability(ctx context.Context, variantID uuid.UUID, target constant.SellState) error {
	variant, err := s.Variants.FindByID(ctx, variantID)
	if err != nil {
		return err
	}
	from := variant.SellState

	var transition error
	switch target {
	case constant.SellStateOutOfStock:
		if variant.SellState != constant.SellStateActive {
			return nil
		}
		transition = variant.SellOut(now())
	case constant.SellStateActive:
		if variant.SellState != constant.SellStateOutOfStock {
			return nil
		}
		transition = variant.Restock(now())
	default:
		return nil
	}
	if transition != nil {
		return transition
	}
	if err := s.Variants.Update(ctx, variant); err != nil {
		return err
	}
	s.recordVariantSystem(ctx, constant.AuditProductVariantUpdated, variant.ID, map[string]any{
		"from": string(from),
		"to":   string(variant.SellState),
	})
	return nil
}

// ownedVariant reads a variant and confirms it belongs to the addressed product. A
// variant that does not belong to the product answers the module's not-found, so a
// variant of another product is indistinguishable from an unknown one.
func (s *Service) ownedVariant(ctx context.Context, productID, variantID uuid.UUID) (*model.Variant, error) {
	variant, err := s.Variants.FindByID(ctx, variantID)
	if err != nil {
		return nil, err
	}
	if variant.ProductID != productID {
		return nil, domainerr.ErrVariantNotFound
	}
	return variant, nil
}
