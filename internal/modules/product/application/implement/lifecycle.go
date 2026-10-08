package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// The whole use-case surface is satisfied now that the lifecycle use cases exist,
// so the compile-time assertion lands with them rather than earlier. It is the one
// declaration of the surface: presentation consumes this interface directly
// (reconciliation with module 03's pattern) rather than declaring a second subset
// that could drift away from it.
var _ appinterface.ProductService = (*Service)(nil)

// ChangeSellState moves a product through its selling life and audits the change
// (FR-022 to FR-026, FR-014, research D8).
//
// Every move is routed through the domain's transition functions: the use case
// loads the entity, asks it to transition, and persists the entity it gets back.
// It never writes the state directly, so a move the transition table does not list
// cannot reach the row (FR-023, Constitution III). A refused move is reported by
// the domain with the state it started from and the row is left untouched
// (FR-024).
//
// A target that is not one of the four states is a request error naming `to`
// before anything is read, so an operator sees a bad target and a legal target in
// an illegal position as two different answers (quickstart 7d vs 7h).
func (s *Service) ChangeSellState(ctx context.Context, in dto.ChangeSellStateInput) (dto.AdminProductDetailOutput, error) {
	if !constant.IsValidSellState(in.To) {
		return dto.AdminProductDetailOutput{}, domainerr.InvalidProductField(
			model.FieldTo, "must be one of COMING_SOON, ACTIVE, OUT_OF_STOCK, DISCONTINUED")
	}

	view, err := s.Products.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	product := view.Product
	from := product.SellState
	if err := product.ChangeSellState(in.To, now()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	if err := s.Products.Update(ctx, &product); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	s.recordProduct(ctx, constant.AuditProductStateChanged, product.ID, map[string]any{
		"from": string(from),
		"to":   string(product.SellState),
	})
	return s.adminDetail(ctx, in.ID)
}
