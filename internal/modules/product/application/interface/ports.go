// Package appinterface declares the product module's use-case interface and the
// ports the application depends on: the product and variant repositories, the
// auditor, the shared media store, the cross-module contracts, the UnitOfWork the
// picture operations need, and the Actor the session supplies.
package appinterface

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// ProductRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type ProductRepository = repository.ProductRepository

// VariantRepository is the variant persistence port the variant use cases depend
// on. It is the domain's contract, aliased here for the same reason as
// ProductRepository: this package names the dependency without declaring a second
// interface that could drift.
type VariantRepository = repository.VariantRepository

// InventoryAvailability is the cross-module contract this module asks module 05:
// the currently available quantity of a set of variants, so the product detail can
// show each variant's stock status without reading module 05's table (FR-003,
// FR-005, research D10, Constitution I).
type InventoryAvailability = contracts.InventoryAvailability

// MediaStore is the shared media port, re-exported here so this module names its
// dependency without declaring a second interface that could drift from the
// shared one. The port itself lives in internal/share/media.
type MediaStore = media.Store

// MediaReference identifies one stored media asset, as internal/share/media
// defines it. It is re-exported for the same reason as MediaStore.
type MediaReference = media.Reference

// CategoryQuery is the cross-module contract this module asks module 03. It
// reports which categories a customer may see and which category a
// customer-facing slug names; the public reads and the `category` filter pass
// those answers to the repository so the filtering runs in SQL rather than in Go
// (FR-004, FR-006, research D1).
type CategoryQuery = contracts.CategoryQuery

// OrderHideGuard is the cross-module contract this module asks module 07: which
// PENDING or PAYMENT_PENDING orders hold given variants, so a move away from
// on-sale can be refused while one is pending without reading module 07's
// tables (specs/016-hide-guard research D1, Constitution I).
type OrderHideGuard = contracts.OrderHideGuard

