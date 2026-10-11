package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
)

// StartInput starts (or reuses) a payment for the caller's order. The owner is
// taken from the session, never from this input (FR-017).
type StartInput struct {
	// OrderID is the order to pay for.
	OrderID uuid.UUID
}

// ReadInput reads the caller's live payment for their order. The owner is taken
// from the session, never from this input (FR-017).
type ReadInput struct {
	// OrderID is the order whose payment is read.
	OrderID uuid.UUID
}

// ListInput pages through the operator's payment list. The zero Status means "every
// payment"; StatusUnmatched is the reconciliation queue (FR-018, research D15).
type ListInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of payments per page.
	PageSize int
	// Status, when set, returns only payments in that state.
	Status *constant.Status
}

// WebhookInput carries the provider's verified fields — nothing here is trusted
// until the signature has verified (research D3). It is what the provider port
// returns and what the webhook use case consumes.
type WebhookInput struct {
	// OrderCode is the provider's order code that identifies the payment.
	OrderCode int64
	// Amount is the confirmed amount, to be compared with the stored amount.
	Amount int64
	// Description is the provider's description of the payment.
	Description string
	// Reference is the provider's transaction reference — the idempotency key.
	Reference string
	// TransactionDateTime is when the provider recorded the transaction.
	TransactionDateTime string
	// Currency is the confirmed currency.
	Currency string
	// PaymentLinkID is the provider's link handle.
	PaymentLinkID string
	// AccountNumber is the payer's account, as the provider reported it.
	AccountNumber string
	// Code and Desc are the provider's own status pair.
	Code string
	Desc string
}

// MoneyView is money as an integer amount in the currency's minor unit plus the
// currency, the shape a response shows (Constitution II).
type MoneyView struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency code.
	Currency string
}

// PaymentView is one payment as the customer reads it (FR-017).
type PaymentView struct {
	// ID identifies the payment.
	ID uuid.UUID
	// OrderID is the order the payment belongs to.
	OrderID uuid.UUID
	// Amount is the amount requested.
	Amount MoneyView
	// Status is where the payment is in its life.
	Status constant.Status
	// Provider is the provider name.
	Provider string
	// OrderCode is the provider's order code. Identifying only.
	OrderCode int64
	// CheckoutURL is the provider's checkout URL. Present while the payment is
	// live.
	CheckoutURL string
	// QRCode is the VietQR payload for QR rendering. Present while the payment is
	// live.
	QRCode string
	// UnmatchedReason is why a confirmation could not be applied. Present only
	// when UNMATCHED.
	UnmatchedReason string
	// CreatedAt is when the payment was created.
	CreatedAt time.Time
	// UpdatedAt is when the payment was last written.
	UpdatedAt time.Time
}

// AdminPaymentView is one payment as the operator reads it: the customer view plus
// the owner and the provider reference (FR-018).
type AdminPaymentView struct {
	// PaymentView is the customer-facing view.
	PaymentView
	// UserID is the account that started the payment.
	UserID uuid.UUID
	// ProviderReference is the provider's transaction reference once it confirmed.
	ProviderReference string
}

// PaymentPage is one page of the operator's payments.
type PaymentPage struct {
	// Payments holds the page's payments, newest first.
	Payments []AdminPaymentView
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of payments the shop has.
	Total int64
}

// CreateLinkInput is what the provider port is asked to create. The signature is
// computed by the adapter from these fields (research D2).
type CreateLinkInput struct {
	// OrderCode is the provider's order code.
	OrderCode int64
	// Amount is the amount to request, in the currency's minor unit.
	Amount int64
	// Description is the short description the payer sees.
	Description string
	// CancelURL is where the provider redirects when the customer cancels.
	CancelURL string
	// ReturnURL is where the provider redirects after payment.
	ReturnURL string
}

// PaymentLink is what the provider returns when a link is created.
type PaymentLink struct {
	// PaymentLinkID is the provider's link handle, used to read and cancel the
	// link.
	PaymentLinkID string
	// CheckoutURL is the URL the customer opens.
	CheckoutURL string
	// QRCode is the VietQR payload for QR rendering.
	QRCode string
}

// CancelLinkInput is what the provider port is asked to cancel.
type CancelLinkInput struct {
	// OrderCode is the provider's order code.
	OrderCode int64
	// PaymentLinkID is the provider's link handle.
	PaymentLinkID string
	// Reason is why the link is being cancelled.
	Reason string
}
