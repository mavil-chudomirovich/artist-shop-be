// Package postgres is the cart module's PostgreSQL adapter. It embeds the shared
// generic repository for its write plumbing and sets an explicit projection, and
// it never opens a transaction: every method joins the transaction the application
// put in the context (Constitution I).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the line table this adapter owns. The cart itself lives in carts; this
// adapter is the only code that reads or writes both.
const table = "cart_items"

// constraintCartsUserKey is the unique index that refuses a second cart for one
// account. A collision is attributed by the constraint name PostgreSQL reports,
// never by the text of its message, so it can be translated into the sentinel the
// use case retries on (research D8).
const constraintCartsUserKey = "carts_user_key"

// uniqueViolationCode is reported for a unique-index rejection (23505).
const uniqueViolationCode = "23505"

// cartItemColumns is the explicit projection of a line row, matching the scan
// arity of scanCartLine. It declares the shape rather than following the physical
// table, so a later ALTER TABLE cannot break a scan. The line's own id and its
// timestamps are not read: the cart addresses a line by its product, and the order
// is applied in SQL (research D7).
var cartItemColumns = []string{
	"product_id",
	"quantity",
	"unit_price_amount",
	"currency",
}

// CartRepository persists the cart and its lines.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. It never calls the generic methods:
// every query here carries a predicate — the owner lookup, the row lock, the
// upsert, the cart-scoped line read — the generic CRUD cannot express. No method
// opens a transaction; each joins the transaction the application put in the
// context (Constitution I).
type CartRepository struct {
	baserepo.Base[model.CartLine, uuid.UUID]
	pool *pgxpool.Pool
}

// NewCartRepository creates the cart repository.
func NewCartRepository(pool *pgxpool.Pool) *CartRepository {
	r := &CartRepository{pool: pool}
	r.Base = baserepo.Base[model.CartLine, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "id",
		Columns:  cartItemColumns,
		// A line is addressed within its cart by product_id; IDValue is only
		// here because Base requires it, and no generic method is ever called.
		IDValue: func(line *model.CartLine) any { return line.ProductID },
		Scan:    scanCartLine,
	}
	return r
}

