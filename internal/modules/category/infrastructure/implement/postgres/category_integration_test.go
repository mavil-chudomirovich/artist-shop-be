//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the category adapter against a real PostgreSQL container.
//
// Its centre of gravity is what a fake repository cannot prove: that the
// migration applies to a fresh database, and that the storage layer refuses a
// duplicate or a malformed value even when the application is bypassed. Most of
// the constraint tests write the table directly — no domain validation, no use
// case — because a guard against a *future* writer can only be proven by
// pretending to be that writer.
//
// Every rejection asserts both the SQLSTATE and the constraint name, so a test
// fails if the constraint it names is removed from the migration: that is the
// mutation check recorded in the task report.

// SQLSTATE codes the constraint tests distinguish.
const (
	sqlStateUnique = "23505"
	sqlStateCheck  = "23514"
)

// The constraints and indexes the migration must create on categories
// (migrations/00005_category.sql, data-model.md).
const (
	ckNormalizedNameShape = "categories_normalized_name_shape_ck"
	ckNormalizedSlugShape = "categories_normalized_slug_shape_ck"
	ckSlugFormat          = "categories_slug_format_ck"
	ckNameLength          = "categories_name_length_ck"
	ckSlugLength          = "categories_slug_length_ck"
	ckDescriptionLength   = "categories_description_length_ck"
	idxOrdering           = "categories_ordering_idx"
)

type categoryFixture struct {
	pool *pgxpool.Pool
	repo *CategoryRepository
}

func newCategoryFixture(t *testing.T) *categoryFixture {
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
	return &categoryFixture{pool: pool, repo: NewCategoryRepository(pool)}
}

// rawInsert writes one row straight into the table, deliberately bypassing the
// adapter and the domain, so a storage-level rule can be proven to be one.
func (f *categoryFixture) rawInsert(ctx context.Context, name, normalizedName, slug, normalizedSlug, description string, position int) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := f.pool.Exec(ctx, query, id, name, normalizedName, slug, normalizedSlug, description, position)
	return id, err
}

func (f *categoryFixture) count(t *testing.T) int {
	t.Helper()
	var total int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM categories`).Scan(&total); err != nil {
		t.Fatalf("count categories: %v", err)
	}
	return total
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

func stringPtr(s string) *string { return &s }
func intPtr(i int) *int          { return &i }

// FR-015, FR-021, FR-023 and the shape/format/length requirements are all
// storage-level here. The migration is applied to a brand new container
// database, which is itself proof that it applies to a fresh database, and the
// constraints it must have created are checked by name.
func TestTheMigrationAppliesToAFreshDatabaseAndCreatesEveryConstraint(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()

	var exists bool
	if err := f.pool.QueryRow(ctx, `SELECT to_regclass('public.categories') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("look up the categories table: %v", err)
	}
	if !exists {
		t.Fatal("the migration did not create the categories table")
	}

	checks := []string{
		ckNormalizedNameShape,
		ckNormalizedSlugShape,
		ckSlugFormat,
		ckNameLength,
		ckSlugLength,
		ckDescriptionLength,
	}
	const checkQuery = `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			WHERE t.relname = 'categories' AND c.conname = $1
		)`
	for _, name := range checks {
		if err := f.pool.QueryRow(ctx, checkQuery, name).Scan(&exists); err != nil {
			t.Fatalf("look up constraint %s: %v", name, err)
		}
		if !exists {
			t.Errorf("the migration did not create constraint %s", name)
		}
	}

	indexes := []string{constraintNormalizedName, constraintNormalizedSlug, idxOrdering}
	const indexQuery = `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'categories' AND indexname = $1
		)`
	for _, name := range indexes {
		if err := f.pool.QueryRow(ctx, indexQuery, name).Scan(&exists); err != nil {
			t.Fatalf("look up index %s: %v", name, err)
		}
		if !exists {
			t.Errorf("the migration did not create index %s", name)
		}
	}
}

