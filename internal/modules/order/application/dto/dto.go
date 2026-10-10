// Package dto defines the order module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. Money is an integer amount
// in the currency's minor unit plus its currency, never a float. The acting
// account is never part of any input: the owner comes from the session
// (FR-020).
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
)

// CheckoutInput turns the caller's cart into an order. The owner is taken from
// the session, never from this input (FR-020).
type CheckoutInput struct {
	// AddressID is the address the customer named, or nil to use the customer's
	// default address. A customer with no address is refused (FR-003, research
	// D7).
	AddressID *uuid.UUID
}

// OrderRefInput addresses one order by its identifier. It is used by the reads
// and the moves that do not change the owner.
type OrderRefInput struct {
	// OrderID is the order being addressed.
	OrderID uuid.UUID
}

// EditLineInput is one line of an edit: the product and how many of it the order
// should hold. The whole line set is replaced, so a product the edit omits is
// removed from the order (FR-015, research D6).
type EditLineInput struct {
	// ProductID is the product the line is for.
	ProductID uuid.UUID
	// Quantity is how many, at least 1.
	Quantity int64
}

// EditInput replaces the caller's order content. The owner is taken from the
// session, never from this input (FR-012, FR-020).
type EditInput struct {
	// OrderID is the order being changed.
	OrderID uuid.UUID
	// AddressID is a saved address to deliver to; nil keeps the current address
	// (FR-015, research D6).
	AddressID *uuid.UUID
	// Lines are the order's new lines. An empty set is refused: an order always
	// carries at least one line (FR-015, research D6).
	Lines []EditLineInput
}

// ListInput pages through an order list. Both lists follow the project's
// existing convention (research D14 of 009).
type ListInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of orders per page.
	PageSize int
	// Status, when set, returns only orders in that state. It is the operator's
	// confirmation-queue filter (FR-026, research D11).
	Status *constant.Status
	// Sort is the order rows are returned in; the zero value is newest first
	// (FR-026, research D11).
	Sort constant.OrderListSort
}

// TransferInput hands a paid order to another account, named by its email. The
// acting administrator is taken from the session, never from this input
// (FR-024).
type TransferInput struct {
	// OrderID is the order being transferred.
	OrderID uuid.UUID
	// Email is the recipient account's email; no account carrying it is refused.
	Email string
}

// MoneyView is money as an integer amount in the currency's minor unit plus the
// currency, the shape a response shows (FR-008).
type MoneyView struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency code.
	Currency string
}

// OrderAddressView is the delivery address a response shows, as captured at
// checkout (FR-003).
type OrderAddressView struct {
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

// OrderLineView is one order line as a use case answers it: a snapshot of the
// product at checkout, with its own line total (FR-002).
type OrderLineView struct {
	// ProductID is the product recorded on the line, not a live reference.
	ProductID uuid.UUID
	// Name and Slug are the product's values as they were at checkout.
	Name string
	Slug string
	// Quantity is the whole number bought.
	Quantity int64
	// UnitPrice is the snapshot unit price.
	UnitPrice MoneyView
	// LineTotal is quantity times the snapshot unit price.
	LineTotal MoneyView
}

// OrderSummaryView is one row of a customer's or the operator's order list
// (FR-018, FR-021).
type OrderSummaryView struct {
	// ID identifies the order.
	ID uuid.UUID
	// Status is where the order is in its life.
	Status constant.Status
	// Total is the committed total.
	Total MoneyView
	// ItemCount is how many lines the order carries.
	ItemCount int64
	// CreatedAt is when the order was placed.
	CreatedAt time.Time
}

// AdminOrderSummaryView is one row of the operator's order list: the customer
// summary plus the account that owns the order (FR-021).
type AdminOrderSummaryView struct {
	// OrderSummaryView is the customer-facing summary.
	OrderSummaryView
	// UserID is the account that owns the order.
	UserID uuid.UUID
}

// OrderView is one order as a customer reads it, in full (FR-018).
type OrderView struct {
	// OrderSummaryView is the list summary the detail shares.
	OrderSummaryView
	// Address is the delivery address snapshot.
	Address OrderAddressView
	// Lines are the order's snapshot lines, in position order.
	Lines []OrderLineView
}

// AdminOrderView is one order as the operator reads it, in full, including its
// owner (FR-021).
type AdminOrderView struct {
	// AdminOrderSummaryView is the list summary the detail shares, including the
	// owner.
	AdminOrderSummaryView
	// Address is the delivery address snapshot.
	Address OrderAddressView
	// Lines are the order's snapshot lines, in position order.
	Lines []OrderLineView
}

// OrderPage is one page of a customer's orders.
type OrderPage struct {
	// Orders holds the page's order summaries, newest first.
	Orders []OrderSummaryView
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of orders the customer has.
	Total int64
}

// AdminOrderPage is one page of every order.
type AdminOrderPage struct {
	// Orders holds the page's order summaries, newest first.
	Orders []AdminOrderSummaryView
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of orders the shop has.
	Total int64
}
