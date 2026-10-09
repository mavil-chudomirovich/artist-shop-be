package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// The availability signals are the system-facing half of the sell state: the
// inventory module asks the product to stop or resume being buyable when a
// product's availability crosses zero (research D4, FR-024 to FR-027). They are
// deliberately separate from ChangeSellState: the administrator path requires and
// audits an administrator, while this path has no human actor at all.
//
// Both signals move through the entity's existing transitions — SellOut and
// Restock — and are tolerant: a product already in the target state, an announced
// product and a retired product are left untouched rather than refused, so a
// correct stock operation is never failed by a signal that changes nothing.

// MarkOutOfStock moves an on-sale product to out of stock (FR-024).
func (s *Service) MarkOutOfStock(ctx context.Context, productID uuid.UUID) error {
	return s.signalAvailability(ctx, productID, constant.SellStateOutOfStock)
}

// MarkOnSale moves an out-of-stock product back on sale (FR-025).
func (s *Service) MarkOnSale(ctx context.Context, productID uuid.UUID) error {
	return s.signalAvailability(ctx, productID, constant.SellStateActive)
}

// signalAvailability applies one availability signal. It is a no-op unless the
// product is in the state the signal leaves (ACTIVE for out-of-stock, OUT_OF_STOCK
// for on sale), which is exactly the tolerance the contract documents: an
// announced or retired product, or one already in the target state, must not move.
// An unknown product is the module's not-found.
func (s *Service) signalAvailability(ctx context.Context, productID uuid.UUID, target constant.SellState) error {
	view, err := s.Products.FindByID(ctx, productID)
	if err != nil {
		return err
	}
	product := view.Product
	from := product.SellState

	var transition error
	switch target {
	case constant.SellStateOutOfStock:
		if product.SellState != constant.SellStateActive {
			return nil
		}
		transition = product.SellOut(now())
	case constant.SellStateActive:
		if product.SellState != constant.SellStateOutOfStock {
			return nil
		}
		transition = product.Restock(now())
	default:
		return nil
	}
	if transition != nil {
		return transition
	}
	if err := s.Products.Update(ctx, &product); err != nil {
		return err
	}
	s.recordProductSystem(ctx, constant.AuditProductStateChanged, product.ID, map[string]any{
		"from": string(from),
		"to":   string(product.SellState),
	})
	return nil
}

// recordProductSystem emits one audit event for a change no human caused. The
// actor is nil and the role empty, which is truthful: an availability change is
// driven by stock, not by an administrator (FR-020, research D13, Constitution
// VI). Emission never fails the business operation.
func (s *Service) recordProductSystem(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess), nil, "", targetTypeProduct, targetID.String(), metadata)
}
