//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

type item struct {
	ID   uuid.UUID
	Name string
}

func scanItem(row Row) (*item, error) {
	var it item
	if err := row.Scan(&it.ID, &it.Name); err != nil {
		return nil, err
	}
	return &it, nil
}

func TestGenericRepositoryCRUD(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE TABLE repo_items (id uuid PRIMARY KEY, name text NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	base := &Base[item, uuid.UUID]{
		Pool:          pool,
		Table:         "repo_items",
		IDColumn:      "id",
		OrderBy:       "name",
		InsertColumns: []string{"id", "name"},
		InsertValues:  func(i *item) []any { return []any{i.ID, i.Name} },
		UpdateColumns: []string{"name"},
		UpdateValues:  func(i *item) []any { return []any{i.Name} },
		IDValue:       func(i *item) any { return i.ID },
		Scan:          scanItem,
	}

	first := &item{ID: uuid.New(), Name: "alpha"}
	second := &item{ID: uuid.New(), Name: "beta"}
	if err := base.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := base.Create(ctx, second); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := base.FindByID(ctx, first.ID)
	if err != nil || got.Name != "alpha" {
		t.Fatalf("FindByID: %v (%+v)", err, got)
	}

	ok, err := base.Exists(ctx, first.ID)
	if err != nil || !ok {
		t.Fatalf("Exists present: %v %v", ok, err)
	}
	ok, err = base.Exists(ctx, uuid.New())
	if err != nil || ok {
		t.Fatalf("Exists absent: %v %v", ok, err)
	}

	// Absent id maps to a not-found error through the scan function.
	if _, err := base.FindByID(ctx, uuid.New()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got %v", err)
	}

	list, total, err := base.FindAll(ctx, 1, 10)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("FindAll: total=%d len=%d err=%v", total, len(list), err)
	}
	if list[0].Name != "alpha" {
		t.Fatalf("expected ordering by name, got %q", list[0].Name)
	}

	first.Name = "gamma"
	if err := base.Update(ctx, first); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = base.FindByID(ctx, first.ID)
	if got.Name != "gamma" {
		t.Fatalf("expected updated name, got %q", got.Name)
	}

	if err := base.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := base.Exists(ctx, first.ID); ok {
		t.Fatal("expected item deleted")
	}
}
