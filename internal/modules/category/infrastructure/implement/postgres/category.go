package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the one table this module owns.
const table = "categories"

// The two unique indexes of migrations/00005_category.sql. The adapter names a
// collision by the constraint the database reported rather than by the text of
// its message, because the message is localized and free to change while the
// constraint name is part of the schema (FR-020).
const (
	constraintNormalizedName = "categories_normalized_name_key"
	constraintNormalizedSlug = "categories_normalized_slug_key"
)

// uniqueViolationCode is the SQLSTATE code PostgreSQL reports for a unique
// index rejection (23505).
const uniqueViolationCode = "23505"

// foreignKeyViolationCode is the SQLSTATE code PostgreSQL reports for a
// foreign-key rejection (23503). The one foreign key pointing at categories is
// products_category_fk, created by feature 006, so a delete refused this way
// means products still reference the category (FR-036, research D15).
const foreignKeyViolationCode = "23503"

// categoryColumns is the projection every read of the categories table selects.
//
// It is deliberately the eight columns a response may carry, matching the "what
// each read path projects" table of data-model.md: the folding keys
// normalized_name and normalized_slug appear in no projection, so they cannot
// leak into a response through this adapter even if a mapper were later changed
// by mistake. The public list and detail narrow this further to the four members
// of FR-004 in the mapper; a single read projection keeps the scan arity fixed.
var categoryColumns = []string{
	"id",
	"name",
	"slug",
	"description",
	"position",
	"is_visible",
	"created_at",
	"updated_at",
}

// CategoryRepository persists categories.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. The projection, not the physical
// table, drives the scan arity: a later `ALTER TABLE categories ADD COLUMN`
// cannot break this adapter.
//
// It never calls the generic FindAll: an unscoped listing would answer the
// administrator surface and the public surface with the same rows, and this
// module has a narrower query for each (ListAll and ListVisible). Every write is
// a single statement; the adapter never opens a transaction, because the
// application layer owns every transaction boundary (Constitution I).
type CategoryRepository struct {
	baserepo.Base[model.Category, uuid.UUID]
	pool *pgxpool.Pool
}

// NewCategoryRepository creates the category repository.
func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	r := &CategoryRepository{pool: pool}
	r.Base = baserepo.Base[model.Category, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "id",
		Columns:  categoryColumns,
		IDValue:  func(c *model.Category) any { return c.ID },
		Scan:     scanCategory,
	}
	return r
}

