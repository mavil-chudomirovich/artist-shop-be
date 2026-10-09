//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
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
