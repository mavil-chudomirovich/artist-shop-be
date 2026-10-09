package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file is US1's use-case contract: the manual stock operations and the
// single-product read, exercised through the real Service over an in-memory
// repository. The fake auditor, lookup and clock make the audit trail, the
// existence answer and the stamped instant observable, which is how the tests
// prove the actor and the note travel onto the ledger and that a refusal writes
// nothing (FR-001 to FR-007, FR-009, FR-012).

// testActor is the administrator the request context carries. The actor is put
// on the context exactly as presentation does from the session, never from the
// input (FR-005).
var testActor = appinterface.Actor{
	ID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
	Role: access.RoleAdmin,
}

func adminContext() context.Context {
	return appinterface.WithActor(context.Background(), testActor)
}

// fixedNow is the instant the injected clock reports, so every stamped row can
// be asserted exactly (research D15).
var fixedNow = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

// fakeRepository is an in-memory InventoryRepository. It embeds the interface so
// only the operations the manual use cases reach are implemented; the rest fail
// loudly if a use case unexpectedly calls them.
type fakeRepository struct {
	domainrepo.InventoryRepository

	physical  int64
	held      int64
	locks     int
	movements []model.Movement
}

func (r *fakeRepository) Level(_ context.Context, _ uuid.UUID) (int64, error) {
	return r.physical, nil
}

func (r *fakeRepository) Increase(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	r.physical += amount
	return r.physical, nil
}

func (r *fakeRepository) Decrease(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	if r.physical < amount {
		return 0, domainerr.InsufficientStock(model.FieldQuantity, r.physical, amount)
	}
	r.physical -= amount
	return r.physical, nil
}

func (r *fakeRepository) LockLevel(_ context.Context, _ uuid.UUID) error {
	r.locks++
	return nil
}

func (r *fakeRepository) InsertMovement(_ context.Context, movement *model.Movement) error {
	r.movements = append(r.movements, *movement)
	return nil
}

func (r *fakeRepository) ActiveHeld(_ context.Context, _ uuid.UUID, _ time.Time) (int64, error) {
	return r.held, nil
}

// recordedAudit is one emitted audit event, reduced to the members the tests
// assert.
type recordedAudit struct {
	action  string
	actorID *uuid.UUID
	role    string
	target  string
	meta    map[string]any
}

// fakeAuditor records every emitted event.
type fakeAuditor struct{ events []recordedAudit }

func (a *fakeAuditor) Record(_ context.Context, action, _ string, actorID *uuid.UUID, actorRole, _, targetID string, metadata map[string]any) {
	a.events = append(a.events, recordedAudit{
		action:  action,
		actorID: actorID,
		role:    actorRole,
		target:  targetID,
		meta:    metadata,
	})
}

// fakeLookup answers the ProductLookup contract with a fixed existence, so the
// not-found and the never-stocked paths can both be driven.
type fakeLookup struct {
	exists bool
	err    error
}

func (l *fakeLookup) ProductExists(context.Context, uuid.UUID) (bool, error) {
	return l.exists, l.err
}

// fixedClock is the injected Clock (research D15).
type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

// passThroughUnitOfWork runs the function with the same context, which is all
// the in-memory fake needs to observe the transaction-scoped writes.
type passThroughUnitOfWork struct{}

