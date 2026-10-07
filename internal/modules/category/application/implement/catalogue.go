// Package implement contains the category module's use-case implementations. It
// depends only on application/interface and domain.
package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
)

// Pagination bounds of the two category listings. They mirror the handler's, so
// a window that reaches a use case from any other entry point is bounded the
// same way instead of asking the database for an unbounded result (research D8).
const (
	// defaultPageSize is the window used when none is requested.
	defaultPageSize = 20
	// maxPageSize is the largest window a listing will return.
	maxPageSize = 100
)

// Service implements the category module's use cases.
//
// The public browse methods answer a visitor and never reveal that a withheld
// category exists (FR-005, FR-007). The administrator methods are added by US2.
type Service struct {
	// Categories persists categories. It never opens a transaction: the
	// application layer owns every boundary (Constitution I).
	Categories domainrepo.CategoryRepository
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the category use-case service.
func New(service Service) *Service { return &service }

// ListPublic returns one page of the categories on display, in the configured
// order (FR-001, FR-002, FR-003).
//
// The window is bounded here as well as in the handler, so a caller that
// bypasses presentation still cannot ask for an unbounded page. An ordering
// error never reaches this method: the repository applies the FR-003 order, and
// this method preserves the order it receives (FR-006).
func (s *Service) ListPublic(ctx context.Context, in dto.ListPublicInput) (dto.PublicCategoryPage, error) {
	page, pageSize := listWindow(in.Page, in.PageSize)
	list, total, err := s.Categories.ListVisible(ctx, page, pageSize)
	if err != nil {
		return dto.PublicCategoryPage{}, err
	}
	return dto.PublicCategoryPage{
		Categories: s.Mapper.PublicCategories(list),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
	}, nil
}

// GetPublicBySlug returns one category on display, addressed by the slug a
// customer-facing link is built from (research D7).
//
// A withheld, removed or unknown slug answers the same not-found, because the
// repository reports all three as ErrCategoryNotFound and this method returns it
// unwrapped (FR-005, SC-004).
func (s *Service) GetPublicBySlug(ctx context.Context, in dto.PublicCategoryRefInput) (dto.PublicCategoryOutput, error) {
	category, err := s.Categories.FindVisibleBySlug(ctx, in.Slug)
	if err != nil {
		return dto.PublicCategoryOutput{}, err
	}
	return s.Mapper.PublicCategory(*category), nil
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
