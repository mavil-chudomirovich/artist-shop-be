package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// VariantGroupView is one variant group together with its own pictures and its
// options, in display order. It is the shape both detail reads answer with
// (data-model.md — Group detail, Admin group reads).
type VariantGroupView struct {
	// Group is the group the row carries.
	Group model.VariantGroup
	// Images holds the group's own pictures in display order. A group with no
	// picture carries a nil slice, which is a valid group (FR-018).
	Images []model.VariantGroupImage
	// Options holds the group's options in display order. The administrator read
	// carries every option; the public read carries only the visible ones
	// (FR-002, FR-013).
	Options []GroupOptionItem
}

// VariantGroupListItem is one row of a group list: the group plus what a list
// shows about its pictures without a follow-up query. Both list paths project the
// same columns so there is one scan shape (FR-009, FR-013).
type VariantGroupListItem struct {
	// Group is the group the row carries.
	Group model.VariantGroup
	// ImageCount is how many pictures the group has, read by the administrator
	// list (FR-013).
	ImageCount int
	// ImageURL is the main picture's link, or "" when the group has none
	// (FR-005).
	ImageURL string
}

// GroupOptionItem is one option of a group as a group read projects it.
//
// An option is an ordinary product, so the projection carries the option's own
// label, price and currency, preorder flag, link and primary picture; nothing is
// inherited from the group or its siblings (FR-016, FR-018). Name, Position and
// SellState are read by the administrator variant only and ignored by the public
// one (FR-013).
type GroupOptionItem struct {
	// ID is the option product's stable identifier.
	ID uuid.UUID
	// Name is the option product's name. Read by the administrator variant only
	// (FR-013).
	Name string
	// Slug is the option product's public link segment (FR-007).
	Slug string
	// Label is the option's value in the group's single choice dimension, for
	// example "Flins" (FR-021).
	Label string
	// Price is the option's own price and currency (FR-016).
	Price model.Price
	// ImageURL is the option's main picture's link, or "" when it has none
	// (FR-018).
	ImageURL string
	// Position is the option product's ordering preference. Read by the
	// administrator variant only (research D5).
	Position int
	// SellState is the option product's selling-life state. Read by the
	// administrator variant only (FR-017).
	SellState constant.SellState
	// IsPreorder reports whether the option is an announced pre-order.
	IsPreorder bool
}

