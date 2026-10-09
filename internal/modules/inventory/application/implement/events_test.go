package implement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// This file is US5's use-case contract: the application of an outside event turns
// a held order into a sale exactly once. The idempotency key is the event's source
// reference, and the tests prove both how a sale is applied and how a repeat is
// accepted as a success rather than a failure (FR-016, FR-020 to FR-023, SC-003).

// saleRepository is the in-memory InventoryRepository the sale use cases reach,
// with the two behaviours a plain fake would miss. It embeds the hold fake for the
// level and the hold storage, and shadows InsertMovement so a second movement
// carrying a reference already on the ledger is refused exactly as the storage's
// partial unique index refuses it (FR-022). It also provides the Increase the
// manual restock path needs, so the manual-vs-sale distinction can be asserted
// through the same repository.
type saleRepository struct {
	*fakeHoldRepository
}

// Increase materialises the level the way the adapter's upsert does, for the one
// test that writes a manual movement before a sale.
func (r *saleRepository) Increase(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	r.physical += amount
	return r.physical, nil
}

// InsertMovement appends a movement, refusing one whose source reference is
// already on the ledger with the sentinel the adapter classifies the unique-index
// violation into (FR-022).
func (r *saleRepository) InsertMovement(_ context.Context, movement *model.Movement) error {
	if movement.SourceReference != nil {
		for i := range r.movements {
			existing := r.movements[i].SourceReference
			if existing != nil && *existing == *movement.SourceReference {
				return domainerr.ErrAlreadyApplied
			}
		}
	}
	r.movements = append(r.movements, *movement)
	return nil
}

// journalUnitOfWork runs the function and restores the fake's state when it
// returns an error, so a refused insert rolls the writes back the way a database
// transaction does. Without it, the sale's rollback path could not be observed
// over the in-memory fake, and a duplicate would appear to have changed the shelf.
type journalUnitOfWork struct{ repo *fakeHoldRepository }

func (u journalUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	physical, locks := u.repo.physical, u.repo.locks
	holds := append([]model.Hold(nil), u.repo.holds...)
	movements := append([]model.Movement(nil), u.repo.movements...)
	err := fn(ctx)
	if err != nil {
		u.repo.physical = physical
		u.repo.locks = locks
		u.repo.holds = holds
		u.repo.movements = movements
	}
	return err
}

// newSaleService builds the real use cases over the sale-aware in-memory
// repository. The lookup always reports the product present, because existence is
// US1's contract and not what these tests probe.
func newSaleService(repo *saleRepository, clock *movableClock, audit *fakeAuditor) *Service {
	return New(Service{
		Inventory: repo,
		Lookup:    &fakeLookup{exists: true},
		Tx:        journalUnitOfWork{repo: repo.fakeHoldRepository},
		Clock:     clock,
		Audit:     audit,
		Mapper:    mapper.New(),
	})
}

// reserve builds one active hold for two units on a shelf of five and returns the
// order it belongs to, so every sale test starts from the same state.
func reserve(t *testing.T, svc *Service, productID uuid.UUID) uuid.UUID {
	t.Helper()
	orderID := uuid.New()
	if err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	return orderID
}

// FR-016, FR-020: consuming a hold lowers the shelf by the held quantity, closes
// the hold as CONSUMED, writes one SALE movement carrying the event's reference,
// and leaves availability unchanged.
func TestApplySaleConsumesTheHoldAndLeavesAvailabilityUnchanged(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	svc := newSaleService(repo, clock, &fakeAuditor{})
	productID := uuid.New()
	ctx := context.Background()
	orderID := reserve(t, svc, productID)

	before, err := svc.Stock(ctx, dto.StockRefInput{ProductID: productID})
	if err != nil {
		t.Fatalf("Stock before: %v", err)
	}
	if before.PhysicalQuantity != 5 || before.HeldQuantity != 2 || before.AvailableQuantity != 3 {
		t.Fatalf("unexpected stock before the sale: %+v", before)
	}

	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-1"}); err != nil {
		t.Fatalf("ApplySale: %v", err)
	}

	if repo.physical != 3 {
		t.Fatalf("a sale must lower the shelf by the held quantity (5-2=3), got %d", repo.physical)
	}
	if len(repo.holds) != 1 || repo.holds[0].Status != constant.HoldStatusConsumed || repo.holds[0].ResolvedAt == nil {
		t.Fatalf("the hold must close as CONSUMED, got %+v", repo.holds)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("a sale must write exactly one movement, got %d", len(repo.movements))
	}
	movement := repo.movements[0]
	if movement.Kind != constant.MovementSale || movement.Delta != -2 || movement.ResultingQuantity != 3 {
		t.Fatalf("unexpected SALE movement: %+v", movement)
	}
	if movement.SourceReference == nil || *movement.SourceReference != "evt-1" {
		t.Fatalf("the sale must carry the event reference, got %v", movement.SourceReference)
	}
	if movement.ActorID != nil {
		t.Fatalf("a system-caused sale has no human actor, got %v", movement.ActorID)
	}

	after, err := svc.Stock(ctx, dto.StockRefInput{ProductID: productID})
	if err != nil {
		t.Fatalf("Stock after: %v", err)
	}
	if after.PhysicalQuantity != 3 || after.HeldQuantity != 0 || after.AvailableQuantity != 3 {
		t.Fatalf("a sale must leave availability unchanged at 3, got %+v", after)
	}
	if after.AvailableQuantity != before.AvailableQuantity {
		t.Fatalf("availability must not move across a sale: %d then %d", before.AvailableQuantity, after.AvailableQuantity)
	}
}

