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

// now reads the wall clock. Every use case that stamps a row goes through it, so
// the domain stays a pure function of its arguments and the clock is injected at
// exactly one place.
func now() time.Time { return time.Now().UTC() }

// ListAdmin returns one page of every product, including the ones not visible to
// customers (FR-010). It applies the same window the public list applies, so an
// out-of-range request is clamped rather than refused, and it preserves the order
// the repository already applied.
func (s *Service) ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminProductPage, error) {
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
// The acting administrator is taken from the context the session filled, never
// from the input (FR-015).
func (s *Service) CreateProduct(ctx context.Context, in dto.CreateProductInput) (dto.AdminProductDetailOutput, error) {
	variantDrafts := make([]model.VariantDraft, 0, len(in.Variants))
	for _, v := range in.Variants {
		variantDrafts = append(variantDrafts, model.VariantDraft{
			Name:        v.Name,
			SKU:         v.SKU,
			PriceAmount: v.PriceAmount,
			Position:    v.Position,
		})
	}

	product, err := model.NewProduct(model.ProductDraft{
		Name:        in.Name,
		Code:        in.Code,
		Description: in.Description,
		Price:       in.Price,
		CategoryID:  in.CategoryID,
		Position:    in.Position,
		Variants:    variantDrafts,
	}, now())
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
