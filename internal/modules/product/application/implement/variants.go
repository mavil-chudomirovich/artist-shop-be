package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// fieldVariantIDs is the contract member a variant reorder names, so a refused
// array answers a field-level detail the client can map straight onto its
// request (FR-005, contracts/error-codes.md).
const fieldVariantIDs = "variantIds"

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
//
// The position is optional: an omitted position places the added variant at the
// front of its product (FR-011, research D3, D7). The front read joins the
// insert's transaction when the service carries one, so two concurrent adds
// cannot read the same front; a supplied position is honoured and the add stays a
// single statement.
func (s *Service) AddVariant(ctx context.Context, in dto.AddVariantInput) (dto.AdminProductDetailOutput, error) {
	var variant *model.Variant

	switch {
	case in.Position != nil:
		created, err := s.createVariant(ctx, in, *in.Position)
		if err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
		variant = created
	case s.Tx == nil:
		position, err := s.Variants.NextFrontPosition(ctx, in.ProductID)
		if err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
		created, err := s.createVariant(ctx, in, position)
		if err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
		variant = created
	default:
		if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
			position, err := s.Variants.NextFrontPosition(txCtx, in.ProductID)
			if err != nil {
				return err
			}
			created, err := s.createVariant(txCtx, in, position)
			if err != nil {
				return err
			}
			variant = created
			return nil
		}); err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
	}

	s.recordVariant(ctx, constant.AuditProductVariantAdded, variant.ID, map[string]any{"productId": in.ProductID.String()})
	return s.adminDetail(ctx, in.ProductID)
}

// createVariant builds and persists one variant at the given position. It is the
// shared tail of AddVariant, so the explicit-position path and both auto-front
// paths persist through the same constructor and repository call.
func (s *Service) createVariant(ctx context.Context, in dto.AddVariantInput, position int) (*model.Variant, error) {
	variant, err := model.NewVariant(in.ProductID, model.VariantDraft{
		Name:        in.Name,
		SKU:         in.SKU,
		PriceAmount: in.PriceAmount,
		Position:    position,
	}, now())
	if err != nil {
		return nil, err
	}
	if err := s.Variants.Create(ctx, variant); err != nil {
		return nil, err
	}
	return variant, nil
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
	if s.Tx == nil {
		return s.changeVariantStateDirect(ctx, in)
	}

	// Every step below — the re-read, the transition, the pending-order check,
	// the write and the cart cleanup — runs inside one transaction, so a racing
	// checkout either completes before the check (and then blocks the hide) or
	// starts after it (specs/016-hide-guard research D3).
	var (
		hid       bool
		auditedID uuid.UUID
		from, to  constant.SellState
		removed   int64
	)
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		variant, err := s.ownedVariant(txCtx, in.ProductID, in.VariantID)
		if err != nil {
			return err
		}
		from = variant.SellState
		if err := variant.ChangeSellState(in.To, now()); err != nil {
			return err
		}
		to = variant.SellState
		if !isMoveAwayFromOnSale(from, to) {
			// A move to on-sale, or between two non-on-sale states, is never
			// guarded and never cleans carts (specs/016-hide-guard FR-005).
			if err := s.Variants.Update(txCtx, variant); err != nil {
				return err
			}
			auditedID = variant.ID
			return nil
		}
		blocking, err := s.guardPendingOrders(txCtx, []uuid.UUID{variant.ID})
		if err != nil {
			return err
		}
		if len(blocking) > 0 {
			return domainerr.VariantHasPendingOrders(blocking)
		}
		if err := s.Variants.Update(txCtx, variant); err != nil {
			return err
		}
		removed, err = s.guardCleanCarts(txCtx, []uuid.UUID{variant.ID})
		if err != nil {
			return err
		}
		hid = true
		auditedID = variant.ID
		return nil
	}); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	// A refusal leaves everything untouched and records nothing (spec SC-001);
	// only a committed write is audited, after the transaction, so the trail
	// can never describe a hide that did not happen.
	if hid {
		s.recordVariantHidden(ctx, in.ProductID, auditedID, from, to, removed)
	} else {
		s.recordVariant(ctx, constant.AuditProductVariantUpdated, auditedID, map[string]any{
			"productId": in.ProductID.String(),
			"from":      string(from),
			"to":        string(to),
		})
	}
	return s.adminDetail(ctx, in.ProductID)
}

