package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the level table this module owns. The ledger and the holds live in
// inventory_transactions and stock_holds; this adapter is the only code that reads
// or writes all three.
const table = "stock_levels"

// The constraint names that carry a meaning this adapter has to translate. A
// missing product is attributed by the constraint name PostgreSQL reports, never
// by the text of its message, because the message is localized and free to change
// while the constraint name is part of the schema (research D2, D12).
const (
	constraintLevelProduct       = "stock_levels_product_fk"
	constraintTransactionProduct = "inventory_transactions_product_fk"
	constraintTransactionSource  = "inventory_transactions_source_key"
	constraintHoldProduct        = "stock_holds_product_fk"
	constraintHoldActive         = "stock_holds_active_key"
)

// foreignKeyViolationCode is reported for a foreign-key rejection (23503) and
// uniqueViolationCode for a unique-index rejection (23505).
const (
	foreignKeyViolationCode = "23503"
	uniqueViolationCode     = "23505"
)

// stockColumns is the explicit projection of a level row, matching the scan arity
// of scanStock. It declares the shape rather than following the physical table, so
// a later ALTER TABLE cannot break a scan. Held is not a column: it is derived from
// the active holds and is never selected here (research D1).
var stockColumns = []string{
	"product_id",
	"quantity",
	"updated_at",
}

// movementColumns is the projection every movement read selects, matching the scan
// arity of scanMovement.
var movementColumns = []string{
	"id",
	"product_id",
	"kind",
	"delta",
	"resulting_quantity",
	"source_reference",
	"actor_id",
	"note",
	"created_at",
}

// holdColumns is the projection every hold read selects, matching the scan arity
// of scanHold.
var holdColumns = []string{
	"id",
	"product_id",
	"order_id",
	"quantity",
	"status",
	"expires_at",
	"created_at",
	"resolved_at",
}

// InventoryRepository persists the level, the ledger and the holds.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. It never calls the generic reads: every
// query here carries a predicate — the active-hold sum, the conditional decrease,
// the ordered history — the generic listing cannot express. No method opens a
// transaction; each joins the transaction the application put in the context
// (Constitution I).
type InventoryRepository struct {
	baserepo.Base[model.Stock, uuid.UUID]
	pool *pgxpool.Pool
}

// NewInventoryRepository creates the inventory repository.
func NewInventoryRepository(pool *pgxpool.Pool) *InventoryRepository {
	r := &InventoryRepository{pool: pool}
	r.Base = baserepo.Base[model.Stock, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "product_id",
		Columns:  stockColumns,
		IDValue:  func(s *model.Stock) any { return s.ProductID },
		Scan:     scanStock,
	}
	return r
}

