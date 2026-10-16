package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeVariant is the audit target type of the inventory events. The target
// of a stock change is the variant, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// variant without scanning the operator's other events (FR-006, Constitution VI).
const targetTypeVariant = "variant"

// Service implements the inventory module's manual stock use cases and the
// single-variant read (US1). The hold, event and history use cases are added by
// the later stories; until they exist this type satisfies the narrower surface
// presentation consumes, not yet the whole InventoryService.
type Service struct {
	// Inventory persists and reads the level, the ledger and the holds. It never
	// opens a transaction; the application owns every boundary (Constitution I).
	Inventory appinterface.InventoryRepository
	// Lookup answers whether a variant exists, so a read or a decrease can answer
	// not-found instead of fabricating a zero, which the foreign key cannot do
	// because it only fires on a write (FR-007, research D4, D12).
	Lookup appinterface.ProductLookup
	// Availability is the signal the use cases send the product module when a
	// change makes a variant's availability cross zero (FR-024, FR-026). It is
	// supplied by module 04's adapter at the composition root. A nil port is
	// tolerated by the reconciliation, so a unit test or read-only construction
	// still runs and only the signal is skipped.
	Availability appinterface.ProductAvailability
	// Tx owns the manual operations' transaction boundary. A level change and the
	// ledger row it produces commit together or not at all (FR-011,
	// Constitution II).
	Tx appinterface.UnitOfWork
	// Clock reads the current instant. It stamps the level and the ledger rows,
	// so a test can observe exactly when a change happened (research D15).
	Clock appinterface.Clock
	// Audit records every manual operation, naming the operator, the variant and
	// the change (FR-006, Constitution VI). A nil auditor is tolerated by the
	// read path so a read-only construction cannot panic.
	Audit appinterface.Auditor
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the inventory use-case service.
func New(service Service) *Service { return &service }

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset. Only "what time is it" is injected; the rules stay
// in the domain (research D15).
func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// requireVariant resolves the variant through the cross-module contract. It is
// asked on every path, because a read and a conditional decrease write no row and
// so never reach the storage foreign key, which is the one path that can answer
// existence on its own (FR-007, research D12).
func (s *Service) requireVariant(ctx context.Context, variantID uuid.UUID) error {
	exists, err := s.Lookup.VariantExists(ctx, variantID)
	if err != nil {
		return err
	}
	if !exists {
		return domainerr.ErrVariantNotFound
	}
	return nil
}

// Stock reads one variant's physical, held and available quantities. An unknown
// variant answers not-found; one that exists but has never been stocked answers
// zero, because a missing level row is understood as zero (FR-007, research D12).
//
// It opens no transaction: the read is a single consistent observation, and the
// availability rule lives in the domain (research D1).
func (s *Service) Stock(ctx context.Context, in dto.StockRefInput) (dto.StockOutput, error) {
	if err := s.requireVariant(ctx, in.VariantID); err != nil {
		return dto.StockOutput{}, err
	}
	return s.readStock(ctx, in.VariantID)
}

// readStock reads the physical quantity, the active-hold sum and derives
// availability. It is the one place both the read method and the writes build
// their answer, so every response carries the same three quantities (FR-007).
func (s *Service) readStock(ctx context.Context, variantID uuid.UUID) (dto.StockOutput, error) {
	now := s.now()
	physical, err := s.Inventory.Level(ctx, variantID)
	if err != nil {
		return dto.StockOutput{}, err
	}
	held, err := s.Inventory.ActiveHeld(ctx, variantID, now)
	if err != nil {
		return dto.StockOutput{}, err
	}
	stock, err := model.NewStock(variantID, physical, held, now)
	if err != nil {
		return dto.StockOutput{}, err
	}
	return s.Mapper.Stock(stock)
}

// Restock increases a variant's physical stock by a positive quantity and writes
// one RESTOCK movement inside a single transaction (FR-001, FR-004, FR-011).
//
// The acting administrator is taken from the context the session filled, never
// from the input, and the movement and the audit entry both name it (FR-005,
// FR-006). A non-positive quantity is refused naming the field before anything is
// read or written (FR-012).
func (s *Service) Restock(ctx context.Context, in dto.RestockInput) (dto.StockOutput, error) {
	if in.Quantity <= 0 {
		return dto.StockOutput{}, domainerr.InvalidValue(model.FieldQuantity, "must be a positive whole number")
	}
	if err := s.requireVariant(ctx, in.VariantID); err != nil {
		return dto.StockOutput{}, err
	}

	now := s.now()
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		return s.reconcileAvailability(txCtx, in.VariantID, now, func() error {
			resulting, err := s.Inventory.Increase(txCtx, in.VariantID, in.Quantity, now)
			if err != nil {
				return err
			}
			return s.writeMovement(txCtx, in.VariantID, constant.MovementRestock, in.Quantity, resulting, in.Note, now)
		})
	}); err != nil {
		return dto.StockOutput{}, err
	}

	s.record(ctx, constant.AuditInventoryRestocked, in.VariantID, map[string]any{
		"kind":  string(constant.MovementRestock),
		"delta": in.Quantity,
	})
	return s.readStock(ctx, in.VariantID)
}

