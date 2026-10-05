//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the address adapter against a real PostgreSQL container.
//
// Its centre of gravity is the "at most one default address" invariant, which
// ADR-003 requires to hold at the *storage* layer: the tests here write the
// is_default column directly, bypassing the use case, and still must not be able
// to create a second default. A test that only went through the use case would
// pass with the index dropped, which is the defect ADR-003 calls out.

type addressFixture struct {
	pool  *pgxpool.Pool
	repo  *AddressRepository
	owner uuid.UUID
}

func newAddressFixture(t *testing.T) *addressFixture {
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

	owner := uuid.New()
	const seed = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, 'address-owner@example.com', 'not-used', 'CUSTOMER', 'active')`
	if _, err := pool.Exec(ctx, seed, owner); err != nil {
		t.Fatalf("seed the owning account: %v", err)
	}
	return &addressFixture{pool: pool, repo: NewAddressRepository(pool), owner: owner}
}

// insert writes one row straight into the table, deliberately bypassing the
// adapter, so a storage-level rule can be proven to be a storage-level rule.
func (f *addressFixture) insert(t *testing.T, isDefault bool, deletedAt *time.Time, provinceCode, wardCode, street string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default, deleted_at, created_at, updated_at)
		VALUES ($1, $2, 'Nguyen Van A', '0912345678', $3, 'Captured province',
			$4, 'Captured ward', $5, $6, $7, now(), now())`
	if _, err := f.pool.Exec(context.Background(), query,
		id, f.owner, provinceCode, wardCode, street, isDefault, deletedAt); err != nil {
		t.Fatalf("insert a raw address row: %v", err)
	}
	return id
}

// defaultCount reads the invariant straight from the storage layer.
func (f *addressFixture) defaultCount(t *testing.T) int {
	t.Helper()
	var total int
	const query = `
		SELECT count(*) FROM addresses
		WHERE user_id = $1 AND is_default AND deleted_at IS NULL`
	if err := f.pool.QueryRow(context.Background(), query, f.owner).Scan(&total); err != nil {
		t.Fatalf("count the visible defaults: %v", err)
	}
	return total
}

