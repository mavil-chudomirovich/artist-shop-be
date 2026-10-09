package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
)

// The pagination window of the movement history. It is the project's usual list
// convention: a page is 1-based, the size defaults to 20 and never exceeds 100
// (research D14). The transport rejects a value outside the bounds before the use
// case runs, so this clamp only protects a direct caller such as a test.
const (
	defaultHistoryPageSize = 20
	maxHistoryPageSize     = 100
)

// History returns one page of a product's physical changes, oldest first (FR-008).
//
// It resolves the product through the ProductLookup contract first, so an unknown
// product answers not-found rather than an empty history — the same rule the
// stock read follows (FR-007, research D12). A product that exists but has no
// movements answers an empty page, not an error.
//
// It opens no transaction: the history is a read, and a single paged query is a
// consistent observation. The order — created_at then id — is the repository
// adapter's, so the story of how a quantity came to be reads in the order it
// happened and stays stable across two reads sharing a timestamp (research D14).
func (s *Service) History(ctx context.Context, in dto.HistoryInput) (dto.MovementPage, error) {
	if err := s.requireProduct(ctx, in.ProductID); err != nil {
		return dto.MovementPage{}, err
	}

	page, pageSize := historyWindow(in.Page, in.PageSize)
	movements, total, err := s.Inventory.Movements(ctx, in.ProductID, page, pageSize)
	if err != nil {
		return dto.MovementPage{}, err
	}
	return dto.MovementPage{
		Movements: s.Mapper.Movements(movements),
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
	}, nil
}

// historyWindow clamps the requested window to the project's bounds, so a direct
// caller that omits or oversteps them still gets the usual page 1 of 20 and the
// response's metadata names the window actually applied (research D14).
func historyWindow(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultHistoryPageSize
	}
	if pageSize > maxHistoryPageSize {
		pageSize = maxHistoryPageSize
	}
	return page, pageSize
}
