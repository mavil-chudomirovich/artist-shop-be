// Package appinterface declares the order module's use-case interface and the
// ports the application depends on: the repository, the UnitOfWork, the injected
// clock, the auditor, and the cross-module contracts it consumes.
package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/repository"
)

// OrderRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type OrderRepository = repository.OrderRepository

// CartCheckout is the cross-module contract the order consumes to read and empty
// the customer's cart. It is declared in internal/contracts and provided by
// module 06 at the composition root (research D1).
type CartCheckout = contracts.CartCheckout

// ProductCatalog is the cross-module contract the order consumes for a line's
// current name, on-sale state and price. It is declared in internal/contracts and
// provided by module 04 at the composition root (research D2).
type ProductCatalog = contracts.ProductCatalog

// InventoryAvailability is the cross-module contract the order consumes for what
// is currently available. It is declared in internal/contracts and provided by
// module 05 at the composition root (research D2).
type InventoryAvailability = contracts.InventoryAvailability

// InventoryReservation is the cross-module contract the order consumes to hold,
// consume and release goods. It is declared in internal/contracts and provided by
// module 05 at the composition root; it is the contract feature 007's deferred D1
// withheld until this consumer existed (research D4).
type InventoryReservation = contracts.InventoryReservation

// CustomerLookupService is the cross-module contract the order consumes for the
// customer's addresses. It is declared in internal/contracts and provided by
// module 02 at the composition root (research D7).
type CustomerLookupService = contracts.CustomerLookupService

// AccountLookup is the cross-module contract the order consumes to resolve a
// transfer recipient by email. It is declared in internal/contracts and provided
// by module 01 at the composition root (research D8).
type AccountLookup = contracts.AccountLookup

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open one
// themselves (Constitution I). A checkout, a transition and a transfer each run
// inside one so the order, its lines and module 05's stock effect commit together
// or not at all (Constitution II).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock reads the current time. It is the seam that makes an order's expiry
// deterministic in tests; only "what time is it" is injected, never a rule
// (research D15).
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Auditor records the module's administrative mutations: ship, complete and
// transfer, each naming the acting administrator and the order (FR-023,
// Constitution VI).
type Auditor interface {
	// Record emits one audit event. Emission never blocks the business operation
	// and never fails it: the shared writer queues, retries and reports a dropped
	// event through the logger instead.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// Notifier sends the order module's emails: the artist's confirmation-needed note
// (on checkout and on an edit that leaves the order awaiting confirmation) and the
// customer's status-change note. Sending is best-effort: it runs after the
// transaction has committed, and a failure is logged by the adapter, never
// propagated, so email can never fail a business operation (FR-019 to FR-021,
// research D9).
type Notifier interface {
	// Send delivers one message to the given recipient. The adapter logs a failure
	// instead of returning it, so a caller never fails an operation because an
	// email could not be delivered (FR-021).
	Send(ctx context.Context, to, subject, body string) error
}

// OrderService is the module's use-case surface.
//
// Every method that reads or changes a customer's order takes the acting account
// from the context the session filled, never from its input, so the order a
// customer reaches is always their own (FR-020). The lifecycle methods are the
// module's own transitions, driven by module 08 (paid), the customer (cancel) and
// an administrator (ship, complete). The paid transition has no HTTP surface:
// module 08 drives it (research D13).
type OrderService interface {
	// Checkout turns the caller's cart into an order: it re-checks every line,
	// snapshots the lines and the delivery address, holds the goods, creates the
	// order and empties the cart, all in one transaction (FR-001 to FR-007,
	// FR-013, FR-017).
	Checkout(ctx context.Context, in dto.CheckoutInput) (dto.OrderView, error)

	// MarkPaid turns an awaiting-payment order into a paid one, consuming its
	// hold into a sale exactly once (FR-014, research D13).
	MarkPaid(ctx context.Context, orderID uuid.UUID, sourceReference string) error
	// Cancel cancels an awaiting-payment order and returns its goods exactly once
	// (FR-015, FR-019).
	Cancel(ctx context.Context, orderID uuid.UUID) error
	// Ship moves a paid order to shipped (FR-022).
	Ship(ctx context.Context, orderID uuid.UUID) error
	// Complete moves a shipped order to completed (FR-022).
	Complete(ctx context.Context, orderID uuid.UUID) error

	// ExpireOrders cancels every awaiting-payment order whose window has passed
	// and returns its goods, exactly once, so an unpaid order cannot hold stock
	// forever. It is the use case the expiry sweeper calls (FR-012, research D6).
	ExpireOrders(ctx context.Context) error

	// ListMine returns one page of the caller's own orders, newest first
	// (FR-018).
	ListMine(ctx context.Context, in dto.ListInput) (dto.OrderPage, error)
	// GetMine reads one of the caller's own orders in full; another customer's
	// identifier answers not-found (FR-018, FR-020).
	GetMine(ctx context.Context, in dto.OrderRefInput) (dto.OrderView, error)
	// CancelMine cancels one of the caller's own orders that is still awaiting
	// payment, and returns it (FR-019, FR-020).
	CancelMine(ctx context.Context, in dto.OrderRefInput) (dto.OrderView, error)

	// ListAll returns one page of every order, newest first, with its owner
	// (FR-021).
	ListAll(ctx context.Context, in dto.ListInput) (dto.AdminOrderPage, error)
	// GetByIDAdmin reads any order in full (FR-021).
	GetByIDAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error)
	// ConfirmByAdmin accepts an order awaiting the artist, holds the whole order
	// all-or-nothing, opens the payment window and records the act (FR-004,
	// FR-005, FR-024).
	ConfirmByAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error)
	// ShipByAdmin moves a paid order to shipped and records the act (FR-022,
	// FR-023).
	ShipByAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error)
	// CompleteByAdmin moves a shipped order to completed and records the act
	// (FR-022, FR-023).
	CompleteByAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error)

	// Transfer hands a paid order to another existing account, named by email,
	// changing only the owner, and records the act (FR-024).
	Transfer(ctx context.Context, in dto.TransferInput) (dto.AdminOrderView, error)
}