// ADR-003: writing a second default row directly, with no use case and no
// transaction in the way, must be rejected by the partial unique index. This is
// the assertion that fails if the index is ever dropped in a refactor.
func TestASecondDefaultRowIsRejectedByThePartialUniqueIndex(t *testing.T) {
	f := newAddressFixture(t)
	f.insert(t, true, nil, "79", "26734", "12 Nguyen Hue")

	const second = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default)
		VALUES ($1, $2, 'Nguyen Van B', '0912345678', '79', 'Captured province',
			'26735', 'Captured ward', '1 Le Loi', true)`
	_, err := f.pool.Exec(context.Background(), second, uuid.New(), f.owner)

	if err == nil {
		t.Fatal("the storage layer accepted a second default address for one account")
	}
	if !isUniqueViolation(err) {
		t.Fatalf("expected a unique violation from addresses_one_default_per_user, got %v", err)
	}
	if got := f.defaultCount(t); got != 1 {
		t.Fatalf("expected exactly one visible default to survive, got %d", got)
	}
}

// The index predicate excludes hidden rows, so hiding the default frees the flag
// again. This is what makes ADR-004's soft delete compatible with ADR-003.
func TestTheDefaultIndexIgnoresHiddenRows(t *testing.T) {
	f := newAddressFixture(t)
	hiddenAt := time.Now().UTC()
	f.insert(t, true, &hiddenAt, "79", "26734", "12 Nguyen Hue")

	// A second default is accepted because the first one is hidden.
	f.insert(t, true, nil, "79", "26735", "1 Le Loi")

	if got := f.defaultCount(t); got != 1 {
		t.Fatalf("expected one visible default, got %d", got)
	}
}

// FR-018: the listing is paginated, returns the total and keeps a stable order —
// the default first, then the most recently updated, with the key breaking ties
// so two writes in the same instant cannot swap places between pages.
func TestListingIsPaginatedDefaultFirstAndReportsTheTotal(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()

	plain := f.insert(t, false, nil, "79", "26734", "12 Nguyen Hue")
	second := f.insert(t, false, nil, "79", "26735", "1 Le Loi")
	preferred := f.insert(t, true, nil, "79", "26736", "2 Le Loi")
	hiddenAt := time.Now().UTC()
	hidden := f.insert(t, false, &hiddenAt, "79", "26737", "3 Le Loi")

	page, total, err := f.repo.ListByOwner(ctx, f.owner, 1, 2)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 non-hidden addresses in the total, got %d", total)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 addresses on the page, got %d", len(page))
	}
	if page[0].ID != preferred || !page[0].IsDefault {
		t.Fatalf("expected the default address first, got %+v", page[0])
	}

	// The second page must not repeat the first one.
	rest, total, err := f.repo.ListByOwner(ctx, f.owner, 2, 2)
	if err != nil {
		t.Fatalf("ListByOwner page 2: %v", err)
	}
	if total != 3 || len(rest) != 1 {
		t.Fatalf("expected the last page to hold 1 of 3, got %d of %d", len(rest), total)
	}
	seen := map[uuid.UUID]bool{page[0].ID: true, page[1].ID: true}
	if seen[rest[0].ID] {
		t.Fatalf("the second page repeated %s", rest[0].ID)
	}
	for _, id := range []uuid.UUID{plain, second} {
		if !seen[id] && rest[0].ID != id {
			t.Fatalf("expected %s in the listing, got %+v and %+v", id, page[1], rest[0])
		}
	}

	// A hidden address is never listed, at any page size.
	all, total, err := f.repo.ListByOwner(ctx, f.owner, 1, 100)
	if err != nil {
		t.Fatalf("ListByOwner full: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected the hidden row to stay out of the total, got %d", total)
	}
	for _, row := range all {
		if row.ID == hidden {
			t.Fatal("a hidden address was listed")
		}
	}

	// The order does not drift between two identical reads.
	repeat, _, err := f.repo.ListByOwner(ctx, f.owner, 1, 2)
	if err != nil {
		t.Fatalf("ListByOwner repeat: %v", err)
	}
	if repeat[0].ID != page[0].ID || repeat[1].ID != page[1].ID {
		t.Fatalf("the listing order is not stable: %+v then %+v", page, repeat)
	}
}

// ADR-004: every read filters on deleted_at IS NULL, so a hidden address, an
// unknown id and another customer's address all come back as the same not-found
// error and the response can never confirm that a hidden address exists (FR-013).
func TestEveryReadFiltersHiddenRows(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	hiddenAt := time.Now().UTC()
	hidden := f.insert(t, false, &hiddenAt, "79", "26734", "12 Nguyen Hue")

	for _, id := range []uuid.UUID{hidden, uuid.New()} {
		if _, err := f.repo.FindByOwner(ctx, f.owner, id); !errors.Is(err, domainerr.ErrAddressNotFound) {
			t.Fatalf("expected ErrAddressNotFound for %s, got %v", id, err)
		}
	}

	other := uuid.New()
	const seedOther = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, 'address-other@example.com', 'not-used', 'CUSTOMER', 'active')`
	if _, err := f.pool.Exec(ctx, seedOther, other); err != nil {
		t.Fatalf("seed a second account: %v", err)
	}
	foreign := uuid.New()
	const seedForeign = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address)
		VALUES ($1, $2, 'Someone Else', '0912345678', '79', 'Captured province',
			'26734', 'Captured ward', '99 Le Loi')`
	if _, err := f.pool.Exec(ctx, seedForeign, foreign, other); err != nil {
		t.Fatalf("seed a foreign address: %v", err)
	}
	if _, err := f.repo.FindByOwner(ctx, f.owner, foreign); !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("another customer's address must not be found, got %v", err)
	}
	list, total, err := f.repo.ListByOwner(ctx, f.owner, 1, 100)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if total != 0 || len(list) != 0 {
		t.Fatalf("another customer's address leaked into the listing: %+v", list)
	}
}

// research D10: the province and ward names are captured into their own columns,
// so they survive a rename of the administrative unit and come back with the
// stored codes.
func TestTheProvinceAndWardNamesAreCapturedInTheirOwnColumns(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	id := uuid.New()
	const query = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address)
		VALUES ($1, $2, 'Nguyen Van A', '0912345678', '79', 'Ho Chi Minh (cu)',
			'11111', 'Phuong Da Xa', '12 Nguyen Hue')`
	if _, err := f.pool.Exec(ctx, query, id, f.owner); err != nil {
		t.Fatalf("insert an address with captured names: %v", err)
	}

	got, err := f.repo.FindByOwner(ctx, f.owner, id)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if got.ProvinceName != "Ho Chi Minh (cu)" || got.WardName != "Phuong Da Xa" {
		t.Fatalf("the captured names did not survive: %+v", got)
	}
	if got.ProvinceCode != "79" || got.WardCode != "11111" {
		t.Fatalf("the stored codes did not survive: %+v", got)
	}
	if got.RecipientPhone != "0912345678" || got.StreetAddress != "12 Nguyen Hue" {
		t.Fatalf("the address text is incomplete: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("both row timestamps must be read back: %+v", got)
	}
}

