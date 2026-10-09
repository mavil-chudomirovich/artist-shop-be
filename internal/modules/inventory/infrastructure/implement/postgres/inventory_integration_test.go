//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	inventorydto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the inventory adapter and the migration against a real
// PostgreSQL container. Its centre of gravity is what a fake repository cannot
// prove: that a level row for a product that does not exist is refused by the
// foreign key; that removing a product takes its level, ledger and hold rows with
// it; that the non-negative and non-zero checks reject a writer that bypassed the
// application; that the partial unique indexes refuse a duplicate source reference
// and a duplicate active hold; and that the history read is stable across two reads
// sharing a timestamp. Most constraint tests write the tables directly, with no
// domain validation and no use case, because a guard against a future writer can
// only be proven by pretending to be that writer.
//
// Every rejection asserts both the SQLSTATE and the constraint name, so a test
// fails if the constraint it names is removed from the migration.

// SQLSTATE codes the constraint tests distinguish.
const (
	sqlStateUnique     = "23505"
	sqlStateForeignKey = "23503"
	sqlStateCheck      = "23514"
)

// The partial indexes the migration must create (data-model.md).
const (
	idxTransactionSource = "inventory_transactions_source_key"
	idxHoldActive        = "stock_holds_active_key"
)

// raw insert statements that skip the domain and the adapter, used by the
// constraint tests.
const (
	insertMovementSQL = `
		INSERT INTO inventory_transactions
			(id, product_id, kind, delta, resulting_quantity, source_reference, actor_id, note, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	insertHoldSQL = `
		INSERT INTO stock_holds
			(id, product_id, order_id, quantity, status, expires_at, created_at, resolved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
)

// inventoryFixture is one migrated PostgreSQL container plus the adapter under
// test.
type inventoryFixture struct {
	pool *pgxpool.Pool
	repo *InventoryRepository
}

func newInventoryFixture(t *testing.T) *inventoryFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return &inventoryFixture{pool: pool, repo: NewInventoryRepository(pool)}
}

// insertProduct writes one category and one on-sale product straight into the
// database, so the inventory tables have a real product to reference. The test may
// do this because it is not production code: the boundary the constitution
// protects is between modules, not between a test and a fixture.
func (f *inventoryFixture) insertProduct(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	categoryID := uuid.New()
	suffix := categoryID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`,
		categoryID, "Category "+suffix, "category-"+suffix,
		"category-"+suffix, "category-"+suffix); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	productID := uuid.New()
	slug := "product-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder)
		VALUES ($1, $2, $3, $4, '', 1000, 'VND', $5, 0, 'ACTIVE', false, false)`,
		productID, "Product "+slug, slug, slug, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return productID
}

// sqlState returns the PostgreSQL SQLSTATE an error carries, or "" for a
// non-database error.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// constraintName returns the constraint an error names, or "".
func constraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// requireConstraint fails unless err is exactly the SQLSTATE and constraint
// expected. Asserting the name, not just the code, is what lets each test be
// mutation-checked against a single constraint.
func requireConstraint(t *testing.T, err error, wantState, wantConstraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the storage layer accepted a row that %s must reject", wantConstraint)
	}
	if got := sqlState(err); got != wantState {
		t.Fatalf("expected SQLSTATE %s (%s), got %q: %v", wantState, wantConstraint, got, err)
	}
	if got := constraintName(err); got != wantConstraint {
		t.Fatalf("expected constraint %s, got %q: %v", wantConstraint, got, err)
	}
}

