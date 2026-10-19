package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// uniqueViolationCode is the SQLSTATE PostgreSQL reports for a unique-index
// rejection (23505), and feedbackOrderKey is the one-per-order index whose
// rejection means a second feedback was attempted (FR-016, research D8).
const (
	uniqueViolationCode = "23505"
	feedbackOrderKey    = "order_feedback_order_key"
)

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
	"facebook_url",
	"payment_expires_at",
	"order_version",
	"confirmed_at",
	"delivered_at",
	"completed_at",
	"completed_by",
	"created_at",
	"updated_at",
}

// photoColumns is the explicit projection of a delivery-evidence row, matching
// the scan arity of scanDeliveryPhoto.
var photoColumns = []string{
	"id",
	"order_id",
	"stage",
	"public_id",
	"url",
	"width",
	"height",
	"position",
	"created_at",
}

// feedbackColumns is the explicit projection of an order-feedback row, matching
// the scan arity of scanFeedback.
var feedbackColumns = []string{
	"id",
	"order_id",
	"author_id",
	"content",
	"created_at",
}

// feedbackPhotoColumns is the explicit projection of a feedback-photo row,
// matching the scan arity of scanFeedbackPhoto.
var feedbackPhotoColumns = []string{
	"id",
	"feedback_id",
	"public_id",
	"url",
	"width",
	"height",
	"position",
	"created_at",
}

// lineColumns is the explicit projection of an order line, matching the scan
// arity of scanLine.
var lineColumns = []string{
	"id",
	"order_id",
	"product_id",
	"variant_id",
	"name",
	"product_code",
	"variant_name",
	"sku",
	"unit_price_amount",
	"currency",
	"quantity",
	"position",
	"created_at",
	"category_id",
	"category_name",
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
			 facebook_url,
			 payment_expires_at, order_version, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
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
		order.Address.FacebookURL,
		order.PaymentExpiresAt,
		order.Version,
		order.ConfirmedAt,
		order.CreatedAt,
		order.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	const lineQuery = `
		INSERT INTO order_items
			(id, order_id, product_id, variant_id, name, product_code, variant_name, sku, unit_price_amount, currency, quantity, position, created_at, category_id, category_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	for i := range order.Lines {
		line := order.Lines[i]
		categoryID, categoryName := categoryColumnValues(line.Category)
		if _, err := r.querier(ctx).Exec(ctx, lineQuery,
			line.ID,
			line.OrderID,
			line.ProductID,
			line.VariantID,
			line.ProductName,
			line.ProductCode,
			line.VariantName,
			line.SKU,
			line.UnitPrice.Amount,
			line.UnitPrice.Currency,
			line.Quantity,
			line.Position,
			line.CreatedAt,
			categoryID,
			categoryName,
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
	if err := r.loadDeliveryFacts(ctx, order); err != nil {
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
	if err := r.loadDeliveryFacts(ctx, order); err != nil {
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
	return r.listSummaries(ctx, ownerID, true, nil, constant.SortNewest, page, size, total)
}

// ListAll returns one page of every order, optionally narrowed to one state and
// ordered as asked, and the total number of them. The default is newest first, the
// operator's general list; status=StatusPending with SortOldest is the FIFO
// confirmation queue (FR-021, FR-026, research D11).
func (r *OrderRepository) ListAll(ctx context.Context, page, size int, status *constant.Status, sort constant.OrderListSort) ([]model.OrderSummary, int64, error) {
	page, size = normalizePage(page, size)

	args := make([]any, 0, 1)
	where := ""
	if status != nil {
		args = append(args, string(*status))
		where = " WHERE status = $1"
	}
	var total int64
	if err := r.querier(ctx).QueryRow(ctx, `SELECT count(*) FROM orders`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}
	return r.listSummaries(ctx, uuid.Nil, false, status, sort, page, size, total)
}

// listSummaries runs the shared list projection, filtering by owner when byOwner
// is set and by state when status is set, and ordering newest-first unless the
// caller asked for oldest-first (the FIFO confirmation queue). The line count is
// computed in the same statement so the operator's and the customer's lists carry
// it without loading the lines (data-model.md).
func (r *OrderRepository) listSummaries(ctx context.Context, ownerID uuid.UUID, byOwner bool, status *constant.Status, sort constant.OrderListSort, page, size int, total int64) ([]model.OrderSummary, int64, error) {
	args := make([]any, 0, 3)
	where := ""
	if byOwner {
		args = append(args, ownerID)
		where = " WHERE o.user_id = $1"
	}
	if status != nil {
		args = append(args, string(*status))
		clause := fmt.Sprintf("o.status = $%d", len(args))
		if where == "" {
			where = " WHERE " + clause
		} else {
			where += " AND " + clause
		}
	}
	args = append(args, size, (page-1)*size)

	order := "o.created_at DESC, o.id DESC"
	if sort == constant.SortOldest {
		order = "o.created_at ASC, o.id ASC"
	}

	query := fmt.Sprintf(`
		SELECT o.id, o.user_id, o.status, o.total_amount, o.currency, o.created_at,
		       COALESCE(c.item_count, 0)
		FROM orders o
		LEFT JOIN (
			SELECT order_id, count(*) AS item_count
			FROM order_items
			GROUP BY order_id
		) c ON c.order_id = o.id%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, where, order, len(args)-1, len(args))

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

