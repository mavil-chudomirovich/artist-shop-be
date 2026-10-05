// Package repository provides a generic repository contract and a pgx-backed
// base implementation (create/update/delete/find/list/exists) shared by all
// modules. Concrete repositories embed Base and add entity-specific queries.
package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
)

// Row is the minimal scan target satisfied by pgx.Row and pgx.Rows.
type Row interface {
	Scan(dest ...any) error
}

// Repository is the generic persistence contract. T is the entity type and ID
// is its identifier type.
type Repository[T any, ID any] interface {
	Create(ctx context.Context, entity *T) error
	Update(ctx context.Context, entity *T) error
	Delete(ctx context.Context, id ID) error
	FindByID(ctx context.Context, id ID) (*T, error)
	FindAll(ctx context.Context, page, size int) ([]T, int64, error)
	Exists(ctx context.Context, id ID) (bool, error)
}

// Base implements Repository using pgx. Embed it in a concrete repository and
// set the fields for the target table.
type Base[T any, ID any] struct {
	// Pool is the fallback querier used when no transaction is in the context.
	Pool database.Querier
	// Table is the table name.
	Table string
	// IDColumn is the primary-key column.
	IDColumn string
	// Columns is the required projection used by the generic read paths
	// (FindByID, FindAll). Each entry becomes one column in the generated
	// SELECT list, so the order and length must match the destinations the Scan
	// function reads. Leaving it empty is a configuration error: the read paths
	// return it before touching the database instead of guessing a projection.
	//
	// Declaring the projection explicitly decouples the scan arity from the
	// physical schema: a later `ALTER TABLE ... ADD COLUMN` does not break
	// repositories embedding Base, and unread columns are not transferred at
	// all.
	Columns []string
	// OrderBy is used by FindAll (e.g. "created_at DESC").
	OrderBy string
	// InsertColumns are the columns written by Create.
	InsertColumns []string
	// InsertValues returns the values matching InsertColumns.
	InsertValues func(*T) []any
	// UpdateColumns are the columns written by Update.
	UpdateColumns []string
	// UpdateValues returns the set values matching UpdateColumns.
	UpdateValues func(*T) []any
	// IDValue returns the primary-key value of an entity.
	IDValue func(*T) any
	// Scan reads one row into an entity. It is used both for the generic read
	// paths and for adapter-specific queries, so the number of destinations it
	// passes must equal the length of the projection the query selects — not the
	// number of columns the table physically has. A mismatch is a configuration
	// error: it is surfaced at query time, either as an empty-Columns error from
	// the generic read paths or as a pgx arity error for adapter queries.
	Scan func(Row) (*T, error)
}

func (b *Base[T, ID]) q(ctx context.Context) database.Querier {
	return database.FromContext(ctx, b.Pool)
}

// selectList returns the projection for the generic read paths. Columns is
// required: there is no `SELECT *` fallback, because a projection that follows
// the physical table makes the scan arity silently depend on the schema and a
// plain `ALTER TABLE ... ADD COLUMN` would break every repository embedding
// Base.
func (b *Base[T, ID]) selectList() (string, error) {
	if len(b.Columns) == 0 {
		return "", fmt.Errorf("repository %s: Columns is empty, it must list the projection matching Scan", b.Table)
	}
	return strings.Join(b.Columns, ", "), nil
}

// Create inserts the entity.
func (b *Base[T, ID]) Create(ctx context.Context, entity *T) error {
	placeholders := make([]string, len(b.InsertColumns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		b.Table, strings.Join(b.InsertColumns, ", "), strings.Join(placeholders, ", "))
	if _, err := b.q(ctx).Exec(ctx, query, b.InsertValues(entity)...); err != nil {
		return fmt.Errorf("create %s: %w", b.Table, err)
	}
	return nil
}

// Update writes the configured columns for the entity identified by IDColumn.
func (b *Base[T, ID]) Update(ctx context.Context, entity *T) error {
	sets := make([]string, len(b.UpdateColumns))
	for i, col := range b.UpdateColumns {
		sets[i] = fmt.Sprintf("%s = $%d", col, i+1)
	}
	idPos := len(b.UpdateColumns) + 1
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d",
		b.Table, strings.Join(sets, ", "), b.IDColumn, idPos)

	args := append(b.UpdateValues(entity), b.IDValue(entity))
	if _, err := b.q(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("update %s: %w", b.Table, err)
	}
	return nil
}

// Delete removes the entity by id.
func (b *Base[T, ID]) Delete(ctx context.Context, id ID) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = $1", b.Table, b.IDColumn)
	if _, err := b.q(ctx).Exec(ctx, query, id); err != nil {
		return fmt.Errorf("delete %s: %w", b.Table, err)
	}
	return nil
}

// FindByID returns one entity by id.
func (b *Base[T, ID]) FindByID(ctx context.Context, id ID) (*T, error) {
	list, err := b.selectList()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", list, b.Table, b.IDColumn)
	return b.Scan(b.q(ctx).QueryRow(ctx, query, id))
}

// Exists reports whether an entity with id exists.
func (b *Base[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1", b.Table, b.IDColumn)
	var one int
	err := b.q(ctx).QueryRow(ctx, query, id).Scan(&one)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("exists %s: %w", b.Table, err)
	}
	return true, nil
}

// FindAll returns a page of entities and the total count.
func (b *Base[T, ID]) FindAll(ctx context.Context, page, size int) ([]T, int64, error) {
	list, err := b.selectList()
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}

	var total int64
	if err := b.q(ctx).QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", b.Table)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count %s: %w", b.Table, err)
	}

	order := b.OrderBy
	if order == "" {
		order = b.IDColumn
	}
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT $1 OFFSET $2", list, b.Table, order)
	rows, err := b.q(ctx).Query(ctx, query, size, (page-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list %s: %w", b.Table, err)
	}
	defer rows.Close()

	out := make([]T, 0, size)
	for rows.Next() {
		entity, err := b.Scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *entity)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, int64(total), nil
}
