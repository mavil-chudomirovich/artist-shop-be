package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeCategory is the audit_logs target type of the category events. The
// target of a mutation is the category, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// category without scanning the actor's other events (Constitution VI).
const targetTypeCategory = "category"

// fieldCategoryIDs is the contract member a category reorder names, so a
// refused array answers a field-level detail the client can map straight onto
// its request (FR-005, contracts/error-codes.md).
const fieldCategoryIDs = "categoryIds"

// fieldAll is the contract member the `all` selector's cap refusal names, so the
// client can map the detail straight onto its query (specs/018-catalog-reorder
// research D10).
const fieldAll = "all"

// maxAdminListAll bounds the complete-list fetch (`?all=true`). A catalogue
// larger than this is refused rather than returning an unbounded result
// (specs/018-catalog-reorder FR-015, research D10).
const maxAdminListAll = 1000

// The whole use-case surface is satisfied now that the maintenance use cases
// exist, so the compile-time assertion lands with them rather than earlier.
var _ appinterface.CategoryService = (*Service)(nil)

// now reads the wall clock. Every use case that stamps a row goes through it, so
// the domain stays a pure function of its arguments and the clock is injected at
// exactly one place.
func now() time.Time { return time.Now().UTC() }

// ListAdmin returns one page of every category, including the ones not on display
// (FR-009). It applies the same window the public list applies, so an
// out-of-range request is clamped rather than refused, and it preserves the
// FR-003 order the repository already applied.
//
// With All set it answers the complete ordered list instead of one page, so a
// whole catalogue can be reordered as a whole (specs/018-catalog-reorder FR-015,
// research D10).
func (s *Service) ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminCategoryPage, error) {
	if in.All {
		return s.listAdminAll(ctx)
	}
	page, pageSize := listWindow(in.Page, in.PageSize)
	list, total, err := s.Categories.ListAll(ctx, page, pageSize)
	if err != nil {
		return dto.AdminCategoryPage{}, err
	}
	return dto.AdminCategoryPage{
		Categories: s.Mapper.AdminCategories(list),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
	}, nil
}

// listAdminAll reads the complete ordered catalogue for the `all` selector in one
// bounded read. It asks the repository for one row beyond the safety maximum, so
// a catalogue at or below the cap is answered whole and one beyond it is refused
// without ever materialising an unbounded result (specs/018-catalog-reorder
// FR-015, research D10). The read reuses the same paged repository call the
// administrator list already uses, so no repository contract changes.
func (s *Service) listAdminAll(ctx context.Context) (dto.AdminCategoryPage, error) {
	list, total, err := s.Categories.ListAll(ctx, 1, maxAdminListAll+1)
	if err != nil {
		return dto.AdminCategoryPage{}, err
	}
	if total > maxAdminListAll {
		return dto.AdminCategoryPage{}, domainerr.InvalidCategoryField(fieldAll, "the list is too large to fetch in full")
	}
	return dto.AdminCategoryPage{
		Categories: s.Mapper.AdminCategories(list),
		Page:       1,
		PageSize:   len(list),
		Total:      total,
	}, nil
}

// GetAdmin returns one category by identifier, including one not on display
// (FR-009). An unknown identifier is reported as the module's not-found.
func (s *Service) GetAdmin(ctx context.Context, in dto.AdminCategoryRefInput) (dto.AdminCategoryOutput, error) {
	category, err := s.Categories.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminCategoryOutput{}, err
	}
	return s.Mapper.AdminCategory(*category), nil
}

// CreateCategory creates a category on display and audits the change (FR-008,
// FR-013).
//
// The entity validates the name, the slug and the description before anything is
// written, so a rejected draft persists nothing and names the offending member
// (FR-018, FR-019, FR-020). A name or slug another category already holds is
// reported by the repository as the matching sentinel, which presentation maps to
// the distinct 409 code (FR-016, FR-017).
//
// The position is optional: an omitted position places the new category at the
// front of the catalogue (FR-011, research D3). The front read and the insert
// run in one transaction when the service carries one, so two concurrent
// creates cannot read the same value; a supplied position is honoured and the
// create stays a single statement (FR-012).
//
// The acting administrator is taken from the context the session filled, never
// from the input (FR-014); auditActor records it, or nil when the call has no
// session behind it.
func (s *Service) CreateCategory(ctx context.Context, in dto.CreateCategoryInput) (dto.AdminCategoryOutput, error) {
	if in.Position == nil {
		return s.createCategoryAtFront(ctx, in)
	}

	category, err := model.NewCategory(model.CategoryDraft{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		Position:    *in.Position,
	}, now())
	if err != nil {
		return dto.AdminCategoryOutput{}, err
	}
	if err := s.Categories.Create(ctx, category); err != nil {
		return dto.AdminCategoryOutput{}, err
	}
	s.recordCategory(ctx, constant.AuditCategoryCreated, category)
	return s.Mapper.AdminCategory(*category), nil
}