// CartHideCleanup is the cross-module contract this module asks module 06:
// remove the hidden variants from every cart inside the same transaction as
// the hide (specs/016-hide-guard research D4, Constitution I).
type CartHideCleanup = contracts.CartHideCleanup

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open one
// themselves (Constitution I).
//
// The picture operations are the module's one genuine use of it: the ten-picture
// ceiling is a count, which no unique index can express, so it is checked while
// the product's row is locked inside this transaction. The guarded variant hide
// is the second: the pending-order check, the state change and the cart cleanup
// commit together or not at all (specs/016-hide-guard research D3).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Auditor records the module's administrative mutations: every product or variant
// create, edit, picture change, state change and removal, naming the target and
// the acting administrator (FR-014, Constitution VI).
type Auditor interface {
	// Record emits one audit event. Emission never blocks the business operation
	// and never fails it: the shared writer queues, retries and reports a dropped
	// event through the logger instead.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// Picture defaults used when the composition leaves the Config values at zero, so
// a zero value still enforces the documented contract instead of accepting any
// upload or storing an unresized image.
//
// The size ceiling is the domain's own constant rather than a second copy of the
// number: the domain is where the upload rule is enforced, and a literal repeated
// here could drift away from it without anything failing. The target width has no
// domain constant — it is an operator setting, not a product rule.
const (
	// DefaultPictureMaxBytes is the 2 MB picture ceiling, the same one an avatar
	// upload already uses.
	DefaultPictureMaxBytes int64 = model.MaxPictureBytes
	// DefaultPictureTargetWidth is the width the media provider is asked to
	// resize a product or variant picture to when the composition leaves it
	// unset. A product picture is a primary visual, not a thumbnail.
	DefaultPictureTargetWidth = 1600
)

// Config carries the composition settings the use cases cannot derive
// themselves. It is resolved once in the composition root.
type Config struct {
	// PictureMaxBytes is the hard ceiling for an uploaded picture. Zero means
	// DefaultPictureMaxBytes.
	PictureMaxBytes int64
	// PictureTargetWidth is the width the media provider is asked to resize to
	// during the upload; the provider is the system of record for the resulting
	// dimensions. Zero means DefaultPictureTargetWidth.
	PictureTargetWidth int
}

// PictureMaxBytesOrDefault returns the configured ceiling, or the documented
// default when the composition left it unset.
func (c Config) PictureMaxBytesOrDefault() int64 {
	if c.PictureMaxBytes <= 0 {
		return DefaultPictureMaxBytes
	}
	return c.PictureMaxBytes
}

// PictureTargetWidthOrDefault returns the configured target width, or the
// documented default when the composition left it unset.
func (c Config) PictureTargetWidthOrDefault() int {
	if c.PictureTargetWidth <= 0 {
		return DefaultPictureTargetWidth
	}
	return c.PictureTargetWidth
}

// ProductService is the module's use-case surface, covering both audiences.
//
// The public methods answer a visitor and never reveal that a withheld product
// exists (FR-003, FR-008). The administrator methods are reached only behind the
// administrator role guard, and take the acting account from the context the
// session filled (ActorFromContext), never from the request (FR-015).
type ProductService interface {
	// ListPublic returns one page of the public catalogue — one entry per product
	// with the lowest on-sale variant price and the default variant's image — in
	// the configured order (FR-001, FR-002).
	ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicProductPage, error)
	// GetPublicByCode returns one product a customer may see: its shared content
	// and gallery plus its visible variants with their stock status, addressed by
	// its code. A hidden, removed or unknown code answers the same not-found
	// (FR-003, FR-005, FR-008).
	GetPublicByCode(ctx context.Context, in dto.PublicProductRefInput) (dto.PublicProductDetailOutput, error)

	// ListAdmin returns one page of every product, including the ones withheld
	// from customers (FR-010).
	ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminProductPage, error)
	// GetAdmin returns one product by identifier, including one not visible to
	// customers, with its gallery and every variant (FR-010).
	GetAdmin(ctx context.Context, in dto.AdminProductRefInput) (dto.AdminProductDetailOutput, error)
	// CreateProduct creates a product together with at least one variant and
	// audits the change (FR-010, FR-012, FR-014).
	CreateProduct(ctx context.Context, in dto.CreateProductInput) (dto.AdminProductDetailOutput, error)
	// UpdateProduct applies a partial edit and audits the change (FR-010,
	// FR-014). The variants are managed through their own operations.
	UpdateProduct(ctx context.Context, in dto.UpdateProductInput) (dto.AdminProductDetailOutput, error)
	// DeleteProduct removes a product and its variants and audits the removal
	// (FR-010, FR-014).
	DeleteProduct(ctx context.Context, in dto.AdminProductRefInput) error

	// AddVariant adds a variant to a product and audits the change (FR-014).
	AddVariant(ctx context.Context, in dto.AddVariantInput) (dto.AdminProductDetailOutput, error)
	// UpdateVariant applies a partial variant edit and audits the change (FR-014).
	UpdateVariant(ctx context.Context, in dto.UpdateVariantInput) (dto.AdminProductDetailOutput, error)
	// RemoveVariant removes a variant, refusing the product's last variant, and
	// audits the removal (FR-012, FR-014).
	RemoveVariant(ctx context.Context, in dto.VariantRefInput) error
	// ChangeVariantState routes a requested variant state through the domain's
	// transition table and audits the change (FR-015).
	ChangeVariantState(ctx context.Context, in dto.ChangeVariantStateInput) (dto.AdminProductDetailOutput, error)

	// MarkVariantOutOfStock is the system-facing signal the inventory module sends
	// when a variant's availability reaches zero. It moves ACTIVE → OUT_OF_STOCK
	// through the entity's own transition, leaves announced and retired variants
	// untouched, and audits the change with no human actor (FR-015, research D4).
	MarkVariantOutOfStock(ctx context.Context, variantID uuid.UUID) error
	// MarkVariantOnSale is the system-facing signal the inventory module sends
	// when a variant's availability rises from zero. It moves OUT_OF_STOCK →
	// ACTIVE through the entity's own transition and leaves announced and retired
	// variants untouched (FR-015, research D4).
	MarkVariantOnSale(ctx context.Context, variantID uuid.UUID) error

	// AddVariantImage validates the upload, stores it through the MediaStore and
	// audits the change. Uploading again replaces the variant's single image
	// (FR-016).
	AddVariantImage(ctx context.Context, in dto.AddVariantImageInput) (dto.AdminProductDetailOutput, error)
	// RemoveVariantImage clears the variant's image, releases the stored asset and
	// audits the change (FR-016).
	RemoveVariantImage(ctx context.Context, in dto.VariantRefInput) (dto.AdminProductDetailOutput, error)

	// AddPicture validates a gallery upload, stores it through the MediaStore and
	// audits the change. The first picture becomes the main one (FR-016).
	AddPicture(ctx context.Context, in dto.AddPictureInput) (dto.AdminProductDetailOutput, error)
	// RemovePicture removes a gallery picture, promotes the next main one when
	// needed, releases the stored asset and audits the change (FR-021).
	RemovePicture(ctx context.Context, in dto.RemovePictureInput) error
	// SetPrimaryPicture makes one gallery picture the product's main one and
	// audits the change.
	SetPrimaryPicture(ctx context.Context, in dto.SetPrimaryPictureInput) (dto.AdminProductDetailOutput, error)
}