// VariantGroupRepository persists variant groups, their own pictures and their
// options.
//
// It does not embed share/repository.Repository[model.VariantGroup, uuid.UUID]:
// the generic Update cannot classify a slug collision or a missing category into
// the sentinels the use cases answer with, the option read is a projection over
// the products table rather than entity CRUD, and the picture operations mirror
// the product ones. Every method is stated explicitly (Constitution I).
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port, and the adapter joins the caller's
// transaction.
type VariantGroupRepository interface {
	// Create inserts a new variant group.
	//
	// A slug another group already holds is reported as
	// domainerr.ErrVariantGroupSlugTaken, so the use case can answer
	// VARIANT_GROUP_SLUG_TAKEN (FR-019). A category identifier no row carries is
	// reported as domainerr.ErrProductInvalid naming model.FieldCategoryID, so
	// the operator is told which field to fix (FR-023). Any other storage failure
	// is reported as itself.
	Create(ctx context.Context, group *model.VariantGroup) error

	// Update writes the editable columns of the group named by its identifier.
	//
	// A collision with a *different* group is reported as
	// domainerr.ErrVariantGroupSlugTaken; writing the slug a group already holds
	// succeeds, because the unique index excludes the row being updated
	// (FR-019). A category identifier no row carries is reported as
	// domainerr.ErrProductInvalid naming model.FieldCategoryID (FR-023). The
	// identifier is never written, so no code path can rewrite it after creation
	// (FR-009). An unknown identifier is reported as
	// domainerr.ErrVariantGroupNotFound.
	Update(ctx context.Context, group *model.VariantGroup) error

	// Delete removes the group row.
	//
	// Removal is refused while options remain: the products_variant_group_fk
	// foreign key is ON DELETE RESTRICT, so the database refuses the delete and
	// the adapter reports it as domainerr.ErrVariantGroupHasOptions (FR-012,
	// FR-025, research D10). On success the group's own pictures cascade away and
	// no product is touched. An unknown identifier is reported as
	// domainerr.ErrVariantGroupNotFound.
	Delete(ctx context.Context, id uuid.UUID) error

	// FindByID returns one group with its pictures and its options, including the
	// options a customer cannot see, so the operator can manage a group that is
	// not currently visible (FR-013). An unknown identifier is reported as
	// domainerr.ErrVariantGroupNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*VariantGroupView, error)

	// FindVisibleGroupBySlug returns one group a customer may see, with its
	// pictures and its visible options.
	//
	// The whole visibility predicate runs here — the group's category is in
	// visibleCategoryIDs AND at least one of its options is visible — so a group
	// that is unknown, hidden by its category, has no visible option or was
	// removed answers the same domainerr.ErrVariantGroupNotFound and the response
	// never confirms that a hidden group exists (FR-006, FR-008, research D7).
	FindVisibleGroupBySlug(ctx context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*VariantGroupView, error)

	// ListAll returns a page of every group, including the ones withheld from
	// customers, plus the total number of them, in the same FR-009 order
	// (position, then created_at, then id). It serves the administrator list
	// (FR-013).
	ListAll(ctx context.Context, page, pageSize int) ([]VariantGroupListItem, int64, error)

	// ListImages returns the group's own pictures in display order. An empty list
	// is valid: a group with no picture is still a group (FR-018).
	ListImages(ctx context.Context, groupID uuid.UUID) ([]model.VariantGroupImage, error)

	// FindImage returns one picture that belongs to the group, including the
	// stored reference the use case needs to release the asset. A picture that
	// does not belong to the group, or a group that does not exist, answers
	// domainerr.ErrVariantGroupNotFound, so the two are indistinguishable
	// (FR-018).
	FindImage(ctx context.Context, groupID, imageID uuid.UUID) (*model.VariantGroupImage, error)

	// AddImage inserts a picture. An unknown group is reported as
	// domainerr.ErrVariantGroupNotFound (FR-018).
	AddImage(ctx context.Context, image *model.VariantGroupImage) error

	// DeleteImage removes one picture that belongs to the group. A picture that
	// does not belong to the group, or a group that does not exist, answers
	// domainerr.ErrVariantGroupNotFound (FR-018).
	DeleteImage(ctx context.Context, groupID, imageID uuid.UUID) error

	// SetPrimaryImage makes one picture the group's main one, clearing any other
	// in the same statement so the partial unique index is never violated. It is
	// idempotent: naming the picture that is already primary is a no-op, not an
	// error (FR-018, research D2). A picture that does not belong to the group,
	// or a group that does not exist, answers domainerr.ErrVariantGroupNotFound.
	SetPrimaryImage(ctx context.Context, groupID, imageID uuid.UUID) error

	// CountImages reports how many pictures the group has. The caller checks the
	// ten-picture ceiling with it before calling the media provider, so a refused
	// eleventh upload leaves no orphaned asset behind (FR-018).
	CountImages(ctx context.Context, groupID uuid.UUID) (int, error)

	// ListOptions returns the group's options in display order (position, then
	// created_at, then id).
	//
	// includeHidden selects the administrator variant (every option, so the
	// operator sees the full group — FR-013) or the public one (visible options
	// only, so "what a customer sees" keeps one definition — FR-002, FR-027).
	// Each option is projected with its own label, price, currency, preorder flag
	// and primary picture; nothing is inherited from the group or its siblings
	// (FR-016, FR-018).
	ListOptions(ctx context.Context, groupID uuid.UUID, includeHidden bool) ([]GroupOptionItem, error)
}