// Damage decreases a variant's physical stock and writes one DAMAGE movement
// inside a single transaction (FR-002, FR-004, FR-011).
//
// The non-negative rule is not checked here: the repository's conditional update
// evaluates it against the current value, and an update that matches no row is
// reported as the insufficient-stock sentinel, which is what lets the handler
// answer 409 rather than 500 (FR-009, FR-010, research D2). What this path does
// check is the cross-table relationship FR-009 names second: under the same level
// row lock it refuses a decrease that would leave the shelf below what active
// holds have promised (T039).
func (s *Service) Damage(ctx context.Context, in dto.DamageInput) (dto.StockOutput, error) {
	if in.Quantity <= 0 {
		return dto.StockOutput{}, domainerr.InvalidValue(model.FieldQuantity, "must be a positive whole number")
	}
	if err := s.requireVariant(ctx, in.VariantID); err != nil {
		return dto.StockOutput{}, err
	}

	now := s.now()
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Inventory.LockLevel(txCtx, in.VariantID); err != nil {
			return err
		}
		return s.reconcileAvailability(txCtx, in.VariantID, now, func() error {
			if err := s.refuseBelowHeld(txCtx, in.VariantID, in.Quantity, now); err != nil {
				return err
			}
			resulting, err := s.Inventory.Decrease(txCtx, in.VariantID, in.Quantity, now)
			if err != nil {
				return err
			}
			return s.writeMovement(txCtx, in.VariantID, constant.MovementDamage, -in.Quantity, resulting, in.Note, now)
		})
	}); err != nil {
		return dto.StockOutput{}, err
	}

	s.record(ctx, constant.AuditInventoryDamaged, in.VariantID, map[string]any{
		"kind":  string(constant.MovementDamage),
		"delta": -in.Quantity,
	})
	return s.readStock(ctx, in.VariantID)
}