// FR-011: the update statement must not name is_default, so the flag survives an
// edit even when the caller hands over an entity that claims otherwise.
func TestUpdateNeverTouchesTheDefaultFlag(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	id := f.insert(t, true, nil, "79", "26734", "12 Nguyen Hue")

	stored, err := f.repo.FindByOwner(ctx, f.owner, id)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	stored.StreetAddress = "1 Le Loi"
	stored.IsDefault = false

	if err := f.repo.Update(ctx, stored); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := f.repo.FindByOwner(ctx, f.owner, id)
	if err != nil {
		t.Fatalf("FindByOwner after the update: %v", err)
	}
	if after.StreetAddress != "1 Le Loi" {
		t.Fatalf("expected the street address to be written, got %q", after.StreetAddress)
	}
	if !after.IsDefault {
		t.Fatal("an edit must preserve the default flag")
	}
}

// Hiding stamps the row and clears the flag in one statement, so an account never
// keeps a default that points at a hidden address (SC-004), and the text the row
// holds is left intact for order history (FR-012).
func TestHideStampsTheRowClearsTheFlagAndKeepsTheText(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	id := f.insert(t, true, nil, "79", "26734", "12 Nguyen Hue")

	hiddenAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := f.repo.Hide(ctx, f.owner, id, hiddenAt); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	var (
		deletedAt *time.Time
		isDefault bool
		recipient string
		province  string
		ward      string
		street    string
	)
	const query = `
		SELECT deleted_at, is_default, recipient_name, province_name, ward_name, street_address
		FROM addresses WHERE id = $1`
	if err := f.pool.QueryRow(ctx, query, id).Scan(
		&deletedAt, &isDefault, &recipient, &province, &ward, &street); err != nil {
		t.Fatalf("read the hidden row: %v", err)
	}
	if deletedAt == nil || !deletedAt.Equal(hiddenAt) {
		t.Fatalf("expected the row stamped at %v, got %v", hiddenAt, deletedAt)
	}
	if isDefault {
		t.Fatal("a hidden address must not keep the default flag")
	}
	if recipient != "Nguyen Van A" || province != "Captured province" ||
		ward != "Captured ward" || street != "12 Nguyen Hue" {
		t.Fatalf("the hidden row lost its text: %s / %s / %s / %s", recipient, province, ward, street)
	}
	if got := f.defaultCount(t); got != 0 {
		t.Fatalf("expected no visible default after hiding the only one, got %d", got)
	}
}