// FR-009, FR-018, FR-019, FR-020, FR-022: every storage-level rule holds against a
// writer that bypassed the application entirely. Each subtest names the one
// constraint it expects, so removing that constraint from the migration makes
// exactly this subtest fail.
func TestStorageConstraintsRejectWhatTheApplicationWouldPrevent(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)

	t.Run("level row for a missing product", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, `INSERT INTO stock_levels (product_id, quantity) VALUES ($1, 1)`, uuid.New())
		requireConstraint(t, err, sqlStateForeignKey, "stock_levels_product_fk")
	})

	t.Run("negative level quantity", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, `INSERT INTO stock_levels (product_id, quantity) VALUES ($1, -1)`, productID)
		requireConstraint(t, err, sqlStateCheck, "stock_levels_quantity_ck")
	})

	t.Run("zero movement delta", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "RESTOCK", 0, 5, nil, nil, time.Now().UTC())
		requireConstraint(t, err, sqlStateCheck, "inventory_transactions_delta_ck")
	})

	t.Run("negative resulting quantity", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "DAMAGE", -1, -1, nil, nil, time.Now().UTC())
		requireConstraint(t, err, sqlStateCheck, "inventory_transactions_resulting_ck")
	})

	t.Run("unknown movement kind", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "ARCHIVED", 1, 1, nil, nil, time.Now().UTC())
		requireConstraint(t, err, sqlStateCheck, "inventory_transactions_kind_ck")
	})

	t.Run("duplicate source reference", func(t *testing.T) {
		reference := "payment-event-1"
		if _, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "SALE", -1, 0, reference, nil, time.Now().UTC()); err != nil {
			t.Fatalf("seed the first movement: %v", err)
		}
		_, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "SALE", -1, 0, reference, nil, time.Now().UTC())
		requireConstraint(t, err, sqlStateUnique, idxTransactionSource)
	})

	t.Run("duplicate active hold", func(t *testing.T) {
		orderID := uuid.New()
		expires := time.Now().UTC().Add(time.Hour)
		if _, err := f.pool.Exec(ctx, insertHoldSQL, uuid.New(), productID, orderID, 1, "ACTIVE", expires, time.Now().UTC(), nil); err != nil {
			t.Fatalf("seed the first hold: %v", err)
		}
		_, err := f.pool.Exec(ctx, insertHoldSQL, uuid.New(), productID, orderID, 1, "ACTIVE", expires, time.Now().UTC(), nil)
		requireConstraint(t, err, sqlStateUnique, idxHoldActive)
	})

	t.Run("active hold carrying a resolution time", func(t *testing.T) {
		now := time.Now().UTC()
		_, err := f.pool.Exec(ctx, insertHoldSQL, uuid.New(), productID, uuid.New(), 1, "ACTIVE", now.Add(time.Hour), now, now)
		requireConstraint(t, err, sqlStateCheck, "stock_holds_resolved_ck")
	})
}