// Adjust corrects a variant's physical stock to a recounted value, recording only
// the signed difference (FR-003, research D9).
//
// The current count is read under the level's row lock, so two concurrent
// corrections cannot each compute a difference from the same stale value. A
// counted value equal to what is stored changes nothing and writes no movement;
// zero is a valid counted value (FR-012, research D9). A correction downward is
// refused when it would leave the shelf below what active holds have promised,
// under the same lock (FR-009, T039). The audit entry is written for the
// operation itself, including the no-op, because FR-006 records every manual
// stock operation.
func (s *Service) Adjust(ctx context.Context, in dto.AdjustmentInput) (dto.StockOutput, error) {
	if in.Quantity < 0 {
		return dto.StockOutput{}, domainerr.InvalidValue(model.FieldQuantity, "must not be negative")
	}
	if err := s.requireVariant(ctx, in.VariantID); err != nil {
		return dto.StockOutput{}, err
	}

	now := s.now()
	var delta int64
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Inventory.LockLevel(txCtx, in.VariantID); err != nil {
			return err
		}
		return s.reconcileAvailability(txCtx, in.VariantID, now, func() error {
			current, err := s.Inventory.Level(txCtx, in.VariantID)
			if err != nil {
				return err
			}
			delta, err = model.AdjustmentDelta(current, in.Quantity)
			if err != nil {
				return err
			}
			if delta == 0 {
				return nil
			}
			if delta > 0 {
				_, err = s.Inventory.Increase(txCtx, in.VariantID, delta, now)
			} else {
				if err = s.refuseBelowHeld(txCtx, in.VariantID, -delta, now); err != nil {
					return err
				}
				_, err = s.Inventory.Decrease(txCtx, in.VariantID, -delta, now)
			}
			if err != nil {
				return err
			}
			return s.writeMovement(txCtx, in.VariantID, constant.MovementAdjustment, delta, in.Quantity, in.Note, now)
		})
	}); err != nil {
		return dto.StockOutput{}, err
	}

	s.record(ctx, constant.AuditInventoryAdjusted, in.VariantID, map[string]any{
		"kind":  string(constant.MovementAdjustment),
		"delta": delta,
	})
	return s.readStock(ctx, in.VariantID)
}

// refuseBelowHeld refuses a physical decrease that would leave the shelf below
// the quantity the variant's active holds have promised (FR-009).
//
// It is deliberately narrow: it only intervenes when the variant actually has an
// active hold, so the non-negative rule stays the conditional update's decision
// (T034) and no application-level read-then-write check is reintroduced for it.
// The cross-table relationship has no single-table constraint, so it is made
// atomic by the level row lock the caller already holds; the caller reads the
// levels and the holds inside the same transaction.
func (s *Service) refuseBelowHeld(ctx context.Context, variantID uuid.UUID, amount int64, now time.Time) error {
	held, err := s.Inventory.ActiveHeld(ctx, variantID, now)
	if err != nil {
		return err
	}
	if held == 0 {
		return nil
	}
	physical, err := s.Inventory.Level(ctx, variantID)
	if err != nil {
		return err
	}
	available, err := model.Availability(physical, held)
	if err != nil {
		return err
	}
	if amount > available {
		return domainerr.InsufficientStock(model.FieldQuantity, available, amount)
	}
	return nil
}

// writeMovement builds and appends one ledger row inside the caller's
// transaction. The actor is the acting administrator taken from the context; a
// manual movement carries no source reference, so it stays distinguishable from a
// sale an outside event caused (research D13).
func (s *Service) writeMovement(ctx context.Context, variantID uuid.UUID, kind constant.MovementKind, delta, resulting int64, note *string, now time.Time) error {
	movement, err := model.NewMovement(variantID, kind, delta, resulting, actorID(ctx), nil, note, now)
	if err != nil {
		return err
	}
	return s.Inventory.InsertMovement(ctx, movement)
}

// record emits one audit event for a manual operation. It names the acting
// administrator and the variant, and never the note, because the free-text note
// is operator data the audit entry has no need to copy (FR-006, Constitution VI).
//
// Emission never fails the business operation: the shared writer queues and
// reports a dropped event through the logger.
func (s *Service) record(ctx context.Context, action string, variantID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		actorID(ctx), actorRole(ctx), targetTypeVariant, variantID.String(), metadata)
}

// actorID returns the acting account of the request, or nil when the call is not
// serving a request.
func actorID(ctx context.Context) *uuid.UUID {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return nil
	}
	id := actor.ID
	return &id
}

// actorRole returns the acting account's role, or an empty string when there is
// no session behind the call.
func actorRole(ctx context.Context) string {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return ""
	}
	return string(actor.Role)
}