func (passThroughUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func newTestService(repo *fakeRepository, lookup appinterface.ProductLookup, audit appinterface.Auditor) *Service {
	return New(Service{
		Inventory: repo,
		Lookup:    lookup,
		Tx:        passThroughUnitOfWork{},
		Clock:     fixedClock{},
		Audit:     audit,
		Mapper:    mapper.New(),
	})
}

// FR-001, FR-004: a restock raises the shelf by exactly the amount and writes
// one RESTOCK movement carrying the actor and the note.
func TestRestockRaisesTheShelfAndWritesOneRestockMovement(t *testing.T) {
	repo := &fakeRepository{physical: 4}
	audit := &fakeAuditor{}
	svc := newTestService(repo, &fakeLookup{exists: true}, audit)
	note := "hàng về kho"
	productID := uuid.New()

	out, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: productID, Quantity: 10, Note: &note})
	if err != nil {
		t.Fatalf("Restock: %v", err)
	}
	if repo.physical != 14 {
		t.Fatalf("expected the shelf to rise to 14, got %d", repo.physical)
	}
	if out.PhysicalQuantity != 14 || out.HeldQuantity != 0 || out.AvailableQuantity != 14 {
		t.Fatalf("unexpected stock answer: %+v", out)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("expected exactly one movement, got %d", len(repo.movements))
	}
	movement := repo.movements[0]
	if movement.Kind != constant.MovementRestock || movement.Delta != 10 || movement.ResultingQuantity != 14 {
		t.Fatalf("unexpected RESTOCK movement: %+v", movement)
	}
	if movement.ActorID == nil || *movement.ActorID != testActor.ID {
		t.Fatalf("the movement must carry the acting administrator, got %+v", movement.ActorID)
	}
	if movement.Note == nil || *movement.Note != note {
		t.Fatalf("the movement must carry the note, got %+v", movement.Note)
	}
	if movement.SourceReference != nil {
		t.Fatalf("a manual movement carries no source reference, got %v", *movement.SourceReference)
	}
}

// FR-002, FR-004: damage lowers the shelf and writes one DAMAGE movement.
func TestDamageLowersTheShelfAndWritesOneDamageMovement(t *testing.T) {
	repo := &fakeRepository{physical: 10}
	svc := newTestService(repo, &fakeLookup{exists: true}, &fakeAuditor{})

	out, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: 4})
	if err != nil {
		t.Fatalf("Damage: %v", err)
	}
	if out.PhysicalQuantity != 6 {
		t.Fatalf("expected the shelf to fall to 6, got %d", out.PhysicalQuantity)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("expected exactly one movement, got %d", len(repo.movements))
	}
	movement := repo.movements[0]
	if movement.Kind != constant.MovementDamage || movement.Delta != -4 || movement.ResultingQuantity != 6 {
		t.Fatalf("unexpected DAMAGE movement: %+v", movement)
	}
}

// FR-002, FR-009: damage larger than the shelf is refused naming the field and
// writes nothing — no movement, no audit, no level change.
func TestDamageLargerThanTheShelfIsRefusedAndWritesNothing(t *testing.T) {
	repo := &fakeRepository{physical: 3}
	audit := &fakeAuditor{}
	svc := newTestService(repo, &fakeLookup{exists: true}, audit)

	_, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: 5})
	if !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	var insufficient *domainerr.InsufficientStockError
	if !errors.As(err, &insufficient) || insufficient.Field != model.FieldQuantity {
		t.Fatalf("the refusal must name quantity, got %+v", err)
	}
	if repo.physical != 3 {
		t.Fatalf("a refused damage changed the shelf: %d", repo.physical)
	}
	if len(repo.movements) != 0 {
		t.Fatalf("a refused damage must write no movement, got %d", len(repo.movements))
	}
	if len(audit.events) != 0 {
		t.Fatalf("a refused damage must write no audit entry, got %d", len(audit.events))
	}
}

// FR-009, quickstart 3b, research D2: the adapter's conditional decrease is the
// sole decision for an oversell — it matches no row and reports the
// insufficient-stock sentinel. The use case must surface that sentinel rather than
// a generic storage error, because presentation maps the sentinel to 409
// INVENTORY_INSUFFICIENT_STOCK and would answer 500 for anything else. The fake
// repository returns exactly the adapter's empty-result error, so this proves the
// translation the handler depends on.
func TestAnEmptyConditionalDecreaseIsReportedAsInsufficientStock(t *testing.T) {
	repo := &fakeRepository{physical: 3}
	audit := &fakeAuditor{}
	svc := newTestService(repo, &fakeLookup{exists: true}, audit)

	_, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: 4})
	if err == nil {
		t.Fatal("a decrease over the shelf must be refused by the conditional update")
	}
	// errors.Is is what the handler's second branch relies on; a generic error
	// would not match and would map to 500.
	if !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected the insufficient-stock sentinel, got %v", err)
	}
	// errors.As is what the handler's first branch relies on to name the field.
	var insufficient *domainerr.InsufficientStockError
	if !errors.As(err, &insufficient) {
		t.Fatalf("expected the typed refusal so the field can be named, got %T: %v", err, err)
	}
	if insufficient.Field != model.FieldQuantity {
		t.Fatalf("the refusal must name quantity, got %q", insufficient.Field)
	}
	if repo.physical != 3 || len(repo.movements) != 0 || len(audit.events) != 0 {
		t.Fatalf("a refused decrease left a trace: shelf %d, %d movements, %d audit events",
			repo.physical, len(repo.movements), len(audit.events))
	}
}

