package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeProduct is the audit_logs target type of the product events. The
// target of a mutation is the product, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// product without scanning the actor's other events (FR-014, Constitution VI).
const targetTypeProduct = "product"

// now reads the wall clock. Every use case that stamps a row goes through it, so
// the domain stays a pure function of its arguments and the clock is injected at
// exactly one place.
func now() time.Time { return time.Now().UTC() }

// ListAdmin returns one page of every product, including the ones not visible to
// customers (FR-011). It applies the same window the public list applies, so an
// out-of-range request is clamped rather than refused, and it preserves the
// FR-009 order the repository already applied.
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
// customers, with its pictures and, for a set, its members (FR-011). An unknown
// identifier is reported as the module's not-found.
func (s *Service) GetAdmin(ctx context.Context, in dto.AdminProductRefInput) (dto.AdminProductDetailOutput, error) {
	return s.adminDetail(ctx, in.ID)
}

// CreateProduct creates a product in COMING_SOON and audits the change (FR-010,
// FR-014).
//
// The entity validates the name, the slug, the description and the price before
// anything is written, so a rejected draft persists nothing and names the
// offending member (FR-030, FR-032). A slug another product already holds is
// reported by the repository as the collision sentinel, and a category
// identifier no row carries as the invalid-field sentinel naming categoryId
// (FR-028, FR-033); both reach the caller through presentation's mapping.
//
// The acting administrator is taken from the context the session filled, never
// from the input (FR-015).
func (s *Service) CreateProduct(ctx context.Context, in dto.CreateProductInput) (dto.AdminProductDetailOutput, error) {
	product, err := model.NewProduct(model.ProductDraft{
		Name:               in.Name,
		Slug:               in.Slug,
		Description:        in.Description,
		Price:              in.Price,
		CategoryID:         in.CategoryID,
		Position:           in.Position,
		IsSet:              in.IsSet,
		IsPreorder:         in.IsPreorder,
		PreorderExpectedAt: in.PreorderExpectedAt,
	}, now())
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Products.Create(ctx, product); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordProduct(ctx, constant.AuditProductCreated, product.ID, map[string]any{"slug": product.Slug})
	return s.adminDetail(ctx, product.ID)
}

// UpdateProduct applies a partial edit and audits the change (FR-012, FR-014).
//
// An omitted member keeps its current value. The entity validates the whole
// change before writing any of it, so a rejection leaves the row untouched
// (FR-024). The sell state is deliberately absent: a transition has its own
// endpoint (research D11). Re-sending the product's own slug succeeds because
// the storage unique index excludes the row being updated (FR-027).
func (s *Service) UpdateProduct(ctx context.Context, in dto.UpdateProductInput) (dto.AdminProductDetailOutput, error) {
	stored, err := s.Products.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	product := stored.Product
	if err := product.Apply(model.ProductEdit{
		Name:               in.Name,
		Slug:               in.Slug,
		Description:        in.Description,
		Price:              in.Price,
		CategoryID:         in.CategoryID,
		Position:           in.Position,
		IsSet:              in.IsSet,
		IsPreorder:         in.IsPreorder,
		PreorderExpectedAt: in.PreorderExpectedAt,
	}, now()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Products.Update(ctx, &product); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordProduct(ctx, constant.AuditProductUpdated, product.ID, map[string]any{"slug": product.Slug})
	return s.adminDetail(ctx, in.ID)
}

// DeleteProduct removes a product and audits the removal (FR-013, FR-014).
//
// Removal is a hard delete (research D13): the row goes, cascading to its
// pictures and set membership rows, and the audit entry stays. An unknown
// identifier — including one already removed — is reported as the module's
// not-found rather than failing (FR-012).
func (s *Service) DeleteProduct(ctx context.Context, in dto.AdminProductRefInput) error {
	if err := s.Products.Delete(ctx, in.ID); err != nil {
		return err
	}
	s.recordProduct(ctx, constant.AuditProductDeleted, in.ID, nil)
	return nil
}

// adminDetail reads one product's administrator detail. For a set it also reads
// the member products, which the public shape never enumerates (research D18).
func (s *Service) adminDetail(ctx context.Context, id uuid.UUID) (dto.AdminProductDetailOutput, error) {
	view, err := s.Products.FindByID(ctx, id)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	var members []domainrepo.SetMember
	if view.Product.IsSet {
		members, err = s.Products.ListSetMembers(ctx, id)
		if err != nil {
			return dto.AdminProductDetailOutput{}, err
		}
	}
	return s.Mapper.AdminProductDetail(*view, members), nil
}

// recordProduct emits one administrative audit event for a product write.
//
// The event always names the acting administrator when the call has a session,
// and the product identifier as the target. Metadata carries only the operator's
// own slug, never the folding key, so an audit entry can never leak the
// normalised value the contract never exposes (FR-014, Constitution VI).
//
// Emission never fails the business operation: the shared writer queues and
// reports a dropped event through the logger. An absent auditor is tolerated so a
// read-only construction of the service cannot panic on a write it never serves.
func (s *Service) recordProduct(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeProduct, targetID.String(), metadata)
}

// auditActor returns the acting account of the request for the audit row, or nil
// when the call is not serving a request. A missing actor is a fact worth
// recording rather than a reason to skip the event.
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