var _ domainrepo.CategoryRepository = (*CategoryRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the
// adapter joins a caller's transaction without ever opening one itself
// (Constitution I).
func (r *CategoryRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// projection returns the SELECT list for this adapter's own queries.
//
// An empty projection is a configuration error, reported before the database is
// touched, exactly as the generic read paths do. There is deliberately no
// `SELECT *` fallback anywhere in this adapter.
func (r *CategoryRepository) projection() (string, error) {
	if len(r.Columns) == 0 {
		return "", fmt.Errorf("repository %s: Columns is empty, it must list the projection matching Scan", r.Table)
	}
	return strings.Join(r.Columns, ", "), nil
}

// Create inserts a new category, deriving both folding keys from Name and Slug
// so the unique indexes always have a correct value to enforce.
//
// The adapter recomputes the keys rather than trusting the entity's copies: the
// fold is deterministic, so recomputing yields exactly the value the entity
// already holds for a well-formed entity, and no write path can store a stale or
// empty key whether or not it read the entity first. Reading the keys instead
// would leave a caller free to store a wrong one.
//
// A name or slug another category already holds is reported as the matching
// domainerr sentinel, so the use case can answer CATEGORY_NAME_TAKEN or
// CATEGORY_SLUG_TAKEN (FR-020, FR-021).
func (r *CategoryRepository) Create(ctx context.Context, category *model.Category) error {
	const query = `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description,
			 position, is_visible, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.querier(ctx).Exec(ctx, query,
		category.ID,
		category.Name,
		model.FoldKey(category.Name),
		category.Slug,
		model.FoldKey(category.Slug),
		category.Description,
		category.Position,
		category.IsVisible,
		category.CreatedAt,
		category.UpdatedAt,
	)
	if err != nil {
		return classifyWriteError("create category", err)
	}
	return nil
}

// Update writes the editable columns of the category named by its identifier.
//
// It derives the folding keys from Name and Slug with the domain's pure folding
// function rather than writing the entity's copies: the fold is deterministic,
// so a same-name self-edit recomputes exactly the key the row already has and
// cannot be mistaken for a collision with another category (data-model.md,
// FR-022). Deriving them is also the only arrangement no caller can get wrong —
// a write that carried empty or stale keys, or built an entity without reading
// one first, could not store anything but the correct key. It never names id in
// the SET list, so no code path can rewrite an identifier after creation
// (FR-015, FR-023).
//
// A collision with a *different* category is reported as the matching sentinel.
// The unique indexes exclude the row being updated, because an UPDATE writes the
// same physical row and a unique index does not compare a row with itself.
func (r *CategoryRepository) Update(ctx context.Context, category *model.Category) error {
	const query = `
		UPDATE categories
		SET name            = $2,
		    normalized_name = $3,
		    slug            = $4,
		    normalized_slug = $5,
		    description     = $6,
		    position        = $7,
		    is_visible      = $8,
		    updated_at      = now()
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query,
		category.ID,
		category.Name,
		model.FoldKey(category.Name),
		category.Slug,
		model.FoldKey(category.Slug),
		category.Description,
		category.Position,
		category.IsVisible,
	)
	if err != nil {
		return classifyWriteError("update category", err)
	}
	// A category the caller believed existed but that is already gone is not
	// something a client can cause; reporting it keeps a silent no-op from
	// looking like a successful write.
	if tag.RowsAffected() == 0 {
		return domainerr.ErrCategoryNotFound
	}
	return nil
}

// Delete removes the row. Removal is a hard delete (research D5); the audit
// entry is what survives it. An unknown identifier is reported as
// domainerr.ErrCategoryNotFound.
//
// A delete PostgreSQL refuses because products still reference the row is
// reported as domainerr.ErrCategoryInUse (FR-036): the restricting foreign key
// added by feature 006 is what refuses it, so the module never checks for
// products itself — that would mean reading module 04's table (research D15).
func (r *CategoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM categories WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id)
	if err != nil {
		return classifyDeleteError("delete category", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrCategoryNotFound
	}
	return nil
}

// FindByID returns one category for the administrator surface, including a
// category that is withheld from customers. An unknown identifier is reported as
// domainerr.ErrCategoryNotFound.
func (r *CategoryRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Category, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	const query = `SELECT %s FROM categories WHERE id = $1`
	row := r.querier(ctx).QueryRow(ctx, fmt.Sprintf(query, list), id)
	return scanCategory(row)
}

// FindVisibleBySlug returns one category for the public surface, addressing it
// by the slug a customer-facing link is built from.
//
// A withheld, removed or unknown slug is reported the same way, as
// domainerr.ErrCategoryNotFound, so the response never confirms that a withheld
// category exists (FR-005, SC-004).
func (r *CategoryRepository) FindVisibleBySlug(ctx context.Context, slug string) (*model.Category, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	const query = `SELECT %s FROM categories WHERE slug = $1 AND is_visible`
	row := r.querier(ctx).QueryRow(ctx, fmt.Sprintf(query, list), slug)
	return scanCategory(row)
}

// IDBySlug resolves the category a slug names, without regard to its display
// state.
//
// It is the read behind internal/contracts.CategoryQuery's CategoryIDBySlug:
// another module addresses a customer-facing category link by its slug and may
// not read this module's table to resolve it (research D1). The second result is
// the found flag: an unknown slug answers (uuid.Nil, false, nil) rather than a
// not-found error, so the caller can answer the public filter's empty list
// directly. A hidden category resolves too; the caller's visibility predicate
// excludes it afterwards, which is what makes a hidden slug and an unknown slug
// answer the same thing (FR-006).
func (r *CategoryRepository) IDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error) {
	const query = `SELECT id FROM categories WHERE slug = $1`
	var id uuid.UUID
	err := r.querier(ctx).QueryRow(ctx, query, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("find category id by slug: %w", err)
	}
	return id, true, nil
}

// ListVisible returns a page of the categories on display plus the total number
// of them, in the FR-003 order.
func (r *CategoryRepository) ListVisible(ctx context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return r.list(ctx, true, page, pageSize)
}

