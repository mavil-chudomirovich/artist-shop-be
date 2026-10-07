// Package repository declares the product module's repository port. The concrete
// implementation (infrastructure/implement/postgres) embeds the generic
// share/repository.Base and satisfies this interface.
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port. The picture operations are the reason
// this module needs one at all — the ten-picture ceiling is counted while the
// caller holds the product's row lock (research D6) — but the lock is a statement
// the adapter runs inside the caller's transaction, never a transaction of its
// own.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// ProductView is one product together with its pictures, in display order. It is
// the shape both detail reads answer with: the public read carries the product
// and its pictures, and the administrator read adds the same product's members by
// a separate call, so a customer-facing view can never carry a set's contents by
// accident (research D18).
type ProductView struct {
	// Product is the product the row carries.
	Product model.Product
	// Pictures holds the product's pictures in display order. A product with no
	// picture carries a nil slice, which is a valid product (research D7).
	Pictures []model.Picture
}

// ProductListItem is one row of a product list: the product plus what a list
// shows about its pictures without a follow-up query.
//
// Both list paths project the same columns so there is one scan shape. The public
// list uses only ImageURL; the administrator list uses ImageURL and
// PictureCount. The mapper — the single place a model becomes a DTO — is what
// keeps an administrator value out of a customer response (FR-008).
type ProductListItem struct {
	// Product is the product the row carries.
	Product model.Product
	// PictureCount is how many pictures the product has. It is read by the
	// administrator list (FR-011) and ignored by the public list.
	PictureCount int
	// ImageURL is the main picture's link, or "" when the product has none. A
	// list entry carries a picture so the list is usable without a follow-up
	// request (FR-004).
	ImageURL string
}

// SetMember is one product inside a combo set, as the administrator detail read
// projects it: the identifier, the name and the slug are all a set listing needs
// (FR-039, research D18).
type SetMember struct {
	// ID is the member product's stable identifier.
	ID uuid.UUID
	// Name is the member product's name.
	Name string
	// Slug is the member product's public link segment.
	Slug string
}

// VisibleListQuery describes one page of the public catalogue.
//
// The visibility predicate travels with the query — the caller passes the set
// module 03 says a customer may see — because it has to run in SQL, not in Go:
// dropping hidden rows after the page is fetched would return a short page and a
// total that counts rows the caller never received (FR-006, research D1).
type VisibleListQuery struct {
	// VisibleCategoryIDs is the set of categories a customer may see, as
	// internal/contracts.CategoryQuery reports it. An empty set answers an
	// empty page, never an error (FR-007).
	VisibleCategoryIDs []uuid.UUID
	// CategoryID narrows the page to one category. It is nil when the caller
	// asked for the whole catalogue; the caller resolves the request's `category`
	// filter to this identifier.
	CategoryID *uuid.UUID
	// Page is 1-based.
	Page int
	// PageSize is the number of products per page.
	PageSize int
}

