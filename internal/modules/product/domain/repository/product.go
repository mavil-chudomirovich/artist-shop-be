package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// ProductView is one product together with its pictures and its variants, in
// display order. It is the shape both detail reads answer with: the public read
// carries the product, its gallery and its visible variants, and the administrator
// read carries the same product with every variant (FR-003, FR-010).
type ProductView struct {
	// Product is the product the row carries.
	Product model.Product
	// Pictures holds the product's gallery in display order. A product with no
	// picture carries a nil slice, which is a valid product.
	Pictures []model.Picture
	// Variants holds the product's variants in display order. The public read
	// carries only the on-sale and out-of-stock ones (FR-005); the administrator
	// read carries every one (FR-010). The first variant is the default variant.
	Variants []model.Variant
	// Summary is the card figure the product detail shows: the lowest on-sale
	// variant price and the default variant's image (FR-002, FR-003).
	Summary ProductSummary
}

// ProductSummary is the card figure of a catalogue entry or product detail: the
// lowest effective price among the product's on-sale variants and its card image,
// which is the default variant's image falling back to the product's primary
// gallery picture (FR-002, FR-003).
type ProductSummary struct {
	// Price is the lowest effective price among the product's on-sale variants, in
	// the product's currency. A product with no on-sale variant has no catalogue
	// presence, so the caller reads this only for a visible product.
	Price model.Price
	// ImageURL is the default variant's image link, else the product's primary
	// gallery picture, else "" when neither exists (FR-002).
	ImageURL string
}

// ProductListItem is one row of the administrator product list: the product plus
// what a list shows about its pictures and variants without a follow-up query.
type ProductListItem struct {
	// Product is the product the row carries.
	Product model.Product
	// PictureCount is how many pictures the product has.
	PictureCount int
	// VariantCount is how many variants the product has.
	VariantCount int
	// ImageURL is the main picture's link, or "" when the product has none. A list
	// entry carries a picture so the list is usable without a follow-up request.
	ImageURL string
}

// CatalogueEntry is one entry of the public catalogue: exactly one per product, with
// the product's variants collapsed into it (FR-001).
//
// Price is the lowest effective price among the product's on-sale variants, so an
// out-of-stock variant's cheaper price never headlines a product (FR-002). ImageURL
// is the default variant's image falling back to the product's primary gallery
// picture (FR-002). The entry no longer carries a type discriminator: there is one
// kind of entry again (research D9).
type CatalogueEntry struct {
	// ID is the product's stable identifier.
	ID uuid.UUID
	// Name is the product's name.
	Name string
	// Slug is the product's public link segment.
	Slug string
	// Price is the lowest on-sale variant price (FR-002).
	Price model.Price
	// ImageURL is the card image link, or "" when none exists (FR-002).
	ImageURL string
	// Position is the ordering preference, an internal projection (FR-001).
	Position int
	// CreatedAt is when the product was created, an internal projection used to
	// break an ordering tie (FR-001).
	CreatedAt time.Time
}

// VisibleListQuery describes one page of the public catalogue.
//
// The visibility predicate travels with the query — the caller passes the set
// module 03 says a customer may see — because it has to run in SQL, not in Go:
// dropping hidden rows after the page is fetched would return a short page and a
// total that counts rows the caller never received (FR-004).
type VisibleListQuery struct {
	// VisibleCategoryIDs is the set of categories a customer may see, as
	// internal/contracts.CategoryQuery reports it. An empty set answers an empty
	// page, never an error.
	VisibleCategoryIDs []uuid.UUID
	// CategoryID narrows the page to one category. It is nil when the caller asked
	// for the whole catalogue; the caller resolves the request's `category` filter
	// to this identifier.
	CategoryID *uuid.UUID
	// Page is 1-based.
	Page int
	// PageSize is the number of products per page.
	PageSize int
}

