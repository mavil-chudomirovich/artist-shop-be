package implement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// This file is US3: the hold lifecycle. A hold sets goods aside for an order
// while it is being paid; it never moves the shelf. Reserving takes the level
// row lock so two concurrent holds cannot set aside the same unit, and every
// decision about what is active is made with the instant the injected clock
// produced, passed to the adapter as a parameter rather than read from the
// database clock (FR-014, FR-015, FR-017, FR-018, research D2, D6, D15).

// Reserve holds a quantity of a product for one order while it is being paid
// (FR-014).
//
// It runs inside the UnitOfWork transaction and takes the level row lock first,
// so two concurrent reserves for the same product are applied against the
// current held quantity in a defined order and cannot both set aside the same
// unit (research D2). Availability is `physical − active holds`, and a hold that
// would exceed it is refused with nothing held (FR-018).
//
// An order that already holds the product is not held twice: the reserve is a
// no-op for it, so a retried checkout never doubles the amount set aside
// (FR-019). The active-hold check under the lock short-circuits the ordinary
// retry; a hold that expired but the sweeper has not yet released is still
// ACTIVE to the storage partial unique index, so the insert's unique violation
// is recognised as the same already-held outcome rather than reported as a
// failure.
func (s *Service) Reserve(ctx context.Context, in dto.ReserveInput) error {
	if in.Quantity <= 0 {
		return domainerr.InvalidValue(model.FieldQuantity, "must be a positive whole number")
	}
	if err := s.requireProduct(ctx, in.ProductID); err != nil {
		return err
	}

	now := s.now()
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Inventory.LockLevel(txCtx, in.ProductID); err != nil {
			return err
		}
		if _, found, err := s.Inventory.FindActiveHold(txCtx, in.OrderID, in.ProductID, now); err != nil {
			return err
		} else if found {
			return nil
		}

		physical, err := s.Inventory.Level(txCtx, in.ProductID)
		if err != nil {
			return err
		}
		held, err := s.Inventory.ActiveHeld(txCtx, in.ProductID, now)
		if err != nil {
			return err
		}
		available, err := model.Availability(physical, held)
		if err != nil {
			return err
		}
		if in.Quantity > available {
			return domainerr.InsufficientStock(model.FieldQuantity, available, in.Quantity)
		}

		return s.reconcileAvailability(txCtx, in.ProductID, now, func() error {
			hold, err := model.NewHold(in.ProductID, in.OrderID, in.Quantity, now)
			if err != nil {
				return err
			}
			if err := s.Inventory.InsertHold(txCtx, hold); err != nil {
				if errors.Is(err, domainerr.ErrHoldAlreadyExists) {
					return nil
				}
				return err
			}
			return nil
		})
	})
}

// Release returns an active hold's quantity to availability, for example when an
// unpaid order is cancelled (FR-017).
//
// It never moves physical stock, and an order with nothing left to release is a
// no-op rather than an error: a retried cancellation, or one arriving after the
// hold already expired, finds nothing and changes nothing, so a release is
// applied at most once (FR-019). The level row lock serialises it against a
// concurrent reserve on the same product.
func (s *Service) Release(ctx context.Context, in dto.ReleaseInput) error {
	now := s.now()
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Inventory.LockLevel(txCtx, in.ProductID); err != nil {
			return err
		}
		hold, found, err := s.Inventory.FindActiveHold(txCtx, in.OrderID, in.ProductID, now)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		return s.reconcileAvailability(txCtx, in.ProductID, now, func() error {
			return s.Inventory.ResolveHold(txCtx, hold.ID, constant.HoldStatusReleased, now)
		})
	})
}

// ExpireHolds releases every hold whose window has passed. It is the use case the
// sweeper calls, and it changes no physical stock (FR-015, research D6).
//
// The selection is made with the instant the injected clock produced, so the
// sweeper and the availability sum agree on what "expired" means. A hold a
// concurrent sweep or consume already resolved is skipped rather than failing the
// sweep, so the operation is idempotent (FR-019).
//
// The holds are grouped by product so each product's availability is reconciled
// once, from the quantity the sweep actually frees. A product whose last held unit
// expires is moved back on sale by that reconciliation, even though the expiry
// predicate already treated the hold as inactive (FR-025, research D6).
func (s *Service) ExpireHolds(ctx context.Context) error {
	now := s.now()
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		holds, err := s.Inventory.ExpiredHolds(txCtx, now)
		if err != nil {
			return err
		}
		for _, group := range groupExpiredHolds(holds) {
			group := group
			if err := s.reconcileExpiry(txCtx, group.productID, now, func() (int64, error) {
				return s.releaseExpired(txCtx, group.holds, now)
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// expiredGroup collects one product's expired holds, so a sweep reconciles each
// product once rather than once per hold.
type expiredGroup struct {
	productID uuid.UUID
	holds     []model.Hold
}

// groupExpiredHolds groups a sweep's expired holds by product, preserving the
// order the read returned them in so a sweep stays reproducible.
func groupExpiredHolds(holds []model.Hold) []expiredGroup {
	order := make([]uuid.UUID, 0, len(holds))
	byProduct := make(map[uuid.UUID]*expiredGroup, len(holds))
	for i := range holds {
		hold := holds[i]
		group, ok := byProduct[hold.ProductID]
		if !ok {
			group = &expiredGroup{productID: hold.ProductID}
			byProduct[hold.ProductID] = group
			order = append(order, hold.ProductID)
		}
		group.holds = append(group.holds, hold)
	}
	out := make([]expiredGroup, 0, len(order))
	for _, productID := range order {
		out = append(out, *byProduct[productID])
	}
	return out
}

// releaseExpired resolves one product's expired holds and returns the quantity it
// actually freed. A hold a concurrent operation already resolved is skipped, so it
// is not counted and the reconciliation stays honest under a race (FR-019).
func (s *Service) releaseExpired(ctx context.Context, holds []model.Hold, now time.Time) (int64, error) {
	var released int64
	for i := range holds {
		hold := holds[i]
		if err := hold.Expire(now); err != nil {
			if errors.Is(err, domainerr.ErrInvalidValue) {
				continue
			}
			return 0, err
		}
		if err := s.Inventory.ResolveHold(ctx, hold.ID, constant.HoldStatusReleased, now); err != nil {
			if errors.Is(err, domainerr.ErrInvalidValue) {
				continue
			}
			return 0, err
		}
		released += hold.Quantity
	}
	return released, nil
}
