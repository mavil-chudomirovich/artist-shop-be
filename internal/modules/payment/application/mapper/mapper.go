package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
)

// Mapper converts payment domain models into application DTOs. It is the single
// place a model becomes a DTO and carries no other dependency, so a change to one
// shape cannot silently reach the other. Money is carried as an integer minor-unit
// value plus its currency, never converted through a float (Constitution II).
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Money maps an amount and its currency to the DTO shape.
func (m *Mapper) Money(amount int64, currency string) dto.MoneyView {
	return dto.MoneyView{Amount: amount, Currency: currency}
}

// Payment maps one payment to the customer-facing shape (FR-017).
func (m *Mapper) Payment(payment model.Payment) dto.PaymentView {
	return dto.PaymentView{
		ID:              payment.ID,
		OrderID:         payment.OrderID,
		Amount:          m.Money(payment.Amount, payment.Currency),
		Status:          payment.Status,
		Provider:        payment.Provider,
		OrderCode:       payment.OrderCode,
		CheckoutURL:     payment.CheckoutURL,
		QRCode:          payment.QRCode,
		UnmatchedReason: payment.UnmatchedReason,
		CreatedAt:       payment.CreatedAt,
		UpdatedAt:       payment.UpdatedAt,
	}
}

// AdminPayment maps one payment to the operator-facing shape, adding the owner and
// the provider reference (FR-018).
func (m *Mapper) AdminPayment(payment model.Payment) dto.AdminPaymentView {
	return dto.AdminPaymentView{
		PaymentView:       m.Payment(payment),
		UserID:            payment.UserID,
		ProviderReference: payment.ProviderReference,
	}
}

// AdminPayments maps a page of payments to the operator-facing shapes, preserving
// the newest-first order the repository applied. A nil list maps to an empty slice,
// never a nil one.
func (m *Mapper) AdminPayments(payments []model.Payment) []dto.AdminPaymentView {
	out := make([]dto.AdminPaymentView, 0, len(payments))
	for _, payment := range payments {
		out = append(out, m.AdminPayment(payment))
	}
	return out
}