// changeVariantStateDirect applies a variant state move without the hide
// guard. It is the construction without a transaction port, mirroring the
// category module's legacy path; production always supplies the port.
func (s *Service) changeVariantStateDirect(ctx context.Context, in dto.ChangeVariantStateInput) (dto.AdminProductDetailOutput, error) {
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

// isMoveAwayFromOnSale reports whether the transition leaves the on-sale
// state. ACTIVE is the only on-sale sell state, so only a move that starts
// there and ends elsewhere is guarded: ACTIVE to OUT_OF_STOCK and ACTIVE to
// DISCONTINUED are checked, while a move to ACTIVE and a non-on-sale to
// non-on-sale move such as COMING_SOON to DISCONTINUED never are
// (specs/016-hide-guard FR-005).
func isMoveAwayFromOnSale(from, to constant.SellState) bool {
	return from == constant.SellStateActive && to != constant.SellStateActive
}

// guardPendingOrders returns the blocking PENDING or PAYMENT_PENDING orders
// holding the given variants in one bulk read. A nil port blocks nothing.
func (s *Service) guardPendingOrders(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
	if s.Orders == nil {
		return []uuid.UUID{}, nil
	}
	return s.Orders.PendingOrdersForVariants(ctx, variantIDs)
}

// guardCleanCarts removes every cart line holding any of the given variants in
// one statement and returns how many lines were removed. A nil port removes
// nothing.
func (s *Service) guardCleanCarts(ctx context.Context, variantIDs []uuid.UUID) (int64, error) {
	if s.Carts == nil {
		return 0, nil
	}
	return s.Carts.RemoveVariantsFromAllCarts(ctx, variantIDs)
}

// recordVariantHidden emits the hide event with the cart-cleanup count as
// metadata, so the trail records how many cart lines the hide removed without
// a new audit action (specs/016-hide-guard research D8). Emission never fails
// the business operation: it runs after the transaction committed.
func (s *Service) recordVariantHidden(ctx context.Context, productID, variantID uuid.UUID, from, to constant.SellState, removedCartLines int64) {
	s.recordVariant(ctx, constant.AuditProductVariantUpdated, variantID, map[string]any{
		"productId":        productID.String(),
		"from":             string(from),
		"to":               string(to),
		"removedCartLines": removedCartLines,
	})
}

// recordVariantSystemBlocked records a system stock-out signal the hide guard
// refused. The variant keeps its on-sale state and the caller never fails
// because of it; the event is what keeps the catalogue from lying silently
// (specs/016-hide-guard research D5, D8). The actor is nil, truthfully naming
// no human.
func (s *Service) recordVariantSystemBlocked(ctx context.Context, variantID uuid.UUID, from, to constant.SellState, blocking []uuid.UUID) {
	blocked := make([]string, 0, len(blocking))
	for _, id := range blocking {
		blocked = append(blocked, id.String())
	}
	s.recordVariantSystem(ctx, constant.AuditProductVariantUpdated, variantID, map[string]any{
		"from":            string(from),
		"to":              string(to),
		"blockedOrderIds": blocked,
	})
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

// signalAvailability applies one availability signal.
//
// A stock-out signal funnels through the same hide guard as an
// operator-initiated move away from on-sale: a variant covered by a pending
// order keeps its on-sale state and the blocked event is recorded, while the
// stock write the signal travels with still commits (specs/016-hide-guard
// research D5). A restock signal is a move back on-sale and never passes
// through the guard.
func (s *Service) signalAvailability(ctx context.Context, variantID uuid.UUID, target constant.SellState) error {
	switch target {
	case constant.SellStateOutOfStock:
		return s.signalStockOut(ctx, variantID)
	case constant.SellStateActive:
		return s.applyAvailabilitySignal(ctx, variantID, target)
	default:
		return nil
	}
}

// signalStockOut moves an on-sale variant to out of stock behind the hide
// guard. A blocked signal is a recorded no-op: it leaves the variant's state
// untouched, touches no cart and answers nil, so the inventory's stock
// operation never fails because of a display rule (research D5). Everything a
// successful signal writes — the state change and the cart cleanup — runs in
// one transaction, joining the caller's stock transaction when there is one.
func (s *Service) signalStockOut(ctx context.Context, variantID uuid.UUID) error {
	if s.Tx == nil {
		return s.applyAvailabilitySignal(ctx, variantID, constant.SellStateOutOfStock)
	}
	var (
		acted   bool
		removed int64
		from    constant.SellState
		blocked []uuid.UUID
	)
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		variant, err := s.Variants.FindByID(txCtx, variantID)
		if err != nil {
			return err
		}
		from = variant.SellState
		if variant.SellState != constant.SellStateActive {
			// A tolerant no-op, as before: an announced or retired variant,
			// or one already out of stock, must not move.
			return nil
		}
		pending, err := s.guardPendingOrders(txCtx, []uuid.UUID{variant.ID})
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			blocked = pending
			return nil
		}
		if err := variant.SellOut(now()); err != nil {
			return err
		}
		if err := s.Variants.Update(txCtx, variant); err != nil {
			return err
		}
		removed, err = s.guardCleanCarts(txCtx, []uuid.UUID{variant.ID})
		if err != nil {
			return err
		}
		acted = true
		return nil
	}); err != nil {
		return err
	}
	if len(blocked) > 0 {
		s.recordVariantSystemBlocked(ctx, variantID, from, constant.SellStateOutOfStock, blocked)
		return nil
	}
	if acted {
		s.recordVariantSystem(ctx, constant.AuditProductVariantUpdated, variantID, map[string]any{
			"from":             string(from),
			"to":               string(constant.SellStateOutOfStock),
			"removedCartLines": removed,
		})
	}
	return nil
}

