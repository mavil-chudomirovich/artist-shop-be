// Package repository declares the order module's persistence port. The concrete
// implementation (infrastructure/implement/postgres) embeds the generic
// share/repository.Base and satisfies this interface. No repository method opens a
// transaction: the application layer owns the boundary through the UnitOfWork
// port (Constitution I).
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// OrderRepository persists the order and its snapshot lines.
//
// It does not embed share/repository.Repository[model.Order, uuid.UUID]: the
// generic reads carry no owner predicate, the ordered line load is not entity
// CRUD, and the status and owner writes are narrow column updates rather than a
// whole-entity update that could overwrite a concurrent transition. Every method
// is stated explicitly; the adapter embeds share/repository.Base only for the
// write plumbing. No method opens a transaction: each joins the transaction the
// application put in the context (Constitution I).
type OrderRepository interface {
	// Create inserts the order and its lines. It joins the caller's transaction,
	// so the order and the lines commit together or not at all (FR-001,
	// Constitution II). A missing owner and a missing product are deliberately
	// not enforced by a foreign key (research D3, D12).
	Create(ctx context.Context, order *model.Order) error

	// FindByID returns one order with its lines in position order, or
	// domainerr.ErrNotFound. It is the operator's read (FR-021). It joins the
	// caller's transaction.
	FindByID(ctx context.Context, id uuid.UUID) (*model.Order, error)

	// FindByOwner returns one order with its lines in position order only when it
	// belongs to ownerID, or domainerr.ErrNotFound. An order that belongs to
	// another customer answers the same not-found an unknown one does, so the
	// route never confirms another customer's order (FR-020). It joins the
	// caller's transaction.
	FindByOwner(ctx context.Context, ownerID, id uuid.UUID) (*model.Order, error)

	// ListOwner returns one page of one owner's orders, newest first (created_at
	// then id), and the total number of them (FR-018, research D14). It joins the
	// caller's transaction.
	ListOwner(ctx context.Context, ownerID uuid.UUID, page, size int) ([]model.OrderSummary, int64, error)

	// ListAll returns one page of every order, optionally narrowed to one state
	// and ordered as asked, and the total number of them. The default (no status,
	// SortNewest) is newest first (created_at then id), the operator's general
	// list; status=StatusPending with SortOldest is the FIFO confirmation queue
	// (FR-021, FR-026, research D11). It joins the caller's transaction.
	ListAll(ctx context.Context, page, size int, status *constant.Status, sort constant.OrderListSort) ([]model.OrderSummary, int64, error)

	// ListExpiredPending returns the identifiers of the orders awaiting payment
	// (PAYMENT_PENDING) whose deadline (payment_expires_at) has passed at the
	// given instant, oldest deadline first, so the expiry sweep is reproducible.
	// An order awaiting the artist has no deadline and is never selected
	// (FR-008, FR-009, research D5). The comparison is against the instant the
	// caller passed, never the database clock, so the order's sweep and the
	// inventory's agree on what has expired. It joins the caller's transaction.
	ListExpiredPending(ctx context.Context, now time.Time) ([]uuid.UUID, error)

	// LockByID returns one order with its lines under the order's row lock, for a
	// transition. The lock serialises two concurrent moves on the same order so
	// the domain transition reads the current state under it (FR-010, research
	// D10). It joins the caller's transaction.
	LockByID(ctx context.Context, id uuid.UUID) (*model.Order, error)

	// UpdateStatus writes one order's state and touches its updated_at. The state
	// is written only by the transition use cases, after the domain transition
	// has accepted the move, so the row always carries a state the domain allows
	// (FR-010). It joins the caller's transaction.
	UpdateStatus(ctx context.Context, id uuid.UUID, status constant.Status, now time.Time) error

	// SaveConfirm persists a confirmed order: its new state, its bumped content
	// version, the confirmation instant and the payment deadline, touching
	// updated_at. It is the confirmation's single write, after every line has
	// been held in the same transaction (FR-005, FR-006, research D3). It joins
	// the caller's transaction.
	SaveConfirm(ctx context.Context, order *model.Order, now time.Time) error

	// ReplaceLines replaces one order's lines with the given set (delete the old
	// set, insert the new one) and persists the order's total, address, state,
	// content version and deadline, touching updated_at. It is the edit's single
	// write (FR-015, FR-016, research D6). It joins the caller's transaction.
	ReplaceLines(ctx context.Context, order *model.Order, now time.Time) error

	// InsertEditHistory records one accepted edit's before/after content snapshot,
	// the actor and the version, in the same transaction as the edit (FR-017,
	// research D14). It joins the caller's transaction.
	InsertEditHistory(ctx context.Context, record model.EditHistory) error

	// UpdateOwner changes one order's owner and touches its updated_at. A
	// transfer changes only the owner; the state, the lines and the totals are
	// untouched (FR-024, research D12). It joins the caller's transaction.
	UpdateOwner(ctx context.Context, id, ownerID uuid.UUID, now time.Time) error
}
