package dto

import (
	"time"

	"github.com/google/uuid"
)

// CheckoutRequest is the body of a checkout (contracts/openapi.yaml,
// CheckoutRequest). `addressId` is a string rather than a uuid.UUID so a value
// that is not a UUID is reported as VALIDATION_ERROR naming `addressId` rather
// than lost to a generic decode failure. It is a pointer so an omitted or null
// member is distinguishable from an empty string, and both an omitted and a null
// member mean "use the customer's default address" (FR-003).
//
// The schema is additionalProperties: false and the handler decodes it with
// unknown members refused, so a client cannot name the acting account.
type CheckoutRequest struct {
	// AddressID is the address the customer named, or nil to use the default.
	AddressID *string `json:"addressId"`
}

// MoneyResponse is money as an integer amount in the currency's minor unit plus
// its currency, the shape the contract shows (FR-008).
type MoneyResponse struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code.
	Currency string `json:"currency"`
}

// OrderLineResponse is one order line as a client reads it (contracts/openapi.yaml,
// OrderLine). `name`, `slug` and `unitPrice` are the product's values as they were
// at checkout; `productId` is the product recorded, not a live reference.
type OrderLineResponse struct {
	// ProductID is the product recorded on the line.
	ProductID uuid.UUID `json:"productId"`
	// Name is the product's name at checkout.
	Name string `json:"name"`
	// Slug is the product's link segment at checkout.
	Slug string `json:"slug"`
	// Quantity is the whole number bought.
	Quantity int64 `json:"quantity"`
	// UnitPrice is the snapshot unit price.
	UnitPrice MoneyResponse `json:"unitPrice"`
	// LineTotal is quantity times the snapshot unit price.
	LineTotal MoneyResponse `json:"lineTotal"`
}

// OrderAddressResponse is the delivery address as it was captured at checkout
// (contracts/openapi.yaml, OrderAddress). A later edit to the customer's address
// never changes a placed order (FR-003).
type OrderAddressResponse struct {
	// RecipientName is the person the order goes to.
	RecipientName string `json:"recipientName"`
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string `json:"recipientPhone"`
	// ProvinceCode and ProvinceName identify the first-level unit.
	ProvinceCode string `json:"provinceCode"`
	ProvinceName string `json:"provinceName"`
	// WardCode and WardName identify the second-level unit.
	WardCode string `json:"wardCode"`
	WardName string `json:"wardName"`
	// StreetAddress is the free-text house number and street.
	StreetAddress string `json:"streetAddress"`
}

// OrderSummaryResponse is one row of the signed-in customer's order list
// (contracts/openapi.yaml, OrderSummary). Money is integer minor units plus the
// currency (FR-008).
type OrderSummaryResponse struct {
	// ID identifies the order.
	ID uuid.UUID `json:"id"`
	// Status is where the order is in its life.
	Status string `json:"status"`
	// Total is the committed total.
	Total MoneyResponse `json:"total"`
	// ItemCount is how many lines the order carries.
	ItemCount int64 `json:"itemCount"`
	// CreatedAt is when the order was placed.
	CreatedAt time.Time `json:"createdAt"`
}

// OrderResponse is one order as a customer reads it in full
// (contracts/openapi.yaml, OrderDetail). The list summary plus the address and the
// lines; money is integer minor units plus the currency and the total carries no
// shipping fee (FR-008, ADR 015 §5).
type OrderResponse struct {
	// ID identifies the order.
	ID uuid.UUID `json:"id"`
	// Status is where the order is in its life.
	Status string `json:"status"`
	// Total is the committed total.
	Total MoneyResponse `json:"total"`
	// ItemCount is how many lines the order carries.
	ItemCount int64 `json:"itemCount"`
	// CreatedAt is when the order was placed.
	CreatedAt time.Time `json:"createdAt"`
	// Address is the delivery address snapshot.
	Address OrderAddressResponse `json:"address"`
	// Lines are the order's snapshot lines, in position order.
	Lines []OrderLineResponse `json:"lines"`
}
