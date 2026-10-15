package implement

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Pagination bounds of the two product listings. They mirror the handler's, so a
// window that reaches a use case from any other entry point is bounded the same
// way instead of asking the database for an unbounded result (research D12).
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
// files of this package and are reached only behind the administrator role
// guard.
type Service struct {
	// Products persists products and reads the catalogue. It never opens a
	// transaction; the application layer owns every boundary
	// (Constitution I).
	Products domainrepo.ProductRepository
	// Groups persists variant groups and reads their own pictures and their
	// options (FR-009 to FR-018). It is the module's second repository,
	// supplied at the composition root beside Products.
	Groups appinterface.VariantGroupRepository
	// Visibility answers the two facts about categories this module cannot read
	// itself: which categories a customer may see, and which category a
	// customer-facing slug names (research D1). It is the cross-module contract
	// supplied at the composition root.
	Visibility appinterface.CategoryQuery
	// Audit records every administrative mutation (FR-014, Constitution VI). It
	// is unused by the public browse use cases, but the composition root always
	// supplies the module's auditor adapter.
	Audit appinterface.Auditor
	// Media stores pictures outside the service through the shared media port
	// (FR-018). It is reached only by the picture use cases, which is what keeps
	// a provider outage from failing a product read or edit.
	Media appinterface.MediaStore
	// Tx owns the picture operations' transaction boundaries. The ten-picture
	// ceiling is a count no unique index can express, so it is checked while the
	// product's row is locked inside this transaction (research D6).
	Tx appinterface.UnitOfWork
	// Config carries the picture upload ceilings the composition resolved. A
	// zero value falls back to the documented contract defaults.
	Config appinterface.Config
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the product use-case service.
func New(service Service) *Service { return &service }

// ListCatalogue returns one page of the merged customer-facing catalogue — the
// standalone products and the variant groups in one ordered result — plus the
// total across both kinds (FR-001, FR-005, FR-006, FR-009).
//
// The visible set is fetched from the cross-module contract exactly once per
// request and passed to the repository, so the whole visibility predicate (a
// group is visible only while one of its options is) runs in SQL rather than in
// Go: dropping hidden rows after the page is fetched would return a short page
// and a total that counts rows the caller never received (FR-006, research D1,
// D4).
//
// The optional `category` filter is a customer-facing slug, so it is resolved
// through the same contract. A slug no category holds answers the empty page
// directly; a *hidden* slug resolves to its identifier and is then excluded by
// the visibility predicate anyway, so the two paths agree without a special
// case (FR-006, research D1).
func (s *Service) ListCatalogue(ctx context.Context, in dto.ListPublicInput) (dto.PublicCataloguePage, error) {
	page, pageSize := listWindow(in.Page, in.PageSize)

	visible, err := s.Visibility.VisibleCategoryIDs(ctx)
	if err != nil {
		return dto.PublicCataloguePage{}, err
	}

	var categoryID *uuid.UUID
	if slug := strings.TrimSpace(in.CategorySlug); slug != "" {
		id, found, err := s.Visibility.CategoryIDBySlug(ctx, slug)
		if err != nil {
			return dto.PublicCataloguePage{}, err
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
		return dto.PublicCataloguePage{}, err
	}
	return dto.PublicCataloguePage{
		Entries:  s.Mapper.PublicCatalogueEntries(items),
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// ListPublic returns one page of the public catalogue. It is the established
// public-listing operation; since the catalogue now carries variant groups as
// first-class entries, it is the merged read ListCatalogue (FR-001, research
// D4). It keeps its name because it is what the public route and its callers ask
// for, while the merge itself lives in ListCatalogue.
func (s *Service) ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicCataloguePage, error) {
	return s.ListCatalogue(ctx, in)
}

// GetPublicBySlug returns one visible product with its description and every
// picture, addressed by the slug a customer-facing link is built from (FR-005).
//
// The same visible set drives the detail read, so a product withheld by its own
// state, retired, removed, unknown or in a hidden category answers the same
// not-found and the response never confirms that a hidden product exists
// (FR-003, research D1).
//
// When the found product is an option of a variant group, the group's shared
// content and its visible siblings are attached as the `variantGroup` selector,
// so a shared link to one option keeps working and the customer can switch
// (FR-007). A group that has since been removed simply yields no selector: the
// option is still a product in its own right.
func (s *Service) GetPublicBySlug(ctx context.Context, in dto.PublicProductRefInput) (dto.PublicProductDetailOutput, error) {
	visible, err := s.Visibility.VisibleCategoryIDs(ctx)
	if err != nil {
		return dto.PublicProductDetailOutput{}, err
	}
	view, err := s.Products.FindVisibleBySlug(ctx, in.Slug, visible)
	if err != nil {
		return dto.PublicProductDetailOutput{}, err
	}
	out := s.Mapper.PublicProductDetail(*view)

	if view.Product.VariantGroupID != nil {
		selector, err := s.publicVariantGroupSelector(ctx, *view.Product.VariantGroupID, visible)
		if err != nil {
			return dto.PublicProductDetailOutput{}, err
		}
		out.VariantGroup = selector
	}
	return out, nil
}

// GetPublicGroupBySlug returns one visible variant group with its shared content,
// its own gallery and its visible options, addressed by the slug a
// customer-facing link is built from (FR-002, FR-005, FR-007).
//
// The whole visibility predicate runs in the repository — the group's category
// is visible and at least one of its options is — so a group that is unknown,
// hidden by its category, has no visible option or was removed answers the same
// not-found and the response never confirms that a hidden group exists
// (FR-006, FR-008).
func (s *Service) GetPublicGroupBySlug(ctx context.Context, in dto.PublicVariantGroupRefInput) (dto.PublicVariantGroupDetailOutput, error) {
	visible, err := s.Visibility.VisibleCategoryIDs(ctx)
	if err != nil {
		return dto.PublicVariantGroupDetailOutput{}, err
	}
	view, err := s.Groups.FindVisibleGroupBySlug(ctx, in.Slug, visible)
	if err != nil {
		return dto.PublicVariantGroupDetailOutput{}, err
	}
	return s.Mapper.PublicVariantGroupDetail(*view), nil
}

// publicVariantGroupSelector builds the `variantGroup` selector of an option's
// detail (FR-007). The group's shared content and gallery come from the group
// repository; the siblings are the group's *visible* options, read under the
// same predicate the public group detail uses, so the selector never shows a
// withheld sibling. A group that no longer exists yields no selector rather than
// failing the product read.
func (s *Service) publicVariantGroupSelector(ctx context.Context, groupID uuid.UUID, visible []uuid.UUID) (*dto.PublicProductVariantGroupOutput, error) {
	group, err := s.Groups.FindByID(ctx, groupID)
	if errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The group is only shown when it would itself be visible: its category is on
	// display. A group whose category is hidden must not leak its name, gallery or
	// siblings through an option's detail (FR-006, FR-008).
	if !slices.Contains(visible, group.Group.CategoryID) {
		return nil, nil
	}
	options, err := s.Products.ListOptionsByGroupIDs(ctx, []uuid.UUID{groupID}, visible)
	if err != nil {
		return nil, err
	}
	// A group with no visible option is not shown to customers (FR-006); if the
	// product reached here it is visible on its own, so its detail is served
	// without a selector rather than with an empty one.
	if len(options[groupID]) == 0 {
		return nil, nil
	}
	return s.Mapper.PublicProductVariantGroup(group.Group, group.Images, options[groupID]), nil
}

// emptyCataloguePage is the answer to a filter that names no category: an empty
// page, not an error (FR-006, FR-007).
func emptyCataloguePage(page, pageSize int) dto.PublicCataloguePage {
	return dto.PublicCataloguePage{Page: page, PageSize: pageSize, Total: 0}
}

// listWindow bounds a requested listing window. An out-of-range window is
// clamped rather than refused, so a caller that asks for page 0 or an enormous
// page still gets a usable answer instead of an error.
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
