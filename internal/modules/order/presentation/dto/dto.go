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

// EditLineRequest is one line of an edit request (contracts/openapi.yaml,
// EditLine). `variantId` is a string rather than a uuid.UUID so a value that is
// not a UUID is reported as VALIDATION_ERROR naming `variantId` rather than lost
// to a generic decode failure. `quantity` must be at least 1, exactly as the
// contract's minimum requires.
type EditLineRequest struct {
	// VariantID is the variant the line is for.
	VariantID string `json:"variantId"`
	// Quantity is how many, at least 1.
	Quantity int64 `json:"quantity"`
}

// EditOrderRequest is the body of a customer edit (contracts/openapi.yaml,
// EditOrderRequest). `lines` is required and replaces the order's whole line set;
// an empty set is refused. `addressId` is a string rather than a uuid.UUID so a
// value that is not a UUID is reported against `addressId`; it is a pointer so an
// omitted or null member is distinguishable and both mean "keep the current
// address" (FR-012, FR-015).
//
// The schema is additionalProperties: false and the handler decodes it with
// unknown members refused, so a client cannot name the acting account.
type EditOrderRequest struct {
	// AddressID is a saved address to deliver to; nil keeps the current one.
	AddressID *string `json:"addressId"`
	// Lines are the order's new lines; an empty set is refused.
	Lines []EditLineRequest `json:"lines"`
}

// TransferRequest is the body of an administrator transfer
// (contracts/openapi.yaml, TransferRequest). `email` names the recipient
// account; no account carrying it is refused. It is a plain string so a missing
// or malformed value is reported as VALIDATION_ERROR naming `email` rather than
// lost to a generic decode failure. The schema is additionalProperties: false and
// the handler decodes it with unknown members refused, so a client cannot name
// the acting administrator or an owner.
type TransferRequest struct {
	// Email is the recipient account's email.
	Email string `json:"email"`
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
// OrderLine). It shows the product name and the variant name as they were at
// checkout; `productId` and `variantId` are the records, not live references. The
// stored product code and SKU are never returned in the customer order view.
type OrderLineResponse struct {
	// ProductID is the product recorded on the line.
	ProductID uuid.UUID `json:"productId"`
	// ProductName is the product's name at checkout.
	ProductName string `json:"productName"`
	// VariantID is the variant recorded on the line.
	VariantID uuid.UUID `json:"variantId"`
	// VariantName is the variant's display name at checkout.
	VariantName string `json:"variantName"`
	// Quantity is the whole number bought.
	Quantity int64 `json:"quantity"`
	// UnitPrice is the snapshot unit price.
	UnitPrice MoneyResponse `json:"unitPrice"`
	// LineTotal is quantity times the snapshot unit price.
	LineTotal MoneyResponse `json:"lineTotal"`
	// Position is the order the line is listed in.
	Position int `json:"position"`
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

// DeliveryPhotoResponse is one piece of delivery evidence as a client reads it
// (contracts/openapi.yaml, DeliveryPhoto). `stage` is the delivery step the photo
// is evidence for, and `position` orders it within that stage (FR-010).
type DeliveryPhotoResponse struct {
	// ID identifies the photo.
	ID uuid.UUID `json:"id"`
	// Stage is the delivery step the photo is evidence for (SHIPPING or
	// DELIVERED).
	Stage string `json:"stage"`
	// URL is the displayable link to the stored image.
	URL string `json:"url"`
	// Width and Height are the stored pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Position is the order within its stage.
	Position int `json:"position"`
}

// FeedbackPhotoResponse is one image attached to the customer's order-level
// feedback as a client reads it (contracts/openapi.yaml, FeedbackPhoto). The
// feedback is multipart, so a client sends a photo as a `photos` file part and
// reads it back here (FR-025).
type FeedbackPhotoResponse struct {
	// ID identifies the photo.
	ID uuid.UUID `json:"id"`
	// URL is the displayable link to the stored image.
	URL string `json:"url"`
	// Width and Height are the stored pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Position is the order within the feedback.
	Position int `json:"position"`
}

// FeedbackResponse is the customer's order-level feedback as a client reads it
// (contracts/openapi.yaml, Feedback). It is present only on a detail answer and
// only once the order carries one (FR-015, FR-017, FR-025).
type FeedbackResponse struct {
	// Content is the feedback text.
	Content string `json:"content"`
	// CreatedAt is when it was written.
	CreatedAt time.Time `json:"createdAt"`
	// Photos are the images attached to the feedback, ordered by position; a
	// feedback with none serialises `photos: []` (FR-025).
	Photos []FeedbackPhotoResponse `json:"photos"`
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
// (contracts/openapi.yaml, OrderDetail). The list summary plus the address, the
// lines and the delivery facts; money is integer minor units plus the currency
// and the total carries no shipping fee (FR-008, ADR 015 A5). The delivery facts
// are detail-only: a list row never carries them (research D11).
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
	// DeliveredAt is when the operator marked the order delivered; null before
	// delivery (FR-010, research D11).
	DeliveredAt *time.Time `json:"deliveredAt"`
	// CompletedAt is when the order completed; null before then (FR-014,
	// research D11).
	CompletedAt *time.Time `json:"completedAt"`
	// DeliveryPhotos is the dispatch and delivery evidence, ordered by stage then
	// position; an order with none serialises `deliveryPhotos: []` (FR-010,
	// research D11).
	DeliveryPhotos []DeliveryPhotoResponse `json:"deliveryPhotos"`
	// Feedback is the customer's order-level feedback, or null (FR-015, FR-017,
	// research D11).
	Feedback *FeedbackResponse `json:"feedback"`
}

// AdminOrderSummaryResponse is one row of the administrator's order list
// (contracts/openapi.yaml, AdminOrderSummary): the customer summary plus the
// account that owns the order.
type AdminOrderSummaryResponse struct {
	// OrderSummaryResponse is the customer-facing summary.
	OrderSummaryResponse
	// UserID is the account that owns the order.
	UserID uuid.UUID `json:"userId"`
}

// AdminOrderResponse is one order as the administrator reads it in full
// (contracts/openapi.yaml, AdminOrderDetail): the customer detail plus the owner.
type AdminOrderResponse struct {
	// OrderResponse is the customer-facing detail.
	OrderResponse
	// UserID is the account that owns the order.
	UserID uuid.UUID `json:"userId"`
}
