package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
)

// This file is US3: a customer's own orders. Every read or change takes the
// acting account from the context the session filled, never from an input, so a
// customer only ever reaches their own orders (FR-018 to FR-020). Reading
// another customer's order, or cancelling one, answers the same not-found an
// unknown order does, so no route confirms that another customer's order exists.
// Cancellation reuses the US2 Cancel transition, so a paid order is refused by
// the state machine (FR-019).

// Pagination bounds of the order lists. They mirror the project's existing
// convention (research D14) and the Page/PageSize parameters of
// contracts/openapi.yaml.
const (
	defaultOrderPageSize = 20
	maxOrderPageSize     = 100
)

// ListMine returns one page of the caller's own orders, newest first (FR-018).
// The owner is the session's, never the input (FR-020).
func (s *Service) ListMine(ctx context.Context, in dto.ListInput) (dto.OrderPage, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderPage{}, err
	}
	page, size := orderListWindow(in.Page, in.PageSize)
	summaries, total, err := s.Orders.ListOwner(ctx, actor.ID, page, size)
	if err != nil {
		return dto.OrderPage{}, err
	}
	return dto.OrderPage{
		Orders:   s.Mapper.Summaries(summaries),
		Page:     page,
		PageSize: size,
		Total:    total,
	}, nil
}

// GetMine reads one of the caller's own orders in full. An order that does not
// exist, or belongs to another customer, answers the same not-found, so the route
// never confirms another customer's order (FR-018, FR-020).
func (s *Service) GetMine(ctx context.Context, in dto.OrderRefInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}
	order, err := s.Orders.FindByOwner(ctx, actor.ID, in.OrderID)
	if err != nil {
		return dto.OrderView{}, err
	}
	return s.Mapper.Order(*order), nil
}

// CancelMine cancels one of the caller's own orders before it is paid — while it
// awaits the artist's confirmation or payment — and returns it. The ownership
// check answers the same not-found an unknown order does, and then the US2 Cancel
// transition releases any hold of every line; both run in one transaction, so the
// order and the released goods commit together or not at all (FR-019, FR-020).
func (s *Service) CancelMine(ctx context.Context, in dto.OrderRefInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}

	var view dto.OrderView
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.FindByOwner(txCtx, actor.ID, in.OrderID)
		if err != nil {
			return err
		}
		if err := s.Cancel(txCtx, order.ID); err != nil {
			return err
		}
		cancelled, err := s.Orders.FindByOwner(txCtx, actor.ID, order.ID)
		if err != nil {
			return err
		}
		view = s.Mapper.Order(*cancelled)
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	return view, nil
}

// orderListWindow bounds a requested listing window. An out-of-range window is
// clamped rather than refused here — presentation has already validated it — so a
// caller that reaches the use case with an absent window still reads a page
// (research D14).
func orderListWindow(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case size < 1:
		size = defaultOrderPageSize
	case size > maxOrderPageSize:
		size = maxOrderPageSize
	}
	return page, size
}
