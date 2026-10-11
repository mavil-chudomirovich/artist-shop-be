// Package appinterface declares the payment module's use-case interface and the
// ports the application depends on: the repository, the UnitOfWork, the injected
// clock, the auditor, the provider port, and the cross-module contract it
// consumes.
package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/repository"
)

// PaymentRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type PaymentRepository = repository.PaymentRepository

// OrderPayment is the cross-module contract the payment consumes to read an
// order's payment-relevant facts and drive its version-aware paid transition. It
// is declared in internal/contracts and provided by module 07's adapter at the
// composition root (research D5).
type OrderPayment = contracts.OrderPayment

// PaymentCancellation is the cross-module contract the payment module provides to
// module 07, so a cancelled order cancels its provider link. Its adapter is
// supplied at the composition root (research D9).
type PaymentCancellation = contracts.PaymentCancellation

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open one
// themselves (Constitution I). Applying a payment, the order's paid transition and
// the sale commit together, or not at all (Constitution II).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock reads the current time. It is the seam that makes the module's timestamps
// deterministic in tests; only "what time is it" is injected, never a rule.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Auditor records the module's payment events: created, applied, cancelled and
// unmatched, each naming the payment and the order (FR-022, Constitution VI). A
// nil auditor is tolerated so a construction without one cannot panic.
type Auditor interface {
	// Record emits one audit event. Emission never blocks the business operation
	// and never fails it.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// Provider is the payment provider port. It is the only place a provider-specific
// type could appear, and here there are none: the port carries the module's own
// DTOs, so the provider the module uses never leaks into the domain or HTTP layers
// (Constitution, research D1, D2, D3).
//
// The verification of a webhook is part of this port because the provider is the
// one component that owns the checksum key and the signature algorithm; the
// webhook use case verifies through the port before it trusts anything (research
// D3).
type Provider interface {
	// CreateLink asks the provider to create a payment link and returns its
	// handle and URLs. A refusal or an unreachable provider is reported as an
	// error; nothing is written when it fails (FR-002, research D2).
	CreateLink(ctx context.Context, in dto.CreateLinkInput) (dto.PaymentLink, error)

	// CancelLink asks the provider to cancel a live link. The call is best-effort
	// from the order's point of view (research D9).
	CancelLink(ctx context.Context, in dto.CancelLinkInput) error

	// VerifyWebhook verifies a raw webhook body's signature and returns the
	// verified data fields. A body whose signature does not verify is refused and
	// nothing is applied (FR-019, research D3).
	VerifyWebhook(ctx context.Context, body []byte) (dto.WebhookInput, error)
}

// PaymentService is the module's use-case surface.
//
// Every method that reads or changes a customer's payment takes the acting account
// from the context the session filled, never from its input (FR-017). CancelForOrder
// is the method module 07 drives through the PaymentCancellation contract; it is
// best-effort and reports nothing.
type PaymentService interface {
	// Start starts (or reuses) a payment for the caller's awaiting-payment order
	// and returns it with its link (FR-001 to FR-003).
	Start(ctx context.Context, in dto.StartInput) (dto.PaymentView, error)
	// Read returns the caller's most recent payment for their order, in any
	// state, or not-found when the order has none (FR-017).
	Read(ctx context.Context, in dto.ReadInput) (dto.PaymentView, error)
	// HandleWebhook applies a verified confirmation exactly once, or records it
	// UNMATCHED when it cannot be applied (FR-009, FR-012, FR-019).
	HandleWebhook(ctx context.Context, in dto.WebhookInput) error
	// CancelForOrder cancels the live payment of a cancelled order, best-effort.
	// It is driven by module 07 after the order's transaction commits (FR-015,
	// research D9).
	CancelForOrder(ctx context.Context, orderID uuid.UUID)
	// ListAll returns one page of every payment, newest first, optionally
	// narrowed to one state; status=UNMATCHED is the reconciliation queue
	// (FR-018).
	ListAll(ctx context.Context, in dto.ListInput) (dto.PaymentPage, error)
}
