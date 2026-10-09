package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// Contract member names used when a rejection has to say which input is wrong.
// They are the exact member names of specs/009-order/contracts/openapi.yaml, so
// presentation can put one straight into the response detail without a second
// vocabulary that could drift away from the contract.
const (
	// FieldProductID is the member carrying a product identifier.
	FieldProductID = "productId"
	// FieldStatus is the member carrying an order state.
	FieldStatus = "status"
)

// Price is money as an integer amount in a currency's minor unit together with
// that currency (FR-008).
//
// Money is never a float in this service: the amount is an integer of minor units,
// stored as an integer and returned as an integer, and the total is the exact sum
// of the lines with no rounding.
type Price struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency code: exactly three uppercase letters.
	Currency string
}

// OrderLine is one item of an order. It holds a snapshot of the product as it was
// at checkout, so nothing an order line shows depends on the product still
// existing (FR-002, research D3).
type OrderLine struct {
	// ID identifies the line.
	ID uuid.UUID
	// OrderID is the order the line belongs to.
	OrderID uuid.UUID
	// ProductID is the product bought, an informational reference recorded for
	// the plan's record of what was bought. It is never read to resolve the
	// product (research D3).
	ProductID uuid.UUID
	// Name is the product's name at checkout.
	Name string
	// Slug is the product's link segment at checkout.
	Slug string
	// UnitPrice is the price at checkout, as an integer amount plus its currency.
	UnitPrice Price
	// Quantity is how many, at least 1.
	Quantity int64
	// Position is the order the lines are listed in.
	Position int
	// CreatedAt is when the line was written. Set once.
	CreatedAt time.Time
}

// LineTotal is the line's contribution to the order: quantity times the snapshot
// unit price, in the same currency. Computed as int64 minor units with no rounding
// (FR-008).
func (l OrderLine) LineTotal() Price {
	return Price{Amount: l.Quantity * l.UnitPrice.Amount, Currency: l.UnitPrice.Currency}
}

// Address is the delivery address captured at checkout, so a later edit to the
// customer's address does not change a placed order (FR-003, research D7).
type Address struct {
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string
	// ProvinceCode and ProvinceName identify the first-level unit.
	ProvinceCode string
	ProvinceName string
	// WardCode and WardName identify the second-level unit.
	WardCode string
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
}

// Order is a purchase a customer has committed to: who owns it, where it goes,
// what it costs, and where it is in its life (spec Key Entities).
type Order struct {
	// ID identifies the order. It is stable for the order's life; the operator
	// and the customer address it by this.
	ID uuid.UUID
	// UserID is the account that owns the order. A loose reference: an order
	// outlives the account it names, and a transfer changes it (research D12).
	UserID uuid.UUID
	// Status is where the order is in its life. Changed only through the
	// transition methods below (FR-010).
	Status constant.Status
	// Total is the committed total: the sum of quantity times the snapshot unit
	// price over the lines, in the currency's minor unit (FR-008).
	Total Price
	// Address is the delivery address snapshot.
	Address Address
	// ExpiresAt is when an unpaid order cancels itself (FR-012, research D6).
	ExpiresAt time.Time
	// CreatedAt is when the order was placed. Set once.
	CreatedAt time.Time
	// UpdatedAt is touched on every write.
	UpdatedAt time.Time
	// Lines are the order's snapshot lines, in position order.
	Lines []OrderLine
}

// NewOrder builds an order in the awaiting-payment state, linking every line to
// it in position order and committing the total to the exact sum of the line
// snapshots (FR-001, FR-002, FR-008).
//
// It generates the order's identifier and the lines' identifiers when they are
// absent, so a caller supplies what was bought and when, not the keys.
func NewOrder(userID uuid.UUID, address Address, lines []OrderLine, expiresAt, now time.Time) *Order {
	order := &Order{
		ID:        uuid.New(),
		UserID:    userID,
		Status:    constant.StatusPendingPayment,
		Address:   address,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
		Lines:     make([]OrderLine, len(lines)),
	}
	for i, line := range lines {
		if line.ID == uuid.Nil {
			line.ID = uuid.New()
		}
		line.OrderID = order.ID
		line.Position = i
		if line.CreatedAt.IsZero() {
			line.CreatedAt = now
		}
		order.Lines[i] = line
	}
	order.Total = LinesTotal(order.Lines)
	return order
}

// LinesTotal returns the sum of the lines' totals, in the first line's currency.
// The MVP shop is single-currency, so every line of one order shares a currency
// and no conversion arises (spec Assumptions). The result is computed as int64
// minor units with no rounding and carries no shipping fee (FR-008, ADR 015 §5).
func LinesTotal(lines []OrderLine) Price {
	var total Price
	for i, line := range lines {
		lineTotal := line.LineTotal()
		if i == 0 {
			total.Currency = lineTotal.Currency
		}
		total.Amount += lineTotal.Amount
	}
	return total
}

// MarkPaid moves an awaiting-payment order to paid. It is the edge module 08's
// confirmed payment drives (FR-009, research D13).
func (o *Order) MarkPaid() error {
	return o.transition(constant.StatusPendingPayment, constant.StatusPaid)
}

// Ship moves a paid order to shipped. It is the edge an administrator drives
// (FR-009, FR-022).
func (o *Order) Ship() error {
	return o.transition(constant.StatusPaid, constant.StatusShipped)
}

// Complete moves a shipped order to completed. It is the edge an administrator
// drives (FR-009, FR-022).
func (o *Order) Complete() error {
	return o.transition(constant.StatusShipped, constant.StatusCompleted)
}

// Cancel moves an awaiting-payment order to cancelled. It is the edge the customer
// and the expiry sweep drive (FR-009, FR-012, FR-019).
func (o *Order) Cancel() error {
	return o.transition(constant.StatusPendingPayment, constant.StatusCancelled)
}

// transition applies one allowed edge. A move the current state does not allow is
// refused with the current state named and leaves the order exactly as it was
// (FR-010, FR-011). It is the only writer of Status, so the transition table
// cannot be bypassed.
func (o *Order) transition(from, to constant.Status) error {
	if o.Status != from {
		return domainerr.StateTransitionInvalid(o.Status, to)
	}
	o.Status = to
	return nil
}

// IsTerminal reports whether the order has reached a state no move leaves. A
// completed or cancelled order is terminal (FR-009, research D9).
func (o Order) IsTerminal() bool {
	return o.Status == constant.StatusCompleted || o.Status == constant.StatusCancelled
}
