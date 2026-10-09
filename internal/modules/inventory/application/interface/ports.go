package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
)

// InventoryRepository is the persistence port the use cases depend on. It is the
// domain's contract, aliased here so this package names the dependency without
// declaring a second interface that could drift (Constitution I).
type InventoryRepository = repository.InventoryRepository

// ProductAvailability is the cross-module contract the inventory module consumes:
// the signal it sends the product module when a product's availability crosses
// zero. It is declared in internal/contracts and provided by module 04 at the
// composition root (FR-024, research D4).
type ProductAvailability = contracts.ProductAvailability

// ProductLookup is the cross-module contract that answers whether a product
// exists, so the read and decrease paths can answer not-found for an unknown
// product rather than a fabricated zero (FR-007, research D4, D12).
type ProductLookup = contracts.ProductLookup

// UnitOfWork runs a function inside a single database transaction. The application
// layer owns every transaction boundary; repositories never open one themselves
// (Constitution I).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Auditor records the module's mutations: every manual operation naming the acting
// administrator, and every outside event naming its source reference with no
// human actor (FR-006, FR-020, Constitution VI).
type Auditor interface {
	// Record emits one audit event. Emission never blocks the business operation
	// and never fails it: the shared writer queues, retries and reports a dropped
	// event through the logger instead.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// Clock reads the current time. It is the single seam that makes the hold window
// and the sweep deterministic in tests: the domain still computes with the
// time.Time values it is handed, and only "what time is it" is injected (research
// D15).
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// InventoryService is the module's use-case surface.
//
// The manual methods are reached only behind the administrator role guard and take
// the acting account from the context the session filled, never from the request
// (FR-005). The hold methods implement the capability module 07 will call; they
// are delivered and tested directly because the consumer does not exist yet
// (research D7).
type InventoryService interface {
	// Restock increases a product's physical stock by a positive quantity and
	// writes one RESTOCK movement (FR-001).
	Restock(ctx context.Context, in dto.RestockInput) (dto.StockOutput, error)
	// Damage decreases a product's physical stock, refusing a decrease that would
	// take it below zero or below what is held (FR-002, FR-009).
	Damage(ctx context.Context, in dto.DamageInput) (dto.StockOutput, error)
	// Adjust corrects a product's physical stock to a recounted value, recording
	// only the signed difference (FR-003).
	Adjust(ctx context.Context, in dto.AdjustmentInput) (dto.StockOutput, error)
	// Stock reads one product's physical, held and available quantities. An
	// unknown product answers not-found (FR-007).
	Stock(ctx context.Context, in dto.StockRefInput) (dto.StockOutput, error)
	// History returns one page of a product's movements, oldest first (FR-008).
	History(ctx context.Context, in dto.HistoryInput) (dto.MovementPage, error)

	// Reserve holds a quantity for an order while it is being paid (FR-014).
	Reserve(ctx context.Context, in dto.ReserveInput) error
	// Release returns an active hold's quantity to availability (FR-017).
	Release(ctx context.Context, in dto.ReleaseInput) error
	// ExpireHolds releases every hold whose window has passed. It is the use case
	// the sweeper calls (FR-015, research D6).
	ExpireHolds(ctx context.Context) error
	// ApplySale consumes a held order's hold and decreases the physical stock
	// exactly once, keyed by the payment event's source reference (FR-016, FR-020).
	ApplySale(ctx context.Context, in dto.SaleInput) error
}
