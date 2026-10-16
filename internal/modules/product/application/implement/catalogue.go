package implement

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Pagination bounds of the two product listings. They mirror the handler's, so a
// window that reaches a use case from any other entry point is bounded the same
// way instead of asking the database for an unbounded result.
const (
	// defaultPageSize is the window used when none is requested.
	defaultPageSize = 20
	// maxPageSize is the largest window a listing will return.
	maxPageSize = 100
)

// Service implements the product module's use cases.
//
// The public browse methods answer a visitor and never reveal that a withheld
// product exists (FR-003, FR-008). The administrator methods live in the other
// files of this package and are reached only behind the administrator role guard.
type Service struct {
	// Products persists products and reads the catalogue. It never opens a
	// transaction; the application layer owns every boundary (Constitution I).
	Products appinterface.ProductRepository
	// Variants persists a product's variants and reads them back. It is the
	// module's second repository, supplied at the composition root beside
	// Products.
	Variants appinterface.VariantRepository
	// Visibility answers the two facts about categories this module cannot read
	// itself: which categories a customer may see, and which category a
	// customer-facing slug names. It is the cross-module contract supplied at the
	// composition root.
	Visibility appinterface.CategoryQuery
	// Availability answers the per-variant stock status the product detail shows.
	// It is the inventory module's read contract, supplied at the composition
	// root, so the product surface never reads module 05's table (research D10,
	// Constitution I).
	Availability appinterface.InventoryAvailability
	// Audit records every administrative mutation (FR-014, Constitution VI). It
	// is unused by the public browse use cases, but the composition root always
	// supplies the module's auditor adapter.
	Audit appinterface.Auditor
	// Media stores pictures outside the service through the shared media port
	// (FR-016). It is reached only by the picture use cases, which is what keeps
	// a provider outage from failing a product read or edit.
	Media appinterface.MediaStore
	// Tx owns the create operation's transaction boundaries and the gallery
	// ceiling's checks. The ten-picture ceiling is a count no unique index can
	// express, so it is checked while the product's row is locked inside this
	// transaction.
	Tx appinterface.UnitOfWork
	// Config carries the picture upload ceilings the composition resolved. A
	// zero value falls back to the documented contract defaults.
	Config appinterface.Config
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the product use-case service.
func New(service Service) *Service { return &service }

// ListPublic returns one page of the customer-facing catalogue — one entry per
// product, with the product's on-sale variants collapsed into a card price and
// image — plus the total (FR-001, FR-002).
//
// The visible set is fetched from the cross-module contract exactly once per
// request and passed to the repository, so the whole visibility predicate (a
// product is visible only while its category is visible and at least one variant
// is on sale) runs in SQL rather than in Go: dropping hidden rows after the page
// is fetched would return a short page and a total that counts rows the caller
// never received (FR-004, research D1).
//
// The optional `category` filter is a customer-facing slug, so it is resolved
// through the same contract. A slug no category holds answers the empty page
// directly; a *hidden* slug resolves to its identifier and is then excluded by
// the visibility predicate anyway, so the two paths agree without a special case
// (FR-006).
func (s *Service) ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicProductPage, error) {
	page, pageSize := listWindow(in.Page, in.PageSize)

	visible, err := s.Visibility.VisibleCategoryIDs(ctx)
	if err != nil {
		return dto.PublicProductPage{}, err
	}

	var categoryID *uuid.UUID
	if slug := strings.TrimSpace(in.CategorySlug); slug != "" {
		id, found, err := s.Visibility.CategoryIDBySlug(ctx, slug)
		if err != nil {
			return dto.PublicProductPage{}, err
		}
		if !found {
			return emptyCataloguePage(page, pageSize), nil
		}
		categoryID = &id
	}

	items, total, err := s.Products.ListCatalogue(ctx, domainrepo.VisibleListQuery{
		VisibleCategoryIDs: visible,
		CategoryID:         categoryID,
		Page:               page,
		PageSize:           pageSize,
	})
	if err != nil {
		return dto.PublicProductPage{}, err
	}
	return dto.PublicProductPage{
		Products: s.Mapper.PublicProducts(items),
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// GetPublicBySlug returns one visible product with its shared content, gallery and
// its visible variants — each with its effective price and stock status —
// addressed by the slug a customer-facing link is built from (FR-003, FR-005).
//
// The same visible set drives the detail read, so a product with no on-sale
// variant, retired, removed, unknown or in a hidden category answers the same
// not-found and the response never confirms that a hidden product exists
// (FR-008). The per-variant stock status costs one bulk inventory read however
// many variants the product lists (research D10).
func (s *Service) GetPublicBySlug(ctx context.Context, in dto.PublicProductRefInput) (dto.PublicProductDetailOutput, error) {
	visible, err := s.Visibility.VisibleCategoryIDs(ctx)
	if err != nil {
		return dto.PublicProductDetailOutput{}, err
	}
	view, err := s.Products.FindVisibleBySlug(ctx, in.Slug, visible)
	if err != nil {
		return dto.PublicProductDetailOutput{}, err
	}
	available, err := s.variantAvailability(ctx, variantIDs(view.Variants))
	if err != nil {
		return dto.PublicProductDetailOutput{}, err
	}
	return s.Mapper.PublicProductDetail(*view, available), nil
}

// variantAvailability reads the currently available quantity of every requested
// variant through the inventory contract, keyed by identifier. An empty request
// answers an empty map without calling the contract.
func (s *Service) variantAvailability(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := make(map[uuid.UUID]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	availabilities, err := s.Availability.AvailableQuantity(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, item := range availabilities {
		out[item.VariantID] = item.Available
	}
	return out, nil
}

// emptyCataloguePage is the answer to a filter that names no category: an empty
// page, not an error (FR-006).
func emptyCataloguePage(page, pageSize int) dto.PublicProductPage {
	return dto.PublicProductPage{Page: page, PageSize: pageSize, Total: 0}
}

// listWindow bounds a requested listing window. An out-of-range window is clamped
// rather than refused, so a caller that asks for page 0 or an enormous page still
// gets a usable answer instead of an error.
func listWindow(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case pageSize < 1:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}
	return page, pageSize
}
