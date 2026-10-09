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

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the cart adapter and the migration against a real
// PostgreSQL container. Its centre of gravity is what a fake repository cannot
// prove: that one cart per account and one line per product are refused by the
// unique indexes; that the quantity and money checks reject a writer that bypassed
// the application; that a cart line survives the removal of its product because
// product_id carries no foreign key; and that two concurrent upserts to one cart
// leave one line whose quantity is the sum. Most constraint tests write the tables
// directly, with no domain validation and no use case, because a guard against a
// future writer can only be proven by pretending to be that writer.
//
// Every rejection asserts both the SQLSTATE and the constraint name, so a test
// fails if the constraint it names is removed from the migration.

// SQLSTATE codes the constraint tests distinguish.
const (
	sqlStateUnique     = "23505"
	sqlStateForeignKey = "23503"
	sqlStateCheck      = "23514"
)

// raw insert statements that skip the domain and the adapter, used by the
// constraint tests.
const (
	insertCartSQL = `INSERT INTO carts (id, user_id) VALUES ($1, $2)`
	insertLineSQL = `
		INSERT INTO cart_items
			(id, cart_id, product_id, quantity, unit_price_amount, currency)
		VALUES ($1, $2, $3, $4, $5, $6)`
)

// cartFixture is one migrated PostgreSQL container plus the adapter under test.
type cartFixture struct {
	pool *pgxpool.Pool
	repo *CartRepository
}

func newCartFixture(t *testing.T) *cartFixture {
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
	return &cartFixture{pool: pool, repo: NewCartRepository(pool)}
}

