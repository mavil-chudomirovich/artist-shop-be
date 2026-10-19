// Package implement holds the payment module's use cases. This file is US1: a
// customer starts (or reuses) a payment for their awaiting-payment order and
// reads it back. Later phases add the webhook, the cancellation and the operator
// list beside it.
package implement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// errNoActor reports a use case reached without a session behind the call. It
// cannot happen through the routes — the handler resolves the session first and
// answers UNAUTHENTICATED before calling — so it is a programming error that maps
// to INTERNAL_ERROR rather than a client-facing refusal.
var errNoActor = errors.New("payment: no acting account in the context")

// maxOrderCodeAttempts bounds the retries of a PayOS order-code collision. The
// code is a random 53-bit integer (research D4), so a collision is astronomically
// rare; the loop exists so the one-in-a-very-large-number case is retried rather
// than surfaced.
const maxOrderCodeAttempts = 3

// targetTypePayment is the audit target type for a payment.
const targetTypePayment = "payment"

// Service implements the payment module's use cases. It reaches the order module
// only through contracts.OrderPayment and the provider only through the Provider
// port; it never reads another module's tables (Constitution I).
type Service struct {
	// Payments persists and reads the payment and its event trail. It never
	// opens a transaction; the application owns every boundary (Constitution I).
	Payments appinterface.PaymentRepository
	// Orders answers an order's payment-relevant facts. It is supplied by
	// module 07's adapter at the composition root (research D5).
	Orders appinterface.OrderPayment
	// Provider creates, cancels and verifies through the payment gateway. It is
	// the only place a provider-specific type could appear (research D1–D3).
	Provider appinterface.Provider
	// Tx owns every transaction boundary, so the payment row and its event
	// commit together or not at all (Constitution II).
	Tx appinterface.UnitOfWork
	// Clock stamps the payment and its event. Only "what time is it" is
	// injected, never a rule.
	Clock appinterface.Clock
	// Audit records the module's payment events in the shared audit trail
	// (Constitution VI). A nil auditor is tolerated.
	Audit appinterface.Auditor
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
	// ReturnURL is where the provider redirects the customer after paying.
	ReturnURL string
	// CancelURL is where the provider redirects the customer when they cancel.
	CancelURL string
	// Logger receives the classified diagnostics an operator acts on — currently
	// the warning for a webhook whose order code matches no payment, whose link
	// is orphaned and must be reconciled out of band (research D7). A nil logger
	// is tolerated.
	Logger *slog.Logger
}

// New creates the payment use-case service.
func New(service Service) *Service { return &service }

// log returns the injected logger, falling back to the default so a construction
// without one cannot panic.
func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset.
func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// actor returns the acting account taken from the context the session filled.
func (s *Service) actor(ctx context.Context) (appinterface.Actor, error) {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return appinterface.Actor{}, errNoActor
	}
	return actor, nil
}

// record writes one audit entry naming the payment and its order. It is
// best-effort and never fails the operation (Constitution VI); a nil auditor is a
// no-op.
func (s *Service) record(ctx context.Context, action string, actor appinterface.Actor, paymentID, orderID uuid.UUID) {
	if s.Audit == nil {
		return
	}
	actorID := actor.ID
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess), &actorID, string(actor.Role),
		targetTypePayment, paymentID.String(), map[string]any{"orderId": orderID.String()})
}

