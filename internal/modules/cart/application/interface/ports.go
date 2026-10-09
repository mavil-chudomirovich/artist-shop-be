// Package appinterface declares the cart module's use-case interface and the
// ports the application depends on: the repository, the two cross-module
// contracts the cart consumes, the UnitOfWork the writes need, the Clock, and the
// Actor the session supplies.
package appinterface

import (
	"context"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
)

// CartRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type CartRepository = repository.CartRepository

// ProductCatalog is the cross-module contract the cart consumes for a product's
// name, on-sale state and price. It is declared in internal/contracts and
// provided by module 04 at the composition root (research D1).
type ProductCatalog = contracts.ProductCatalog

// InventoryAvailability is the cross-module contract the cart consumes for what
// is currently available. It is declared in internal/contracts and provided by
// module 05 at the composition root (research D2). It is a read, not the
// reservation contract module 05's deferred.md D1 still owes to the order flow.
type InventoryAvailability = contracts.InventoryAvailability

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open one
// themselves (Constitution I). An add or a quantity change runs inside one so the
// cart's row lock, the line write and the availability decision commit together
// or not at all (research D9).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock reads the current time. It is the seam that stamps the cart's writes
// deterministically in tests; only "what time is it" is injected, never a rule.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// CartService is the module's use-case surface.
//
// Every method takes the acting customer from the context the session filled,
// never from its input, so the cart an operation reaches is always the caller's
// own (FR-010). The cart never reserves or holds stock: no method here reaches an
// inventory write (FR-011, research D12).
type CartService interface {
	// Get returns the caller's cart, re-checking every line against the product's
	// current facts. A customer with no cart answers an empty cart rather than an
	// error, and reading never creates a row (FR-004, FR-012, research D8).
	Get(ctx context.Context, in dto.GetCartInput) (dto.CartView, error)
	// Add adds a product with a positive whole quantity, creating the cart if it
	// is absent and raising an existing line rather than adding a second one
	// (FR-001, FR-002).
	Add(ctx context.Context, in dto.AddItemInput) (dto.CartView, error)
	// ChangeQuantity sets the line's quantity, refusing a product that is not on
	// sale and a quantity above what is available (FR-003, FR-005, FR-007).
	ChangeQuantity(ctx context.Context, in dto.ChangeQuantityInput) (dto.CartView, error)
	// Remove deletes one line. A line this cart does not hold answers not-found
	// (FR-003, error-codes.md).
	Remove(ctx context.Context, in dto.RemoveItemInput) error
}
