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
)

// This file is US3's use-case contract: the hold lifecycle exercised through the
// real Service over an in-memory repository and a movable clock. The clock is the
// seam that makes "fifteen minutes pass" an instant rather than a wait, and the
// in-memory holds are what let the tests assert that reserving, releasing and
// expiring move availability without ever touching the shelf (FR-014, FR-015,
// FR-017, FR-018, FR-019, research D15).

// movableClock is the injected Clock with an instant a test can advance, so the
// fifteen-minute window is observed without sleeping (research D15).
type movableClock struct{ at time.Time }

func (c *movableClock) Now() time.Time { return c.at }

func (c *movableClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// fakeHoldRepository is an in-memory InventoryRepository with real hold storage.
// It embeds the interface so only the operations the hold use cases reach are
// implemented; the active predicate is the domain's own IsActive, so the fake
// applies exactly the rule the real adapter applies in SQL.
type fakeHoldRepository struct {
	domainrepo.InventoryRepository

	physical  int64
	holds     []model.Hold
	movements []model.Movement
	locks     int
}

func (r *fakeHoldRepository) Level(_ context.Context, _ uuid.UUID) (int64, error) {
	return r.physical, nil
}

func (r *fakeHoldRepository) Decrease(_ context.Context, _ uuid.UUID, amount int64, _ time.Time) (int64, error) {
	if r.physical < amount {
		return 0, domainerr.InsufficientStock(model.FieldQuantity, r.physical, amount)
	}
	r.physical -= amount
	return r.physical, nil
}

func (r *fakeHoldRepository) LockLevel(_ context.Context, _ uuid.UUID) error {
	r.locks++
	return nil
}

func (r *fakeHoldRepository) InsertMovement(_ context.Context, movement *model.Movement) error {
	r.movements = append(r.movements, *movement)
	return nil
}

func (r *fakeHoldRepository) ActiveHeld(_ context.Context, productID uuid.UUID, now time.Time) (int64, error) {
	var total int64
	for i := range r.holds {
		hold := r.holds[i]
		if hold.ProductID == productID && hold.IsActive(now) {
			total += hold.Quantity
		}
	}
	return total, nil
}

func (r *fakeHoldRepository) InsertHold(_ context.Context, hold *model.Hold) error {
	for i := range r.holds {
		existing := r.holds[i]
		if existing.OrderID == hold.OrderID && existing.ProductID == hold.ProductID && existing.Status == constant.HoldStatusActive {
			return domainerr.ErrHoldAlreadyExists
		}
	}
	r.holds = append(r.holds, *hold)
	return nil
}

func (r *fakeHoldRepository) FindActiveHold(_ context.Context, orderID, productID uuid.UUID, now time.Time) (*model.Hold, bool, error) {
	for i := range r.holds {
		hold := &r.holds[i]
		if hold.OrderID == orderID && hold.ProductID == productID && hold.IsActive(now) {
			return hold, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeHoldRepository) ResolveHold(_ context.Context, holdID uuid.UUID, status constant.HoldStatus, at time.Time) error {
	for i := range r.holds {
		if r.holds[i].ID == holdID {
			if r.holds[i].Status != constant.HoldStatusActive {
				return domainerr.InvalidValue(model.FieldHold, "is already resolved")
			}
			r.holds[i].Status = status
			moment := at
			r.holds[i].ResolvedAt = &moment
			return nil
		}
	}
	return domainerr.InvalidValue(model.FieldHold, "is not active")
}

func (r *fakeHoldRepository) ExpiredHolds(_ context.Context, now time.Time) ([]model.Hold, error) {
	out := make([]model.Hold, 0)
	for i := range r.holds {
		hold := r.holds[i]
		if hold.Status == constant.HoldStatusActive && !hold.ExpiresAt.After(now) {
			out = append(out, hold)
		}
	}
	return out, nil
}

// newHoldService builds the real use cases over the hold-aware in-memory
// repository. The lookup always reports the product present, because existence is
// US1's contract and not what these tests probe.
func newHoldService(repo appinterface.InventoryRepository, clock appinterface.Clock) *Service {
	return New(Service{
		Inventory: repo,
		Lookup:    &fakeLookup{exists: true},
		Tx:        passThroughUnitOfWork{},
		Clock:     clock,
		Audit:     &fakeAuditor{},
		Mapper:    mapper.New(),
	})
}

// FR-014: reserving reduces availability while leaving physical stock unchanged,
// and the hold expires one HoldTTL after the injected instant.
func TestReserveReducesAvailabilityWithoutMovingTheShelf(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	if err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if repo.locks < 1 {
		t.Fatal("a reserve must take the level row lock so two holds cannot set aside the same unit")
	}
	if repo.physical != 5 {
		t.Fatalf("a hold must not move the shelf, got %d", repo.physical)
	}
	if len(repo.holds) != 1 {
		t.Fatalf("expected exactly one hold, got %d", len(repo.holds))
	}
	hold := repo.holds[0]
	if hold.Status != constant.HoldStatusActive || hold.ResolvedAt != nil {
		t.Fatalf("a fresh hold is active and unresolved: %+v", hold)
	}
	if !hold.ExpiresAt.Equal(fixedNow.Add(constant.HoldTTL)) {
		t.Fatalf("the hold must expire one HoldTTL after the injected instant, got %s", hold.ExpiresAt)
	}

	out, err := svc.Stock(context.Background(), dto.StockRefInput{ProductID: productID})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if out.PhysicalQuantity != 5 || out.HeldQuantity != 2 || out.AvailableQuantity != 3 {
		t.Fatalf("unexpected stock: %+v", out)
	}
}

// FR-018: a hold larger than what is available is refused and nothing is held.
func TestReserveMoreThanAvailableIsRefused(t *testing.T) {
	repo := &fakeHoldRepository{physical: 3}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)

	err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: uuid.New(), ProductID: uuid.New(), Quantity: 4})
	if !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if len(repo.holds) != 0 || repo.physical != 3 {
		t.Fatalf("a refused hold must change nothing: %d holds, shelf %d", len(repo.holds), repo.physical)
	}
}

