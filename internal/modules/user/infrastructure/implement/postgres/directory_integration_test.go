//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// directoryFixture is one migrated PostgreSQL container plus the directory
// adapter under test.
type directoryFixture struct {
	pool *pgxpool.Pool
	repo *DirectoryRepository
}

func newDirectoryFixture(t *testing.T) *directoryFixture {
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
	return &directoryFixture{pool: pool, repo: NewDirectoryRepository(pool)}
}

// insertUser writes one raw account and returns its identifier. The test writes
// the role/status/created_at columns directly, bypassing every use case, because
// the KPI predicate is a storage-level rule that must hold for any writer.
func (f *directoryFixture) insertUser(t *testing.T, role, status string, createdAt time.Time, displayName *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO users (id, email, password_hash, role, status, display_name, created_at, updated_at)
		VALUES ($1, $2, 'not-used', $3, $4, $5, $6, $6)`
	if _, err := f.pool.Exec(context.Background(), query,
		id, "directory-"+id.String()+"@example.com", role, status, displayName, createdAt); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// FR-015, FR-016: only active CUSTOMER accounts count, and newUsersThisWeek is
// bounded by the supplied [from, to) window — including its exact boundary.
func TestDirectoryKPIsFiltersRoleStatusAndTheWeekBoundary(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	from := now.Add(-7 * 24 * time.Hour)
	to := now.Add(time.Hour)

	// Active customers: one inside the window, one before it.
	inWeek := f.insertUser(t, "CUSTOMER", "active", from.Add(time.Second), nil)
	beforeWeek := f.insertUser(t, "CUSTOMER", "active", from.Add(-time.Second), nil)
	// Excluded by status or role, even when created inside the window.
	f.insertUser(t, "CUSTOMER", "pending", now, nil)
	f.insertUser(t, "CUSTOMER", "disabled", now, nil)
	f.insertUser(t, "ADMIN", "active", now, nil)

	kpis, err := f.repo.KPIs(ctx, model.TimeWindow{From: from, To: to})
	if err != nil {
		t.Fatalf("KPIs: %v", err)
	}
	if kpis.TotalUsers != 2 {
		t.Fatalf("totalUsers = %d, want the two active customers (%s, %s)", kpis.TotalUsers, inWeek, beforeWeek)
	}
	if kpis.NewUsersThisWeek != 1 {
		t.Fatalf("newUsersThisWeek = %d, want only the account inside the window", kpis.NewUsersThisWeek)
	}
}

// FR-026, research D8: the bulk summary includes a disabled account (it still
// labels its old order), reports an unset display name as empty and omits an id
// that no account carries.
func TestDirectorySummariesBulkRead(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()

	active := f.insertUser(t, "CUSTOMER", "active", time.Now().UTC(), ptrString("Nguyễn Minh Anh"))
	disabled := f.insertUser(t, "CUSTOMER", "disabled", time.Now().UTC(), nil)
	unknown := uuid.New()

	summaries, err := f.repo.SummariesByIDs(ctx, []uuid.UUID{active, disabled, unknown})
	if err != nil {
		t.Fatalf("SummariesByIDs: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected the two existing accounts, got %d", len(summaries))
	}
	byID := make(map[uuid.UUID]model.CustomerSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.ID] = summary
	}
	if got := byID[active]; got.DisplayName != "Nguyễn Minh Anh" {
		t.Fatalf("the active account's display name did not map: %+v", got)
	}
	disabledSummary, ok := byID[disabled]
	if !ok {
		t.Fatal("a disabled account must still label its old order")
	}
	if disabledSummary.DisplayName != "" {
		t.Fatalf("an unset display name must read as empty, got %q", disabledSummary.DisplayName)
	}
	if _, ok := byID[unknown]; ok {
		t.Fatal("an id no account carries must be absent from the result")
	}

	empty, err := f.repo.SummariesByIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("an empty id set must answer an empty slice, got %d, %v", len(empty), err)
	}
}

func ptrString(value string) *string { return &value }
