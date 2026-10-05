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

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// AddressRepository persists the shipping addresses this module owns.
//
// It embeds share/repository.Base for its projection plumbing and sets an explicit
// Columns projection, because there is no `SELECT *` fallback: a star projection
// ties the scan arity to the physical table, so a plain
// `ALTER TABLE addresses ADD COLUMN` would break every repository embedding Base.
// The same rule governs this adapter's own queries — see projection.
//
// Two columns are never writable through this adapter:
//
//   - is_default, because the flag changes only through the ClearDefault/SetDefault
//     transition inside the caller's transaction (FR-010). The update statement
//     simply does not name it, which is what makes an edit preserve the flag by
//     construction (FR-011).
//   - deleted_at, because an address is hidden, never deleted (ADR-004).
//
// Every read filters on deleted_at IS NULL, so a hidden address is indistinguishable
// from an unknown one and can never leak into a customer's list (research D4).
type AddressRepository struct {
	baserepo.Base[model.Address, uuid.UUID]
	pool *pgxpool.Pool
}

// addressColumns is the projection every read of the addresses table selects. It
// matches the destinations of scanAddress exactly, and it is declared here once so
// the Base projection and the adapter's own statements cannot drift apart.
var addressColumns = []string{
	"id",
	"user_id",
	"recipient_name",
	"recipient_phone",
	"province_code",
	"province_name",
	"ward_code",
	"ward_name",
	"street_address",
	"is_default",
	"deleted_at",
	"created_at",
	"updated_at",
}

// NewAddressRepository creates the address repository.
func NewAddressRepository(pool *pgxpool.Pool) *AddressRepository {
	r := &AddressRepository{pool: pool}
	r.Base = baserepo.Base[model.Address, uuid.UUID]{
		Pool:     pool,
		Table:    "addresses",
		IDColumn: "id",
		// The read projection is exactly what scanAddress reads: the key, the
		// owning account, the five stored text members, the two state columns and
		// the two row timestamps the listing orders by.
		Columns: addressColumns,
		IDValue: func(a *model.Address) any { return a.ID },
		Scan:    scanAddress,
	}
	return r
}

