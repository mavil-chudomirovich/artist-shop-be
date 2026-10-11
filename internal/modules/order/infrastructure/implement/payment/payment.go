// Package payment adapts module 07's order use cases and repository to the
// cross-module contracts.OrderPayment interface, so the payment module (08) can
// read an order's payment-relevant facts and drive its version-aware paid
// transition without ever reading the order's tables (Constitution I, research D5).
//
// It is the only code that crosses the order→payment boundary: the payment module
// depends on the interface in internal/contracts, and this adapter — supplied at
// the composition root — is what satisfies it.
package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// Adapter implements contracts.OrderPayment over the order module's own use-case
// surface and repository. The use cases own the version-aware paid transition and
// the per-line sale; the repository answers the payment-relevant facts.
type Adapter struct {
	orders  appinterface.OrderRepository
	service appinterface.OrderService
}

var _ contracts.OrderPayment = (*Adapter)(nil)

// New creates the adapter over the order repository and the order use-case
// surface. Both are the module's own ports; no other module is imported.
func New(orders appinterface.OrderRepository, service appinterface.OrderService) *Adapter {
	return &Adapter{orders: orders, service: service}
}

// PaymentTarget returns the facts a payment needs about an order: its owner, its
// total (the order total, with no shipping fee), its content version, its payment
// deadline and whether it is awaiting payment. A missing order answers
// contracts.ErrOrderNotFound, so the payment module can answer not-found without
// importing the order module's error vocabulary.
func (a *Adapter) PaymentTarget(ctx context.Context, orderID uuid.UUID) (contracts.OrderPaymentTarget, error) {
	order, err := a.orders.FindByID(ctx, orderID)
	if err != nil {
		if errors.Is(err, domainerr.ErrNotFound) {
			return contracts.OrderPaymentTarget{}, contracts.ErrOrderNotFound
		}
		return contracts.OrderPaymentTarget{}, err
	}

	target := contracts.OrderPaymentTarget{
		UserID:          order.UserID,
		Amount:          order.Total.Amount,
		Currency:        order.Total.Currency,
		Version:         order.Version,
		AwaitingPayment: order.Status == constant.StatusPaymentPending,
	}
	if order.PaymentExpiresAt != nil {
		target.PaymentExpiresAt = *order.PaymentExpiresAt
	}
	return target, nil
}

// MarkPaid drives the order's version-aware paid transition through module 07's
// use case. Module 07's own refusals are translated onto the contract's sentinels
// so the payment module can classify them without importing the order module.
func (a *Adapter) MarkPaid(ctx context.Context, orderID uuid.UUID, sourceReference string, expectedVersion int64) error {
	if err := a.service.MarkPaid(ctx, orderID, sourceReference, expectedVersion); err != nil {
		return translateMarkPaidError(err)
	}
	return nil
}

// translateMarkPaidError maps module 07's refusals onto the contract's sentinels
// (docs/system-design/contract-purity.md). The original refusal is kept wrapped so
// a caller that only needs the class can use errors.Is, and one that wants the
// detail (the two versions, the current state) can read the message.
func translateMarkPaidError(err error) error {
	switch {
	case errors.Is(err, domainerr.ErrNotFound):
		return contracts.ErrOrderNotFound
	case errors.Is(err, domainerr.ErrStalePayment):
		return fmt.Errorf("%w: %v", contracts.ErrOrderStale, err)
	case errors.Is(err, domainerr.ErrStateTransitionInvalid):
		return fmt.Errorf("%w: %v", contracts.ErrOrderNotPayable, err)
	default:
		return err
	}
}
