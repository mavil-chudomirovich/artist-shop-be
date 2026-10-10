package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is US3: a customer changes an order before paying. EditMine replaces
// the order's line set (and, when named, its delivery address) in one UnitOfWork
// transaction, under the order's row lock. The owner is the session's, never an
// input, so another customer's order answers the same not-found an unknown one
// does. Editing an order awaiting the artist leaves it awaiting the artist and
// touches no stock; editing an awaiting-payment order releases its hold exactly
// once and returns it to awaiting confirmation, so the artist must confirm the new
// content afresh (FR-012 to FR-017, research D6, D7).

// EditMine changes one of the caller's own orders while it still awaits the
// artist or payment, and returns the edited order.
//
// Every move runs in one transaction that locks the order (LockByID), so the
// domain transition reads the current state under the lock and the new line set,
// the total, the content version and the edit history commit together or not at
// all. Each new line is re-checked against the product's current sale state and
// against what is available, and its name, slug, unit price and currency are
// re-snapshotted: a price that changed since the order was placed is re-taken,
// never refused, because the customer is choosing the items again (FR-015,
// research D6).
//
// A refusal — the order is paid or beyond, the edit would leave it empty, a line
// is off sale or above what is available, or the named address is not the
// customer's — leaves the order exactly as it was, because it is returned before
// any mutation and before any hold is released beyond the transaction that rolls
// back.
func (s *Service) EditMine(ctx context.Context, in dto.EditInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}

	var view dto.OrderView
	var edited *model.Order
	var previousStatus constant.Status
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		// The owner is the session's, never the input: another customer's order
		// answers the same not-found an unknown one does, so a route never
		// confirms whose order it is (FR-022).
		if order.UserID != actor.ID {
			return domainerr.ErrNotFound
		}
		if !order.IsEditable() {
			return domainerr.ErrNotEditable
		}
		previousStatus = order.Status

		before := model.EditSnapshot{
			Address: order.Address,
			Lines:   append([]model.OrderLine(nil), order.Lines...),
		}

		// An edit of an awaiting-payment order frees the goods it held: the old
		// content no longer exists, so the old hold must not linger. The release
		// is idempotent in module 05, so a retried edit never frees stock twice
		// (FR-014, research D7).
		if order.Status == constant.StatusPaymentPending {
			for _, line := range order.Lines {
				if err := s.Reservations.Release(txCtx, order.ID, line.ProductID); err != nil {
					return err
				}
			}
		}

		// An order always carries at least one line; the customer cancels
		// instead of editing it away (FR-015).
		if len(in.Lines) == 0 {
			return domainerr.ErrEmptyOrder
		}

		now := s.now()
		lines, err := s.editedLines(txCtx, order.ID, in.Lines, now)
		if err != nil {
			return err
		}

		address := order.Address
		if in.AddressID != nil {
			resolved, err := s.deliveryAddress(txCtx, order.UserID, in.AddressID)
			if err != nil {
				return err
			}
			address = resolved
		}

		order.Lines = lines
		order.Total = model.LinesTotal(lines)
		order.Address = address
		// Edit bumps the content version and, when the order awaited payment,
		// returns it to awaiting confirmation and clears its confirmation instant
		// and deadline (FR-013, FR-014, FR-017).
		if err := order.Edit(); err != nil {
			return err
		}
		order.UpdatedAt = now

		if err := s.Orders.ReplaceLines(txCtx, order, now); err != nil {
			return err
		}
		// The before/after content and the actor are recorded in the same
		// transaction as the edit, so an accepted edit is always auditable
		// (FR-017).
		if err := s.Orders.InsertEditHistory(txCtx, model.EditHistory{
			ID:      uuid.New(),
			OrderID: order.ID,
			Version: order.Version,
			ActorID: actor.ID,
			Before:  before,
			After: model.EditSnapshot{
				Address: order.Address,
				Lines:   append([]model.OrderLine(nil), order.Lines...),
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}

		edited = order
		view = s.Mapper.Order(*order)
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	// An accepted edit leaves the order awaiting confirmation, so the artist is
	// told it needs confirming afresh. The customer is told only when the edit
	// changed the status (a PAYMENT_PENDING edit returns it to PENDING); an edit
	// that kept the status unchanged does not email the customer (FR-019, FR-020,
	// FR-021, research D9).
	s.notifyArtistConfirmationNeeded(ctx, edited)
	if edited.Status != previousStatus {
		s.notifyCustomerStatusChange(ctx, edited)
	}
	return view, nil
}

// editedLines re-checks each requested line against the product's current sale
// state and what is available and re-snapshots it — name, slug, unit price and
// currency — in position order. One bulk read of the products and one of
// availability serve however many lines the edit names (FR-015, research D6).
//
// It refuses the whole edit on the first line that is off sale or removed, or
// that asks for more than is available, naming the item; nothing is written when
// it refuses, because the caller returns the error and the transaction rolls back.
func (s *Service) editedLines(ctx context.Context, orderID uuid.UUID, inputs []dto.EditLineInput, now time.Time) ([]model.OrderLine, error) {
	productIDs := make([]uuid.UUID, 0, len(inputs))
	for _, input := range inputs {
		productIDs = append(productIDs, input.ProductID)
	}

	summaries, err := s.Products.Products(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]contracts.ProductSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.ID] = summary
	}

	availabilities, err := s.Availability.AvailableQuantity(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	availableByID := make(map[uuid.UUID]int64, len(availabilities))
	for _, item := range availabilities {
		availableByID[item.ProductID] = item.Available
	}

	lines := make([]model.OrderLine, 0, len(inputs))
	for i, input := range inputs {
		summary, ok := byID[input.ProductID]
		if !ok || !summary.OnSale {
			return nil, domainerr.ItemNotPurchasable(input.ProductID)
		}
		available := availableByID[input.ProductID]
		if input.Quantity > available {
			return nil, domainerr.QuantityExceedsAvailable(input.ProductID, available, input.Quantity)
		}
		lines = append(lines, model.OrderLine{
			ID:        uuid.New(),
			OrderID:   orderID,
			ProductID: input.ProductID,
			Name:      summary.Name,
			Slug:      summary.Slug,
			UnitPrice: model.Price{Amount: summary.Price.Amount, Currency: summary.Price.Currency},
			Quantity:  input.Quantity,
			Position:  i,
			CreatedAt: now,
		})
	}
	return lines, nil
}