// Hiding a row twice or hiding somebody else's row is reported as not found, so a
// retry after a lost response is safe and cross-account access is indistinguishable
// from a typo.
func TestHideRefusesAnUnknownHiddenOrForeignRow(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	id := f.insert(t, false, nil, "79", "26734", "12 Nguyen Hue")
	now := time.Now().UTC()

	if err := f.repo.Hide(ctx, f.owner, uuid.New(), now); !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound for an unknown id, got %v", err)
	}
	if err := f.repo.Hide(ctx, f.owner, id, now); err != nil {
		t.Fatalf("first hide: %v", err)
	}
	if err := f.repo.Hide(ctx, f.owner, id, now); !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound when hiding twice, got %v", err)
	}
}

// FR-010: the two writes of the default transition run inside one transaction the
// application layer opened, and the adapter joins it through the context instead of
// opening one of its own (Constitution I). A rollback therefore undoes both.
func TestTheDefaultTransitionJoinsTheCallersTransaction(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	db := &database.DB{Pool: f.pool}

	previous := f.insert(t, true, nil, "79", "26734", "12 Nguyen Hue")
	next := f.insert(t, false, nil, "79", "26735", "1 Le Loi")

	// A rolled-back transaction leaves the previous default untouched.
	rollback := errors.New("the caller changed its mind")
	err := db.WithinTx(ctx, func(txCtx context.Context) error {
		if err := f.repo.ClearDefault(txCtx, f.owner); err != nil {
			return err
		}
		if err := f.repo.SetDefault(txCtx, next); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("expected the caller's error, got %v", err)
	}
	if got := f.defaultCount(t); got != 1 {
		t.Fatalf("a rolled-back transition left %d defaults, want 1", got)
	}
	stillPrevious, err := f.repo.FindByOwner(ctx, f.owner, previous)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if !stillPrevious.IsDefault {
		t.Fatal("a rolled-back transition must leave the previous default in place")
	}

	// The same two writes inside a committed transaction move the flag.
	if err := db.WithinTx(ctx, func(txCtx context.Context) error {
		if err := f.repo.ClearDefault(txCtx, f.owner); err != nil {
			return err
		}
		return f.repo.SetDefault(txCtx, next)
	}); err != nil {
		t.Fatalf("commit the transition: %v", err)
	}
	if got := f.defaultCount(t); got != 1 {
		t.Fatalf("expected exactly one default, got %d", got)
	}
	promoted, err := f.repo.FindByOwner(ctx, f.owner, next)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if !promoted.IsDefault {
		t.Fatal("the promoted address is not the default")
	}
	demoted, err := f.repo.FindByOwner(ctx, f.owner, previous)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if demoted.IsDefault {
		t.Fatal("the previous default was not cleared")
	}
}

// The partial unique index is the last line of defence: even a caller that skipped
// ClearDefault cannot leave two defaults behind, and the adapter says so in the
// error rather than swallowing it.
func TestSetDefaultWithoutClearingIsRejectedAndReported(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	f.insert(t, true, nil, "79", "26734", "12 Nguyen Hue")
	second := f.insert(t, false, nil, "79", "26735", "1 Le Loi")

	err := f.repo.SetDefault(ctx, second)

	if err == nil {
		t.Fatal("expected the index to reject a second default")
	}
	if !strings.Contains(err.Error(), "addresses_one_default_per_user") {
		t.Fatalf("the error should name the index, got %v", err)
	}
	if got := f.defaultCount(t); got != 1 {
		t.Fatalf("expected the single default to survive, got %d", got)
	}
}

// Setting a hidden address as the default is refused: the statement carries the
// same deleted_at IS NULL filter as every other read (SC-004).
func TestSetDefaultRefusesAHiddenAddress(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	hiddenAt := time.Now().UTC()
	id := f.insert(t, false, &hiddenAt, "79", "26734", "12 Nguyen Hue")

	err := f.repo.SetDefault(ctx, id)

	if !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound, got %v", err)
	}
	if got := f.defaultCount(t); got != 0 {
		t.Fatalf("a hidden address became the default: %d visible defaults", got)
	}
}

