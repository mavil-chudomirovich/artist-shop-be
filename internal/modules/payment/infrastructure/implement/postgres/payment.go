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

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the payment table this adapter owns. The events live in payment_events;
// this adapter is the only module code that reads or writes both.
const table = "payments"

// orderCodeKey is the unique index the Create path translates into a retryable
// collision, so the use case can generate a fresh code without inspecting a
// PostgreSQL error (research D4).
const orderCodeKey = "payments_order_code_key"

// livePaymentKey is the partial unique index that enforces at most one live
// payment per order. A concurrent start that lost the insert race is reported as
// domainerr.ErrLivePaymentExists, so the use case reuses the winner's payment
// instead of failing (FR-005, Constitution II).
const livePaymentKey = "payments_live_order_key"

// uniqueViolationCode is reported for a unique-index rejection (23505).
const uniqueViolationCode = "23505"

// paymentColumns is the explicit projection of a payment row, matching the scan
// arity of scanPayment. It declares the shape rather than following the physical
// table, so a later ALTER TABLE cannot break a scan.
var paymentColumns = []string{
	"id",
	"order_id",
	"user_id",
	"order_code",
	"amount",
	"currency",
	"description",
	"order_version",
	"status",
	"provider",
	"provider_reference",
	"payment_link_id",
	"checkout_url",
	"qr_code",
	"unmatched_reason",
	"created_at",
	"updated_at",
}

// PaymentRepository persists the payment and its events.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. No method opens a transaction; each joins
// the transaction the application put in the context (Constitution I).
type PaymentRepository struct {
	baserepo.Base[model.Payment, uuid.UUID]
	pool *pgxpool.Pool
}

// NewPaymentRepository creates the payment repository.
func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	r := &PaymentRepository{pool: pool}
	r.Base = baserepo.Base[model.Payment, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "id",
		Columns:  paymentColumns,
		IDValue:  func(payment *model.Payment) any { return payment.ID },
		Scan:     scanPayment,
	}
	return r
}