// insertUser writes one account straight into the database, so the cart tables
// have a real owner to reference. The test may do this because it is not
// production code: the boundary the constitution protects is between modules, not
// between a test and a fixture.
func (f *cartFixture) insertUser(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	id := uuid.New()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'hash')`,
		id, "user-"+id.String()[:8]+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// insertProduct writes one category and one on-sale product straight into the
// database, so the survival test has a real product whose removal must not take
// the cart line with it.
func (f *cartFixture) insertProduct(t *testing.T) uuid.UUID {
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

// insertCart writes an empty cart for an owner and returns its identifier.
func (f *cartFixture) insertCart(t *testing.T, userID uuid.UUID) uuid.UUID {
	t.Helper()
	cartID := uuid.New()
	if _, err := f.pool.Exec(context.Background(), insertCartSQL, cartID, userID); err != nil {
		t.Fatalf("insert cart: %v", err)
	}
	return cartID
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

// FR-001, FR-002, FR-006, FR-009: every storage-level rule holds against a writer
// that bypassed the application entirely. Each subtest names the one constraint it
// expects, so removing that constraint from the migration makes exactly this
// subtest fail.
func TestCartStorageConstraintsRejectWhatTheApplicationWouldPrevent(t *testing.T) {
	f := newCartFixture(t)
	ctx := context.Background()
	userID := f.insertUser(t)
	cartID := f.insertCart(t, userID)

	t.Run("second cart for one account", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertCartSQL, uuid.New(), userID)
		requireConstraint(t, err, sqlStateUnique, "carts_user_key")
	})

	t.Run("second line for one product", func(t *testing.T) {
		productID := uuid.New()
		if _, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, productID, 1, 1000, "VND"); err != nil {
			t.Fatalf("seed the first line: %v", err)
		}
		_, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, productID, 1, 1000, "VND")
		requireConstraint(t, err, sqlStateUnique, "cart_items_cart_product_key")
	})

	t.Run("zero quantity", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, uuid.New(), 0, 1000, "VND")
		requireConstraint(t, err, sqlStateCheck, "cart_items_quantity_ck")
	})

	t.Run("non-positive unit price", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, uuid.New(), 1, 0, "VND")
		requireConstraint(t, err, sqlStateCheck, "cart_items_unit_price_ck")
	})

	t.Run("malformed currency", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, uuid.New(), 1, 1000, "vnd")
		requireConstraint(t, err, sqlStateCheck, "cart_items_currency_ck")
	})

	t.Run("line for a missing cart", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), uuid.New(), uuid.New(), 1, 1000, "VND")
		requireConstraint(t, err, sqlStateForeignKey, "cart_items_cart_fk")
	})
}

// FR-001: a removed account takes its cart and its lines with it, through the
// cascading foreign keys.
func TestRemovingAnAccountCascadesItsCart(t *testing.T) {
	f := newCartFixture(t)
	ctx := context.Background()
	userID := f.insertUser(t)
	cartID := f.insertCart(t, userID)
	if _, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, uuid.New(), 2, 1000, "VND"); err != nil {
		t.Fatalf("seed the line: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete the account: %v", err)
	}

	for _, table := range []string{"carts", "cart_items"} {
		var count int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still holds %d rows after the account was removed", table, count)
		}
	}
}

// FR-012, research D3: removing a product leaves the customer's cart line in
// place, because product_id carries no foreign key. A cascading key would silently
// delete the line; a restricting key would block the product's removal, which
// module 04 deliberately allows.
func TestCartLineSurvivesTheRemovalOfItsProduct(t *testing.T) {
	f := newCartFixture(t)
	ctx := context.Background()
	userID := f.insertUser(t)
	cartID := f.insertCart(t, userID)
	productID := f.insertProduct(t)

	if _, err := f.pool.Exec(ctx, insertLineSQL, uuid.New(), cartID, productID, 2, 1000, "VND"); err != nil {
		t.Fatalf("seed the line: %v", err)
	}

	// The removal must succeed: the loose reference does not block it.
	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID); err != nil {
		t.Fatalf("delete the product: %v", err)
	}

	lines, err := f.repo.Lines(ctx, cartID)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if len(lines) != 1 || lines[0].ProductID != productID {
		t.Fatalf("the cart line must survive its product's removal, got %+v", lines)
	}
}

// research D8: a second concurrent first add is refused by the unique index on
// carts.user_id, and the adapter classifies that refusal into the sentinel the use
// case retries on.
func TestSecondCartForOneAccountIsClassifiedAsAlreadyExisting(t *testing.T) {
	f := newCartFixture(t)
	ctx := context.Background()
	userID := f.insertUser(t)

	if err := f.repo.Create(ctx, model.NewCart(userID)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := f.repo.Create(ctx, model.NewCart(userID)); !errors.Is(err, domainerr.ErrCartAlreadyExists) {
		t.Fatalf("expected ErrCartAlreadyExists, got %v", err)
	}

	found, ok, err := f.repo.FindByOwner(ctx, userID)
	if err != nil || !ok {
		t.Fatalf("FindByOwner: ok=%v err=%v", ok, err)
	}
	if found.UserID != userID {
		t.Fatalf("found cart owned by %s, want %s", found.UserID, userID)
	}
}

// SC-002, quickstart 9c: two concurrent upserts of the same product to one cart
// converge on a single line whose quantity is the sum, never two lines. The
// (cart_id, product_id) unique index and the ON CONFLICT summing update are what
// make it so.
func TestConcurrentUpsertsLeaveOneLineWithTheSummedQuantity(t *testing.T) {
	f := newCartFixture(t)
	ctx := context.Background()
	userID := f.insertUser(t)
	cart := model.NewCart(userID)
	if err := f.repo.Create(ctx, cart); err != nil {
		t.Fatalf("Create cart: %v", err)
	}

	productID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	line, err := model.NewCartLine(productID, 1, model.Price{Amount: 1000, Currency: "VND"})
	if err != nil {
		t.Fatalf("NewCartLine: %v", err)
	}

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
			errs[i] = f.repo.UpsertLine(ctx, cart.ID, line, now)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	lines, err := f.repo.Lines(ctx, cart.ID)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d", len(lines))
	}
	if lines[0].Quantity != 2 {
		t.Fatalf("expected quantity 2 (1+1), got %d", lines[0].Quantity)
	}
}