// FR-021: a replay of an already-applied reference changes nothing and is
// accepted as success rather than reported as a failure.
func TestReplayingAnAppliedSaleChangesNothing(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	svc := newSaleService(repo, clock, &fakeAuditor{})
	productID := uuid.New()
	ctx := context.Background()
	orderID := reserve(t, svc, productID)
	in := dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-1"}

	if err := svc.ApplySale(ctx, in); err != nil {
		t.Fatalf("first ApplySale: %v", err)
	}
	physical, movements, status := repo.physical, len(repo.movements), repo.holds[0].Status

	if err := svc.ApplySale(ctx, in); err != nil {
		t.Fatalf("a replay must be accepted as success, got %v", err)
	}
	if repo.physical != physical {
		t.Fatalf("a replay must not change the shelf: %d then %d", physical, repo.physical)
	}
	if len(repo.movements) != movements {
		t.Fatalf("a replay must not write a second movement: %d then %d", movements, len(repo.movements))
	}
	if repo.holds[0].Status != status {
		t.Fatalf("a replay must not touch the hold: %s then %s", status, repo.holds[0].Status)
	}
}

// FR-022, SC-003: two applications of one event reaching the storage at the same
// instant — simulated by a ledger that already carries the reference while the
// hold is still active — leave exactly one change. The partial unique index is the
// guard, and the refusal is accepted as success.
func TestADuplicateRefusedByTheSourceReferenceIndexIsAccepted(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	svc := newSaleService(repo, clock, &fakeAuditor{})
	productID := uuid.New()
	ctx := context.Background()
	orderID := reserve(t, svc, productID)

	reference := "evt-race"
	winner, err := model.NewMovement(productID, constant.MovementSale, -2, 3, nil, &reference, nil, fixedNow)
	if err != nil {
		t.Fatalf("NewMovement: %v", err)
	}
	repo.movements = append(repo.movements, *winner)

	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: reference}); err != nil {
		t.Fatalf("a duplicate refused by the storage index must be accepted as success, got %v", err)
	}
	if repo.physical != 5 {
		t.Fatalf("the refused duplicate must roll the decrease back, shelf %d", repo.physical)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("the refused duplicate must leave exactly one movement, got %d", len(repo.movements))
	}
	if repo.holds[0].Status != constant.HoldStatusActive {
		t.Fatalf("the rollback must leave the hold active, got %s", repo.holds[0].Status)
	}
}

// FR-023: an event that would take the shelf below zero is refused and records no
// movement. Physical stock normally never falls below what is held, so the fake is
// placed in that inconsistent state directly to exercise the refusal.
func TestASaleThatWouldGoBelowZeroIsRefused(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 1}}
	clock := &movableClock{at: fixedNow}
	audit := &fakeAuditor{}
	svc := newSaleService(repo, clock, audit)
	productID, orderID := uuid.New(), uuid.New()
	ctx := context.Background()

	hold, err := model.NewHold(productID, orderID, 2, fixedNow)
	if err != nil {
		t.Fatalf("NewHold: %v", err)
	}
	repo.holds = []model.Hold{*hold}
	repo.physical = 1

	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-short"}); !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if repo.physical != 1 || len(repo.movements) != 0 {
		t.Fatalf("a refused sale must change nothing: shelf %d, %d movements", repo.physical, len(repo.movements))
	}
	if len(audit.events) != 0 {
		t.Fatalf("a refused sale must write no audit entry, got %d", len(audit.events))
	}
	if repo.holds[0].Status != constant.HoldStatusActive {
		t.Fatalf("a refused sale must not consume the hold, got %s", repo.holds[0].Status)
	}
}