var _ domainrepo.InventoryRepository = (*InventoryRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the adapter
// joins a caller's transaction without ever opening one itself (Constitution I).
func (r *InventoryRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Level returns the stored physical quantity. A product with no row answers zero,
// because a product that has never been stocked is understood as zero (research
// D12).
func (r *InventoryRepository) Level(ctx context.Context, productID uuid.UUID) (int64, error) {
	const query = `SELECT quantity FROM stock_levels WHERE product_id = $1`
	var quantity int64
	err := r.querier(ctx).QueryRow(ctx, query, productID).Scan(&quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read stock level: %w", err)
	}
	return quantity, nil
}

// Increase adds amount to the product's quantity, materialising the level row on
// the first call, and returns the new quantity. The upsert is what makes a level
// row appear lazily; the foreign key refuses a row for a product that does not
// exist (research D12).
func (r *InventoryRepository) Increase(ctx context.Context, productID uuid.UUID, amount int64, now time.Time) (int64, error) {
	const query = `
		INSERT INTO stock_levels (product_id, quantity, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (product_id) DO UPDATE
		SET quantity   = stock_levels.quantity + EXCLUDED.quantity,
		    updated_at = EXCLUDED.updated_at
		RETURNING quantity`
	var quantity int64
	if err := r.querier(ctx).QueryRow(ctx, query, productID, amount, now).Scan(&quantity); err != nil {
		return 0, classifyWriteError("increase stock level", err)
	}
	return quantity, nil
}

// Decrease removes amount from the product's quantity and returns the new
// quantity. It is a conditional update, so the storage itself evaluates the
// no-negative rule against the current value; an update that matches no row is
// reported as the insufficient-stock sentinel, which is what lets the use case
// answer 409 rather than 500 (FR-010, research D2).
func (r *InventoryRepository) Decrease(ctx context.Context, productID uuid.UUID, amount int64, now time.Time) (int64, error) {
	const query = `
		UPDATE stock_levels
		SET quantity   = quantity - $2,
		    updated_at = $3
		WHERE product_id = $1 AND quantity >= $2
		RETURNING quantity`
	var quantity int64
	err := r.querier(ctx).QueryRow(ctx, query, productID, amount, now).Scan(&quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainerr.InsufficientStock(model.FieldQuantity, 0, amount)
	}
	if err != nil {
		return 0, fmt.Errorf("decrease stock level: %w", err)
	}
	return quantity, nil
}

// LockLevel takes the level's row lock inside the caller's transaction. A product
// with no level row has nothing to lock and answers success, because its
// availability is zero and no hold can be placed against it (research D2, D12).
func (r *InventoryRepository) LockLevel(ctx context.Context, productID uuid.UUID) error {
	const query = `SELECT product_id FROM stock_levels WHERE product_id = $1 FOR UPDATE`
	var locked uuid.UUID
	err := r.querier(ctx).QueryRow(ctx, query, productID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock stock level: %w", err)
	}
	return nil
}

// InsertMovement appends one immutable ledger row. A product no row carries is
// reported as the not-found sentinel through the foreign key; a duplicate source
// reference is refused by the partial unique index and left for the use case to
// interpret (FR-020, FR-022).
func (r *InventoryRepository) InsertMovement(ctx context.Context, movement *model.Movement) error {
	const query = `
		INSERT INTO inventory_transactions
			(id, product_id, kind, delta, resulting_quantity, source_reference, actor_id, note, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.querier(ctx).Exec(ctx, query,
		movement.ID,
		movement.ProductID,
		string(movement.Kind),
		movement.Delta,
		movement.ResultingQuantity,
		movement.SourceReference,
		movement.ActorID,
		movement.Note,
		movement.CreatedAt,
	)
	if err != nil {
		return classifyWriteError("insert inventory movement", err)
	}
	return nil
}

// Movements returns one page of a product's changes, oldest first, ordered by
// created_at then id so the order is stable across two reads sharing a timestamp
// (FR-008, research D14), plus the total number of them.
func (r *InventoryRepository) Movements(ctx context.Context, productID uuid.UUID, page, pageSize int) ([]model.Movement, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	var total int64
	const countQuery = `SELECT count(*) FROM inventory_transactions WHERE product_id = $1`
	if err := r.querier(ctx).QueryRow(ctx, countQuery, productID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count inventory movements: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT %s FROM inventory_transactions
		WHERE product_id = $1
		ORDER BY created_at, id
		LIMIT $2 OFFSET $3`, projection(movementColumns))
	rows, err := r.querier(ctx).Query(ctx, query, productID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list inventory movements: %w", err)
	}
	defer rows.Close()

	out := make([]model.Movement, 0, pageSize)
	for rows.Next() {
		movement, err := scanMovement(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *movement)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// InsertHold records one active hold. A product no row carries is reported as the
// not-found sentinel through the foreign key; a second active hold for the same
// order and product is refused by the partial unique index (FR-019).
func (r *InventoryRepository) InsertHold(ctx context.Context, hold *model.Hold) error {
	const query = `
		INSERT INTO stock_holds
			(id, product_id, order_id, quantity, status, expires_at, created_at, resolved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.querier(ctx).Exec(ctx, query,
		hold.ID,
		hold.ProductID,
		hold.OrderID,
		hold.Quantity,
		string(hold.Status),
		hold.ExpiresAt,
		hold.CreatedAt,
		hold.ResolvedAt,
	)
	if err != nil {
		return classifyWriteError("insert stock hold", err)
	}
	return nil
}

// ActiveHeld returns the total quantity a product's active holds set aside at the
// given instant. The comparison is against the instant the caller passed, never
// the database clock, so the sum and the sweeper agree on what has expired
// (FR-018, research D6, D15).
func (r *InventoryRepository) ActiveHeld(ctx context.Context, productID uuid.UUID, now time.Time) (int64, error) {
	const query = `
		SELECT COALESCE(sum(quantity), 0)
		FROM stock_holds
		WHERE product_id = $1 AND status = 'ACTIVE' AND expires_at > $2`
	var held int64
	if err := r.querier(ctx).QueryRow(ctx, query, productID, now).Scan(&held); err != nil {
		return 0, fmt.Errorf("sum active stock holds: %w", err)
	}
	return held, nil
}

// FindActiveHold returns the active hold of one order and product, reporting
// whether one exists. An expired hold is not active even before it is swept, so a
// payment arriving after expiry finds nothing to consume (FR-015, research D6).
func (r *InventoryRepository) FindActiveHold(ctx context.Context, orderID, productID uuid.UUID, now time.Time) (*model.Hold, bool, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM stock_holds
		WHERE order_id = $1 AND product_id = $2 AND status = 'ACTIVE' AND expires_at > $3`,
		projection(holdColumns))
	hold, err := scanHold(r.querier(ctx).QueryRow(ctx, query, orderID, productID, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find active stock hold: %w", err)
	}
	return hold, true, nil
}

// ResolveHold ends one still-active hold with the given status and moment. A hold
// that is already resolved is reported as the invalid-value sentinel, so a retried
// release or consume cannot resolve the same hold twice (FR-019).
func (r *InventoryRepository) ResolveHold(ctx context.Context, holdID uuid.UUID, status constant.HoldStatus, at time.Time) error {
	const query = `UPDATE stock_holds SET status = $2, resolved_at = $3 WHERE id = $1 AND status = 'ACTIVE'`
	tag, err := r.querier(ctx).Exec(ctx, query, holdID, string(status), at)
	if err != nil {
		return fmt.Errorf("resolve stock hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.InvalidValue(model.FieldHold, "is already resolved")
	}
	return nil
}

// ExpiredHolds returns the active holds whose expiry has passed at the given
// instant, ordered by expiry then id so a sweep is reproducible. The comparison is
// against the instant the caller passed, never the database clock (FR-015, research
// D6, D15).
func (r *InventoryRepository) ExpiredHolds(ctx context.Context, now time.Time) ([]model.Hold, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM stock_holds
		WHERE status = 'ACTIVE' AND expires_at <= $1
		ORDER BY expires_at, id`, projection(holdColumns))
	rows, err := r.querier(ctx).Query(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("list expired stock holds: %w", err)
	}
	defer rows.Close()

	out := make([]model.Hold, 0)
	for rows.Next() {
		hold, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *hold)
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
	out := ""
	for i, column := range columns {
		if i > 0 {
			out += ", "
		}
		out += column
	}
	return out
}

// scanStock reads one row of stockColumns into a level. It is the Scan the generic
// Base is configured with; the adapter's own reads return quantities rather than a
// whole entity, so it is only exercised through Base.
func scanStock(row baserepo.Row) (*model.Stock, error) {
	var stock model.Stock
	if err := row.Scan(&stock.ProductID, &stock.Quantity, &stock.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan stock level: %w", err)
	}
	return &stock, nil
}

// scanMovement reads one row of movementColumns.
func scanMovement(row baserepo.Row) (*model.Movement, error) {
	var (
		movement model.Movement
		kind     string
	)
	if err := row.Scan(
		&movement.ID,
		&movement.ProductID,
		&kind,
		&movement.Delta,
		&movement.ResultingQuantity,
		&movement.SourceReference,
		&movement.ActorID,
		&movement.Note,
		&movement.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan inventory movement: %w", err)
	}
	movement.Kind = constant.MovementKind(kind)
	return &movement, nil
}

// scanHold reads one row of holdColumns. A no-rows error is returned unwrapped so
// the caller can recognise it (FindActiveHold); any other failure is wrapped.
func scanHold(row baserepo.Row) (*model.Hold, error) {
	var (
		hold   model.Hold
		status string
	)
	err := row.Scan(
		&hold.ID,
		&hold.ProductID,
		&hold.OrderID,
		&hold.Quantity,
		&status,
		&hold.ExpiresAt,
		&hold.CreatedAt,
		&hold.ResolvedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("scan stock hold: %w", err)
	}
	hold.Status = constant.HoldStatus(status)
	return &hold, nil
}

// classifyWriteError labels a failed write and maps the storage guard to the
// module's sentinel. A foreign-key refusal of a missing product becomes the
// not-found sentinel (research D2, D12); a refusal of a second active hold by the
// partial unique index becomes ErrHoldAlreadyExists, which the reserve use case
// treats as already applied rather than a failure (FR-019); and a refusal of a
// movement whose source reference another movement already carries becomes
// ErrAlreadyApplied, which the sale use case answers as success so a concurrent
// duplicate changes stock only once (FR-020 to FR-022, research D5). Any other
// failure is returned wrapped.
func classifyWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case foreignKeyViolationCode:
			switch pgErr.ConstraintName {
			case constraintLevelProduct, constraintTransactionProduct, constraintHoldProduct:
				return domainerr.ErrProductNotFound
			}
		case uniqueViolationCode:
			switch pgErr.ConstraintName {
			case constraintHoldActive:
				return domainerr.ErrHoldAlreadyExists
			case constraintTransactionSource:
				return domainerr.ErrAlreadyApplied
			}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