// FR-011, research D11: removing a product takes its level, its ledger and its
// holds with it, through the cascading foreign keys, and leaves nothing behind.
func TestRemovingAProductCascadesItsInventory(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)

	if _, err := f.pool.Exec(ctx, `INSERT INTO stock_levels (product_id, quantity) VALUES ($1, 5)`, productID); err != nil {
		t.Fatalf("seed the level: %v", err)
	}
	if _, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "RESTOCK", 5, 5, nil, nil, time.Now().UTC()); err != nil {
		t.Fatalf("seed the movement: %v", err)
	}
	if _, err := f.pool.Exec(ctx, insertHoldSQL, uuid.New(), productID, uuid.New(), 1, "ACTIVE", time.Now().UTC().Add(time.Hour), time.Now().UTC(), nil); err != nil {
		t.Fatalf("seed the hold: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID); err != nil {
		t.Fatalf("delete the product: %v", err)
	}

	for _, table := range []string{"stock_levels", "inventory_transactions", "stock_holds"} {
		var count int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE product_id = $1`, productID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still holds %d rows for the removed product", table, count)
		}
	}
}

// FR-009, FR-010: the adapter's conditional decrease is what refuses an oversell,
// and a never-stocked product reads as zero rather than not-found.
func TestTheAdapterRefusesOversell(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)

	level, err := f.repo.Level(ctx, productID)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	if level != 0 {
		t.Fatalf("a never-stocked product reads as 0, got %d", level)
	}

	if _, err := f.repo.Increase(ctx, productID, 3, time.Now().UTC()); err != nil {
		t.Fatalf("Increase: %v", err)
	}
	if _, err := f.repo.Decrease(ctx, productID, 5, time.Now().UTC()); !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if level, err := f.repo.Level(ctx, productID); err != nil || level != 3 {
		t.Fatalf("a refused decrease changed the level: %d/%v", level, err)
	}
}

// research D2, D12: a level row for a product that does not exist is refused by
// the foreign key, which the adapter classifies into the not-found sentinel.
func TestIncreaseForAMissingProductIsNotFound(t *testing.T) {
	f := newInventoryFixture(t)

	if _, err := f.repo.Increase(context.Background(), uuid.New(), 1, time.Now().UTC()); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// FR-008, research D14: the history is ordered by created_at then id, so two reads
// sharing a timestamp return the same order.
func TestHistoryReadIsStableAcrossTwoReadsSharingATimestamp(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)
	shared := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if _, err := f.pool.Exec(ctx, insertMovementSQL, uuid.New(), productID, "RESTOCK", 1, int64(i+1), nil, nil, shared); err != nil {
			t.Fatalf("insert movement %d: %v", i, err)
		}
	}

	first, total, err := f.repo.Movements(ctx, productID, 1, 20)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if total != 3 || len(first) != 3 {
		t.Fatalf("expected three movements, got %d/%d", total, len(first))
	}

	second, _, err := f.repo.Movements(ctx, productID, 1, 20)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("the two reads disagree on the page length: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("the history order changed between two reads sharing a timestamp: %s vs %s", first[i].ID, second[i].ID)
		}
	}
}

// holdClock is the injected clock the hold integration test drives, so the
// fifteen-minute window is observed by moving the instant rather than waiting
// (research D15).
type holdClock struct{ at time.Time }

func (c *holdClock) Now() time.Time          { return c.at }
func (c *holdClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// existingProducts answers the ProductLookup contract with "present", so the
// hold test can focus on the hold rather than on existence (research D4).
type existingProducts struct{}

func (existingProducts) ProductExists(context.Context, uuid.UUID) (bool, error) { return true, nil }

// FR-014, FR-015, FR-018, research D2, D6, D15: the hold lifecycle against real
// PostgreSQL. A reserve leaves the stored level unchanged while the active-hold
// sum rises; an expired hold is excluded from that sum even before it is swept;
// and the expire use case resolves it without touching the shelf. This runs the
// real use cases over the real adapter, because what a fake cannot prove is that
// the SQL-level active predicate uses the injected instant rather than the
// database clock.
func TestHoldLifecycleAgainstPostgres(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)

	clock := &holdClock{at: time.Now().UTC().Truncate(time.Millisecond)}
	svc := inventoryimplement.New(inventoryimplement.Service{
		Inventory: f.repo,
		Lookup:    existingProducts{},
		Tx:        &database.DB{Pool: f.pool},
		Clock:     clock,
		Mapper:    inventorymapper.New(),
	})

	if _, err := f.repo.Increase(ctx, productID, 5, clock.Now()); err != nil {
		t.Fatalf("seed the level: %v", err)
	}

	orderID := uuid.New()
	if err := svc.Reserve(ctx, inventorydto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// The shelf did not move and the active-hold sum rose.
	level, err := f.repo.Level(ctx, productID)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	if level != 5 {
		t.Fatalf("a hold must not move the shelf, got %d", level)
	}
	held, err := f.repo.ActiveHeld(ctx, productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld: %v", err)
	}
	if held != 2 {
		t.Fatalf("expected 2 held, got %d", held)
	}

	// Past the window, an unswept hold is already excluded from availability.
	clock.advance(16 * time.Minute)
	held, err = f.repo.ActiveHeld(ctx, productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld after expiry: %v", err)
	}
	if held != 0 {
		t.Fatalf("an expired hold must not be counted before it is swept, got %d", held)
	}

	var (
		status     string
		resolvedAt *time.Time
	)
	readHold := func() {
		t.Helper()
		if err := f.pool.QueryRow(ctx,
			`SELECT status, resolved_at FROM stock_holds WHERE order_id = $1 AND product_id = $2`,
			orderID, productID).Scan(&status, &resolvedAt); err != nil {
			t.Fatalf("read the hold: %v", err)
		}
	}
	readHold()
	if status != string(constant.HoldStatusActive) || resolvedAt != nil {
		t.Fatalf("the unswept hold must still be ACTIVE, got %s/%v", status, resolvedAt)
	}

	// The expire use case resolves it and it stays uncounted.
	if err := svc.ExpireHolds(ctx); err != nil {
		t.Fatalf("ExpireHolds: %v", err)
	}
	readHold()
	if status != string(constant.HoldStatusReleased) || resolvedAt == nil {
		t.Fatalf("expire must resolve the hold, got %s/%v", status, resolvedAt)
	}
	held, err = f.repo.ActiveHeld(ctx, productID, clock.Now())
	if err != nil {
		t.Fatalf("ActiveHeld after sweep: %v", err)
	}
	if held != 0 {
		t.Fatalf("a resolved hold must not be counted, got %d", held)
	}
	if level, err := f.repo.Level(ctx, productID); err != nil || level != 5 {
		t.Fatalf("expiring a hold must not move the shelf: %d/%v", level, err)
	}
}

// FR-020 to FR-022, research D5: the adapter classifies the storage's refusal of a
// duplicate source reference into the already-applied sentinel, so the sale use
// case can answer success rather than a storage failure. This is the
// classification a concurrent duplicate depends on.
func TestDuplicateSourceReferenceIsClassifiedAsAlreadyApplied(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, err := f.repo.Increase(ctx, productID, 5, now); err != nil {
		t.Fatalf("seed the level: %v", err)
	}

	reference := "evt-classify-1"
	first, err := model.NewMovement(productID, constant.MovementSale, -1, 4, nil, &reference, nil, now)
	if err != nil {
		t.Fatalf("NewMovement(first): %v", err)
	}
	if err := f.repo.InsertMovement(ctx, first); err != nil {
		t.Fatalf("first InsertMovement: %v", err)
	}

	second, err := model.NewMovement(productID, constant.MovementSale, -1, 4, nil, &reference, nil, now)
	if err != nil {
		t.Fatalf("NewMovement(second): %v", err)
	}
	if err := f.repo.InsertMovement(ctx, second); !errors.Is(err, domainerr.ErrAlreadyApplied) {
		t.Fatalf("expected ErrAlreadyApplied, got %v", err)
	}
}

// FR-016, FR-020 to FR-022, SC-003, quickstart 8b/8c: two concurrent applications
// of the same source reference change stock exactly once. The partial unique index
// on the source reference, not an application pre-check, is what refuses the
// second, and the use case accepts that refusal as success rather than an error.
func TestConcurrentSaleApplicationsChangeStockExactlyOnce(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	productID := f.insertProduct(t)

	clock := &holdClock{at: time.Now().UTC().Truncate(time.Millisecond)}
	svc := inventoryimplement.New(inventoryimplement.Service{
		Inventory: f.repo,
		Lookup:    existingProducts{},
		Tx:        &database.DB{Pool: f.pool},
		Clock:     clock,
		Mapper:    inventorymapper.New(),
	})

	if _, err := f.repo.Increase(ctx, productID, 5, clock.Now()); err != nil {
		t.Fatalf("seed the level: %v", err)
	}
	orderID := uuid.New()
	if err := svc.Reserve(ctx, inventorydto.ReserveInput{OrderID: orderID, ProductID: productID, Quantity: 2}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	in := inventorydto.SaleInput{OrderID: orderID, ProductID: productID, SourceReference: "evt-concurrent-1"}
	var (
		wg    sync.WaitGroup
		errs  [2]error
		start = make(chan struct{})
	)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = svc.ApplySale(ctx, in)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("application %d: a duplicate must be accepted as success, got %v", i, err)
		}
	}

	level, err := f.repo.Level(ctx, productID)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	if level != 3 {
		t.Fatalf("the shelf must fall exactly once (5-2=3), got %d", level)
	}

	var movements int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM inventory_transactions WHERE source_reference = $1`,
		in.SourceReference).Scan(&movements); err != nil {
		t.Fatalf("count movements by reference: %v", err)
	}
	if movements != 1 {
		t.Fatalf("exactly one movement must carry the reference, got %d", movements)
	}

	var sales int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM inventory_transactions WHERE product_id = $1 AND kind = 'SALE'`,
		productID).Scan(&sales); err != nil {
		t.Fatalf("count SALE movements: %v", err)
	}
	if sales != 1 {
		t.Fatalf("exactly one SALE movement must exist, got %d", sales)
	}

	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM stock_holds WHERE order_id = $1 AND product_id = $2`,
		orderID, productID).Scan(&status); err != nil {
		t.Fatalf("read the hold: %v", err)
	}
	if status != string(constant.HoldStatusConsumed) {
		t.Fatalf("the hold must close as CONSUMED, got %s", status)
	}
}