// FR-003, research D9: an adjustment sets the counted value and records only the
// signed difference; correcting to the stored value writes no movement.
func TestAdjustmentRecordsOnlyTheDifferenceAndANoOpWritesNothing(t *testing.T) {
	repo := &fakeRepository{physical: 10}
	svc := newTestService(repo, &fakeLookup{exists: true}, &fakeAuditor{})
	productID := uuid.New()

	out, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: productID, Quantity: 7})
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if out.PhysicalQuantity != 7 {
		t.Fatalf("expected the counted value 7, got %d", out.PhysicalQuantity)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("expected one ADJUSTMENT movement, got %d", len(repo.movements))
	}
	movement := repo.movements[0]
	if movement.Kind != constant.MovementAdjustment || movement.Delta != -3 || movement.ResultingQuantity != 7 {
		t.Fatalf("unexpected ADJUSTMENT movement: %+v", movement)
	}

	// Correcting to the value already stored changes nothing.
	if _, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: productID, Quantity: 7}); err != nil {
		t.Fatalf("Adjust to the stored value: %v", err)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("a no-op adjustment must write no movement, got %d", len(repo.movements))
	}
	if repo.physical != 7 {
		t.Fatalf("a no-op adjustment changed the shelf: %d", repo.physical)
	}
}

// FR-012, quickstart 4c: an adjustment accepts zero as a valid counted value.
func TestAdjustmentAcceptsZeroAsACountedValue(t *testing.T) {
	repo := &fakeRepository{physical: 5}
	svc := newTestService(repo, &fakeLookup{exists: true}, &fakeAuditor{})

	out, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: uuid.New(), Quantity: 0})
	if err != nil {
		t.Fatalf("Adjust to zero: %v", err)
	}
	if out.PhysicalQuantity != 0 || repo.physical != 0 {
		t.Fatalf("zero is a valid count, got %d/%d", out.PhysicalQuantity, repo.physical)
	}
	if len(repo.movements) != 1 || repo.movements[0].Delta != -5 {
		t.Fatalf("expected one ADJUSTMENT movement of -5, got %+v", repo.movements)
	}
}

// FR-012: a zero or negative quantity on a restock or a damage is refused naming
// `quantity`, and nothing is written. (A fractional quantity is refused by the
// transport, because a quantity is an integer by the time it reaches a use case.)
func TestNonPositiveQuantityIsRefusedNamingTheField(t *testing.T) {
	cases := []struct {
		name string
		call func(svc *Service) error
	}{
		{"restock zero", func(svc *Service) error {
			_, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: uuid.New(), Quantity: 0})
			return err
		}},
		{"restock negative", func(svc *Service) error {
			_, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: uuid.New(), Quantity: -1})
			return err
		}},
		{"damage zero", func(svc *Service) error {
			_, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: 0})
			return err
		}},
		{"damage negative", func(svc *Service) error {
			_, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: -1})
			return err
		}},
		{"adjustment negative", func(svc *Service) error {
			_, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: uuid.New(), Quantity: -1})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{physical: 5}
			audit := &fakeAuditor{}
			svc := newTestService(repo, &fakeLookup{exists: true}, audit)

			err := tc.call(svc)
			if !errors.Is(err, domainerr.ErrInvalidValue) {
				t.Fatalf("expected ErrInvalidValue, got %v", err)
			}
			var invalid *domainerr.InvalidValueError
			if !errors.As(err, &invalid) || invalid.Field != model.FieldQuantity {
				t.Fatalf("the rejection must name quantity, got %+v", err)
			}
			if len(repo.movements) != 0 || repo.physical != 5 {
				t.Fatalf("a refused quantity must write nothing: %d movements, shelf %d", len(repo.movements), repo.physical)
			}
			if len(audit.events) != 0 {
				t.Fatalf("a refused quantity must write no audit entry, got %d", len(audit.events))
			}
		})
	}
}