// FR-021 and FR-018/FR-019: every storage-level rule holds against a writer that
// bypassed the application entirely. Each subtest names the one constraint it
// expects, so removing that constraint from the migration makes exactly this
// subtest fail.
func TestStorageConstraintsRejectWhatTheApplicationWouldPrevent(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()

	t.Run("duplicate normalised name", func(t *testing.T) {
		if _, err := f.rawInsert(ctx, "Tranh", "tranh", "tranh", "tranh", "", 0); err != nil {
			t.Fatalf("seed the first row: %v", err)
		}
		_, err := f.rawInsert(ctx, "Tranh khac", "tranh", "tranh-khac", "tranh-khac", "", 0)
		requireConstraint(t, err, sqlStateUnique, constraintNormalizedName)
	})

	t.Run("duplicate normalised slug", func(t *testing.T) {
		if _, err := f.rawInsert(ctx, "Mot", "mot", "mot", "mot", "", 0); err != nil {
			t.Fatalf("seed the first row: %v", err)
		}
		_, err := f.rawInsert(ctx, "Hai", "hai", "mot", "mot", "", 0)
		requireConstraint(t, err, sqlStateUnique, constraintNormalizedSlug)
	})

	t.Run("un-normalised normalised_name", func(t *testing.T) {
		// The slug is valid in every case, so only the name-shape check can be
		// the one that rejects the row.
		cases := []struct {
			name           string
			normalizedName string
			slug           string
		}{
			{"leading space", " Tranh-a ", "shape-name-a"},
			{"trailing space", "tranh-b ", "shape-name-b"},
			{"uppercase", "Tranh-c", "shape-name-c"},
		}
		for _, tc := range cases {
			_, err := f.rawInsert(ctx, tc.name, tc.normalizedName, tc.slug, tc.slug, "", 0)
			requireConstraint(t, err, sqlStateCheck, ckNormalizedNameShape)
		}
	})

	t.Run("un-normalised normalised_slug", func(t *testing.T) {
		// The slug itself stays valid so only the slug-shape check rejects it.
		cases := []struct {
			name           string
			normalizedSlug string
			slug           string
		}{
			{"leading space", " slug-a ", "shape-slug-a"},
			{"trailing space", "slug-b ", "shape-slug-b"},
			{"uppercase", "Slug-c", "shape-slug-c"},
		}
		for _, tc := range cases {
			_, err := f.rawInsert(ctx, tc.name, "shape-name-"+tc.slug, tc.slug, tc.normalizedSlug, "", 0)
			requireConstraint(t, err, sqlStateCheck, ckNormalizedSlugShape)
		}
	})

	t.Run("slug format", func(t *testing.T) {
		// The normalised slug is kept valid (trimmed, lowercase) and every row
		// carries a distinct name, so only the format check can reject it.
		cases := []struct {
			slug string
		}{
			{"tranh_son"},
			{"tranh-"},
			{"-tranh"},
			{"a--b"},
			{"TranhSon"},
		}
		for i, tc := range cases {
			name := "Format" + string(rune('A'+i))
			normalized := strings.ToLower(tc.slug)
			_, err := f.rawInsert(ctx, name, strings.ToLower(name), tc.slug, normalized, "", 0)
			requireConstraint(t, err, sqlStateCheck, ckSlugFormat)
		}
	})

	t.Run("name length", func(t *testing.T) {
		long := strings.Repeat("a", 121)
		_, err := f.rawInsert(ctx, long, long, "name-length", "name-length", "", 0)
		requireConstraint(t, err, sqlStateCheck, ckNameLength)
	})

	t.Run("slug length", func(t *testing.T) {
		long := strings.Repeat("a", 141)
		_, err := f.rawInsert(ctx, "Slug length", "slug-length", long, long, "", 0)
		requireConstraint(t, err, sqlStateCheck, ckSlugLength)
	})

	t.Run("description length", func(t *testing.T) {
		long := strings.Repeat("a", 2001)
		_, err := f.rawInsert(ctx, "Description length", "description-length", "description-length", "description-length", long, 0)
		requireConstraint(t, err, sqlStateCheck, ckDescriptionLength)
	})
}