var _ domainrepo.PaymentRepository = (*PaymentRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the adapter
// joins a caller's transaction without ever opening one itself (Constitution I).
func (r *PaymentRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create inserts a payment. The unique index on order_code is what refuses a
// collision, reported as domainerr.ErrOrderCodeCollision so the caller can retry
// with a fresh code; the partial unique index on the live PENDING row is what
// refuses a second live payment for an order, reported as
// domainerr.ErrLivePaymentExists so the caller reuses the winner's (FR-005,
// Constitution II). Nullable columns are stored as NULL rather than the empty
// string the model carries, so the partial unique index on provider_reference is
// not fooled by an empty value (research D4, D7).
func (r *PaymentRepository) Create(ctx context.Context, payment *model.Payment) error {
	const query = `
		INSERT INTO payments
			(id, order_id, user_id, order_code, amount, currency, description,
			 order_version, status, provider, provider_reference, payment_link_id,
			 checkout_url, qr_code, unmatched_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
	if _, err := r.querier(ctx).Exec(ctx, query,
		payment.ID,
		payment.OrderID,
		payment.UserID,
		payment.OrderCode,
		payment.Amount,
		payment.Currency,
		payment.Description,
		payment.OrderVersion,
		string(payment.Status),
		payment.Provider,
		nullIfEmpty(payment.ProviderReference),
		nullIfEmpty(payment.PaymentLinkID),
		nullIfEmpty(payment.CheckoutURL),
		nullIfEmpty(payment.QRCode),
		nullIfEmpty(payment.UnmatchedReason),
		payment.CreatedAt,
		payment.UpdatedAt,
	); err != nil {
		if isUniqueViolation(err, orderCodeKey) {
			return domainerr.ErrOrderCodeCollision
		}
		if isUniqueViolation(err, livePaymentKey) {
			return domainerr.ErrLivePaymentExists
		}
		return fmt.Errorf("create payment: %w", err)
	}
	return nil
}

// CreateEvent appends one event row to a payment's history — the module's own
// audit trail (data-model.md, FR-010, FR-022). An empty detail is stored as NULL,
// and a nil actor (the provider's webhook) is stored as NULL (FR-019).
func (r *PaymentRepository) CreateEvent(ctx context.Context, event *model.PaymentEvent) error {
	const query = `
		INSERT INTO payment_events (id, payment_id, action, actor_id, detail, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := r.querier(ctx).Exec(ctx, query,
		event.ID,
		event.PaymentID,
		event.Action,
		event.ActorID,
		nullIfEmpty(event.Detail),
		event.CreatedAt,
	); err != nil {
		return fmt.Errorf("create payment event: %w", err)
	}
	return nil
}

// FindByID returns one payment, or domainerr.ErrNotFound.
func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Payment, error) {
	query := fmt.Sprintf(`SELECT %s FROM payments WHERE id = $1`, projection(paymentColumns))
	return r.find(ctx, query, id)
}

// FindByOrder returns every payment of one order, newest first (created_at then
// id).
func (r *PaymentRepository) FindByOrder(ctx context.Context, orderID uuid.UUID) ([]model.Payment, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM payments WHERE order_id = $1 ORDER BY created_at DESC, id DESC`,
		projection(paymentColumns))
	return r.list(ctx, query, orderID)
}

// FindLiveByOrder returns the live (PENDING) payment of one order, or
// domainerr.ErrNotFound when the order has none. A cancelled order whose payment is
// already terminal answers not-found, so a second cancel is a no-op.
func (r *PaymentRepository) FindLiveByOrder(ctx context.Context, orderID uuid.UUID) (*model.Payment, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM payments WHERE order_id = $1 AND status = $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		projection(paymentColumns))
	return r.find(ctx, query, orderID, string(constant.StatusPending))
}

// FindByOrderCode returns the payment carrying the provider's order code, or
// domainerr.ErrNotFound. It is the webhook's lookup.
func (r *PaymentRepository) FindByOrderCode(ctx context.Context, orderCode int64) (*model.Payment, error) {
	query := fmt.Sprintf(`SELECT %s FROM payments WHERE order_code = $1`, projection(paymentColumns))
	return r.find(ctx, query, orderCode)
}

// ListAll returns one page of every payment, newest first, optionally narrowed to
// one state, and the total number of them. status=UNMATCHED is the reconciliation
// queue (FR-018, research D15).
func (r *PaymentRepository) ListAll(ctx context.Context, page, size int, status *constant.Status) ([]model.Payment, int64, error) {
	page, size = normalizePage(page, size)

	args := make([]any, 0, 1)
	where := ""
	if status != nil {
		args = append(args, string(*status))
		where = " WHERE status = $1"
	}

	var total int64
	if err := r.querier(ctx).QueryRow(ctx, `SELECT count(*) FROM payments`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count payments: %w", err)
	}

	listArgs := append(append([]any{}, args...), size, (page-1)*size)
	query := fmt.Sprintf(
		`SELECT %s FROM payments%s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		projection(paymentColumns), where, len(listArgs)-1, len(listArgs))
	payments, err := r.list(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	return payments, total, nil
}

// LockByID returns one payment under its row lock, for a transition. The lock
// serialises two concurrent confirmations of the same webhook so the apply happens
// once (FR-009, Constitution II).
func (r *PaymentRepository) LockByID(ctx context.Context, id uuid.UUID) (*model.Payment, error) {
	query := fmt.Sprintf(`SELECT %s FROM payments WHERE id = $1 FOR UPDATE`, projection(paymentColumns))
	return r.find(ctx, query, id)
}

// UpdateStatus writes one payment's status and touches updated_at. It is the
// cancel path, where no provider reference or reason is written.
func (r *PaymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status constant.Status, now time.Time) error {
	return r.update(ctx, id, `UPDATE payments SET status = $2, updated_at = $3 WHERE id = $1`, string(status), now)
}

// ApplyPaid marks a payment PAID and stores the provider's transaction reference in
// one write, so the status and the reference it belongs to commit together
// (FR-009, research D7).
func (r *PaymentRepository) ApplyPaid(ctx context.Context, id uuid.UUID, providerReference string, now time.Time) error {
	return r.update(ctx, id, `UPDATE payments SET status = $2, provider_reference = $3, updated_at = $4 WHERE id = $1`,
		string(constant.StatusPaid), providerReference, now)
}

// RecordUnmatched marks a payment UNMATCHED and stores the reason in one write,
// satisfying the (status = 'UNMATCHED') = (reason IS NOT NULL) check (FR-012,
// FR-013).
func (r *PaymentRepository) RecordUnmatched(ctx context.Context, id uuid.UUID, reason string, now time.Time) error {
	return r.update(ctx, id, `UPDATE payments SET status = $2, unmatched_reason = $3, updated_at = $4 WHERE id = $1`,
		string(constant.StatusUnmatched), reason, now)
}

// find runs a query that returns at most one payment, translating pgx.ErrNoRows
// into domainerr.ErrNotFound.
func (r *PaymentRepository) find(ctx context.Context, query string, args ...any) (*model.Payment, error) {
	payment, err := scanPayment(r.querier(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return payment, nil
}

// list runs a query that returns many payments.
func (r *PaymentRepository) list(ctx context.Context, query string, args ...any) ([]model.Payment, error) {
	rows, err := r.querier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	out := make([]model.Payment, 0)
	for rows.Next() {
		payment, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *payment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// update runs a narrow write and reports a missing row as domainerr.ErrNotFound.
func (r *PaymentRepository) update(ctx context.Context, id uuid.UUID, query string, args ...any) error {
	tag, err := r.querier(ctx).Exec(ctx, query, append([]any{id}, args...)...)
	if err != nil {
		return fmt.Errorf("update payment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrNotFound
	}
	return nil
}

// scanPayment reads one row of paymentColumns. A no-rows error is returned
// unwrapped so the caller can recognise it; any other failure is wrapped. The
// nullable columns are read through pointers so NULL becomes the model's empty
// string.
func scanPayment(row baserepo.Row) (*model.Payment, error) {
	var (
		payment           model.Payment
		status            string
		providerReference *string
		paymentLinkID     *string
		checkoutURL       *string
		qrCode            *string
		unmatchedReason   *string
	)
	err := row.Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.UserID,
		&payment.OrderCode,
		&payment.Amount,
		&payment.Currency,
		&payment.Description,
		&payment.OrderVersion,
		&status,
		&payment.Provider,
		&providerReference,
		&paymentLinkID,
		&checkoutURL,
		&qrCode,
		&unmatchedReason,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("scan payment: %w", err)
	}
	payment.Status = constant.Status(status)
	payment.ProviderReference = deref(providerReference)
	payment.PaymentLinkID = deref(paymentLinkID)
	payment.CheckoutURL = deref(checkoutURL)
	payment.QRCode = deref(qrCode)
	payment.UnmatchedReason = deref(unmatchedReason)
	return &payment, nil
}

// isUniqueViolation reports whether err is a unique-index rejection of the named
// constraint. A missing product is attributed by the constraint name PostgreSQL
// reports, never by the text of its message (research D4).
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraint
	}
	return false
}

// normalizePage applies the module's pagination convention: a page below 1 becomes
// 1, and a size outside 1..100 becomes the default 20 (research D15).
func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	return page, size
}

// projection joins a declared column list for a SELECT. Every query builds its
// projection from a package-level list, never from caller text.
func projection(columns []string) string {
	return strings.Join(columns, ", ")
}

// nullIfEmpty stores an empty column as NULL, so absence is one value rather than
// two. It matters for provider_reference: the partial unique index applies only
// WHERE provider_reference IS NOT NULL, so an empty string must never be stored.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// deref reads a nullable text column into the model's empty string.
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
