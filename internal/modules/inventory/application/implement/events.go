package implement

import (
	"context"
	"errors"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is US5: the application of an outside event that turns a held order
// into a sale, exactly once. The event's source reference is the idempotency key,
// and the storage's partial unique index on it is what refuses a duplicate —
// including one arriving at the same instant — rather than an application-level
// pre-check (FR-020 to FR-023, research D5).

// ApplySale consumes a held order's hold: the physical stock falls by the held
// quantity, the hold closes as CONSUMED, and one SALE movement carrying the
// event's source reference is appended, all inside the UnitOfWork transaction
// (FR-016, FR-020).
//
// It calls no reconciliation, deliberately. A sale lowers the physical stock and
// closes the hold by the same quantity, so availability — physical minus active
// holds — does not move and nothing crosses zero, which is exactly why paying
// does not change what is available (FR-016, research D1, D10).
//
// Applying the same source reference again changes nothing and is accepted as
// success rather than reported as a failure, so a retrying caller stops retrying
// (FR-021). Two paths reach that conclusion: the hold is no longer active — it
// was already consumed, or released or expired — so there is nothing to consume;
// or the storage refuses a second movement through the source-reference unique
// index, which is classified as already applied and rolls the transaction back,
// so a concurrent duplicate leaves exactly one change and no second movement
// (FR-022). An event that would take the physical stock below zero is refused and
// records no movement (FR-023).
func (s *Service) ApplySale(ctx context.Context, in dto.SaleInput) error {
	if in.SourceReference == "" {
		return domainerr.InvalidValue(model.FieldSourceReference, "must name the outside event")
	}
	if err := s.requireProduct(ctx, in.ProductID); err != nil {
		return err
	}

	now := s.now()
	applied := false
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		hold, found, err := s.Inventory.FindActiveHold(txCtx, in.OrderID, in.ProductID, now)
		if err != nil {
			return err
		}
		if !found {
			// Nothing is set aside for this order and product: the hold was
			// already turned into a sale, or it was released or expired. There is
			// nothing to consume and nothing to change, and FR-021 makes this a
			// success rather than a failure.
			return nil
		}

		// The conditional decrease is what decides whether the sale is possible;
		// an event that would take the shelf below zero matches no row and is
		// refused with no movement (FR-009, FR-023).
		resulting, err := s.Inventory.Decrease(txCtx, in.ProductID, hold.Quantity, now)
		if err != nil {
			return err
		}

		// The movement is written before the hold is resolved so the unique index
		// on the source reference is the storage guard a concurrent duplicate
		// meets; the insert failing rolls back the decrease and the resolution
		// together (FR-022).
		reference := in.SourceReference
		movement, err := model.NewMovement(in.ProductID, constant.MovementSale, -hold.Quantity, resulting, nil, &reference, nil, now)
		if err != nil {
			return err
		}
		if err := s.Inventory.InsertMovement(txCtx, movement); err != nil {
			return err
		}
		if err := s.Inventory.ResolveHold(txCtx, hold.ID, constant.HoldStatusConsumed, now); err != nil {
			// A hold another operation resolved between the lookup and here
			// cannot be consumed; that is the same already-applied outcome, so
			// the whole transaction rolls back and the caller is told success
			// rather than failure (FR-019, FR-021).
			if errors.Is(err, domainerr.ErrInvalidValue) {
				return domainerr.ErrAlreadyApplied
			}
			return err
		}

		applied = true
		return nil
	})
	if errors.Is(err, domainerr.ErrAlreadyApplied) {
		// The duplicate was refused by the storage guard: nothing changed, and
		// the repeat is a success (FR-021).
		return nil
	}
	if err != nil {
		return err
	}

	if applied {
		s.recordSale(ctx, in)
	}
	return nil
}

// recordSale emits the audit entry for an applied sale. It names the event's
// source reference with a nil actor — no human caused it — so a payment event's
// inventory effect is auditable even though the payment module does not exist yet
// (FR-020, Constitution VI, research D13). It carries no customer's personal
// data: the reference identifies the event and the target is the product whose
// quantity moved.
func (s *Service) recordSale(ctx context.Context, in dto.SaleInput) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, constant.AuditInventorySaleApplied, string(audit.OutcomeSuccess),
		nil, "", targetTypeProduct, in.ProductID.String(),
		map[string]any{
			"kind":            string(constant.MovementSale),
			"sourceReference": in.SourceReference,
		})
}
