package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the order table this adapter owns. The lines live in order_items; this
// adapter is the only code that reads or writes both.
const table = "orders"

// orderColumns is the explicit projection of an order row, matching the scan
// arity of scanOrder. It declares the shape rather than following the physical
// table, so a later ALTER TABLE cannot break a scan.
var orderColumns = []string{
	"id",
	"user_id",
	"status",
	"total_amount",
	"currency",
	"recipient_name",
	"recipient_phone",
	"province_code",
	"province_name",
	"ward_code",
	"ward_name",
	"street_address",
	"expires_at",
	"created_at",
	"updated_at",
}

// lineColumns is the explicit projection of an order line, matching the scan
// arity of scanLine.
var lineColumns = []string{
	"id",
	"order_id",
	"product_id",
	"name",
	"slug",
	"unit_price_amount",
	"currency",
	"quantity",
	"position",
	"created_at",
}

// OrderRepository persists the order and its lines.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. It never calls the generic reads: every
// query here carries a predicate — the owner lookup, the row lock, the ordered
// line load, the list projections — the generic CRUD cannot express. No method
// opens a transaction; each joins the transaction the application put in the
// context (Constitution I).
type OrderRepository struct {
	baserepo.Base[model.Order, uuid.UUID]
	pool *pgxpool.Pool
}

// NewOrderRepository creates the order repository.
func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
	r := &OrderRepository{pool: pool}
	r.Base = baserepo.Base[model.Order, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "id",
		Columns:  orderColumns,
		IDValue:  func(order *model.Order) any { return order.ID },
		Scan:     scanOrder,
	}
	return r
}