var _ domainrepo.AddressRepository = (*AddressRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the adapter
// joins a caller's transaction without ever opening one itself (Constitution I).
// That is what makes the two writes of the default-flag transition one indivisible
// outcome (FR-010).
func (r *AddressRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// projection returns the SELECT list for this adapter's own queries.
//
// An empty projection is a configuration error, reported before the database is
// touched, exactly as the generic read paths do. There is deliberately no
// `SELECT *` fallback anywhere in this adapter: a projection that follows the
// physical table makes the scan arity silently depend on the schema.
func (r *AddressRepository) projection() (string, error) {
	if len(r.Columns) == 0 {
		return "", fmt.Errorf("repository %s: Columns is empty, it must list the projection matching Scan", r.Table)
	}
	return strings.Join(r.Columns, ", "), nil
}

// Create inserts a new address of an account.
//
// The stored province and ward names travel in their own columns rather than being
// derived at read time, so a later rename of an administrative unit cannot rewrite
// what the customer entered (research D10).
func (r *AddressRepository) Create(ctx context.Context, address *model.Address) error {
	const query = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default, deleted_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	_, err := r.querier(ctx).Exec(ctx, query,
		address.ID,
		address.UserID,
		address.RecipientName,
		address.RecipientPhone,
		address.ProvinceCode,
		address.ProvinceName,
		address.WardCode,
		address.WardName,
		address.StreetAddress,
		address.IsDefault,
		address.DeletedAt,
		address.CreatedAt,
		address.UpdatedAt,
	)
	if err != nil {
		return wrapWriteError("insert address", err)
	}
	return nil
}

// Update writes the editable columns of the address, preserving the default flag.
//
// The statement does not name is_default or deleted_at, so an edit can neither move
// the account's default nor resurrect a hidden row whatever the caller handed over
// (FR-011, ADR-004). The owner and the hidden marker are part of the WHERE clause,
// so the write is scoped to the account that owns a visible address and reports the
// same not-found error as a read would (FR-013).
func (r *AddressRepository) Update(ctx context.Context, address *model.Address) error {
	const query = `
		UPDATE addresses
		SET recipient_name  = $3,
		    recipient_phone = $4,
		    province_code   = $5,
		    province_name   = $6,
		    ward_code       = $7,
		    ward_name       = $8,
		    street_address  = $9,
		    updated_at      = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	tag, err := r.querier(ctx).Exec(ctx, query,
		address.ID,
		address.UserID,
		address.RecipientName,
		address.RecipientPhone,
		address.ProvinceCode,
		address.ProvinceName,
		address.WardCode,
		address.WardName,
		address.StreetAddress,
	)
	if err != nil {
		return fmt.Errorf("update address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrAddressNotFound
	}
	return nil
}

// FindByOwner returns one non-hidden address of the given account.
//
// The owner and the hidden marker are both part of the WHERE clause, so an unknown
// id, a hidden address and another customer's address are all reported as the same
// domainerr.ErrAddressNotFound. The response therefore never confirms that an
// address somebody else owns exists (FR-013, SC-003).
func (r *AddressRepository) FindByOwner(ctx context.Context, userID, addressID uuid.UUID) (*model.Address, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	// The identifiers are bound first so the projection and the scan destinations
	// stay independent of where the filters fall.
	const query = `
		SELECT %s FROM addresses
		WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL`
	row := r.querier(ctx).QueryRow(ctx, fmt.Sprintf(query, list), userID, addressID)
	return scanAddress(row)
}

// ListByOwner returns a page of the account's non-hidden addresses plus the total
// count of them.
//
// The order is the default address first and then the most recently updated, with
// the key breaking ties, so a page boundary cannot show the same address twice or
// drop one (FR-018). The default-first ordering is also what lets the order module
// offer the default as the starting choice (FR-007d).
func (r *AddressRepository) ListByOwner(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]model.Address, int64, error) {
	list, err := r.projection()
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	// The total counts every non-hidden address of the account, not just the page,
	// so a client knows how many pages there are.
	const countQuery = `
		SELECT count(*) FROM addresses WHERE user_id = $1 AND deleted_at IS NULL`
	var total int64
	if err := r.querier(ctx).QueryRow(ctx, countQuery, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count addresses: %w", err)
	}

	const pageQuery = `
		SELECT %s FROM addresses
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY is_default DESC, updated_at DESC, id
		LIMIT $2 OFFSET $3`
	rows, err := r.querier(ctx).Query(ctx, fmt.Sprintf(pageQuery, list),
		userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list addresses: %w", err)
	}
	defer rows.Close()

	// An empty page is a nil slice rather than an allocated empty one, so the
	// mapper's "no addresses" answer stays a JSON null instead of [].
	out := make([]model.Address, 0, pageSize)
	for rows.Next() {
		address, err := scanAddress(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *address)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ClearDefault removes the default flag from every non-hidden address of the
// account.
//
// It is always the first half of the SetDefault transition; running it alone would
// leave the account with no default at all, which is why the transition runs inside
// the caller's transaction (FR-010).
func (r *AddressRepository) ClearDefault(ctx context.Context, userID uuid.UUID) error {
	const query = `
		UPDATE addresses SET is_default = false
		WHERE user_id = $1 AND is_default AND deleted_at IS NULL`
	if _, err := r.querier(ctx).Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("clear the default address: %w", err)
	}
	return nil
}

// SetDefault marks one address as the account's default.
//
// It takes no owner identifier: the caller resolves ownership with FindByOwner
// inside the same transaction and MUST run ClearDefault immediately before this
// call, so the previous default is already gone when the partial unique index on
// (user_id) WHERE is_default AND deleted_at IS NULL judges the write (ADR-003).
// Setting the flag without clearing first is therefore rejected by the database, and
// the error names the index instead of swallowing it.
//
// The hidden marker stays in the WHERE clause: a hidden address can never be the
// default, whether it was hidden before or by a race with this statement (SC-004).
func (r *AddressRepository) SetDefault(ctx context.Context, addressID uuid.UUID) error {
	const query = `
		UPDATE addresses SET is_default = true, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.querier(ctx).Exec(ctx, query, addressID)
	if err != nil {
		return wrapWriteError("set the default address", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrAddressNotFound
	}
	return nil
}

// Hide stamps the address with hiddenAt instead of deleting the row.
//
// Clearing the flag in the same statement is what keeps SC-004 true: with
// deleted_at set, the partial unique index stops counting the row, so a second
// default could be created beside it unless the flag went with it (ADR-003,
// ADR-004). The address text is left intact so past orders keep the address the
// customer actually used (FR-012).
func (r *AddressRepository) Hide(ctx context.Context, userID, addressID uuid.UUID, hiddenAt time.Time) error {
	const query = `
		UPDATE addresses SET deleted_at = $3, is_default = false, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	tag, err := r.querier(ctx).Exec(ctx, query, addressID, userID, hiddenAt)
	if err != nil {
		return fmt.Errorf("hide address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrAddressNotFound
	}
	return nil
}

// wrapWriteError labels a failed write and names the index when the partial unique
// index refused it, so the storage-level guarantee is visible in the log instead of
// surfacing as an anonymous constraint violation.
func wrapWriteError(operation string, err error) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%s: %w (a second default address for one account is rejected by addresses_one_default_per_user)",
			operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// isUniqueViolation reports whether the storage layer refused a unique constraint.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// scanAddress reads one row of the projection declared in NewAddressRepository. The
// number of destinations must match that projection exactly.
func scanAddress(row baserepo.Row) (*model.Address, error) {
	var address model.Address
	err := row.Scan(
		&address.ID,
		&address.UserID,
		&address.RecipientName,
		&address.RecipientPhone,
		&address.ProvinceCode,
		&address.ProvinceName,
		&address.WardCode,
		&address.WardName,
		&address.StreetAddress,
		&address.IsDefault,
		&address.DeletedAt,
		&address.CreatedAt,
		&address.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrAddressNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan address: %w", err)
	}
	return &address, nil
}
