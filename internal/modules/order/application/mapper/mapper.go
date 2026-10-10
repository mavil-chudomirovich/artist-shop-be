// Package mapper converts the order domain models into application DTOs. It is
// the single place a model becomes a DTO and carries no other dependency, so a
// change to one shape cannot silently reach the other. Money is carried as an
// integer minor-unit value plus its currency, never converted through a float
// (FR-008).
package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// Mapper converts order domain models into application DTOs.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Money maps one price to its DTO shape.
func (m *Mapper) Money(price model.Price) dto.MoneyView {
	return dto.MoneyView{Amount: price.Amount, Currency: price.Currency}
}

// Address maps the delivery-address snapshot to its DTO shape.
func (m *Mapper) Address(address model.Address) dto.OrderAddressView {
	return dto.OrderAddressView{
		RecipientName:  address.RecipientName,
		RecipientPhone: address.RecipientPhone,
		ProvinceCode:   address.ProvinceCode,
		ProvinceName:   address.ProvinceName,
		WardCode:       address.WardCode,
		WardName:       address.WardName,
		StreetAddress:  address.StreetAddress,
	}
}

// Line maps one snapshot line to its DTO shape, carrying its own line total
// (FR-002).
func (m *Mapper) Line(line model.OrderLine) dto.OrderLineView {
	return dto.OrderLineView{
		ProductID: line.ProductID,
		Name:      line.Name,
		Slug:      line.Slug,
		Quantity:  line.Quantity,
		UnitPrice: m.Money(line.UnitPrice),
		LineTotal: m.Money(line.LineTotal()),
	}
}

// Lines maps an order's lines, preserving their position order. A nil list maps
// to an empty slice, never a nil one, so a detail serialises `lines: []` rather
// than `lines: null`.
func (m *Mapper) Lines(lines []model.OrderLine) []dto.OrderLineView {
	out := make([]dto.OrderLineView, 0, len(lines))
	for _, line := range lines {
		out = append(out, m.Line(line))
	}
	return out
}

// Summary maps one list row to the customer-facing shape (FR-018).
func (m *Mapper) Summary(summary model.OrderSummary) dto.OrderSummaryView {
	return dto.OrderSummaryView{
		ID:        summary.ID,
		Status:    summary.Status,
		Total:     m.Money(summary.Total),
		ItemCount: summary.ItemCount,
		CreatedAt: summary.CreatedAt,
	}
}

// AdminSummary maps one list row to the operator-facing shape, adding the owner
// (FR-021).
func (m *Mapper) AdminSummary(summary model.OrderSummary) dto.AdminOrderSummaryView {
	return dto.AdminOrderSummaryView{
		OrderSummaryView: m.Summary(summary),
		UserID:           summary.UserID,
	}
}

// Summaries maps a page of list rows to the customer-facing shapes, preserving
// the newest-first order the repository applied.
func (m *Mapper) Summaries(summaries []model.OrderSummary) []dto.OrderSummaryView {
	out := make([]dto.OrderSummaryView, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, m.Summary(summary))
	}
	return out
}

// AdminSummaries maps a page of list rows to the operator-facing shapes,
// preserving the newest-first order the repository applied.
func (m *Mapper) AdminSummaries(summaries []model.OrderSummary) []dto.AdminOrderSummaryView {
	out := make([]dto.AdminOrderSummaryView, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, m.AdminSummary(summary))
	}
	return out
}

// Order maps a whole order to the customer's detail shape. The line count is the
// number of lines the order carries, so the summary and the detail cannot
// disagree. The order's content version is deliberately not mapped: it is an
// internal seam for module 08, never a client-facing value (research D10).
func (m *Mapper) Order(order model.Order) dto.OrderView {
	return dto.OrderView{
		OrderSummaryView: dto.OrderSummaryView{
			ID:        order.ID,
			Status:    order.Status,
			Total:     m.Money(order.Total),
			ItemCount: int64(len(order.Lines)),
			CreatedAt: order.CreatedAt,
		},
		Address: m.Address(order.Address),
		Lines:   m.Lines(order.Lines),
	}
}

// AdminOrder maps a whole order to the operator's detail shape, including its
// owner (FR-021).
func (m *Mapper) AdminOrder(order model.Order) dto.AdminOrderView {
	return dto.AdminOrderView{
		AdminOrderSummaryView: dto.AdminOrderSummaryView{
			OrderSummaryView: dto.OrderSummaryView{
				ID:        order.ID,
				Status:    order.Status,
				Total:     m.Money(order.Total),
				ItemCount: int64(len(order.Lines)),
				CreatedAt: order.CreatedAt,
			},
			UserID: order.UserID,
		},
		Address: m.Address(order.Address),
		Lines:   m.Lines(order.Lines),
	}
}