// createCategoryAtFront creates a category whose position was omitted, placing it
// at the front of the catalogue: one less than the current smallest position, or
// zero when the catalogue is empty (FR-011, research D3).
//
// The front read and the insert run inside the same transaction when the service
// carries one, so two concurrent creates cannot read the same front. A
// construction without a transaction port resolves the read directly.
func (s *Service) createCategoryAtFront(ctx context.Context, in dto.CreateCategoryInput) (dto.AdminCategoryOutput, error) {
	create := func(txCtx context.Context) (*model.Category, error) {
		front, err := s.Categories.NextFrontPosition(txCtx)
		if err != nil {
			return nil, err
		}
		category, err := model.NewCategory(model.CategoryDraft{
			Name:        in.Name,
			Slug:        in.Slug,
			Description: in.Description,
			Position:    front,
		}, now())
		if err != nil {
			return nil, err
		}
		if err := s.Categories.Create(txCtx, category); err != nil {
			return nil, err
		}
		return category, nil
	}

	var category *model.Category
	if s.Tx != nil {
		if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
			var innerErr error
			category, innerErr = create(txCtx)
			return innerErr
		}); err != nil {
			return dto.AdminCategoryOutput{}, err
		}
	} else {
		var err error
		category, err = create(ctx)
		if err != nil {
			return dto.AdminCategoryOutput{}, err
		}
	}

	s.recordCategory(ctx, constant.AuditCategoryCreated, category)
	return s.Mapper.AdminCategory(*category), nil
}

// UpdateCategory applies a partial edit and audits each transition it performs
// (FR-010, FR-011, FR-013).
//
// An omitted member keeps its current value. The content members—name, slug,
// description, position—move through the entity's Apply, which validates the
// whole change before writing any of it, so a rejection leaves the row untouched
// (FR-022). The display state is never assigned directly: it moves only through
// Hide or Show, and only when the request asks for the other state. A request to
// hide a hidden category (or show a visible one) is a no-op, not an error, and
// records nothing (data-model.md).
//
// A patch that carries both a content change and a display change performs both
// and records both actions: they are two transitions of the transition table, and
// a single PATCH is allowed to ask for two. A patch that asks for no change at
// all answers the current category without writing or auditing anything.
//
// Hiding a visible category (turning its display off) runs through the pending
// orders guard in updateCategoryGuarded: the category's variants are resolved,
// checked against PENDING and PAYMENT_PENDING orders, and — only when none
// covers them — hidden with every cart line holding those variants removed in
// the same transaction (specs/016-hide-guard FR-004, FR-007, research D3, D4).
// Showing a category again, or an edit that does not touch visibility, never
// runs the guard and never restores removed cart lines.
func (s *Service) UpdateCategory(ctx context.Context, in dto.UpdateCategoryInput) (dto.AdminCategoryOutput, error) {
	category, err := s.Categories.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminCategoryOutput{}, err
	}

	if in.IsVisible != nil && !*in.IsVisible && category.IsVisible && s.Tx != nil {
		return s.updateCategoryGuarded(ctx, in)
	}

	contentChanged := in.Name != nil || in.Slug != nil || in.Description != nil || in.Position != nil
	if contentChanged {
		if err := category.Apply(model.CategoryEdit{
			Name:        in.Name,
			Slug:        in.Slug,
			Description: in.Description,
			Position:    in.Position,
		}, now()); err != nil {
			return dto.AdminCategoryOutput{}, err
		}
	}

	displayChanged := false
	displayAction := ""
	if in.IsVisible != nil {
		if *in.IsVisible {
			displayChanged = category.Show(now())
			displayAction = constant.AuditCategoryShown
		} else {
			displayChanged = category.Hide(now())
			displayAction = constant.AuditCategoryHidden
		}
	}

	if !contentChanged && !displayChanged {
		// Nothing changed: there is no write to make and no transition to
		// trace, so the current category is returned as it stands. This is
		// what makes a repeated hide or show a no-op rather than an error.
		return s.Mapper.AdminCategory(*category), nil
	}

	if err := s.Categories.Update(ctx, category); err != nil {
		return dto.AdminCategoryOutput{}, err
	}
	if contentChanged {
		s.recordCategory(ctx, constant.AuditCategoryUpdated, category)
	}
	if displayChanged {
		s.recordCategory(ctx, displayAction, category)
	}
	return s.Mapper.AdminCategory(*category), nil
}