// FR-015: a hold stops counting once the injected clock passes its expiry, even
// before the sweeper reaches it, and ExpireHolds then resolves it without moving
// the shelf.
func TestAnExpiredHoldReturnsItsQuantityToAvailability(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	if err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	clock.advance(constant.HoldTTL + time.Minute)

	// Before any sweep, the expired hold is already excluded from availability.
	held, err := repo.ActiveHeld(context.Background(), productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld: %v", err)
	}
	if held != 0 {
		t.Fatalf("an expired hold must not be counted, got %d", held)
	}
	if repo.holds[0].Status != constant.HoldStatusActive {
		t.Fatal("the hold must still be unswept at this point")
	}
	out, err := svc.Stock(context.Background(), dto.StockRefInput{ProductID: productID})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if out.PhysicalQuantity != 5 || out.HeldQuantity != 0 || out.AvailableQuantity != 5 {
		t.Fatalf("an expired hold must return availability without moving the shelf: %+v", out)
	}

	if err := svc.ExpireHolds(context.Background()); err != nil {
		t.Fatalf("ExpireHolds: %v", err)
	}
	if repo.holds[0].Status != constant.HoldStatusReleased || repo.holds[0].ResolvedAt == nil {
		t.Fatalf("the sweeper must resolve the hold: %+v", repo.holds[0])
	}
	if repo.physical != 5 {
		t.Fatalf("expiring a hold must not move the shelf, got %d", repo.physical)
	}
}

// FR-017: releasing a hold returns its quantity with no physical change.
func TestReleaseReturnsTheQuantityWithoutMovingTheShelf(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	if err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := svc.Release(context.Background(), dto.ReleaseInput{OrderID: orderID, ProductID: productID}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if repo.holds[0].Status != constant.HoldStatusReleased || repo.holds[0].ResolvedAt == nil {
		t.Fatalf("release must resolve the hold: %+v", repo.holds[0])
	}
	if repo.physical != 5 {
		t.Fatalf("release must not move the shelf, got %d", repo.physical)
	}
	held, err := repo.ActiveHeld(context.Background(), productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld: %v", err)
	}
	if held != 0 {
		t.Fatalf("a released hold must not count, got %d", held)
	}
}

// FR-019: a second reserve for the same order and product does not double-hold.
func TestASecondReserveForTheSameOrderDoesNotDoubleHold(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	in := dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("a repeated reserve must not fail: %v", err)
	}
	if len(repo.holds) != 1 {
		t.Fatalf("a repeated reserve must not create a second hold, got %d", len(repo.holds))
	}
	held, err := repo.ActiveHeld(context.Background(), productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld: %v", err)
	}
	if held != 2 {
		t.Fatalf("the order holds 2 units, not %d", held)
	}
}

// FR-019: a hold that expired but the sweeper has not yet released is still
// ACTIVE to the storage partial unique index, so a fresh reserve for the same
// order and product meets that guard and is recognised as already held rather
// than reported as a failure. This is the path the active-hold pre-check cannot
// see, because the hold is no longer active to the availability predicate.
func TestReserveAfterExpiryDoesNotDoubleHold(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	in := dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	clock.advance(constant.HoldTTL + time.Minute)

	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("a reserve meeting the storage guard must be a no-op, got %v", err)
	}
	if len(repo.holds) != 1 {
		t.Fatalf("the unswept hold must not be doubled, got %d holds", len(repo.holds))
	}
}

// FR-009: a physical decrease that would leave the shelf below what an active
// hold has promised is refused, while a decrease down to the held floor is
// allowed. This is the cross-table rule a single-table constraint cannot express.
func TestDamageBelowHeldIsRefused(t *testing.T) {
	repo := &fakeHoldRepository{physical: 5}
	clock := &movableClock{at: fixedNow}
	svc := newHoldService(repo, clock)
	productID, orderID := uuid.New(), uuid.New()

	if err := svc.Reserve(context.Background(), dto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 3}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// 5 - 3 = 2 would fall below the 3 units promised: refused.
	_, err := svc.Damage(context.Background(), dto.DamageInput{ProductID: productID, Quantity: 3})
	if !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if repo.physical != 5 || len(repo.movements) != 0 {
		t.Fatalf("a refused damage must change nothing: shelf %d, %d movements", repo.physical, len(repo.movements))
	}

	// 5 - 2 = 3 leaves exactly what is held: allowed.
	if _, err := svc.Damage(context.Background(), dto.DamageInput{ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("a damage down to the held floor must be allowed: %v", err)
	}
	if repo.physical != 3 || len(repo.movements) != 1 {
		t.Fatalf("expected the shelf at 3 with one movement, got %d/%d", repo.physical, len(repo.movements))
	}
}
