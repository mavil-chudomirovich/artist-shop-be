// Package appinterface declares the product module's use-case interface and the
// ports the application depends on: the repository, the auditor, the shared media
// store, the cross-module visibility contract, the UnitOfWork the picture
// operations need, and the Actor the session supplies.
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

// MediaStore is the shared media port, re-exported here so this module names its
// dependency without declaring a second interface that could drift from the
// shared one (research D2). The port itself lives in internal/share/media.
type MediaStore = media.Store

// MediaReference identifies one stored media asset, as internal/share/media
// defines it. It is re-exported for the same reason as MediaStore.
type MediaReference = media.Reference

// CategoryQuery is the cross-module contract this module asks module 03. It
// reports which categories a customer may see and which category a
// customer-facing slug names; the public reads and the `category` filter pass
// those answers to the repository so the filtering runs in SQL rather than in Go
// (FR-002, FR-006, research D1).
type CategoryQuery = contracts.CategoryQuery

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open one
// themselves (Constitution I).
//
// The picture operations are the module's one genuine use of it: the ten-picture
// ceiling is a count, which no unique index can express, so it is checked while
// the product's row is locked inside this transaction (research D6).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Auditor records the module's administrative mutations: every create, edit,
// picture change, state change and removal, naming the product and the acting
// administrator (FR-014, Constitution VI).
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
// number: the domain is where FR-019 is enforced, and a literal repeated here
// could drift away from it without anything failing. The target width has no
// domain constant — it is an operator setting, not a product rule — so research
// D16's documented default is named here.
const (
	// DefaultPictureMaxBytes is the 2 MB picture ceiling of FR-019, the same one
	// an avatar upload already uses.
	DefaultPictureMaxBytes int64 = model.MaxPictureBytes
	// DefaultPictureTargetWidth is the width the media provider is asked to
	// resize a product picture to when the composition leaves it unset
	// (research D16). A product picture is a primary visual, not a thumbnail.
	DefaultPictureTargetWidth = 1600
)

// Config carries the composition settings the use cases cannot derive
// themselves. It is resolved once in the composition root.
type Config struct {
	// PictureMaxBytes is the hard ceiling for an uploaded picture (FR-019). Zero
	// means DefaultPictureMaxBytes.
	PictureMaxBytes int64
	// PictureTargetWidth is the width the media provider is asked to resize to
	// during the upload; the provider is the system of record for the resulting
	// dimensions (research D16). Zero means DefaultPictureTargetWidth. Unlike the
	// avatar, no width guard is applied: the product contract declares no bound.
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
	// ListPublic returns one page of the products a customer may see, in the
	// configured order (FR-001 to FR-009).
	ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicProductPage, error)
	// GetPublicBySlug returns one product a customer may see, with its
	// description and every picture, addressed by its slug. A hidden, removed or
	// unknown slug answers the same not-found (FR-005).
	GetPublicBySlug(ctx context.Context, in dto.PublicProductRefInput) (dto.PublicProductDetailOutput, error)

	// ListAdmin returns one page of every product, including the ones withheld
	// from customers (FR-011).
	ListAdmin(ctx context.Context, in dto.ListAdminInput) (dto.AdminProductPage, error)
	// GetAdmin returns one product by identifier, including one not visible to
	// customers, with its pictures and, for a set, its members (FR-011).
	GetAdmin(ctx context.Context, in dto.AdminProductRefInput) (dto.AdminProductDetailOutput, error)
	// CreateProduct creates a product in COMING_SOON and audits the change
	// (FR-010, FR-014).
	CreateProduct(ctx context.Context, in dto.CreateProductInput) (dto.AdminProductDetailOutput, error)
	// UpdateProduct applies a partial edit and audits the change (FR-012,
	// FR-014). The sell state is never changed here; the transition endpoint
	// owns it (research D11).
	UpdateProduct(ctx context.Context, in dto.UpdateProductInput) (dto.AdminProductDetailOutput, error)
	// DeleteProduct removes a product and audits the removal (FR-013, FR-014).
	DeleteProduct(ctx context.Context, in dto.AdminProductRefInput) error
	// ChangeSellState routes a requested state through the domain's transition
	// table and audits the change (FR-022 to FR-026, research D8).
	ChangeSellState(ctx context.Context, in dto.ChangeSellStateInput) (dto.AdminProductDetailOutput, error)

	// AddPicture validates the upload, stores it through the MediaStore and
	// audits the change. The first picture becomes the main one (FR-016 to
	// FR-020).
	AddPicture(ctx context.Context, in dto.AddPictureInput) (dto.AdminProductDetailOutput, error)
	// RemovePicture removes a picture, promotes the next main one when needed,
	// releases the stored asset and audits the change (FR-021, research D7, D14).
	RemovePicture(ctx context.Context, in dto.RemovePictureInput) error
	// SetPrimaryPicture makes one picture the product's main one and audits the
	// change (FR-017, research D7).
	SetPrimaryPicture(ctx context.Context, in dto.SetPrimaryPictureInput) (dto.AdminProductDetailOutput, error)
}