// FR-020, FR-021: the adapter must attribute a unique-index rejection to the
// field that collided, by constraint name, so the use case can answer the two
// different codes. A name collision and a slug collision go through the same
// archive path and must not be confused.
func TestAUniqueViolationIsReportedAsTheMatchingSentinel(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first, err := model.NewCategory(model.CategoryDraft{
		Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Description: "", Position: 0,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, first); err != nil {
		t.Fatalf("Create the first category: %v", err)
	}

	// A different slug but the same folded name.
	nameCollision, err := model.NewCategory(model.CategoryDraft{
		Name: "  TRANH SƠN DẦU  ", Slug: "khac", Description: "", Position: 1,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, nameCollision); !errors.Is(err, domainerr.ErrCategoryNameTaken) {
		t.Fatalf("expected ErrCategoryNameTaken, got %v", err)
	}

	// A different name but the same folded slug.
	slugCollision, err := model.NewCategory(model.CategoryDraft{
		Name: "Khac", Slug: "tranh-son-dau", Description: "", Position: 2,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, slugCollision); !errors.Is(err, domainerr.ErrCategorySlugTaken) {
		t.Fatalf("expected ErrCategorySlugTaken, got %v", err)
	}

	if got := f.count(t); got != 1 {
		t.Fatalf("expected exactly one category to have survived, got %d", got)
	}
}

// FR-022, SC-006: re-sending a category's own name and slug — including when
// only the description changes — must succeed. The unique indexes compare rows,
// and the adapter writes the folding keys the entity already holds, so a row is
// never a duplicate of itself.
func TestASameNameSelfEditDoesNotTripTheUniqueIndexes(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	category, err := model.NewCategory(model.CategoryDraft{
		Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Description: "old", Position: 0,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, category); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := f.repo.FindByID(ctx, category.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := loaded.Apply(model.CategoryEdit{
		Name:        stringPtr("Tranh sơn dầu"),
		Slug:        stringPtr("tranh-son-dau"),
		Description: stringPtr("new"),
	}, now.Add(time.Minute)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.repo.Update(ctx, loaded); err != nil {
		t.Fatalf("a same-name self-edit must succeed, got %v", err)
	}

	after, err := f.repo.FindByID(ctx, category.ID)
	if err != nil {
		t.Fatalf("FindByID after the self-edit: %v", err)
	}
	if after.Description != "new" {
		t.Fatalf("the self-edit was not written: %+v", after)
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("expected one row after a self-edit, got %d", got)
	}
}

// FR-016, FR-017, D2, D3: a category read back through the eight-column
// projection carries folding keys derived from the name and the slug it just
// read, because a rehydrated entity must satisfy the same invariant as a
// constructed one. The adapter derives both keys from the name and the slug on
// every write, so a read-modify-write that changes only the visibility leaves
// the stored keys correct and non-empty. This fails on the old behaviour, which
// left a rehydrated entity's keys empty.
func TestAReadModifyWriteKeepsTheFoldingKeysCorrect(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	category, err := model.NewCategory(model.CategoryDraft{
		Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Description: "", Position: 0,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, category); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read it back: the projection is the eight response columns, so the keys are
	// not read from storage — they are derived from the name and the slug. A
	// rehydrated entity must carry the keys its own invariant promises.
	loaded, err := f.repo.FindByID(ctx, category.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if want := model.FoldKey("Tranh sơn dầu"); loaded.NormalizedName != want {
		t.Fatalf("the read did not derive normalized_name: got %q, want %q",
			loaded.NormalizedName, want)
	}
	if want := model.FoldKey("tranh-son-dau"); loaded.NormalizedSlug != want {
		t.Fatalf("the read did not derive normalized_slug: got %q, want %q",
			loaded.NormalizedSlug, want)
	}

	// Change only the visibility, then write the entity back.
	loaded.Hide(now.Add(time.Minute))
	if err := f.repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update after hiding: %v", err)
	}

	var storedNameKey, storedSlugKey string
	if err := f.pool.QueryRow(ctx,
		`SELECT normalized_name, normalized_slug FROM categories WHERE id = $1`, category.ID).
		Scan(&storedNameKey, &storedSlugKey); err != nil {
		t.Fatalf("read the stored folding keys: %v", err)
	}
	if storedNameKey == "" || storedSlugKey == "" {
		t.Fatalf("the update stored an empty folding key: %q / %q", storedNameKey, storedSlugKey)
	}
	if storedNameKey != model.FoldKey("Tranh sơn dầu") || storedSlugKey != model.FoldKey("tranh-son-dau") {
		t.Fatalf("the stored folding keys are wrong: %q / %q, want %q / %q",
			storedNameKey, storedSlugKey, model.FoldKey("Tranh sơn dầu"), model.FoldKey("tranh-son-dau"))
	}
}

// requireDerivedFoldingKeys fails unless the category's folding keys are exactly
// the domain's fold of the name and the slug it carries.
func requireDerivedFoldingKeys(t *testing.T, category *model.Category) {
	t.Helper()
	if want := model.FoldKey(category.Name); category.NormalizedName != want {
		t.Fatalf("normalized_name is %q, want the fold of %q which is %q",
			category.NormalizedName, category.Name, want)
	}
	if want := model.FoldKey(category.Slug); category.NormalizedSlug != want {
		t.Fatalf("normalized_slug is %q, want the fold of %q which is %q",
			category.NormalizedSlug, category.Slug, want)
	}
}

// FR-016, FR-017: an entity materialised from storage satisfies the same
// invariant as one the domain constructs — the folding keys agree with the name
// and the slug. Both read paths, the administrator one by identifier and the
// public one by slug, therefore return a category whose keys are the domain's
// fold of the values it carries.
func TestARehydratedCategoryCarriesTheDerivedFoldingKeys(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	category, err := model.NewCategory(model.CategoryDraft{
		Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Description: "", Position: 0,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, category); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byID, err := f.repo.FindByID(ctx, category.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	requireDerivedFoldingKeys(t, byID)

	bySlug, err := f.repo.FindVisibleBySlug(ctx, "tranh-son-dau")
	if err != nil {
		t.Fatalf("FindVisibleBySlug: %v", err)
	}
	requireDerivedFoldingKeys(t, bySlug)

	if byID.ID != bySlug.ID {
		t.Fatalf("the two read paths returned different categories: %s and %s", byID.ID, bySlug.ID)
	}
}

// FR-003, SC-002: with two categories sharing a position, two identical reads
// return the same order. The rows carry the same created_at, so the identifier —
// the immutable tie-break — is what keeps the order stable.
func TestReadsAreOrderedByPositionAndStableAcrossTwoReads(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	fixed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)

	drafts := []model.CategoryDraft{
		{Name: "Alpha", Slug: "alpha", Description: "first", Position: 5},
		{Name: "Beta", Slug: "beta", Description: "second", Position: 5},
		{Name: "Gamma", Slug: "gamma", Description: "third", Position: 5},
		{Name: "Delta", Slug: "delta", Description: "preferred", Position: 1},
	}
	for _, draft := range drafts {
		category, err := model.NewCategory(draft, fixed)
		if err != nil {
			t.Fatalf("NewCategory: %v", err)
		}
		if err := f.repo.Create(ctx, category); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	first, total, err := f.repo.ListVisible(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if total != int64(len(drafts)) || len(first) != len(drafts) {
		t.Fatalf("expected %d categories, got %d of %d", len(drafts), len(first), total)
	}
	if first[0].Position != 1 {
		t.Fatalf("expected the lowest position first, got position %d", first[0].Position)
	}
	seen := map[uuid.UUID]bool{}
	for _, category := range first {
		if seen[category.ID] {
			t.Fatalf("category %s appeared twice on one page", category.ID)
		}
		seen[category.ID] = true
	}

	second, _, err := f.repo.ListVisible(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListVisible repeat: %v", err)
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("the order is not stable: read one %s, read two %s",
				first[i].ID, second[i].ID)
		}
	}
}

// FR-015, FR-023, SC-006: the identifier never changes, and no update path can
// write it. An edit, a hide and a show each leave the row's identifier and its
// count intact; an update that claims a different identifier matches nothing and
// is reported as not found rather than creating a row.
func TestTheIdentifierIsStableAcrossEveryUpdateAndNoPathWritesIt(t *testing.T) {
	f := newCategoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	category, err := model.NewCategory(model.CategoryDraft{
		Name: "Tranh", Slug: "tranh", Description: "", Position: 0,
	}, now)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if err := f.repo.Create(ctx, category); err != nil {
		t.Fatalf("Create: %v", err)
	}
	original := category.ID

	// An edit that changes every editable member.
	loaded, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if loaded.ID != original {
		t.Fatalf("the read returned id %s, want %s", loaded.ID, original)
	}
	if err := loaded.Apply(model.CategoryEdit{
		Name:        stringPtr("Tranh mới"),
		Slug:        stringPtr("tranh-moi"),
		Description: stringPtr("a description"),
		Position:    intPtr(7),
	}, now.Add(time.Minute)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterEdit, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after the edit: %v", err)
	}
	if afterEdit.ID != original {
		t.Fatalf("the identifier changed after an edit: %s", afterEdit.ID)
	}
	if afterEdit.Name != "Tranh mới" || afterEdit.Slug != "tranh-moi" ||
		afterEdit.Description != "a description" || afterEdit.Position != 7 {
		t.Fatalf("the edit was not written: %+v", afterEdit)
	}
	// The adapter derives the folding keys from the name and slug it writes, so
	// the unique indexes keep a correct value to enforce.
	var storedNameKey, storedSlugKey string
	if err := f.pool.QueryRow(ctx,
		`SELECT normalized_name, normalized_slug FROM categories WHERE id = $1`, original).
		Scan(&storedNameKey, &storedSlugKey); err != nil {
		t.Fatalf("read the folding keys: %v", err)
	}
	if storedNameKey != "tranh mới" || storedSlugKey != "tranh-moi" {
		t.Fatalf("the update did not write the entity's folding keys: %q / %q", storedNameKey, storedSlugKey)
	}

	// Hide, then show, through the same update path.
	hidden, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID before hiding: %v", err)
	}
	// A no-op edit refreshes updated_at and, in the domain, the folding keys;
	// hide and show themselves do not touch name or slug.
	if err := hidden.Apply(model.CategoryEdit{}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Apply (hide): %v", err)
	}
	hidden.Hide(now.Add(2 * time.Minute))
	if err := f.repo.Update(ctx, hidden); err != nil {
		t.Fatalf("Update (hide): %v", err)
	}
	readBack, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after hiding: %v", err)
	}
	if readBack.IsVisible {
		t.Fatal("the category was not hidden")
	}
	if readBack.ID != original {
		t.Fatalf("the identifier changed after hiding: %s", readBack.ID)
	}

	shown, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID before showing: %v", err)
	}
	if err := shown.Apply(model.CategoryEdit{}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("Apply (show): %v", err)
	}
	shown.Show(now.Add(3 * time.Minute))
	if err := f.repo.Update(ctx, shown); err != nil {
		t.Fatalf("Update (show): %v", err)
	}
	back, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after showing: %v", err)
	}
	if !back.IsVisible || back.ID != original {
		t.Fatalf("the category was not shown or its id moved: %+v", back)
	}

	if got := f.count(t); got != 1 {
		t.Fatalf("every update must act on the one row, count is %d", got)
	}

	// An update carrying a different identifier must not rewrite or create a
	// row: it matches nothing and is reported as not found.
	forged := *back
	forged.ID = uuid.New()
	if err := f.repo.Update(ctx, &forged); !errors.Is(err, domainerr.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound for a forged identifier, got %v", err)
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("a forged-identifier update created or removed a row, count is %d", got)
	}
	survivor, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after the forged update: %v", err)
	}
	if survivor.ID != original {
		t.Fatalf("the surviving row's identifier moved: %s", survivor.ID)
	}
}
