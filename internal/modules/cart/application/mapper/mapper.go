// Package mapper converts the cart domain model into application DTOs. It is the
// single place a model becomes a DTO and carries no other dependency, so a change
// to one shape cannot silently reach the other.
package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
)

// Mapper converts cart domain models into application DTOs.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Money maps one price to its DTO shape. The amount is carried as an integer
// minor-unit value, never converted through a float (FR-009).
func (m *Mapper) Money(price model.Price) dto.MoneyView {
	return dto.MoneyView{Amount: price.Amount, Currency: price.Currency}
}

// Subtotal maps a cart's subtotal, answering nil when the cart has none — the
// empty cart, which carries no money and therefore no currency (FR-004, spec
// Assumptions).
func (m *Mapper) Subtotal(price model.Price, present bool) *dto.MoneyView {
	if !present {
		return nil
	}
	money := m.Money(price)
	return &money
}

// Line maps one cart line plus the live product facts into a DTO line. The
// buyable decision and the optional available quantity come from the entity's own
// rules, so the shape a client is shown is the same rule the use case enforces
// (FR-012, research D10).
func (m *Mapper) Line(line model.CartLine, facts model.ProductFacts, name, slug *string) dto.LineView {
	view := dto.LineView{
		ProductID: line.ProductID,
		Name:      name,
		Slug:      slug,
		Quantity:  line.Quantity,
		UnitPrice: m.Money(line.UnitPrice),
		LineTotal: m.Money(line.LineTotal()),
		Buyable:   line.Buyable(facts),
	}
	if available, reported := line.ReportedAvailability(facts); reported {
		view.AvailableQuantity = &available
	}
	return view
}

// Cart maps a cart's lines and subtotal into the DTO view. An absent list maps to
// an empty slice, never a nil one, so an empty cart serialises as `lines: []`
// rather than `lines: null` (quickstart scenario 1).
func (m *Mapper) Cart(lines []dto.LineView, subtotal *dto.MoneyView) dto.CartView {
	if lines == nil {
		lines = []dto.LineView{}
	}
	return dto.CartView{Lines: lines, Subtotal: subtotal}
}