// ListAll returns a page of every category including the ones not on display,
// plus the total number of them, in the same FR-003 order. It serves the
// administrator list (FR-009).
func (r *CategoryRepository) ListAll(ctx context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return r.list(ctx, false, page, pageSize)
}

// VisibleIDs returns the identifiers of every category on display, in the FR-003
// order.
//
// It is the read behind internal/contracts.CategoryQuery: the whole set is
// what lets another module filter its own catalogue in SQL without reading this
// module's table (research D1). The order is deterministic but is not part of
// that contract, because the caller uses the values as a set.
func (r *CategoryRepository) VisibleIDs(ctx context.Context) ([]uuid.UUID, error) {
	const query = `SELECT id FROM categories WHERE is_visible ORDER BY position, created_at, id`
	rows, err := r.querier(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list visible category ids: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan visible category id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// list runs the shared paged read for one of the two audiences.
//
// The order is position, then created_at, then id, ascending: the same order as
// categories_ordering_idx and the only order FR-003 permits. The identifier
// breaks the remaining tie and is immutable, which is what makes the order
// reproducible across identical requests.
func (r *CategoryRepository) list(ctx context.Context, onlyVisible bool, page, pageSize int) ([]model.Category, int64, error) {
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

	// filter is a constant chosen by this function, never caller input, so it
	// cannot carry a value into the SQL; every caller-supplied value is bound.
	filter := ""
	if onlyVisible {
		filter = " WHERE is_visible"
	}

	var total int64
	countQuery := "SELECT count(*) FROM categories" + filter
	if err := r.querier(ctx).QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count categories: %w", err)
	}

	pageQuery := fmt.Sprintf(
		"SELECT %s FROM categories%s ORDER BY position, created_at, id LIMIT $1 OFFSET $2",
		list, filter)
	rows, err := r.querier(ctx).Query(ctx, pageQuery, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	out := make([]model.Category, 0, pageSize)
	for rows.Next() {
		category, err := scanCategory(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *category)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// classifyWriteError labels a failed write and maps a unique-index rejection to
// the module sentinel that names the colliding field.
//
// Attribution is by the constraint name PostgreSQL reports, never by its
// message text. A unique violation that names neither of the two constraints is
// returned unwrapped rather than guessed at, so an unexpected index cannot be
// silently reported as a name or slug collision.
func classifyWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		switch pgErr.ConstraintName {
		case constraintNormalizedName:
			return domainerr.ErrCategoryNameTaken
		case constraintNormalizedSlug:
			return domainerr.ErrCategorySlugTaken
		default:
			return err
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// classifyDeleteError labels a failed delete and maps a foreign-key rejection to
// the module sentinel that reports the category is still referenced (FR-036).
//
// Attribution is by the SQLSTATE PostgreSQL reports, never by its message text:
// the message is localized and free to change while the SQLSTATE is part of the
// protocol. products_category_fk is the only foreign key that points at
// categories, so a 23503 from this delete can only mean products still reference
// the row. Any other failure is returned unwrapped rather than guessed at, so an
// unexpected error cannot be quietly reported as a category in use.
func classifyDeleteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolationCode {
		return domainerr.ErrCategoryInUse
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// scanCategory reads one row of the projection declared in NewCategoryRepository.
// The number of destinations must match that projection exactly.
//
// The folding keys are not read: they are in no projection and are never needed
// by a response. They are derived here from the name and the slug just scanned,
// with the domain's pure folding function, because the entity's invariant — the
// keys always agree with Name and Slug — must hold for a category rehydrated
// from storage exactly as it does for a constructed one. Reading the two columns
// instead would add them to the projection; deriving keeps it at eight.
func scanCategory(row baserepo.Row) (*model.Category, error) {
	var category model.Category
	err := row.Scan(
		&category.ID,
		&category.Name,
		&category.Slug,
		&category.Description,
		&category.Position,
		&category.IsVisible,
		&category.CreatedAt,
		&category.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrCategoryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan category: %w", err)
	}
	category.NormalizedName = model.FoldKey(category.Name)
	category.NormalizedSlug = model.FoldKey(category.Slug)
	return &category, nil
}