// ProductRepository persists products and their pictures, and serves the
// variant-aware product reads.
//
// It does not embed share/repository.Repository[model.Product, uuid.UUID]: the
// generic FindAll cannot express the public visibility predicate, the generic Update
// cannot classify a slug/code collision or a missing category into the sentinels the
// use cases answer with, and the picture and catalogue operations are not entity
// CRUD at all. Every method is stated explicitly, and the adapter embeds
// share/repository.Base only for the write plumbing, with an explicit Columns
// projection.
type ProductRepository interface {
	// Create inserts a new product.
	//
	// A slug another product already holds is reported as
	// domainerr.ErrProductSlugTaken, and a code another product already holds as
	// domainerr.ErrProductCodeTaken, so the use case can answer
	// PRODUCT_SLUG_TAKEN / PRODUCT_CODE_TAKEN (FR-011). A category identifier no row
	// carries is reported as domainerr.ErrProductInvalid naming model.FieldCategoryID.
	// Any other storage failure is reported as itself.
	Create(ctx context.Context, product *model.Product) error

	// Update writes the editable columns of the product named by its identifier,
	// recomputing the folding key from the slug it is about to store.
	//
	// A collision with a *different* product is reported as
	// domainerr.ErrProductSlugTaken or domainerr.ErrProductCodeTaken; writing the
	// value the product already holds succeeds, because the unique index excludes
	// the row being updated, so a product is never a duplicate of itself (FR-011). A
	// category identifier no row carries is reported as domainerr.ErrProductInvalid
	// naming model.FieldCategoryID. The identifier is never written. An unknown
	// identifier is reported as domainerr.ErrProductNotFound.
	Update(ctx context.Context, product *model.Product) error

	// Delete removes the row. Removal is a hard delete; the audit entry is what
	// survives it, and the product's pictures and variants are removed with it
	// through the cascading foreign keys (FR-012). An unknown identifier is reported
	// as domainerr.ErrProductNotFound.
	Delete(ctx context.Context, id uuid.UUID) error

	// ListCatalogue returns one page of the public catalogue — exactly one entry per
	// product — plus the total number of entries.
	//
	// The whole predicate runs in this single statement: a product is visible when
	// its category is in query.VisibleCategoryIDs and at least one of its variants
	// is on sale (FR-004). The entry's price is the lowest effective price among the
	// product's on-sale variants and its image is the default variant's image falling
	// back to the product's primary gallery picture (FR-001, FR-002). The order is
	// position, then created_at, then id (FR-001), and a COUNT(*) over the same rows
	// gives the total, so pagination and the total stay correct.
	ListCatalogue(ctx context.Context, query VisibleListQuery) ([]CatalogueEntry, int64, error)

	// FindVisibleBySlug returns one product a customer may see, with its pictures and
	// its visible variants, addressing it by the slug a customer-facing link is built
	// from (FR-003).
	//
	// The same visibility predicate as ListCatalogue runs here, so a product that is
	// hidden (no on-sale variant), retired, in a hidden category, removed or unknown
	// is reported the same way, as domainerr.ErrProductNotFound, and the response
	// never confirms that a hidden product exists (FR-008). The variants carried are
	// the on-sale and out-of-stock ones only (FR-005), ordered by position.
	FindVisibleBySlug(ctx context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*ProductView, error)

	// ListAll returns a page of every product, including the ones withheld from
	// customers, plus the total number of them, in the same order (position, then
	// created_at, then id). It serves the administrator list (FR-010).
	ListAll(ctx context.Context, page, pageSize int) ([]ProductListItem, int64, error)

	// FindByID returns one product with its pictures and every variant, for the
	// administrator surface, including a product withheld from customers (FR-010).
	// An unknown identifier is reported as domainerr.ErrProductNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*ProductView, error)

	// Exists reports whether a product carries the identifier, without reading the
	// row. It answers the cross-module lookup a consumer asks: a read or a decrease
	// writes no product row, so the foreign key cannot answer it, and the product
	// module is the only place that can. It joins the caller's transaction.
	Exists(ctx context.Context, id uuid.UUID) (bool, error)

	// FindByIDs returns every requested product that exists, without pictures or
	// variants, in one read. An identifier no product carries is absent from the
	// result rather than an error. It joins the caller's transaction and never opens
	// one itself.
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Product, error)

	// LockProduct takes the product's row lock inside the caller's transaction, so
	// the picture count that follows cannot be read by two concurrent uploads as the
	// same value. An unknown identifier is reported as domainerr.ErrProductNotFound.
	LockProduct(ctx context.Context, id uuid.UUID) error

	// CountPictures reports how many pictures the product has. The caller checks the
	// ten-picture ceiling with it before calling the media provider, so a refused
	// eleventh upload leaves no orphaned asset behind.
	CountPictures(ctx context.Context, productID uuid.UUID) (int, error)

	// ListPictures returns the product's pictures in display order. An empty list is
	// valid: a product with no picture is still a product.
	ListPictures(ctx context.Context, productID uuid.UUID) ([]model.Picture, error)

	// FindPicture returns one picture that belongs to the product, including the
	// stored reference the use case needs to release the asset. A picture that does
	// not belong to the product, or a product that does not exist, answers
	// domainerr.ErrProductNotFound, so the two are indistinguishable.
	FindPicture(ctx context.Context, productID, pictureID uuid.UUID) (*model.Picture, error)

	// AddPicture inserts a picture. An unknown product is reported as
	// domainerr.ErrProductNotFound.
	AddPicture(ctx context.Context, picture *model.Picture) error

	// DeletePicture removes one picture that belongs to the product. A picture that
	// does not belong to the product, or a product that does not exist, answers
	// domainerr.ErrProductNotFound.
	DeletePicture(ctx context.Context, productID, pictureID uuid.UUID) error

	// SetPrimaryPicture makes one picture the product's main one, clearing any other
	// in the same statement so the partial unique index is never violated. It is
	// idempotent: naming the picture that is already primary is a no-op, not an
	// error. A picture that does not belong to the product, or a product that does
	// not exist, answers domainerr.ErrProductNotFound.
	SetPrimaryPicture(ctx context.Context, productID, pictureID uuid.UUID) error
}
