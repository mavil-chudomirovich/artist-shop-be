package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// The audit_logs target types of the product events. A product mutation targets
// the product and a variant mutation targets the variant, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// entity without scanning the actor's other events (FR-014, Constitution VI).
const (
	// targetTypeProduct is the audit_logs target type of the product events.
	targetTypeProduct = "product"
	// targetTypeVariant is the audit_logs target type of the variant events.
	targetTypeVariant = "product_variant"
)

// fieldProductIDs is the contract member a product reorder names, so a refused
// array answers a field-level detail the client can map straight onto its
// request (FR-005, contracts/error-codes.md).
const fieldProductIDs = "productIds"

// fieldAll is the contract member the `all` selector's cap refusal names, so the
// client can map the detail straight onto its query (specs/018-catalog-reorder
// research D10).
const fieldAll = "all"

// maxAdminListAll bounds the complete-list fetch (`?all=true`). A catalogue
// larger than this is refused rather than returning an unbounded result
// (specs/018-catalog-reorder FR-015, research D10).
const maxAdminListAll = 1000

// now reads the wall clock. Every use case that stamps a row goes through it, so
// the domain stays a pure function of its arguments and the clock is injected at
// exactly one place.
func now() time.Time { return time.Now().UTC() }

// ListAdmin returns one page of every product, including the ones not visible to
// customers (FR-010). It applies the same window the public list applies, so an
// out-of-range request is clamped rather than refused, and it preserves the order
// the repository already applied.
//
// With All set it answers the complete ordered list instead of one page, so a
// whole catalogue can be reordered as a whole (specs/018-catalog-reorder FR-015,
// research D10).
func (s *Service) ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminProductPage, error) {
	if in.All {
		return s.listAdminAll(ctx)
	}
	page, pageSize := listWindow(in.Page, in.PageSize)
	items, total, err := s.Products.ListAll(ctx, page, pageSize)
	if err != nil {
		return dto.AdminProductPage{}, err
	}
	return dto.AdminProductPage{
		Products: s.Mapper.AdminProducts(items),
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// listAdminAll reads the complete ordered catalogue for the `all` selector in one
// bounded read. It asks the repository for one row beyond the safety maximum, so
// a catalogue at or below the cap is answered whole and one beyond it is refused
// without ever materialising an unbounded result (specs/018-catalog-reorder
// FR-015, research D10). The read reuses the same paged repository call the
// administrator list already uses, so no repository contract changes.
func (s *Service) listAdminAll(ctx context.Context) (dto.AdminProductPage, error) {
	items, total, err := s.Products.ListAll(ctx, 1, maxAdminListAll+1)
	if err != nil {
		return dto.AdminProductPage{}, err
	}
	if total > maxAdminListAll {
		return dto.AdminProductPage{}, domainerr.InvalidProductField(fieldAll, "the list is too large to fetch in full")
	}
	return dto.AdminProductPage{
		Products: s.Mapper.AdminProducts(items),
		Page:     1,
		PageSize: len(items),
		Total:    total,
	}, nil
}

// GetAdmin returns one product by identifier, including one not visible to
// customers, with its gallery and every variant (FR-010). An unknown identifier is
// reported as the module's not-found.
func (s *Service) GetAdmin(ctx context.Context, in dto.AdminProductRefInput) (dto.AdminProductDetailOutput, error) {
	return s.adminDetail(ctx, in.ID)
}

// CreateProduct creates a product together with at least one variant and audits
// the change (FR-010, FR-012, FR-014).
//
// The product and each variant are validated before anything is written, so a
// rejected draft persists nothing and names the offending member. The product and
// its variants are written in one transaction, so a product is never stored
// without its variants (FR-012, research D14). A code another product already
// holds is reported by the repository as the collision sentinel, a SKU another
// variant already holds as the SKU sentinel, and a category identifier no row
// carries as the invalid-field sentinel naming categoryId.
//
// The positions are optional (FR-011, research D3, D7): an omitted product
// position is resolved to the front of the catalogue inside the same transaction
// as the insert, and a variant whose position is omitted takes its index in the
// payload, so the first payload variant is the default. A supplied position is
// honoured.
//
// The acting administrator is taken from the context the session filled, never
// from the input (FR-015).
func (s *Service) CreateProduct(ctx context.Context, in dto.CreateProductInput) (dto.AdminProductDetailOutput, error) {
	variantDrafts := make([]model.VariantDraft, 0, len(in.Variants))
	for i, v := range in.Variants {
		// An omitted variant position follows the payload order, so the first
		// payload variant is the default variant (research D7).
		position := i
		if v.Position != nil {
			position = *v.Position
		}
		variantDrafts = append(variantDrafts, model.VariantDraft{
			Name:        v.Name,
			SKU:         v.SKU,
			PriceAmount: v.PriceAmount,
			Position:    position,
		})
	}

	productDraft := model.ProductDraft{
		Name:        in.Name,
		Code:        in.Code,
		Description: in.Description,
		Price:       in.Price,
		CategoryID:  in.CategoryID,
		Variants:    variantDrafts,
	}
	if in.Position != nil {
		productDraft.Position = *in.Position
	}
	product, err := model.NewProduct(productDraft, now())
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	variants := make([]model.Variant, 0, len(variantDrafts))
	for _, draft := range variantDrafts {
		variant, err := model.NewVariant(product.ID, draft, now())
		if err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
		variants = append(variants, *variant)
	}

	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if in.Position == nil {
			// An omitted product position places it at the front of the
			// catalogue. The read joins the insert's transaction, so two
			// concurrent creates cannot read the same front (research D3).
			front, err := s.Products.NextFrontPosition(txCtx)
			if err != nil {
				return err
			}
			product.Position = front
		}
		if err := s.Products.Create(txCtx, product); err != nil {
			return err
		}
		for i := range variants {
			if err := s.Variants.Create(txCtx, &variants[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	s.recordProduct(ctx, constant.AuditProductCreated, product.ID, map[string]any{"code": product.Code})
	return s.adminDetail(ctx, product.ID)
}

// UpdateProduct applies a partial edit and audits the change (FR-010, FR-014).
//
// An omitted member keeps its current value. The entity validates the whole change
// before writing any of it, so a rejection leaves the row untouched. The variants
// are managed through their own operations, so they are absent here. Re-sending
// the product's own code succeeds because the storage unique index
// excludes the row being updated (FR-011).
//
// The display state is never assigned directly: it moves only through Hide or
// Show, and only when the request asks for the other state. Turning the flag
// off while the product is visible runs through the pending-orders guard in
// updateProductGuarded: all of the product's variants are checked against
// PENDING and PAYMENT_PENDING orders, and — only when none covers them — the
// flag turns off with every cart line holding those variants removed in the
// same transaction (specs/016-hide-guard FR-006, FR-007, research D3, D4).
// Turning the flag back on, or an edit that does not touch the flag, never
// runs the guard and never restores removed cart lines.
func (s *Service) UpdateProduct(ctx context.Context, in dto.UpdateProductInput) (dto.AdminProductDetailOutput, error) {
	stored, err := s.Products.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	if in.IsVisible != nil && !*in.IsVisible && stored.Product.IsVisible && s.Tx != nil {
		return s.updateProductGuarded(ctx, in)
	}

	product := stored.Product
	contentChanged := in.Name != nil || in.Code != nil || in.Description != nil ||
		in.Price != nil || in.CategoryID != nil || in.Position != nil
	if contentChanged {
		if err := product.Apply(model.ProductEdit{
			Name:        in.Name,
			Code:        in.Code,
			Description: in.Description,
			Price:       in.Price,
			CategoryID:  in.CategoryID,
			Position:    in.Position,
		}, now()); err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
	}

	displayChanged := false
	if in.IsVisible != nil {
		if *in.IsVisible {
			displayChanged = product.Show(now())
		} else {
			displayChanged = product.Hide(now())
		}
	}

	if !contentChanged && !displayChanged {
		// Nothing changed: there is no write to make and no transition to
		// trace, so the current product is returned as it stands. This is
		// what makes a repeated hide or show a no-op rather than an error.
		return s.adminDetail(ctx, in.ID)
	}

	if err := s.Products.Update(ctx, &product); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if contentChanged {
		s.recordProduct(ctx, constant.AuditProductUpdated, product.ID, map[string]any{"code": product.Code})
	}
	if displayChanged {
		s.recordProductHidden(ctx, product.ID, product.Code, 0)
	}
	return s.adminDetail(ctx, in.ID)
}

// updateProductGuarded turns a visible product's flag off behind the
// pending-orders guard. The caller has already established that the request
// asks to hide and the product is currently visible; everything below — the
// re-read, the content edit, the pending-order check over ALL of the product's
// variants, the hide and the cart cleanup — runs inside one transaction.
//
// A blocking order refuses the whole patch with the order identifiers: neither
// the hide nor any content change the same patch carries is persisted, and no
// cart line is touched (spec SC-001). Showing again is never guarded and never
// restores removed cart lines.
func (s *Service) updateProductGuarded(ctx context.Context, in dto.UpdateProductInput) (dto.AdminProductDetailOutput, error) {
	contentChanged := in.Name != nil || in.Code != nil || in.Description != nil ||
		in.Price != nil || in.CategoryID != nil || in.Position != nil

	var audited model.Product
	var removed int64
	var wroteContent, hid bool

	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		view, err := s.Products.FindByID(txCtx, in.ID)
		if err != nil {
			return err
		}
		product := view.Product
		if contentChanged {
			if err := product.Apply(model.ProductEdit{
				Name:        in.Name,
				Code:        in.Code,
				Description: in.Description,
				Price:       in.Price,
				CategoryID:  in.CategoryID,
				Position:    in.Position,
			}, now()); err != nil {
				return err
			}
		}
		if !product.Hide(now()) {
			// A racing hide already took the product off display: there is
			// nothing left to guard. A content change the patch carries is
			// still persisted; a patch that asks for nothing else answers the
			// current product without writing or auditing anything.
			if contentChanged {
				if err := s.Products.Update(txCtx, &product); err != nil {
					return err
				}
				wroteContent = true
				audited = product
			}
			return nil
		}

		variantIDs := variantIDs(view.Variants)
		blocking, err := s.guardPendingOrders(txCtx, variantIDs)
		if err != nil {
			return err
		}
		if len(blocking) > 0 {
			return domainerr.ProductHasPendingOrders(blocking)
		}

		if err := s.Products.Update(txCtx, &product); err != nil {
			return err
		}
		removed, err = s.guardCleanCarts(txCtx, variantIDs)
		if err != nil {
			return err
		}
		hid = true
		wroteContent = contentChanged
		audited = product
		return nil
	}); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	if wroteContent {
		s.recordProduct(ctx, constant.AuditProductUpdated, audited.ID, map[string]any{"code": audited.Code})
	}
	if hid {
		s.recordProductHidden(ctx, audited.ID, audited.Code, removed)
	}
	return s.adminDetail(ctx, in.ID)
}

// DeleteProduct removes a product and audits the removal (FR-010, FR-014).
//
// Removal is a hard delete: the row goes, cascading to its pictures and its
// variants, and the audit entry stays. An unknown identifier — including one
// already removed — is reported as the module's not-found rather than failing.
func (s *Service) DeleteProduct(ctx context.Context, in dto.AdminProductRefInput) error {
	if err := s.Products.Delete(ctx, in.ID); err != nil {
		return err
	}
	s.recordProduct(ctx, constant.AuditProductDeleted, in.ID, nil)
	return nil
}

// ReorderProducts replaces the catalogue-wide product order with the complete,
// ordered set the request carries (specs/018-catalog-reorder FR-002, FR-004).
//
// The refusals are checked in the order research D2 fixes, so each maps to the
// code the client can act on: an empty array is a request-shape problem
// (VALIDATION_ERROR); a repeated identifier answers the dedicated duplicate
// code; the sibling read then distinguishes a missing sibling (VALIDATION_ERROR)
// from an identifier that names no product (PRODUCT_NOT_FOUND). Only once the
// request is the complete sibling set does the single bulk write run, and its
// affected-row count is required to equal the submitted size as a defensive
// invariant against a race the pre-checks missed. A refusal writes nothing, so
// the stored order is unchanged (FR-004, SC-001).
//
// The sibling read, the checks and the bulk write run inside one transaction
// when the service carries one, so a defensive refusal after the write rolls it
// back instead of leaving a partially applied order (FR-004, research D2). A
// construction without a transaction port resolves the work directly.
//
// The reorder emits exactly one audit event: a bulk write has no single target
// row, so the affected ids travel in the metadata and the target names the
// ordered scope (research D9, FR-010). It is emitted after the transaction
// returns successfully. The acting administrator comes from the session context,
// never from the input.
func (s *Service) ReorderProducts(ctx context.Context, in dto.ReorderProductsInput) (dto.ReorderResult, error) {
	if len(in.ProductIDs) == 0 {
		return dto.ReorderResult{}, domainerr.InvalidProductField(fieldProductIDs, "must not be empty")
	}
	if hasDuplicateIDs(in.ProductIDs) {
		return dto.ReorderResult{}, domainerr.ErrProductReorderDuplicateID
	}

	reorder := func(txCtx context.Context) (dto.ReorderResult, error) {
		siblings, err := s.Products.ListIDs(txCtx)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		existing := idSet(siblings)
		for _, id := range in.ProductIDs {
			if !existing[id] {
				return dto.ReorderResult{}, domainerr.ErrProductNotFound
			}
		}
		if len(in.ProductIDs) != len(siblings) {
			return dto.ReorderResult{}, domainerr.InvalidProductField(fieldProductIDs, "must name every product")
		}

		updated, err := s.Products.Reorder(txCtx, in.ProductIDs)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		if updated != int64(len(in.ProductIDs)) {
			return dto.ReorderResult{}, domainerr.ErrProductNotFound
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

	s.recordProductReordered(ctx, in.ProductIDs)
	return result, nil
}

// recordProductReordered emits the one audit event of a bulk product reorder. A
// bulk write has no single target row, so the target names the ordered scope
// (the product list) with an empty identifier and the affected identifiers
// travel in the metadata (research D9). Emission never fails the business
// operation.
func (s *Service) recordProductReordered(ctx context.Context, ids []uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditProductReordered, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeProduct, "",
		map[string]any{"count": len(ids), "ids": idStrings(ids)},
	)
}

// hasDuplicateIDs reports whether the array names the same identifier twice, so a
// reorder can answer the dedicated duplicate code before any read or write
// (research D2, FR-006).
func hasDuplicateIDs(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// idSet turns an identifier slice into a membership set, so a completeness check
// is a map lookup rather than a nested scan.
func idSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// idStrings is the metadata form of the affected identifiers: the contract's
// string representation, so an audit reader sees the same values a client sent.
func idStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// adminDetail reads one product's administrator detail: the product, its gallery
// and every variant, including the ones a customer cannot see (FR-010).
func (s *Service) adminDetail(ctx context.Context, id uuid.UUID) (dto.AdminProductDetailOutput, error) {
	view, err := s.Products.FindByID(ctx, id)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	return s.Mapper.AdminProductDetail(*view), nil
}

// variantIDs extracts the identifiers of a variant slice, preserving order.
func variantIDs(variants []model.Variant) []uuid.UUID {
	if len(variants) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(variants))
	for _, variant := range variants {
		out = append(out, variant.ID)
	}
	return out
}

// recordProduct emits one administrative audit event for a product write.
//
// The event always names the acting administrator when the call has a session, and
// the product identifier as the target. Metadata carries only the operator's own
// code, so an audit entry can never leak a value the contract never exposes
// (FR-014, Constitution VI).
//
// Emission never fails the business operation: the shared writer queues and reports
// a dropped event through the logger. An absent auditor is tolerated so a read-only
// construction of the service cannot panic on a write it never serves.
func (s *Service) recordProduct(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	s.record(ctx, targetTypeProduct, action, targetID, metadata)
}

// recordProductHidden emits the hide event with the cart-cleanup count as
// metadata, so the trail records how many cart lines the hide removed without
// a new audit action (specs/016-hide-guard research D8). Emission never fails
// the business operation: it runs after the transaction committed.
func (s *Service) recordProductHidden(ctx context.Context, productID uuid.UUID, code string, removedCartLines int64) {
	s.recordProduct(ctx, constant.AuditProductUpdated, productID,
		map[string]any{"code": code, "removedCartLines": removedCartLines})
}

// recordVariant emits one administrative audit event for a variant write, naming
// the variant as the target.
func (s *Service) recordVariant(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	s.record(ctx, targetTypeVariant, action, targetID, metadata)
}

// record emits one administrative audit event with the acting account taken from
// the session context.
func (s *Service) record(ctx context.Context, targetType, action string, targetID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetType, targetID.String(), metadata)
}

// recordVariantSystem emits one audit event for a variant change no human caused.
// The actor is nil and the role empty, which is truthful: an availability change is
// driven by stock, not by an administrator (FR-015, Constitution VI). Emission
// never fails the business operation.
func (s *Service) recordVariantSystem(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess), nil, "", targetTypeVariant, targetID.String(), metadata)
}

// auditActor returns the acting account of the request for the audit row, or nil
// when the call is not serving a request. A missing actor is a fact worth recording
// rather than a reason to skip the event.
func auditActor(ctx context.Context) *uuid.UUID {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return nil
	}
	id := actor.ID
	return &id
}

// auditActorRole returns the acting account's role for the audit row, or an empty
// string when there is no session behind the call.
func auditActorRole(ctx context.Context) string {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return ""
	}
	return string(actor.Role)
}