// ListExpiredPending returns the identifiers of the orders awaiting payment
// (PAYMENT_PENDING) whose payment deadline has passed at the given instant, oldest
// deadline first, so a sweep is reproducible. It uses orders_expiry_idx
// (status, payment_expires_at) and compares against the instant the caller passed,
// never the database clock, so the order's sweep and the inventory's agree on what
// has expired. An order awaiting the artist has a NULL deadline and is never
// selected (FR-008, FR-009, research D5).
func (r *OrderRepository) ListExpiredPending(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	const query = `SELECT id FROM orders WHERE status = $1 AND payment_expires_at IS NOT NULL AND payment_expires_at <= $2 ORDER BY payment_expires_at, id`
	rows, err := r.querier(ctx).Query(ctx, query, string(constant.StatusPaymentPending), now)
	if err != nil {
		return nil, fmt.Errorf("list expired orders: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired order id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveConfirm persists a confirmed order: its new state, its bumped content
// version, the confirmation instant and the payment deadline, touching updated_at.
// It is the confirmation's single write, run after every line has been held in the
// same transaction, so a confirmed order never awaits payment without its goods
// (FR-005, FR-006, research D3).
func (r *OrderRepository) SaveConfirm(ctx context.Context, order *model.Order, now time.Time) error {
	const query = `
		UPDATE orders
		SET status = $2, order_version = $3, confirmed_at = $4, payment_expires_at = $5, updated_at = $6
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query,
		order.ID,
		string(order.Status),
		order.Version,
		order.ConfirmedAt,
		order.PaymentExpiresAt,
		now,
	)
	if err != nil {
		return fmt.Errorf("save order confirmation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// ReplaceLines replaces one order's lines with the given set — it deletes the old
// set and inserts the new one — and persists the order's total, address, state,
// content version and deadline, touching updated_at. It is the edit's single write
// and joins the caller's transaction, so the line set and the order commit
// together or not at all (FR-015, FR-016, research D6).
func (r *OrderRepository) ReplaceLines(ctx context.Context, order *model.Order, now time.Time) error {
	if _, err := r.querier(ctx).Exec(ctx, `DELETE FROM order_items WHERE order_id = $1`, order.ID); err != nil {
		return fmt.Errorf("delete order lines: %w", err)
	}

	const lineQuery = `
		INSERT INTO order_items
			(id, order_id, product_id, variant_id, name, product_code, variant_name, sku, unit_price_amount, currency, quantity, position, created_at, category_id, category_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	for i := range order.Lines {
		line := order.Lines[i]
		lineID := line.ID
		if lineID == uuid.Nil {
			lineID = uuid.New()
		}
		categoryID, categoryName := categoryColumnValues(line.Category)
		if _, err := r.querier(ctx).Exec(ctx, lineQuery,
			lineID,
			order.ID,
			line.ProductID,
			line.VariantID,
			line.ProductName,
			line.ProductCode,
			line.VariantName,
			line.SKU,
			line.UnitPrice.Amount,
			line.UnitPrice.Currency,
			line.Quantity,
			line.Position,
			line.CreatedAt,
			categoryID,
			categoryName,
		); err != nil {
			return fmt.Errorf("insert order line: %w", err)
		}
	}

	const orderQuery = `
		UPDATE orders
		SET status = $2, total_amount = $3, currency = $4,
		    recipient_name = $5, recipient_phone = $6,
		    province_code = $7, province_name = $8, ward_code = $9, ward_name = $10, street_address = $11,
		    facebook_url = $12,
		    order_version = $13, confirmed_at = $14, payment_expires_at = $15, updated_at = $16
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, orderQuery,
		order.ID,
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
		order.Address.FacebookURL,
		order.Version,
		order.ConfirmedAt,
		order.PaymentExpiresAt,
		now,
	)
	if err != nil {
		return fmt.Errorf("update edited order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// InsertEditHistory records one accepted edit's before/after content snapshot, the
// actor and the version, in the same transaction as the edit (FR-017, research
// D14). The content is stored as JSON documents; it is read whole, never queried
// by field.
func (r *OrderRepository) InsertEditHistory(ctx context.Context, record model.EditHistory) error {
	before, err := json.Marshal(record.Before)
	if err != nil {
		return fmt.Errorf("marshal edit history before: %w", err)
	}
	after, err := json.Marshal(record.After)
	if err != nil {
		return fmt.Errorf("marshal edit history after: %w", err)
	}
	id := record.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	const query = `
		INSERT INTO order_edit_history
			(id, order_id, version, actor_id, "before", "after", created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := r.querier(ctx).Exec(ctx, query,
		id,
		record.OrderID,
		record.Version,
		record.ActorID,
		before,
		after,
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert order edit history: %w", err)
	}
	return nil
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

// StartShipping persists a paid order's dispatch: its new SHIPPING state and its
// updated_at, or domainerr.ErrNotFound when no order carries the identifier
// (FR-005).
func (r *OrderRepository) StartShipping(ctx context.Context, order *model.Order, now time.Time) error {
	const query = `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, order.ID, string(order.Status), now)
	if err != nil {
		return fmt.Errorf("save order shipping: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// SaveDelivered persists an in-transit order's delivery: its new DELIVERED state,
// the delivered instant and its updated_at, or domainerr.ErrNotFound when no
// order carries the identifier (FR-007, FR-008).
func (r *OrderRepository) SaveDelivered(ctx context.Context, order *model.Order, now time.Time) error {
	const query = `UPDATE orders SET status = $2, delivered_at = $3, updated_at = $4 WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, order.ID, string(order.Status), order.DeliveredAt, now)
	if err != nil {
		return fmt.Errorf("save order delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// SaveCompleted persists a delivered order's completion: its new COMPLETED state,
// the completion instant, the acting customer (nil for the sweep) and its
// updated_at, or domainerr.ErrNotFound when no order carries the identifier
// (FR-014).
func (r *OrderRepository) SaveCompleted(ctx context.Context, order *model.Order, now time.Time) error {
	const query = `UPDATE orders SET status = $2, completed_at = $3, completed_by = $4, updated_at = $5 WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, order.ID, string(order.Status), order.CompletedAt, order.CompletedBy, now)
	if err != nil {
		return fmt.Errorf("save order completion: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// ListDeliveryPhotos returns one order's delivery evidence, ordered by stage then
// position then id, so the detail read projects it deterministically (FR-010,
// research D3).
func (r *OrderRepository) ListDeliveryPhotos(ctx context.Context, orderID uuid.UUID) ([]model.DeliveryPhoto, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM order_delivery_photos WHERE order_id = $1 ORDER BY stage, position, id`,
		projection(photoColumns))
	rows, err := r.querier(ctx).Query(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order delivery photos: %w", err)
	}
	defer rows.Close()

	out := make([]model.DeliveryPhoto, 0)
	for rows.Next() {
		photo, err := scanDeliveryPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *photo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AddDeliveryPhotos inserts every photo of one order's delivery step, each stamped
// with the caller's instant, in one statement per row. The per-stage ceiling was
// checked under the order's row lock before this write (FR-006, FR-008, research
// D5).
func (r *OrderRepository) AddDeliveryPhotos(ctx context.Context, orderID uuid.UUID, photos []model.DeliveryPhoto, now time.Time) error {
	const query = `
		INSERT INTO order_delivery_photos
			(id, order_id, stage, public_id, url, width, height, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	for i := range photos {
		photo := photos[i]
		photoID := photo.ID
		if photoID == uuid.Nil {
			photoID = uuid.New()
		}
		if _, err := r.querier(ctx).Exec(ctx, query,
			photoID,
			orderID,
			string(photo.Stage),
			photo.PublicID,
			photo.URL,
			photo.Width,
			photo.Height,
			photo.Position,
			now,
		); err != nil {
			return fmt.Errorf("insert order delivery photo: %w", err)
		}
	}
	return nil
}

// CountDeliveryPhotos returns how many photos one order already carries at the
// given stage, so the use case can enforce the five-photo ceiling under the row
// lock (FR-006, FR-008, research D3, D5).
func (r *OrderRepository) CountDeliveryPhotos(ctx context.Context, orderID uuid.UUID, stage model.Stage) (int, error) {
	const query = `SELECT count(*) FROM order_delivery_photos WHERE order_id = $1 AND stage = $2`
	var count int
	if err := r.querier(ctx).QueryRow(ctx, query, orderID, string(stage)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count order delivery photos: %w", err)
	}
	return count, nil
}

// InsertFeedback records the customer's one order-level feedback. The unique index
// on (order_id) is the storage-level guarantee that an order takes at most one
// feedback, so a second insert is refused by the database (FR-016, research D8).
//
// A rejection by that index is translated to domainerr.ErrFeedbackAlreadySent, so
// a submission that loses a race to a concurrent one still answers the documented
// refusal rather than leaking a storage error. Any other failure is wrapped as
// usual.
func (r *OrderRepository) InsertFeedback(ctx context.Context, feedback model.Feedback) error {
	id := feedback.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	const query = `
		INSERT INTO order_feedback (id, order_id, author_id, content, created_at)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := r.querier(ctx).Exec(ctx, query,
		id,
		feedback.OrderID,
		feedback.AuthorID,
		feedback.Content,
		feedback.CreatedAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == feedbackOrderKey {
			return domainerr.ErrFeedbackAlreadySent
		}
		return fmt.Errorf("insert order feedback: %w", err)
	}
	return nil
}

// FindFeedback returns the order's feedback, or domainerr.ErrNotFound when it has
// none, so the detail read projects it (FR-015, FR-017). Its photos are attached,
// ordered by position then id (FR-025).
func (r *OrderRepository) FindFeedback(ctx context.Context, orderID uuid.UUID) (*model.Feedback, error) {
	query := fmt.Sprintf(`SELECT %s FROM order_feedback WHERE order_id = $1`, projection(feedbackColumns))
	feedback, err := scanFeedback(r.querier(ctx).QueryRow(ctx, query, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	photos, err := r.ListFeedbackPhotos(ctx, feedback.ID)
	if err != nil {
		return nil, err
	}
	feedback.Photos = photos
	return feedback, nil
}

// InsertFeedbackPhotos inserts every photo of one feedback, each stamped with the
// caller's instant, in one statement per row. The five-photo ceiling was checked
// before this write (FR-025, research D16).
func (r *OrderRepository) InsertFeedbackPhotos(ctx context.Context, feedbackID uuid.UUID, photos []model.FeedbackPhoto, now time.Time) error {
	const query = `
		INSERT INTO order_feedback_photos
			(id, feedback_id, public_id, url, width, height, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	for i := range photos {
		photo := photos[i]
		photoID := photo.ID
		if photoID == uuid.Nil {
			photoID = uuid.New()
		}
		if _, err := r.querier(ctx).Exec(ctx, query,
			photoID,
			feedbackID,
			photo.PublicID,
			photo.URL,
			photo.Width,
			photo.Height,
			photo.Position,
			now,
		); err != nil {
			return fmt.Errorf("insert order feedback photo: %w", err)
		}
	}
	return nil
}

// CountFeedbackPhotos returns how many photos one feedback carries, so the use
// case can enforce the five-photo ceiling (FR-025, research D16).
func (r *OrderRepository) CountFeedbackPhotos(ctx context.Context, feedbackID uuid.UUID) (int, error) {
	const query = `SELECT count(*) FROM order_feedback_photos WHERE feedback_id = $1`
	var count int
	if err := r.querier(ctx).QueryRow(ctx, query, feedbackID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count order feedback photos: %w", err)
	}
	return count, nil
}

// ListFeedbackPhotos returns one feedback's photos, ordered by position then id,
// so the detail read projects them deterministically (FR-025, research D16).
func (r *OrderRepository) ListFeedbackPhotos(ctx context.Context, feedbackID uuid.UUID) ([]model.FeedbackPhoto, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM order_feedback_photos WHERE feedback_id = $1 ORDER BY position, id`,
		projection(feedbackPhotoColumns))
	rows, err := r.querier(ctx).Query(ctx, query, feedbackID)
	if err != nil {
		return nil, fmt.Errorf("list order feedback photos: %w", err)
	}
	defer rows.Close()

	out := make([]model.FeedbackPhoto, 0)
	for rows.Next() {
		photo, err := scanFeedbackPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *photo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAwaitingCompletion returns the identifiers of the orders that are DELIVERED
// and whose delivered_at is at or before the cutoff, oldest delivered first, so
// the completion sweep is reproducible (FR-012, research D6). It uses
// orders_completion_idx (status, delivered_at) and compares against the instant
// the caller passed, never the database clock.
func (r *OrderRepository) ListAwaitingCompletion(ctx context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	const query = `SELECT id FROM orders WHERE status = $1 AND delivered_at IS NOT NULL AND delivered_at <= $2 ORDER BY delivered_at, id`
	rows, err := r.querier(ctx).Query(ctx, query, string(constant.StatusDelivered), cutoff)
	if err != nil {
		return nil, fmt.Errorf("list orders awaiting completion: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan awaiting-completion order id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FindPendingOrdersByVariants returns the identifiers of the PENDING or
// PAYMENT_PENDING orders that hold at least one of the given variants, so a
// hide guard can refuse while one is pending (specs/016-hide-guard research
// D1, data-model.md).
//
// It is one bounded statement however many variants are passed: the order lines
// are joined to their orders and filtered to the two pending states, so hiding
// a category of hundreds of variants stays bounded (spec SC-004). An empty
// input answers an empty slice without touching the database. It joins the
// caller's transaction.
func (r *OrderRepository) FindPendingOrdersByVariants(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(variantIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	const query = `
		SELECT DISTINCT oi.order_id
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE oi.variant_id = ANY($1)
		  AND o.status IN ($2, $3)
		ORDER BY oi.order_id`
	rows, err := r.querier(ctx).Query(ctx, query,
		variantIDs,
		string(constant.StatusPending),
		string(constant.StatusPaymentPending),
	)
	if err != nil {
		return nil, fmt.Errorf("find pending orders by variants: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending order id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// KPIs answers the dashboard's revenue and order figures in one
// conditional-aggregate query (FR-010, FR-012, FR-013, FR-014, research D7). The
// two windows are supplied by the application in the shop timezone; revenue counts
// only the four earned states — PAID, SHIPPING, DELIVERED and COMPLETED — and VND,
// while the order counts include every state. The sums are cast to bigint so the
// adapter never reads a numeric.
func (r *OrderRepository) KPIs(ctx context.Context, current, last model.TimeWindow) (model.OrderKPIs, error) {
	const query = `
		SELECT
			COALESCE(sum(total_amount) FILTER (
				WHERE status IN ('PAID', 'SHIPPING', 'DELIVERED', 'COMPLETED')
				  AND currency = 'VND'
				  AND created_at >= $1 AND created_at < $2
			), 0)::bigint,
			COALESCE(sum(total_amount) FILTER (
				WHERE status IN ('PAID', 'SHIPPING', 'DELIVERED', 'COMPLETED')
				  AND currency = 'VND'
				  AND created_at >= $3 AND created_at < $4
			), 0)::bigint,
			count(*) FILTER (WHERE created_at >= $1 AND created_at < $2)::bigint,
			count(*) FILTER (WHERE created_at >= $3 AND created_at < $4)::bigint,
			count(*)::bigint,
			count(*) FILTER (WHERE status = 'PENDING')::bigint
		FROM orders`
	var kpis model.OrderKPIs
	if err := r.querier(ctx).QueryRow(ctx, query,
		current.From, current.To,
		last.From, last.To,
	).Scan(
		&kpis.CurrentRevenue.Amount,
		&kpis.LastRevenue.Amount,
		&kpis.CurrentOrders,
		&kpis.LastOrders,
		&kpis.TotalOrders,
		&kpis.PendingConfirmation,
	); err != nil {
		return model.OrderKPIs{}, fmt.Errorf("read order kpis: %w", err)
	}
	return kpis, nil
}

// DailyRevenue answers the window's revenue grouped by shop-timezone day. The
// database buckets with the IANA name the application supplied, so a late-evening
// UTC instant lands on the operator's calendar day; the application zero-fills the
// days the result does not carry (FR-019, research D7).
func (r *OrderRepository) DailyRevenue(ctx context.Context, window model.TimeWindow, timeZone string) ([]model.DailyRevenue, error) {
	const query = `
		SELECT to_char(o.created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
		       sum(o.total_amount)::bigint AS amount
		FROM orders o
		WHERE o.status IN ('PAID', 'SHIPPING', 'DELIVERED', 'COMPLETED')
		  AND o.currency = 'VND'
		  AND o.created_at >= $1 AND o.created_at < $2
		GROUP BY day
		ORDER BY day`
	rows, err := r.querier(ctx).Query(ctx, query, window.From, window.To, timeZone)
	if err != nil {
		return nil, fmt.Errorf("read daily revenue: %w", err)
	}
	defer rows.Close()

	out := make([]model.DailyRevenue, 0)
	for rows.Next() {
		var day model.DailyRevenue
		if err := rows.Scan(&day.Date, &day.Amount.Amount); err != nil {
			return nil, fmt.Errorf("scan daily revenue: %w", err)
		}
		out = append(out, day)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CategoryRevenue answers the window's revenue grouped by the category snapshot on
// each order line. Line totals are the same numbers that make up orders.total_amount,
// so the breakdown (including the unresolved bucket) reconciles with the revenue KPI.
// A null category_id is returned as a nil id; the application names that bucket
// "Khác" (FR-021, FR-023, research D7).
func (r *OrderRepository) CategoryRevenue(ctx context.Context, window model.TimeWindow) ([]model.CategoryRevenue, error) {
	const query = `
		SELECT oi.category_id,
		       COALESCE(oi.category_name, '') AS category_name,
		       sum(oi.quantity * oi.unit_price_amount)::bigint AS amount
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		WHERE o.status IN ('PAID', 'SHIPPING', 'DELIVERED', 'COMPLETED')
		  AND o.currency = 'VND'
		  AND o.created_at >= $1 AND o.created_at < $2
		GROUP BY oi.category_id, oi.category_name
		ORDER BY amount DESC, category_name ASC, oi.category_id ASC`
	rows, err := r.querier(ctx).Query(ctx, query, window.From, window.To)
	if err != nil {
		return nil, fmt.Errorf("read category revenue: %w", err)
	}
	defer rows.Close()

	out := make([]model.CategoryRevenue, 0)
	for rows.Next() {
		var revenue model.CategoryRevenue
		if err := rows.Scan(&revenue.CategoryID, &revenue.CategoryName, &revenue.Amount.Amount); err != nil {
			return nil, fmt.Errorf("scan category revenue: %w", err)
		}
		out = append(out, revenue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RecentOrders answers at most limit newest orders, newest first (created_at then
// id). Two lateral projections keep the statement single and bounded: the first
// line by position and the line/quantity counts. Any order state is included
// (FR-025, FR-026, research D10).
func (r *OrderRepository) RecentOrders(ctx context.Context, limit int) ([]model.RecentOrder, error) {
	if limit <= 0 {
		return []model.RecentOrder{}, nil
	}
	const query = `
		SELECT o.id, o.user_id, o.total_amount, o.currency, o.status, o.created_at,
		       COALESCE(first_line.name, '') AS first_line_name,
		       COALESCE(first_line.variant_name, '') AS first_line_variant_name,
		       COALESCE(first_line.quantity, 0)::bigint AS first_line_quantity,
		       counts.line_count,
		       counts.total_quantity
		FROM orders o
		LEFT JOIN LATERAL (
			SELECT oi.name, oi.variant_name, oi.quantity
			FROM order_items oi
			WHERE oi.order_id = o.id
			ORDER BY oi.position, oi.id
			LIMIT 1
		) first_line ON true
		LEFT JOIN LATERAL (
			SELECT count(*)::bigint AS line_count,
			       COALESCE(sum(oi.quantity), 0)::bigint AS total_quantity
			FROM order_items oi
			WHERE oi.order_id = o.id
		) counts ON true
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT $1`
	rows, err := r.querier(ctx).Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("read recent orders: %w", err)
	}
	defer rows.Close()

	out := make([]model.RecentOrder, 0, limit)
	for rows.Next() {
		var (
			recent model.RecentOrder
			status string
		)
		if err := rows.Scan(
			&recent.ID,
			&recent.CustomerID,
			&recent.Total.Amount,
			&recent.Total.Currency,
			&status,
			&recent.CreatedAt,
			&recent.FirstLineName,
			&recent.FirstLineVariantName,
			&recent.FirstLineQuantity,
			&recent.LineCount,
			&recent.TotalQuantity,
		); err != nil {
			return nil, fmt.Errorf("scan recent order: %w", err)
		}
		recent.Status = constant.Status(status)
		out = append(out, recent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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

// loadDeliveryFacts reads one order's delivery evidence and its feedback and
// attaches them, so the detail read projects them while a summary read does not
// (FR-010, FR-015, research D11). An order with no feedback carries a nil Feedback,
// not an error.
func (r *OrderRepository) loadDeliveryFacts(ctx context.Context, order *model.Order) error {
	photos, err := r.ListDeliveryPhotos(ctx, order.ID)
	if err != nil {
		return err
	}
	order.DeliveryPhotos = photos

	feedback, err := r.FindFeedback(ctx, order.ID)
	if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
		return err
	}
	order.Feedback = feedback
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
		&order.Address.FacebookURL,
		&order.PaymentExpiresAt,
		&order.Version,
		&order.ConfirmedAt,
		&order.DeliveredAt,
		&order.CompletedAt,
		&order.CompletedBy,
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
	var (
		line         model.OrderLine
		categoryID   *uuid.UUID
		categoryName *string
	)
	if err := row.Scan(
		&line.ID,
		&line.OrderID,
		&line.ProductID,
		&line.VariantID,
		&line.ProductName,
		&line.ProductCode,
		&line.VariantName,
		&line.SKU,
		&line.UnitPrice.Amount,
		&line.UnitPrice.Currency,
		&line.Quantity,
		&line.Position,
		&line.CreatedAt,
		&categoryID,
		&categoryName,
	); err != nil {
		return nil, fmt.Errorf("scan order line: %w", err)
	}
	if categoryID != nil && categoryName != nil {
		line.Category = &model.CategorySnapshot{ID: *categoryID, Name: *categoryName}
	}
	return &line, nil
}

// categoryColumnValues flattens an optional category snapshot into the two
// nullable column values the line inserts write. A nil snapshot — or one whose
// identifier is the nil UUID, which no real category carries — writes two NULLs,
// the state order_items_category_ck accepts for a legacy line (T007).
func categoryColumnValues(category *model.CategorySnapshot) (id, name any) {
	if category == nil || category.ID == uuid.Nil {
		return nil, nil
	}
	return category.ID, category.Name
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

// scanDeliveryPhoto reads one row of photoColumns.
func scanDeliveryPhoto(row baserepo.Row) (*model.DeliveryPhoto, error) {
	var (
		photo model.DeliveryPhoto
		stage string
	)
	if err := row.Scan(
		&photo.ID,
		&photo.OrderID,
		&stage,
		&photo.PublicID,
		&photo.URL,
		&photo.Width,
		&photo.Height,
		&photo.Position,
		&photo.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan order delivery photo: %w", err)
	}
	photo.Stage = model.Stage(stage)
	return &photo, nil
}

// scanFeedback reads one row of feedbackColumns. A no-rows error is returned
// unwrapped so the caller can recognise it; any other failure is wrapped.
func scanFeedback(row baserepo.Row) (*model.Feedback, error) {
	var feedback model.Feedback
	if err := row.Scan(
		&feedback.ID,
		&feedback.OrderID,
		&feedback.AuthorID,
		&feedback.Content,
		&feedback.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("scan order feedback: %w", err)
	}
	return &feedback, nil
}

// scanFeedbackPhoto reads one row of feedbackPhotoColumns.
func scanFeedbackPhoto(row baserepo.Row) (*model.FeedbackPhoto, error) {
	var photo model.FeedbackPhoto
	if err := row.Scan(
		&photo.ID,
		&photo.FeedbackID,
		&photo.PublicID,
		&photo.URL,
		&photo.Width,
		&photo.Height,
		&photo.Position,
		&photo.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan order feedback photo: %w", err)
	}
	return &photo, nil
}