// updateCategoryGuarded hides a visible category behind the pending-orders
// guard. The caller has already established that the request asks to hide and
// the category is currently visible; everything below — the re-read, the
// content edit, the variant resolution, the pending-order check, the hide and
// the cart cleanup — runs inside one transaction.
//
// A blocking order refuses the whole patch with the order identifiers: neither
// the hide nor any content change the same patch carries is persisted, and no
// cart line is touched (spec SC-001). Showing again is never guarded and never
// restores removed cart lines.
func (s *Service) updateCategoryGuarded(ctx context.Context, in dto.UpdateCategoryInput) (dto.AdminCategoryOutput, error) {
	contentChanged := in.Name != nil || in.Slug != nil || in.Description != nil || in.Position != nil

	var out dto.AdminCategoryOutput
	var audited model.Category
	var removed int64
	var wroteContent, hid bool

	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		category, err := s.Categories.FindByID(txCtx, in.ID)
		if err != nil {
			return err
		}
		if contentChanged {
			if err := category.Apply(model.CategoryEdit{
				Name:        in.Name,
				Slug:        in.Slug,
				Description: in.Description,
				Position:    in.Position,
			}, now()); err != nil {
				return err
			}
		}
		if !category.Hide(now()) {
			// A racing hide already took the category off display: there is
			// nothing left to guard. A content change the patch carries is
			// still persisted; a patch that asks for nothing else answers the
			// current category without writing or auditing anything.
			if contentChanged {
				if err := s.Categories.Update(txCtx, category); err != nil {
					return err
				}
				wroteContent = true
				audited = *category
			}
			out = s.Mapper.AdminCategory(*category)
			return nil
		}

		variantIDs, err := s.guardVariantIDs(txCtx, category.ID)
		if err != nil {
			return err
		}
		blocking, err := s.guardPendingOrders(txCtx, variantIDs)
		if err != nil {
			return err
		}
		if len(blocking) > 0 {
			return domainerr.CategoryHasPendingOrders(blocking)
		}

		if err := s.Categories.Update(txCtx, category); err != nil {
			return err
		}
		removed, err = s.guardCleanCarts(txCtx, variantIDs)
		if err != nil {
			return err
		}
		hid = true
		wroteContent = contentChanged
		audited = *category
		out = s.Mapper.AdminCategory(*category)
		return nil
	}); err != nil {
		return dto.AdminCategoryOutput{}, err
	}

	if wroteContent {
		s.recordCategory(ctx, constant.AuditCategoryUpdated, &audited)
	}
	if hid {
		s.recordCategoryHidden(ctx, &audited, removed)
	}
	return out, nil
}

// guardVariantIDs resolves the category's variants through the product
// contract in one bulk read. A nil port resolves none.
func (s *Service) guardVariantIDs(ctx context.Context, categoryID uuid.UUID) ([]uuid.UUID, error) {
	if s.Variants == nil {
		return []uuid.UUID{}, nil
	}
	return s.Variants.VariantIDsByCategories(ctx, []uuid.UUID{categoryID})
}

// guardPendingOrders returns the blocking PENDING or PAYMENT_PENDING orders
// holding any of the given variants in one bulk read. A nil port blocks
// nothing.
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

// DeleteCategory removes a category and audits the removal (FR-012, FR-013).
//
// Removal is a hard delete (research D5): the row goes, the audit entry stays. An
// unknown identifier—including one already removed—is reported by the repository
// as the module's not-found rather than failing (FR-012).
func (s *Service) DeleteCategory(ctx context.Context, in dto.AdminCategoryRefInput) error {
	if err := s.Categories.Delete(ctx, in.ID); err != nil {
		return err
	}
	s.recordCategory(ctx, constant.AuditCategoryDeleted, &model.Category{ID: in.ID})
	return nil
}

