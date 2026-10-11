package dto

import (
	"time"

	"github.com/google/uuid"
)

// MoneyResponse is money as an integer amount in the currency's minor unit plus
// its currency, the shape contracts/openapi.yaml shows (Constitution II).
type MoneyResponse struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code.
	Currency string `json:"currency"`
}

// PaymentResponse is one payment as the customer reads it
// (contracts/openapi.yaml, Payment). It is the shape both the start and the read
// answer with. `checkoutUrl` and `qrCode` are present while the payment is live;
// `unmatchedReason` is present only when it is UNMATCHED.
type PaymentResponse struct {
	// ID identifies the payment.
	ID uuid.UUID `json:"id"`
	// OrderID is the order the payment belongs to.
	OrderID uuid.UUID `json:"orderId"`
	// Amount is the amount requested.
	Amount MoneyResponse `json:"amount"`
	// Status is where the payment is in its life.
	Status string `json:"status"`
	// Provider is the provider name.
	Provider string `json:"provider"`
	// OrderCode is the provider's order code. Identifying only.
	OrderCode int64 `json:"orderCode"`
	// CheckoutURL is the provider's checkout URL the customer opens.
	CheckoutURL string `json:"checkoutUrl"`
	// QRCode is the VietQR payload for QR rendering.
	QRCode string `json:"qrCode"`
	// UnmatchedReason is why a confirmation could not be applied.
	UnmatchedReason string `json:"unmatchedReason"`
	// CreatedAt is when the payment was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the payment was last written.
	UpdatedAt time.Time `json:"updatedAt"`
}

// AdminPaymentResponse is one payment as the operator reads it
// (contracts/openapi.yaml, AdminPayment): the customer payment plus the owner and
// the provider's transaction reference. It is the row shape of the operator's
// list, where `status=UNMATCHED` is the reconciliation queue (FR-018).
type AdminPaymentResponse struct {
	// PaymentResponse is the customer-facing payment.
	PaymentResponse
	// UserID is the account that started the payment.
	UserID uuid.UUID `json:"userId"`
	// ProviderReference is the provider's transaction reference once it confirmed.
	ProviderReference string `json:"providerReference"`
}