var _ domainrepo.OrderRepository = (*OrderRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the adapter
// joins a caller's transaction without ever opening one itself (Constitution I).
func (r *OrderRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create inserts the order and its lines. The order is written first, then each
// line in the order it was supplied; both join the caller's transaction, so the
// order and its lines commit together or not at all (FR-001, Constitution II).
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) error {
	const orderQuery = `
		INSERT INTO orders
			(id, user_id, status, total_amount, currency,
			 recipient_name, recipient_phone,
			 province_code, province_name, ward_code, ward_name, street_address,
			 expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	if _, err := r.querier(ctx).Exec(ctx, orderQuery,
		order.ID,
		order.UserID,
		string(order.Status),
		order.Total.Amount,
		order.Total.Currency,
		order.Address.RecipientName,
		order.Address.RecipientPhone,
		order.Address.ProvinceCode,
		order.Address.ProvinceName,
		order.Address.WardCode,
		order.Address.WardName,
		order.Address.StreetAddress,
		order.ExpiresAt,
		order.CreatedAt,
		order.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	const lineQuery = `
		INSERT INTO order_items
			(id, order_id, product_id, name, slug, unit_price_amount, currency, quantity, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	for i := range order.Lines {
		line := order.Lines[i]
		if _, err := r.querier(ctx).Exec(ctx, lineQuery,
			line.ID,
			line.OrderID,
			line.ProductID,
			line.Name,
			line.Slug,
			line.UnitPrice.Amount,
			line.UnitPrice.Currency,
			line.Quantity,
			line.Position,
			line.CreatedAt,
		); err != nil {
			return fmt.Errorf("create order line: %w", err)
		}
	}
	return nil
}

// FindByID returns one order with its lines in position order, or
// domainerr.ErrNotFound. It is the operator's read (FR-021).
func (r *OrderRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Order, error) {
	query := fmt.Sprintf(`SELECT %s FROM orders WHERE id = $1`, projection(orderColumns))
	order, err := scanOrder(r.querier(ctx).QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// FindByOwner returns one order with its lines only when it belongs to ownerID,
// or domainerr.ErrNotFound. An order that belongs to another customer answers the
// same not-found an unknown one does, so the route never confirms another
// customer's order (FR-020).
func (r *OrderRepository) FindByOwner(ctx context.Context, ownerID, id uuid.UUID) (*model.Order, error) {
	query := fmt.Sprintf(`SELECT %s FROM orders WHERE id = $1 AND user_id = $2`, projection(orderColumns))
	order, err := scanOrder(r.querier(ctx).QueryRow(ctx, query, id, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// LockByID returns one order with its lines under the order's row lock, for a
// transition. The lock serialises two concurrent moves on the same order so the
// domain transition reads the current state under it (FR-010).
func (r *OrderRepository) LockByID(ctx context.Context, id uuid.UUID) (*model.Order, error) {
	query := fmt.Sprintf(`SELECT %s FROM orders WHERE id = $1 FOR UPDATE`, projection(orderColumns))
	order, err := scanOrder(r.querier(ctx).QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// ListOwner returns one page of one owner's orders, newest first (created_at then
// id), and the total number of them (FR-018, research D14).
func (r *OrderRepository) ListOwner(ctx context.Context, ownerID uuid.UUID, page, size int) ([]model.OrderSummary, int64, error) {
	page, size = normalizePage(page, size)

	var total int64
	const countQuery = `SELECT count(*) FROM orders WHERE user_id = $1`
	if err := r.querier(ctx).QueryRow(ctx, countQuery, ownerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders by owner: %w", err)
	}
	return r.listSummaries(ctx, ownerID, true, page, size, total)
}

// ListAll returns one page of every order, newest first (created_at then id), and
// the total number of them (FR-021, research D14).
func (r *OrderRepository) ListAll(ctx context.Context, page, size int) ([]model.OrderSummary, int64, error) {
	page, size = normalizePage(page, size)

	var total int64
	if err := r.querier(ctx).QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}
	return r.listSummaries(ctx, uuid.Nil, false, page, size, total)
}

// listSummaries runs the shared newest-first list projection, filtering by owner
// when byOwner is set. The line count is computed in the same statement so the
// operator's and the customer's lists carry it without loading the lines
// (data-model.md).
func (r *OrderRepository) listSummaries(ctx context.Context, ownerID uuid.UUID, byOwner bool, page, size int, total int64) ([]model.OrderSummary, int64, error) {
	where := ""
	args := make([]any, 0, 3)
	if byOwner {
		where = " WHERE o.user_id = $1"
		args = append(args, ownerID)
	}
	args = append(args, size, (page-1)*size)

	query := fmt.Sprintf(`
		SELECT o.id, o.user_id, o.status, o.total_amount, o.currency, o.created_at,
		       COALESCE(c.item_count, 0)
		FROM orders o
		LEFT JOIN (
			SELECT order_id, count(*) AS item_count
			FROM order_items
			GROUP BY order_id
		) c ON c.order_id = o.id%s
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.querier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	out := make([]model.OrderSummary, 0, size)
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *summary)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateStatus writes one order's state and touches its updated_at. The state is
// written only after the domain transition has accepted the move, so the row
// always carries a state the domain allows (FR-010).
func (r *OrderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status constant.Status, now time.Time) error {
	const query = `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id, string(status), now)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// UpdateOwner changes one order's owner and touches its updated_at. A transfer
// changes only the owner; the state, the lines and the totals are untouched
// (FR-024, research D12).
func (r *OrderRepository) UpdateOwner(ctx context.Context, id, ownerID uuid.UUID, now time.Time) error {
	const query = `UPDATE orders SET user_id = $2, updated_at = $3 WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id, ownerID, now)
	if err != nil {
		return fmt.Errorf("update order owner: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// loadLines reads one order's lines in position order and attaches them, so a
// detail read never depends on a product still existing (research D3).
func (r *OrderRepository) loadLines(ctx context.Context, order *model.Order) error {
	query := fmt.Sprintf(
		`SELECT %s FROM order_items WHERE order_id = $1 ORDER BY position, id`,
		projection(lineColumns))
	rows, err := r.querier(ctx).Query(ctx, query, order.ID)
	if err != nil {
		return fmt.Errorf("list order lines: %w", err)
	}
	defer rows.Close()

	out := make([]model.OrderLine, 0)
	for rows.Next() {
		line, err := scanLine(rows)
		if err != nil {
			return err
		}
		out = append(out, *line)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	order.Lines = out
	return nil
}

// normalizePage applies the module's pagination convention, mirroring the shared
// generic repository: a page below 1 becomes 1, and a size outside 1..100 becomes
// the default 20, so a caller that passes an absent or out-of-range window reads
// the same page the foundation's other lists do (research D14).
func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	return page, size
}

// projection joins a declared column list for a SELECT. Every query in this
// adapter builds its projection from a package-level list, never from caller text,
// so a value can never reach the SQL grammar.
func projection(columns []string) string {
	return strings.Join(columns, ", ")
}

// scanOrder reads one row of orderColumns. A no-rows error is returned unwrapped
// so the caller can recognise it; any other failure is wrapped.
func scanOrder(row baserepo.Row) (*model.Order, error) {
	var (
		order  model.Order
		status string
	)
	err := row.Scan(
		&order.ID,
		&order.UserID,
		&status,
		&order.Total.Amount,
		&order.Total.Currency,
		&order.Address.RecipientName,
		&order.Address.RecipientPhone,
		&order.Address.ProvinceCode,
		&order.Address.ProvinceName,
		&order.Address.WardCode,
		&order.Address.WardName,
		&order.Address.StreetAddress,
		&order.ExpiresAt,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("scan order: %w", err)
	}
	order.Status = constant.Status(status)
	return &order, nil
}

// scanLine reads one row of lineColumns.
func scanLine(row baserepo.Row) (*model.OrderLine, error) {
	var line model.OrderLine
	if err := row.Scan(
		&line.ID,
		&line.OrderID,
		&line.ProductID,
		&line.Name,
		&line.Slug,
		&line.UnitPrice.Amount,
		&line.UnitPrice.Currency,
		&line.Quantity,
		&line.Position,
		&line.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan order line: %w", err)
	}
	return &line, nil
}

// scanSummary reads one row of the list projection.
func scanSummary(row baserepo.Row) (*model.OrderSummary, error) {
	var (
		summary model.OrderSummary
		status  string
	)
	if err := row.Scan(
		&summary.ID,
		&summary.UserID,
		&status,
		&summary.Total.Amount,
		&summary.Total.Currency,
		&summary.CreatedAt,
		&summary.ItemCount,
	); err != nil {
		return nil, fmt.Errorf("scan order summary: %w", err)
	}
	summary.Status = constant.Status(status)
	return &summary, nil
}