// FR-007, research D12: an unknown product answers not-found while one that
// exists but has never been stocked answers zero.
func TestAnUnknownProductIsNotFoundWhileANeverStockedOneIsZero(t *testing.T) {
	unknown := newTestService(&fakeRepository{}, &fakeLookup{exists: false}, &fakeAuditor{})
	if _, err := unknown.Stock(adminContext(), dto.StockRefInput{ProductID: uuid.New()}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound for an unknown product, got %v", err)
	}

	neverStocked := newTestService(&fakeRepository{}, &fakeLookup{exists: true}, &fakeAuditor{})
	out, err := neverStocked.Stock(adminContext(), dto.StockRefInput{ProductID: uuid.New()})
	if err != nil {
		t.Fatalf("Stock of a never-stocked product: %v", err)
	}
	if out.PhysicalQuantity != 0 || out.HeldQuantity != 0 || out.AvailableQuantity != 0 {
		t.Fatalf("a never-stocked product answers zero, got %+v", out)
	}
}

// FR-005, research D12: every write path also resolves existence through the
// lookup, so an operation on an unknown product answers not-found and writes
// nothing.
func TestWritesAgainstAnUnknownProductAnswerNotFoundAndWriteNothing(t *testing.T) {
	repo := &fakeRepository{physical: 5}
	svc := newTestService(repo, &fakeLookup{exists: false}, &fakeAuditor{})

	if _, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: uuid.New(), Quantity: 1}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("restock: expected ErrProductNotFound, got %v", err)
	}
	if _, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: uuid.New(), Quantity: 1}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("damage: expected ErrProductNotFound, got %v", err)
	}
	if _, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: uuid.New(), Quantity: 1}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("adjustment: expected ErrProductNotFound, got %v", err)
	}
	if repo.physical != 5 || len(repo.movements) != 0 {
		t.Fatalf("a not-found write must change nothing: shelf %d, %d movements", repo.physical, len(repo.movements))
	}
}

// FR-006, SC-006: each manual operation records its own audit action naming the
// acting administrator and the product.
func TestEachManualOperationRecordsItsAuditAction(t *testing.T) {
	repo := &fakeRepository{physical: 10}
	audit := &fakeAuditor{}
	svc := newTestService(repo, &fakeLookup{exists: true}, audit)
	productID := uuid.New()

	if _, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Restock: %v", err)
	}
	if _, err := svc.Damage(adminContext(), dto.DamageInput{ProductID: productID, Quantity: 1}); err != nil {
		t.Fatalf("Damage: %v", err)
	}
	if _, err := svc.Adjust(adminContext(), dto.AdjustmentInput{ProductID: productID, Quantity: 3}); err != nil {
		t.Fatalf("Adjust: %v", err)
	}

	want := []string{constant.AuditInventoryRestocked, constant.AuditInventoryDamaged, constant.AuditInventoryAdjusted}
	if len(audit.events) != len(want) {
		t.Fatalf("expected %d audit events, got %d: %+v", len(want), len(audit.events), audit.events)
	}
	for i, action := range want {
		event := audit.events[i]
		if event.action != action {
			t.Fatalf("event %d: expected %s, got %s", i, action, event.action)
		}
		if event.actorID == nil || *event.actorID != testActor.ID || event.role != string(access.RoleAdmin) {
			t.Fatalf("event %d must name the acting administrator, got %+v", i, event)
		}
		if event.target != productID.String() {
			t.Fatalf("event %d must name the product, got %s", i, event.target)
		}
	}
}