// The generic read paths are never used for addresses — an unscoped FindByID would
// read a hidden row and FindAll would enumerate every customer's addresses — but
// the adapter still declares its projection explicitly, and an empty projection is
// a configuration error rather than a `SELECT *` fallback.
func TestAnEmptyProjectionIsAConfigurationError(t *testing.T) {
	f := newAddressFixture(t)
	repo := NewAddressRepository(f.pool)
	repo.Columns = nil

	_, err := repo.FindByOwner(context.Background(), f.owner, uuid.New())

	if err == nil {
		t.Fatal("expected a configuration error when the projection is empty")
	}
	if !strings.Contains(err.Error(), "addresses") || !strings.Contains(err.Error(), "Columns") {
		t.Fatalf("the error should name the table and Columns, got %v", err)
	}

	if _, _, err := repo.ListByOwner(context.Background(), f.owner, 1, 20); err == nil {
		t.Fatal("expected the listing to refuse an empty projection too")
	}
}

// The projection, not the physical table, drives the scan arity: a later
// `ALTER TABLE addresses ADD COLUMN` must not break this adapter.
func TestTheProjectionSurvivesAnExtraColumn(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	id := f.insert(t, false, nil, "79", "26734", "12 Nguyen Hue")

	if _, err := f.pool.Exec(ctx, `ALTER TABLE addresses ADD COLUMN delivery_note text`); err != nil {
		t.Fatalf("add an unrelated column: %v", err)
	}

	got, err := f.repo.FindByOwner(ctx, f.owner, id)
	if err != nil {
		t.Fatalf("FindByOwner after an extra column: %v", err)
	}
	if got.StreetAddress != "12 Nguyen Hue" {
		t.Fatalf("unexpected row after an extra column: %+v", got)
	}
	if _, _, err := f.repo.ListByOwner(ctx, f.owner, 1, 20); err != nil {
		t.Fatalf("ListByOwner after an extra column: %v", err)
	}
}

// Create writes every stored column from the entity, so a caller cannot smuggle a
// hidden marker or a timestamp past the domain.
func TestCreateWritesEveryStoredColumn(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	created := time.Now().UTC().Truncate(time.Microsecond)
	address := &model.Address{
		ID:             uuid.New(),
		UserID:         f.owner,
		RecipientName:  "  Nguyen Van A  ",
		RecipientPhone: "0912345678",
		ProvinceCode:   "79",
		ProvinceName:   "Ho Chi Minh",
		WardCode:       "26734",
		WardName:       "Phuong Ben Nghe",
		StreetAddress:  "12 Nguyen Hue",
		CreatedAt:      created,
		UpdatedAt:      created,
	}

	if err := f.repo.Create(ctx, address); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.repo.FindByOwner(ctx, f.owner, address.ID)
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if got.RecipientName != address.RecipientName || got.ProvinceName != "Ho Chi Minh" ||
		got.WardName != "Phuong Ben Nghe" || got.StreetAddress != "12 Nguyen Hue" {
		t.Fatalf("unexpected stored row: %+v", got)
	}
	if got.IsDefault {
		t.Fatal("a created address must not be the default unless the entity says so")
	}
	if got.DeletedAt != nil {
		t.Fatalf("a created address must be visible, got hidden at %v", got.DeletedAt)
	}
	if !got.CreatedAt.Equal(created) || !got.UpdatedAt.Equal(created) {
		t.Fatalf("expected the entity's timestamps to be stored, got %v / %v", got.CreatedAt, got.UpdatedAt)
	}
}
