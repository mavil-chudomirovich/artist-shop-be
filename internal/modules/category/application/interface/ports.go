// Package appinterface declares the category module's use-case interface and the
// ports the application depends on: the repository, the auditor and the Actor
// the session supplies.
package appinterface

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
)

// CategoryRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type CategoryRepository = repository.CategoryRepository

// Auditor records the module's administrative mutations: every create, edit,
// display change and removal, naming the category and the acting administrator
// (FR-013, Constitution VI).
type Auditor interface {
	// Record emits one audit event. Emission never blocks the business
	// operation and never fails it: the shared writer queues, retries and
	// reports a dropped event through the logger instead.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// CategoryService is the module's use-case surface, covering both audiences.
//
// The public methods answer a visitor and never reveal that a withheld category
// exists (FR-005, FR-007). The administrator methods are reached only behind the
// administrator role guard, and take the acting account from the context the
// session filled (ActorFromContext), never from the request (FR-014).
type CategoryService interface {
	// ListPublic returns one page of the categories on display, in the
	// configured order (FR-001, FR-002, FR-003).
	ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicCategoryPage, error)
	// GetPublicBySlug returns one category on display, addressed by its slug. A
	// withheld, removed or unknown slug answers the same not-found (FR-005).
	GetPublicBySlug(ctx context.Context, in dto.PublicCategoryRefInput) (dto.PublicCategoryOutput, error)

	// ListAdmin returns one page of every category, including the ones not on
	// display (FR-009).
	ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminCategoryPage, error)
	// GetAdmin returns one category by identifier, including one not on display
	// (FR-009).
	GetAdmin(ctx context.Context, in dto.AdminCategoryRefInput) (dto.AdminCategoryOutput, error)
	// CreateCategory creates a category on display and audits the change
	// (FR-008, FR-013).
	CreateCategory(ctx context.Context, in dto.CreateCategoryInput) (dto.AdminCategoryOutput, error)
	// UpdateCategory applies a partial edit and audits the change (FR-010,
	// FR-011, FR-013).
	UpdateCategory(ctx context.Context, in dto.UpdateCategoryInput) (dto.AdminCategoryOutput, error)
	// DeleteCategory removes a category and audits the removal (FR-012,
	// FR-013).
	DeleteCategory(ctx context.Context, in dto.AdminCategoryRefInput) error
}
