package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeCategory is the audit_logs target type of the category events. The
// target of a mutation is the category, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// category without scanning the actor's other events (Constitution VI).
const targetTypeCategory = "category"

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
func (s *Service) ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminCategoryPage, error) {
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
// The acting administrator is taken from the context the session filled, never
// from the input (FR-014); auditActor records it, or nil when the call has no
// session behind it.
func (s *Service) CreateCategory(ctx context.Context, in dto.CreateCategoryInput) (dto.AdminCategoryOutput, error) {
	category, err := model.NewCategory(model.CategoryDraft{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		Position:    in.Position,
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
func (s *Service) UpdateCategory(ctx context.Context, in dto.UpdateCategoryInput) (dto.AdminCategoryOutput, error) {
	category, err := s.Categories.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminCategoryOutput{}, err
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