// Start starts (or reuses) a payment for the caller's awaiting-payment order
// (FR-001 to FR-006). It reads the order through the OrderPayment contract and
// refuses one that is not awaiting payment (PAYMENT_ORDER_NOT_PAYABLE) or that
// belongs to someone else (answered as not-found, so the route never confirms
// another customer's order). When a live payment already exists it is reused and
// the same link is returned; a live payment whose link can no longer be returned
// is refused so an operator reconciles it. Two concurrent starts converge on one
// live payment: the storage's partial unique index refuses the second PENDING row
// (ErrLivePaymentExists) and the loser re-reads and reuses the winner's.
//
// The provider is called BEFORE the transaction that writes the row, so the
// payment's own transaction stays short and never spans the outbound HTTP call: a
// slow or failing provider cannot hold a database connection open or roll a
// transaction back around a live link (research D7). A provider failure therefore
// returns ErrProviderFailure with nothing written and the order unchanged
// (FR-006). Only the write of the PENDING row (with the order's content version
// and the provider's link) and of the PAYMENT_CREATED event runs inside the
// UnitOfWork, and they commit together or not at all (Constitution II).
func (s *Service) Start(ctx context.Context, in dto.StartInput) (dto.PaymentView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.PaymentView{}, err
	}

	target, err := s.Orders.PaymentTarget(ctx, in.OrderID)
	if err != nil {
		return dto.PaymentView{}, orderTargetError(err)
	}
	// The owner is always the session's; another customer's order answers the
	// same not-found an unknown order does (FR-003, FR-017).
	if target.UserID != actor.ID {
		return dto.PaymentView{}, domainerr.ErrOrderNotFound
	}
	if !target.AwaitingPayment {
		return dto.PaymentView{}, domainerr.ErrOrderNotPayable
	}

	// A live payment is reused (the 2026-10-11 clarification): a repeat start
	// returns the same link rather than creating a second request (FR-005). A
	// live row whose link can no longer be returned — its checkout URL is gone —
	// is refused so an operator reconciles it (PAYMENT_ALREADY_LIVE).
	live, err := s.Payments.FindLiveByOrder(ctx, in.OrderID)
	switch {
	case err == nil:
		return s.reuseLive(live)
	case !errors.Is(err, domainerr.ErrNotFound):
		return dto.PaymentView{}, err
	}

	// No live payment: create one. The provider call sits outside the
	// transaction (research D7); the write that follows commits the row and its
	// event together. A collision on the random order code retries with a fresh
	// one rather than surfacing (research D4). The link carries the order's own
	// payment deadline (payment_expires_at) as PayOS `expiredAt`, so the
	// provider-side link expires with the shop-side window; a zero deadline
	// (unreachable after the AwaitingPayment check above) omits the field.
	var expiredAt int64
	if !target.PaymentExpiresAt.IsZero() {
		expiredAt = target.PaymentExpiresAt.Unix()
	}
	for attempt := 0; attempt < maxOrderCodeAttempts; attempt++ {
		code, err := model.NewOrderCode()
		if err != nil {
			return dto.PaymentView{}, err
		}
		description := paymentDescription(code)
		link, err := s.Provider.CreateLink(ctx, dto.CreateLinkInput{
			OrderCode:   code,
			Amount:      target.Amount,
			Description: description,
			CancelURL:   s.CancelURL,
			ReturnURL:   s.ReturnURL,
			ExpiredAt:   expiredAt,
		})
		if err != nil {
			// The provider refused or was unreachable: nothing is written and
			// the order is unchanged (FR-006).
			return dto.PaymentView{}, domainerr.ProviderFailure(err)
		}

		payment := model.NewPayment(in.OrderID, actor.ID, code, target.Amount, target.Currency,
			description, target.Version, s.now())
		payment.PaymentLinkID = link.PaymentLinkID
		payment.CheckoutURL = link.CheckoutURL
		payment.QRCode = link.QRCode

		err = s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
			if err := s.Payments.Create(txCtx, payment); err != nil {
				return err
			}
			actorID := actor.ID
			return s.Payments.CreateEvent(txCtx, model.NewPaymentEvent(
				payment.ID, constant.AuditPaymentCreated, &actorID, description, s.now()))
		})
		if errors.Is(err, domainerr.ErrLivePaymentExists) {
			// A concurrent start created the order's live payment between our
			// read and our insert. The partial unique index refused our second
			// row; reuse the winner's live payment instead of failing, so two
			// concurrent starts converge on one link (FR-005, Constitution II).
			live, findErr := s.Payments.FindLiveByOrder(ctx, in.OrderID)
			if findErr != nil {
				return dto.PaymentView{}, findErr
			}
			return s.reuseLive(live)
		}
		if errors.Is(err, domainerr.ErrOrderCodeCollision) {
			continue
		}
		if err != nil {
			return dto.PaymentView{}, err
		}
		s.record(ctx, constant.AuditPaymentCreated, actor, payment.ID, payment.OrderID)
		return s.Mapper.Payment(*payment), nil
	}

	// Every attempt collided; the caller's retry is the code's own fallback.
	return dto.PaymentView{}, domainerr.ErrOrderCodeCollision
}

// reuseLive returns a live payment as the customer's view, refusing one whose
// checkout URL can no longer be returned so an operator reconciles it instead of
// a second link being made (FR-005). It is shared by the pre-check and by the
// recovery from a concurrent start that won the insert race.
func (s *Service) reuseLive(live *model.Payment) (dto.PaymentView, error) {
	if live.CheckoutURL == "" {
		return dto.PaymentView{}, domainerr.ErrAlreadyLive
	}
	return s.Mapper.Payment(*live), nil
}

// Read returns the caller's most recent payment for their order, in any state
// (FR-017): the customer sees the order's payment state, so a payment that has
// reached PAID, CANCELLED or UNMATCHED must still be readable. An order that
// does not exist, or belongs to another customer, answers the same not-found an
// unknown order does, so no route confirms another customer's order; an order of
// the caller's with no payment at all answers PAYMENT_NOT_FOUND.
func (s *Service) Read(ctx context.Context, in dto.ReadInput) (dto.PaymentView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.PaymentView{}, err
	}

	target, err := s.Orders.PaymentTarget(ctx, in.OrderID)
	if err != nil {
		return dto.PaymentView{}, orderTargetError(err)
	}
	if target.UserID != actor.ID {
		return dto.PaymentView{}, domainerr.ErrOrderNotFound
	}

	// FindByOrder returns the order's payments newest first (created_at then id),
	// so the first is the most recent. The read is not restricted to the live
	// state: a terminal payment is still the customer's payment state.
	payments, err := s.Payments.FindByOrder(ctx, in.OrderID)
	if err != nil {
		return dto.PaymentView{}, err
	}
	if len(payments) == 0 {
		return dto.PaymentView{}, domainerr.ErrNotFound
	}
	return s.Mapper.Payment(payments[0]), nil
}

// orderTargetError translates the OrderPayment contract's not-found into the
// module's own sentinel, so the use case classifies a refusal without importing
// the order module's error vocabulary (docs/system-design/contract-purity.md).
func orderTargetError(err error) error {
	if errors.Is(err, contracts.ErrOrderNotFound) {
		return domainerr.ErrOrderNotFound
	}
	return err
}

// paymentDescription builds the short description PayOS shows the payer. The
// provider documentation notes a nine-character limit for non-PayOS-linked bank
// accounts, so it carries a stable, short, code-derived reference (research D2).
func paymentDescription(orderCode int64) string {
	return fmt.Sprintf("DH%07d", orderCode%10000000)
}