// applyAvailabilitySignal applies one availability signal without the hide
// guard. It is a tolerant no-op unless the variant is in the state the signal
// leaves (ACTIVE for out-of-stock, OUT_OF_STOCK for on sale): an announced or
// retired variant, or one already in the target state, must not move, so a
// correct stock operation is never failed by a signal that changes nothing. An
// unknown variant is the module's not-found.
func (s *Service) applyAvailabilitySignal(ctx context.Context, variantID uuid.UUID, target constant.SellState) error {
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

// ReorderVariants replaces the order of one product's variants with the
// complete, ordered set the request carries (specs/018-catalog-reorder FR-003,
// FR-004, FR-014).
//
// The refusals are checked in the order research D2 fixes. An empty array is a
// request-shape problem (VALIDATION_ERROR) and a repeated identifier answers the
// dedicated duplicate code; the parent is then confirmed through the product
// read, so a reorder for an unknown product answers PRODUCT_NOT_FOUND before any
// variant is considered. The sibling read then scopes the set to the product: an
// identifier that is not one of its variants answers
// VARIANT_BELONGS_TO_OTHER_PRODUCT, whether it belongs to another product or to
// none, which is what makes a cross-product write impossible even if this check
// were bypassed (research D4). A missing sibling is a validation error. Only the
// complete sibling set reaches the single bulk write, whose affected-row count is
// required to equal the submitted size as a defensive invariant. A refusal
// writes nothing, so the stored order is unchanged (FR-004, SC-001).
//
// The parent existence check, the sibling read and the bulk write run inside one
// transaction when the service carries one, so a defensive refusal after the
// write rolls it back instead of leaving a partially applied order (FR-004,
// research D2). A construction without a transaction port resolves the work
// directly.
//
// The stored order is the submitted order, so the first variant becomes the
// default variant (FR-014). Exactly one audit event is emitted, targeting the
// parent product and carrying the affected ids in its metadata (research D9,
// FR-010); it is emitted after the transaction returns successfully. The acting
// administrator comes from the session context, never from the input.
func (s *Service) ReorderVariants(ctx context.Context, in dto.ReorderVariantsInput) (dto.ReorderResult, error) {
	if len(in.VariantIDs) == 0 {
		return dto.ReorderResult{}, domainerr.InvalidProductField(fieldVariantIDs, "must not be empty")
	}
	if hasDuplicateIDs(in.VariantIDs) {
		return dto.ReorderResult{}, domainerr.ErrVariantReorderDuplicateID
	}

	reorder := func(txCtx context.Context) (dto.ReorderResult, error) {
		exists, err := s.Products.Exists(txCtx, in.ProductID)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		if !exists {
			return dto.ReorderResult{}, domainerr.ErrProductNotFound
		}
		siblings, err := s.Variants.ListIDsByProduct(txCtx, in.ProductID)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		existing := idSet(siblings)
		for _, id := range in.VariantIDs {
			if !existing[id] {
				return dto.ReorderResult{}, domainerr.ErrVariantBelongsToOtherProduct
			}
		}
		if len(in.VariantIDs) != len(siblings) {
			return dto.ReorderResult{}, domainerr.InvalidProductField(fieldVariantIDs, "must name every variant")
		}

		updated, err := s.Variants.Reorder(txCtx, in.ProductID, in.VariantIDs)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		if updated != int64(len(in.VariantIDs)) {
			return dto.ReorderResult{}, domainerr.ErrVariantBelongsToOtherProduct
		}
		return dto.ReorderResult{UpdatedCount: updated}, nil
	}

	var result dto.ReorderResult
	if s.Tx != nil {
		if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
			var innerErr error
			result, innerErr = reorder(txCtx)
			return innerErr
		}); err != nil {
			return dto.ReorderResult{}, err
		}
	} else {
		var err error
		result, err = reorder(ctx)
		if err != nil {
			return dto.ReorderResult{}, err
		}
	}

	s.recordVariantsReordered(ctx, in.ProductID, in.VariantIDs)
	return result, nil
}

// recordVariantsReordered emits the one audit event of a bulk variant reorder.
// The event targets the parent product, because a bulk write has no single target
// row, and the affected variant identifiers travel in the metadata (research D9).
// Emission never fails the business operation.
func (s *Service) recordVariantsReordered(ctx context.Context, productID uuid.UUID, ids []uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditProductVariantsReordered, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeProduct, productID.String(),
		map[string]any{"count": len(ids), "ids": idStrings(ids)},
	)
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
