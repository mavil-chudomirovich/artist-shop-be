//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

func setup(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

func TestUserRepositoryLifecycle(t *testing.T) {
	pool, ctx := setup(t)
	repo := NewUserRepository(pool)

	account := &model.Account{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: "hash",
		Role:         access.RoleCustomer,
		Status:       constant.StatusPending,
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, account); !errors.Is(err, domainerr.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}

	got, err := repo.ByEmail(ctx, "user@example.com")
	if err != nil || got.ID != account.ID {
		t.Fatalf("ByEmail: %v (%+v)", err, got)
	}
	if _, err := repo.ByEmail(ctx, "missing@example.com"); !errors.Is(err, domainerr.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if err := repo.Activate(ctx, account.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if err := repo.UpdatePassword(ctx, account.ID, "new-hash"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, _ = repo.ByID(ctx, account.ID)
	if got.Status != constant.StatusActive || got.PasswordHash != "new-hash" {
		t.Fatalf("unexpected account after update: %+v", got)
	}
}

func TestSessionRepositoryLifecycle(t *testing.T) {
	pool, ctx := setup(t)
	users := NewUserRepository(pool)
	sessions := NewSessionRepository(pool)

	account := &model.Account{ID: uuid.New(), Email: "s@example.com", PasswordHash: "h", Role: access.RoleCustomer, Status: constant.StatusActive}
	if err := users.Create(ctx, account); err != nil {
		t.Fatalf("create user: %v", err)
	}

	first := &model.Session{ID: uuid.New(), UserID: account.ID, RefreshTokenHash: "h1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := sessions.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}
	second := &model.Session{ID: uuid.New(), UserID: account.ID, RefreshTokenHash: "h2", ExpiresAt: time.Now().Add(time.Hour)}
	if err := sessions.Rotate(ctx, first.ID, second); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	old, _ := sessions.ByTokenHash(ctx, "h1")
	if old.RevokedAt == nil {
		t.Fatal("expected old session revoked")
	}
	if fresh, err := sessions.ByTokenHash(ctx, "h2"); err != nil || fresh.ID != second.ID {
		t.Fatalf("expected new session, got %v", err)
	}

	if err := sessions.RevokeAllForUser(ctx, account.ID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	newer, _ := sessions.ByTokenHash(ctx, "h2")
	if newer.RevokedAt == nil {
		t.Fatal("expected all sessions revoked")
	}
}

func TestResetRepositoryLifecycle(t *testing.T) {
	pool, ctx := setup(t)
	users := NewUserRepository(pool)
	resets := NewResetRepository(pool)

	account := &model.Account{ID: uuid.New(), Email: "p@example.com", PasswordHash: "h", Role: access.RoleCustomer, Status: constant.StatusActive}
	if err := users.Create(ctx, account); err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := &model.ResetRequest{ID: uuid.New(), UserID: account.ID, TokenHash: "t1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := resets.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := resets.ByTokenHash(ctx, "t1")
	if err != nil || got.ID != req.ID {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if err := resets.Consume(ctx, req.ID); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	got, _ = resets.ByTokenHash(ctx, "t1")
	if got.UsedAt == nil {
		t.Fatal("expected reset request consumed")
	}
}
