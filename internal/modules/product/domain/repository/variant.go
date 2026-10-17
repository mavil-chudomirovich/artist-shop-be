// Package repository declares the product module's repository ports. The concrete
// implementations (infrastructure/implement/postgres) satisfy these interfaces.
//
// A repository MUST NOT open a transaction: the application layer owns the boundary
// through the UnitOfWork port. Every read/write joins the caller's transaction.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// VariantFacts is the read projection the cross-module variant-facts contract
// answers with (specs/015-product-sku-model contracts; research D6): the variant's
// identity, the product it belongs to, the snapshots the cart and order keep and
// the buyability facts — everything a consumer needs and nothing more. It carries
// no image and no position, which the consumer never shows.
//
// Price is the variant's effective price (its own amount, else the product's
// default), in the product's currency (FR-017, FR-024). OnSale is true exactly when
// the variant's sell state is ACTIVE; a variant whose product flag is off or whose
// category is hidden is absent from the read rather than reported with
// OnSale=false (specs/016-hide-guard FR-006), so a consumer can refuse a
// not-buyable line without re-deriving the rule (FR-007).
type VariantFacts struct {
	// VariantID is the variant's stable identifier.
	VariantID uuid.UUID
	// ProductID is the product the variant belongs to.
	ProductID uuid.UUID
	// ProductName is the product's name at read time, for the cart/order snapshot
	// (FR-019).
	ProductName string
	// ProductCode is the product code at read time (FR-019). It is both the
	// operator-facing code and the product's link segment, so a consumer keeps the
	// product's public link without resolving the product.
	ProductCode string
	// CategoryID is the category the product currently belongs to (FR-022). The
	// facts read always populates it for a visible variant, so the order can
	// snapshot it on the line at checkout/edit.
	CategoryID uuid.UUID
	// CategoryName is the category's current name (FR-022). It is snapshotted on
	// the same order line, so a later rename cannot change a past sale's label.
	CategoryName string
	// VariantName is the variant's display name at read time (FR-019).
	VariantName string
	// SKU is the variant's SKU at read time (FR-019). It is stored on the order
	// line but never shown to a customer.
	SKU string
	// OnSale reports whether the variant is ACTIVE (FR-007).
	OnSale bool
	// Price is the variant's effective price (FR-017).
	Price model.Price
}

// VariantRepository persists a product's variants and reads them back.
//
// It does not embed share/repository.Repository[model.Variant, uuid.UUID]: the
// generic Update cannot classify a SKU collision into the sentinel the use cases
// answer with, the ordered list is a projection over the product, and the facts
// read is a join across two tables. Every method is stated explicitly
// (Constitution I).
type VariantRepository interface {
	// Create inserts a new variant.
	//
	// A SKU another variant already holds is reported as
	// domainerr.ErrVariantSKUTaken, so the use case can answer VARIANT_SKU_TAKEN
	// (FR-011). A product identifier no row carries is reported as
	// domainerr.ErrVariantNotFound, so the operator is told the parent is gone
	// rather than shown a server failure. Any other storage failure is reported as
	// itself.
	Create(ctx context.Context, variant *model.Variant) error

	// Update writes the editable columns of the variant named by its identifier.
	//
	// A collision with a *different* variant is reported as
	// domainerr.ErrVariantSKUTaken; writing the SKU a variant already holds
	// succeeds, because the unique index excludes the row being updated (FR-011).
	// The identifier and the product are never written, so no code path can move a
	// variant to another product. An unknown identifier is reported as
	// domainerr.ErrVariantNotFound.
	Update(ctx context.Context, variant *model.Variant) error

	// Delete removes the variant row. Removal is a hard delete; the audit entry is
	// what survives it. The application refuses removing a product's last variant
	// before calling this (FR-012). An unknown identifier is reported as
	// domainerr.ErrVariantNotFound.
	Delete(ctx context.Context, id uuid.UUID) error

	// FindByID returns one variant, whatever its sell state, for the administrator
	// surface (FR-010). An unknown identifier is reported as
	// domainerr.ErrVariantNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*model.Variant, error)

	// ListByProduct returns the product's variants in display order (position, then
	// created_at, then id). The administrator read carries every variant, so the
	// operator sees the full product (FR-010). A product with no variant carries an
	// empty slice; the application never lets that state be stored (FR-012).
	ListByProduct(ctx context.Context, productID uuid.UUID) ([]model.Variant, error)

	// CountByProduct reports how many variants the product has. The application uses
	// it to refuse removing the last variant (FR-012).
	CountByProduct(ctx context.Context, productID uuid.UUID) (int, error)

	// FindFactsByIDs returns the facts of every requested variant in one read. It
	// is the read behind the cross-module variant-facts contract that the cart and
	// order consume (research D6): a consumer needs the current name, code, variant
	// name, SKU, on-sale state and effective price of a whole set of variants at
	// once, and a per-variant read would make its cost grow with the set. An
	// identifier no variant carries is absent from the result rather than an error,
	// so the caller can tell a removed variant from an available one. It joins the
	// caller's transaction and never opens one itself.
	FindFactsByIDs(ctx context.Context, ids []uuid.UUID) ([]VariantFacts, error)

	// DiscontinuedIDs returns the subset of the given identifiers whose variant
	// is DISCONTINUED, in a stable order. It is the read behind the cross-module
	// ProductVariantStates contract: the admin dashboard drops these variants from
	// its low-stock candidates without reading module 04's table (FR-018,
	// research D9). An empty input answers an empty slice without touching the
	// database. It joins the caller's transaction and never opens one itself.
	DiscontinuedIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}