// ReorderCategories replaces the catalogue-wide category order with the
// complete, ordered set the request carries (specs/018-catalog-reorder FR-001,
// FR-004).
//
// The refusals are checked in the order research D2 fixes, so each maps to the
// code the client can act on: an empty array is a request-shape problem
// (VALIDATION_ERROR); a repeated identifier answers the dedicated duplicate
// code; the sibling read then distinguishes a missing sibling (VALIDATION_ERROR)
// from an identifier that names no category (CATEGORY_NOT_FOUND). Only once the
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
// never from the input (FR-014).
func (s *Service) ReorderCategories(ctx context.Context, in dto.ReorderCategoriesInput) (dto.ReorderResult, error) {
	if len(in.CategoryIDs) == 0 {
		return dto.ReorderResult{}, domainerr.InvalidCategoryField(fieldCategoryIDs, "must not be empty")
	}
	if hasDuplicateIDs(in.CategoryIDs) {
		return dto.ReorderResult{}, domainerr.ErrCategoryReorderDuplicateID
	}

	reorder := func(txCtx context.Context) (dto.ReorderResult, error) {
		siblings, err := s.Categories.ListIDs(txCtx)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		existing := idSet(siblings)
		for _, id := range in.CategoryIDs {
			if !existing[id] {
				return dto.ReorderResult{}, domainerr.ErrCategoryNotFound
			}
		}
		if len(in.CategoryIDs) != len(siblings) {
			return dto.ReorderResult{}, domainerr.InvalidCategoryField(fieldCategoryIDs, "must name every category")
		}

		updated, err := s.Categories.Reorder(txCtx, in.CategoryIDs)
		if err != nil {
			return dto.ReorderResult{}, err
		}
		if updated != int64(len(in.CategoryIDs)) {
			return dto.ReorderResult{}, domainerr.ErrCategoryNotFound
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

	s.recordCategoryReordered(ctx, in.CategoryIDs)
	return result, nil
}

// recordCategoryReordered emits the one audit event of a bulk reorder. A bulk
// write has no single target row, so the target names the ordered scope (the
// category list) with an empty identifier and the affected identifiers travel in
// the metadata (research D9). Emission never fails the business operation.
func (s *Service) recordCategoryReordered(ctx context.Context, ids []uuid.UUID) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditCategoryReordered, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeCategory, "",
		map[string]any{"count": len(ids), "ids": idStrings(ids)},
	)
}

// hasDuplicateIDs reports whether the array names the same identifier twice, so
// the use case can answer the dedicated duplicate code before any read or write
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

// idSet turns an identifier slice into a membership set, so the completeness
// check is a map lookup rather than a nested scan.
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

// recordCategory emits one administrative audit event for a category transition.
//
// The event always names the acting administrator when the call has a session,
// and the category identifier as the target. The metadata carries the operator's
// own name and slug for a human reading the trail; the folding keys are
// deliberately absent, so an audit entry can never leak the normalised values the
// contract never exposes (FR-013, Constitution VI).
//
// Emission never fails the business operation: the shared writer queues and
// reports a dropped event through the logger. An absent auditor is tolerated so a
// read-only construction of the service cannot panic on a write it never serves.
func (s *Service) recordCategory(ctx context.Context, action string, category *model.Category) {
	if s.Audit == nil {
		return
	}
	// A category read for a removal carries only its identifier, so the metadata
	// is built only when there is something to say.
	var metadata map[string]any
	if category.Name != "" || category.Slug != "" {
		metadata = map[string]any{"name": category.Name, "slug": category.Slug}
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeCategory, category.ID.String(), metadata,
	)
}

// recordCategoryHidden emits the hide event with the cart-cleanup count as
// metadata, so the trail records how many cart lines the hide removed without
// a new audit action (specs/016-hide-guard research D8). Emission never fails
// the business operation: it runs after the transaction committed.
func (s *Service) recordCategoryHidden(ctx context.Context, category *model.Category, removedCartLines int64) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditCategoryHidden, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeCategory, category.ID.String(),
		map[string]any{"name": category.Name, "slug": category.Slug, "removedCartLines": removedCartLines},
	)
}

// auditActor returns the acting account of the request for the audit row.
//
// It reports nil when the call is not serving a request. A missing actor is a
// fact worth recording rather than a reason to skip the event, so the row is
// written either way.
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
