// Package repository declares the cart module's repository port. The concrete
// implementation (infrastructure/implement/postgres) embeds the generic
// share/repository.Base and satisfies this interface.
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port. An add or a quantity change runs inside a
// transaction that locks the cart's row (research D9), but the lock is a statement
// this adapter runs inside the caller's transaction, never a transaction of its
// own.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
)

// CartRepository persists the cart and its lines.
//
// It does not embed share/repository.Repository[model.CartLine, uuid.UUID]: the
// generic FindAll has no cart predicate, the generic Update cannot classify a
// missing line into the not-found sentinel the use cases answer with, and the
// upsert that sums a repeated product's quantity is not entity CRUD at all. Every
// method is stated explicitly, and the adapter embeds share/repository.Base only
// for the write plumbing, with an explicit Columns projection.
type CartRepository interface {
	// FindByOwner returns the caller's cart, reporting whether one exists. A
	// customer with no cart is not an error: the cart is created lazily on the
	// first add, and a read before any add answers an empty cart without creating
	// a row (research D8). It joins the caller's transaction.
	FindByOwner(ctx context.Context, userID uuid.UUID) (*model.Cart, bool, error)

	// Create inserts the cart for its owner. A cart another concurrent first add
	// created in the same instant is refused by the unique index on user_id and
	// reported as domainerr.ErrCartAlreadyExists, so the use case can retry by
	// finding the cart the other writer created (research D8). It joins the
	// caller's transaction.
	Create(ctx context.Context, cart *model.Cart) error

	// Lock takes the cart's row lock inside the caller's transaction, so two
	// writes to the same cart serialise and a line can never be left holding more
	// than what was available when its write ran (research D9). An unknown cart
	// is reported as domainerr.ErrProductNotFound.
	Lock(ctx context.Context, cartID uuid.UUID) error

	// UpsertLine adds a line, or raises the quantity of the line already held for
	// the product rather than creating a second line (FR-002). The upsert is on
	// the (cart_id, product_id) unique index, so two concurrent adds converge on
	// one line whose quantity is the sum (research D3, D9). It joins the caller's
	// transaction.
	UpsertLine(ctx context.Context, cartID uuid.UUID, line model.CartLine, now time.Time) error

	// SetQuantity sets a line's quantity to exactly the requested value. A line
	// this cart does not hold is reported as domainerr.ErrProductNotFound
	// (error-codes.md). It joins the caller's transaction.
	SetQuantity(ctx context.Context, cartID, productID uuid.UUID, quantity int64, now time.Time) error

	// DeleteLine removes one line. A line this cart does not hold is reported as
	// domainerr.ErrProductNotFound (error-codes.md). It joins the caller's
	// transaction.
	DeleteLine(ctx context.Context, cartID, productID uuid.UUID) error

	// Lines returns the cart's lines in a stable order — created_at then id — so
	// two reads return the same order even when they share a timestamp. It joins
	// the caller's transaction.
	Lines(ctx context.Context, cartID uuid.UUID) ([]model.CartLine, error)
}