// ProductRepository persists products, their pictures and their set membership.
//
// It does not embed share/repository.Repository[model.Product, uuid.UUID]: the
// generic FindAll cannot express the public visibility predicate, the generic
// Update cannot classify a slug collision or a missing category into the
// sentinels the use cases answer with, and the picture and membership operations
// are not entity CRUD at all. Every method is stated explicitly, and the adapter
// embeds share/repository.Base only for the write plumbing, with an explicit
// Columns projection.
type ProductRepository interface {
	// Create inserts a new product.
	//
	// A slug another product already holds is reported as
	// domainerr.ErrProductSlugTaken, so the use case can answer
	// PRODUCT_SLUG_TAKEN (FR-028). A category identifier no row carries is
	// reported as domainerr.ErrProductInvalid naming model.FieldCategoryID, so
	// the operator is told which field to fix rather than shown a server failure
	// (FR-033). Any other storage failure is reported as itself.
	Create(ctx context.Context, product *model.Product) error

	// Update writes the editable columns of the product named by its identifier,
	// recomputing the folding key from the slug it is about to store.
	//
	// A collision with a *different* product is reported as
	// domainerr.ErrProductSlugTaken; writing the slug a product already holds
	// succeeds, because the unique index excludes the row being updated, so a
	// product is never a duplicate of itself (FR-028). A category identifier no
	// row carries is reported as domainerr.ErrProductInvalid naming
	// model.FieldCategoryID (FR-033). The identifier is never written, so no code
	// path can rewrite it after creation (FR-015). An unknown identifier is
	// reported as domainerr.ErrProductNotFound.
	Update(ctx context.Context, product *model.Product) error

	// Delete removes the row. Removal is a hard delete (research D13); the audit
	// entry is what survives it, and the product's pictures and membership rows
	// are removed with it through the cascading foreign keys. An unknown
	// identifier is reported as domainerr.ErrProductNotFound.
	Delete(ctx context.Context, id uuid.UUID) error

	// ListVisible returns a page of the products a customer may see, plus the
	// total number of them.
	//
	// The whole predicate — (sell_state = 'ACTIVE' OR is_preorder) AND
	// category_id = ANY($n) — runs in this query, so pagination and the total
	// stay correct (FR-002, FR-006, research D1). The order is position, then
	// created_at, then id, ascending and identical across identical requests
	// (FR-009).
	ListVisible(ctx context.Context, query VisibleListQuery) ([]ProductListItem, int64, error)

	// FindVisibleBySlug returns one product a customer may see, with its pictures,
	// addressing it by the slug a customer-facing link is built from.
	//
	// The same visibility predicate as ListVisible runs here, so a product that is
	// not on sale and not a pre-order, retired, in a hidden category, removed or
	// unknown is reported the same way, as domainerr.ErrProductNotFound, and the
	// response never confirms that a hidden product exists (FR-003, research D1).
	FindVisibleBySlug(ctx context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*ProductView, error)

	// ListAll returns a page of every product, including the ones withheld from
	// customers, plus the total number of them, in the same FR-009 order. It
	// serves the administrator list (FR-011).
	ListAll(ctx context.Context, page, pageSize int) ([]ProductListItem, int64, error)

	// FindByID returns one product with its pictures for the administrator
	// surface, including one withheld from customers. An unknown identifier is
	// reported as domainerr.ErrProductNotFound (FR-011).
	FindByID(ctx context.Context, id uuid.UUID) (*ProductView, error)

	// LockProduct takes the product's row lock inside the caller's transaction,
	// so the picture count that follows cannot be read by two concurrent uploads
	// as the same value (research D6). An unknown identifier is reported as
	// domainerr.ErrProductNotFound.
	//
	// It joins the transaction the application put in the context and never opens
	// one itself; a caller that invokes it outside a transaction is not protected
	// from the race.
	LockProduct(ctx context.Context, id uuid.UUID) error

	// CountPictures reports how many pictures the product has. The caller checks
	// the ten-picture ceiling with it before calling the media provider, so a
	// refused eleventh upload leaves no orphaned asset behind (FR-020).
	CountPictures(ctx context.Context, productID uuid.UUID) (int, error)

	// ListPictures returns the product's pictures in display order. An empty list
	// is valid: a product with no picture is still a product (research D7).
	ListPictures(ctx context.Context, productID uuid.UUID) ([]model.Picture, error)

	// FindPicture returns one picture that belongs to the product, including the
	// stored reference the use case needs to release the asset. A picture that
	// does not belong to the product, or a product that does not exist, answers
	// domainerr.ErrProductNotFound, so the two are indistinguishable (FR-016).
	FindPicture(ctx context.Context, productID, pictureID uuid.UUID) (*model.Picture, error)

	// AddPicture inserts a picture. An unknown product is reported as
	// domainerr.ErrProductNotFound.
	AddPicture(ctx context.Context, picture *model.Picture) error

	// DeletePicture removes one picture that belongs to the product. A picture
	// that does not belong to the product, or a product that does not exist,
	// answers domainerr.ErrProductNotFound.
	DeletePicture(ctx context.Context, productID, pictureID uuid.UUID) error

	// SetPrimaryPicture makes one picture the product's main one, clearing any
	// other in the same statement so the partial unique index is never violated.
	// It is idempotent: naming the picture that is already primary is a no-op,
	// not an error (research D7). A picture that does not belong to the product,
	// or a product that does not exist, answers domainerr.ErrProductNotFound.
	SetPrimaryPicture(ctx context.Context, productID, pictureID uuid.UUID) error

	// ReplaceSetMembers replaces the whole member list of a set, in the order
	// given. It is called inside the same transaction as the product write, so a
	// set is never stored without its members (FR-039). A member identifier no
	// product carries is reported as domainerr.ErrProductInvalid naming
	// model.FieldMemberProductIDs.
	ReplaceSetMembers(ctx context.Context, setProductID uuid.UUID, memberIDs []uuid.UUID) error

	// ListSetMembers returns the products inside a set, in the order they are
	// listed. It serves the administrator detail only: the public shape does not
	// enumerate a set's contents (FR-039, research D18).
	ListSetMembers(ctx context.Context, setProductID uuid.UUID) ([]SetMember, error)
}