var _ domainrepo.CartRepository = (*CartRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the adapter
// joins a caller's transaction without ever opening one itself (Constitution I).
func (r *CartRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// FindByOwner returns the caller's cart, reporting whether one exists. A customer
// with no cart answers found=false and no error, because the cart is created
// lazily on the first add (research D8).
func (r *CartRepository) FindByOwner(ctx context.Context, userID uuid.UUID) (*model.Cart, bool, error) {
	const query = `SELECT id, user_id FROM carts WHERE user_id = $1`
	cart := model.Cart{}
	if err := r.querier(ctx).QueryRow(ctx, query, userID).Scan(&cart.ID, &cart.UserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("find cart by owner: %w", err)
	}
	return &cart, true, nil
}

// Create inserts the cart for its owner. A cart another concurrent first add
// created in the same instant is refused by the unique index on user_id and
// reported as domainerr.ErrCartAlreadyExists, so the use case can retry by finding
// the cart the other writer created (research D8). The timestamps are the
// database's own defaults.
func (r *CartRepository) Create(ctx context.Context, cart *model.Cart) error {
	const query = `INSERT INTO carts (id, user_id) VALUES ($1, $2)`
	if _, err := r.querier(ctx).Exec(ctx, query, cart.ID, cart.UserID); err != nil {
		return r.classify("create cart", err)
	}
	return nil
}

// Lock takes the cart's row lock inside the caller's transaction, so two writes to
// the same cart serialise and a line can never be left holding more than what was
// available when its write ran (research D9). SELECT ... FOR UPDATE joins the
// transaction the application put in the context and never opens one itself; a
// caller that invokes it outside a transaction is not protected from the race. An
// unknown cart is reported as not-found.
func (r *CartRepository) Lock(ctx context.Context, cartID uuid.UUID) error {
	const query = `SELECT id FROM carts WHERE id = $1 FOR UPDATE`
	var locked uuid.UUID
	err := r.querier(ctx).QueryRow(ctx, query, cartID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.ErrProductNotFound
	}
	if err != nil {
		return fmt.Errorf("lock cart: %w", err)
	}
	return nil
}

// UpsertLine adds a line, or raises the quantity of the line already held for the
// product rather than creating a second line (FR-002). The upsert is on the
// (cart_id, product_id) unique index, so two concurrent adds converge on one line
// whose quantity is the sum: the quantity is added even when another writer won
// the insert (research D3, D9).
//
// The captured unit price is deliberately not overwritten on conflict: the line
// keeps the price it was first added at, so raising the quantity does not move the
// price of units the customer already saw (FR-008, research D4).
func (r *CartRepository) UpsertLine(ctx context.Context, cartID uuid.UUID, line model.CartLine, now time.Time) error {
	const query = `
		INSERT INTO cart_items
			(id, cart_id, product_id, quantity, unit_price_amount, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (cart_id, product_id) DO UPDATE
		SET quantity   = cart_items.quantity + EXCLUDED.quantity,
		    updated_at = EXCLUDED.updated_at`
	if _, err := r.querier(ctx).Exec(ctx, query,
		uuid.New(),
		cartID,
		line.ProductID,
		line.Quantity,
		line.UnitPrice.Amount,
		line.UnitPrice.Currency,
		now,
	); err != nil {
		return fmt.Errorf("upsert cart line: %w", err)
	}
	return nil
}

// SetQuantity sets a line's quantity to exactly the requested value. An update
// that matches no row means this cart does not hold the product, which is reported
// as not-found (error-codes.md).
func (r *CartRepository) SetQuantity(ctx context.Context, cartID, productID uuid.UUID, quantity int64, now time.Time) error {
	const query = `UPDATE cart_items SET quantity = $3, updated_at = $4 WHERE cart_id = $1 AND product_id = $2`
	tag, err := r.querier(ctx).Exec(ctx, query, cartID, productID, quantity, now)
	if err != nil {
		return fmt.Errorf("set cart line quantity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrProductNotFound
	}
	return nil
}

// DeleteLine removes one line. A line this cart does not hold is reported as
// not-found, so an identifier that is not a line answers the same not-found an
// unknown product answers and the route never confirms another cart's contents
// (error-codes.md).
func (r *CartRepository) DeleteLine(ctx context.Context, cartID, productID uuid.UUID) error {
	const query = `DELETE FROM cart_items WHERE cart_id = $1 AND product_id = $2`
	tag, err := r.querier(ctx).Exec(ctx, query, cartID, productID)
	if err != nil {
		return fmt.Errorf("delete cart line: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrProductNotFound
	}
	return nil
}

// Lines returns the cart's lines in a stable order — created_at then id — so two
// reads return the same order even when they share a timestamp.
func (r *CartRepository) Lines(ctx context.Context, cartID uuid.UUID) ([]model.CartLine, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM cart_items WHERE cart_id = $1 ORDER BY created_at, id`,
		projection(cartItemColumns))
	rows, err := r.querier(ctx).Query(ctx, query, cartID)
	if err != nil {
		return nil, fmt.Errorf("list cart lines: %w", err)
	}
	defer rows.Close()

	out := make([]model.CartLine, 0)
	for rows.Next() {
		line, err := scanCartLine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// projection joins a declared column list for a SELECT. Every query in this
// adapter builds its projection from a package-level list, never from caller text,
// so a value can never reach the SQL grammar.
func projection(columns []string) string {
	return strings.Join(columns, ", ")
}

// scanCartLine reads one row of cartItemColumns.
func scanCartLine(row baserepo.Row) (*model.CartLine, error) {
	var line model.CartLine
	if err := row.Scan(
		&line.ProductID,
		&line.Quantity,
		&line.UnitPrice.Amount,
		&line.UnitPrice.Currency,
	); err != nil {
		return nil, fmt.Errorf("scan cart line: %w", err)
	}
	return &line, nil
}

// classify labels a failed cart write and maps the account-uniqueness refusal to
// the sentinel the use case retries on. Any other failure is returned wrapped.
func (r *CartRepository) classify(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraintCartsUserKey {
		return domainerr.ErrCartAlreadyExists
	}
	return fmt.Errorf("%s: %w", operation, err)
}
