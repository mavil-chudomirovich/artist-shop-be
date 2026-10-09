package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// InventoryRepository persists the three inventory facts: the live physical level,
// the append-only ledger and the holds. The concrete implementation
// (infrastructure/implement/postgres) embeds the generic share/repository.Base and
// satisfies this interface.
//
// Concurrency is the whole reason the level is a row of its own. A decrease is a
// conditional update performed by the storage, so two writers cannot both observe
// the same quantity and both write; a hold is created under the level's row lock,
// so two holds cannot set aside the same unit (research D2). No method opens a
// transaction: the application owns the boundary through the UnitOfWork port, and
// every method here joins the transaction the context already carries
// (Constitution I).
//
// The instant the injected clock produced is a parameter on every method whose
// answer depends on what has expired, and it is never the database's own now(), so
// the sweeper and the availability sum agree on what "expired" means (research
// D15).
type InventoryRepository interface {
	// Level returns the stored physical quantity of a product. A product with no
	// row answers zero, because a product that has never been stocked is
	// understood as zero rather than not-found (research D12). Existence is
	// answered by the ProductLookup contract, not here.
	Level(ctx context.Context, productID uuid.UUID) (int64, error)

	// Increase adds a positive amount to a product's physical quantity,
	// materialising the level row on the first call, and returns the new quantity.
	//
	// The upsert is what makes a level row appear lazily (research D12). A
	// product no row carries is reported as domainerr.ErrProductNotFound through
	// the foreign key, which is the storage guard for the one path that writes a
	// row.
	Increase(ctx context.Context, productID uuid.UUID, amount int64, now time.Time) (int64, error)

	// Decrease removes a positive amount from a product's physical quantity and
	// returns the new quantity.
	//
	// It is a conditional update — `WHERE product_id = $1 AND quantity >= $2` — so
	// the no-negative rule is evaluated by the storage against the current value
	// and no application-level read-then-write race exists (research D2). An update
	// that matches no row is reported as domainerr.ErrInsufficientStock, which is
	// what lets the use case answer 409 rather than 500 (FR-010).
	Decrease(ctx context.Context, productID uuid.UUID, amount int64, now time.Time) (int64, error)

	// LockLevel takes the level's row lock inside the caller's transaction, so the
	// availability a hold is checked against cannot be read by two concurrent
	// reserves as the same value (research D2). A product with no level row has
	// nothing to lock and answers success, because its availability is zero and no
	// hold can be placed against it.
	LockLevel(ctx context.Context, productID uuid.UUID) error

	// InsertMovement appends one immutable ledger row. A source reference another
	// movement already carries is refused by the partial unique index; the
	// translation of that refusal into an "already applied" outcome belongs to the
	// use case that installs it (FR-020, FR-022). A product no row carries is
	// reported as domainerr.ErrProductNotFound.
	InsertMovement(ctx context.Context, movement *model.Movement) error

	// Movements returns one page of a product's physical changes, oldest first,
	// ordered by created_at then id so the order is stable across two reads sharing
	// a timestamp (FR-008, research D14), plus the total number of them.
	Movements(ctx context.Context, productID uuid.UUID, page, pageSize int) ([]model.Movement, int64, error)

	// InsertHold records one active hold. The partial unique index on the active
	// (order_id, product_id) pair is what refuses a second hold for the same order
	// and product, reported as domainerr.ErrHoldAlreadyExists so the reserve use
	// case can treat it as already applied rather than as a failure (FR-019); a
	// product no row carries is reported as domainerr.ErrProductNotFound.
	InsertHold(ctx context.Context, hold *model.Hold) error

	// ActiveHeld returns the total quantity a product's holds currently set aside:
	// those that are ACTIVE and whose expiry is still in the future at the given
	// instant. An expired hold that the sweeper has not yet reached is excluded, so
	// reads agree with the sweeper on what "expired" means (FR-018, research D6).
	ActiveHeld(ctx context.Context, productID uuid.UUID, now time.Time) (int64, error)

	// FindActiveHold returns the active hold of one order and product, reporting
	// whether one exists rather than a not-found, so a caller can distinguish
	// "nothing to release" from a storage failure. An expired hold is not active
	// even before it is swept (FR-015, FR-019, research D6).
	FindActiveHold(ctx context.Context, orderID, productID uuid.UUID, now time.Time) (*model.Hold, bool, error)

	// ResolveHold ends one hold with the given status and moment. It only touches a
	// hold that is still ACTIVE, so a retried release or consume cannot resolve the
	// same hold twice (FR-019). A hold that is already resolved is reported as
	// domainerr.ErrInvalidValue naming the hold.
	ResolveHold(ctx context.Context, holdID uuid.UUID, status constant.HoldStatus, at time.Time) error

	// ExpiredHolds returns the active holds whose expiry has passed at the given
	// instant, so the sweeper can release them (FR-015, research D6). The order is
	// the expiry, then id, so a sweep is reproducible.
	ExpiredHolds(ctx context.Context, now time.Time) ([]model.Hold, error)
}
