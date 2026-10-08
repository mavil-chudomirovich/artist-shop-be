// Package repository declares the category module's repository port. The
// concrete implementation (infrastructure/implement/postgres) embeds the generic
// share/repository.Base and satisfies this interface.
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port. Category use cases are single-statement
// writes, so none currently needs one, but the rule holds regardless.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
)

// CategoryRepository persists categories.
//
// It does not embed share/repository.Repository[model.Category, uuid.UUID]: the
// generic FindAll cannot express "only the categories on display", the generic
// ordering cannot express the FR-003 tie-break, and the generic Update cannot
// classify the two unique-constraint collisions into the sentinels the use cases
// answer with. Every method is therefore stated explicitly and the adapter
// embeds share/repository.Base only for the write plumbing, with an explicit
// Columns projection.
type CategoryRepository interface {
	// Create inserts a new category. It writes both normalised columns, so the
	// unique indexes have a value to enforce.
	//
	// A name or slug another category already holds is reported as
	// domainerr.ErrCategoryNameTaken or domainerr.ErrCategorySlugTaken, so the
	// use case can answer the two different codes (FR-020). Any other storage
	// failure is reported as itself.
	Create(ctx context.Context, category *model.Category) error

	// Update writes the editable columns of the category named by its
	// identifier, including both normalised columns.
	//
	// A collision with a *different* category is reported as
	// domainerr.ErrCategoryNameTaken or domainerr.ErrCategorySlugTaken. Writing
	// the values a category already holds succeeds, because the unique indexes
	// exclude the row being updated: a category is never a duplicate of itself
	// (FR-022).
	Update(ctx context.Context, category *model.Category) error

	// Delete removes the row. Removal is a hard delete (research D5); the audit
	// entry is what survives it. It reports domainerr.ErrCategoryNotFound when
	// no row carries the identifier, and domainerr.ErrCategoryInUse when the
	// category is still referenced by products — the refusal comes from the
	// restricting foreign key added by feature 006 (FR-036).
	Delete(ctx context.Context, id uuid.UUID) error

	// FindByID returns one category for the administrator surface, including a
	// category that is withheld from customers. An unknown identifier is
	// reported as domainerr.ErrCategoryNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*model.Category, error)

	// FindVisibleBySlug returns one category for the public surface, addressing
	// it by the slug a customer-facing link is built from. A category that is
	// withheld, removed or unknown is reported the same way, as
	// domainerr.ErrCategoryNotFound, so the response never confirms that a
	// withheld category exists (FR-005).
	FindVisibleBySlug(ctx context.Context, slug string) (*model.Category, error)

	// ListVisible returns a page of the categories on display, plus the total
	// number of them. The order is the FR-003 order — position, then created_at,
	// then id — and is identical across identical requests.
	ListVisible(ctx context.Context, page, pageSize int) ([]model.Category, int64, error)

	// ListAll returns a page of every category, including the ones not on
	// display, plus the total number of them, in the same FR-003 order. It
	// serves the administrator list (FR-009).
	ListAll(ctx context.Context, page, pageSize int) ([]model.Category, int64, error)

	// VisibleIDs returns the identifiers of every category on display. It is
	// the read behind the cross-module query contract
	// (internal/contracts.CategoryQuery): the whole set is what lets another
	// module filter its own catalogue in SQL without reading this module's table
	// (research D1). The order is deterministic but is not part of the contract,
	// because the caller uses the values as a set.
	VisibleIDs(ctx context.Context) ([]uuid.UUID, error)

	// IDBySlug resolves the category a slug names, without regard to its display
	// state. It is the read behind the cross-module query contract's
	// CategoryIDBySlug: another module addresses a customer-facing category link
	// by its slug and must not read this module's table to resolve it (research
	// D1). The second result reports whether a row carries the slug; an unknown
	// slug answers (uuid.Nil, false, nil) rather than a not-found error, so the
	// caller can answer the public filter directly. A hidden category still
	// resolves: the caller's visibility predicate excludes it afterwards.
	IDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error)
}