// FR-015: a hold that is no longer active cannot be consumed. An expired hold is
// already unavailable to others, so a payment arriving after it changes nothing
// and is not an error (error-codes.md).
func TestASaleAgainstAnInactiveHoldIsNotConsumed(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	svc := newSaleService(repo, clock, &fakeAuditor{})
	productID := uuid.New()
	ctx := context.Background()
	orderID := reserve(t, svc, productID)

	clock.advance(constant.HoldTTL + time.Minute)

	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-late"}); err != nil {
		t.Fatalf("an expired hold is not an error, got %v", err)
	}
	if repo.physical != 5 || len(repo.movements) != 0 {
		t.Fatalf("nothing may be consumed from an inactive hold: shelf %d, %d movements", repo.physical, len(repo.movements))
	}
	if repo.holds[0].Status != constant.HoldStatusActive {
		t.Fatalf("the unswept hold stays ACTIVE and untouched, got %s", repo.holds[0].Status)
	}
}

// FR-020, Constitution VI, research D13: an applied sale writes one audit entry
// naming the source reference with no human actor, and it carries no customer's
// personal data.
func TestTheSaleAuditNamesTheSourceReferenceWithNoHumanActor(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	audit := &fakeAuditor{}
	svc := newSaleService(repo, clock, audit)
	productID := uuid.New()
	ctx := context.Background()
	orderID := reserve(t, svc, productID)

	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-9"}); err != nil {
		t.Fatalf("ApplySale: %v", err)
	}

	if len(audit.events) != 1 {
		t.Fatalf("a sale must write exactly one audit entry, got %d", len(audit.events))
	}
	event := audit.events[0]
	if event.action != constant.AuditInventorySaleApplied {
		t.Fatalf("expected %s, got %s", constant.AuditInventorySaleApplied, event.action)
	}
	if event.actorID != nil || event.role != "" {
		t.Fatalf("a system-caused sale has no human actor, got id=%v role=%q", event.actorID, event.role)
	}
	if event.target != productID.String() {
		t.Fatalf("the audit entry must name the product, got %s", event.target)
	}
	if got, _ := event.meta["sourceReference"].(string); got != "evt-9" {
		t.Fatalf("the audit entry must name the source reference, got %v", event.meta["sourceReference"])
	}
	assertNoPersonalData(t, event.meta)
}

// FR-006, research D13: a manual movement carries no source reference while a sale
// carries one, so the two paths stay distinguishable in the history an operator
// reads.
func TestAManualMovementCarriesNoReferenceWhileASaleCarriesOne(t *testing.T) {
	repo := &saleRepository{fakeHoldRepository: &fakeHoldRepository{physical: 5}}
	clock := &movableClock{at: fixedNow}
	svc := newSaleService(repo, clock, &fakeAuditor{})
	productID := uuid.New()
	ctx := context.Background()

	if _, err := svc.Restock(adminContext(), dto.RestockInput{ProductID: productID, Quantity: 1}); err != nil {
		t.Fatalf("Restock: %v", err)
	}
	orderID := reserve(t, svc, productID)
	if err := svc.ApplySale(ctx, dto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-7"}); err != nil {
		t.Fatalf("ApplySale: %v", err)
	}

	if len(repo.movements) != 2 {
		t.Fatalf("expected a manual movement and a sale, got %d", len(repo.movements))
	}
	manual, sale := repo.movements[0], repo.movements[1]
	if manual.Kind != constant.MovementRestock {
		t.Fatalf("expected the first movement to be RESTOCK, got %s", manual.Kind)
	}
	if manual.SourceReference != nil {
		t.Fatalf("a manual movement carries no source reference, got %q", *manual.SourceReference)
	}
	if manual.ActorID == nil || *manual.ActorID != testActor.ID {
		t.Fatalf("a manual movement names the administrator, got %v", manual.ActorID)
	}
	if sale.Kind != constant.MovementSale {
		t.Fatalf("expected the second movement to be SALE, got %s", sale.Kind)
	}
	if sale.SourceReference == nil || *sale.SourceReference != "evt-7" {
		t.Fatalf("a sale carries its event reference, got %v", sale.SourceReference)
	}
	if sale.ActorID != nil {
		t.Fatalf("a sale has no human actor, got %v", sale.ActorID)
	}
}

// assertNoPersonalData fails when an audit metadata member names customer personal
// data, which no inventory audit entry may carry (FR-006, Constitution VI).
func assertNoPersonalData(t *testing.T, meta map[string]any) {
	t.Helper()
	forbidden := map[string]struct{}{
		"email": {}, "name": {}, "fullname": {}, "full_name": {},
		"phone": {}, "address": {}, "customer": {}, "customerid": {}, "customer_id": {},
	}
	for key := range meta {
		if _, bad := forbidden[strings.ToLower(key)]; bad {
			t.Fatalf("the audit entry carries customer personal data under %q", key)
		}
	}
}
