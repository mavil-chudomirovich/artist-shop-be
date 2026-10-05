// Package postgres implements the auth module's repository ports with pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// UserRepository is the PostgreSQL implementation of the user repository port.
type UserRepository struct {
	repository.Base[model.Account, uuid.UUID]
	pool *pgxpool.Pool
}

// NewUserRepository creates a user repository.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	r := &UserRepository{pool: pool}
	r.Base = repository.Base[model.Account, uuid.UUID]{
		Pool:          pool,
		Table:         "users",
		IDColumn:      "id",
		OrderBy:       "created_at DESC",
		InsertColumns: []string{"id", "email", "password_hash", "role", "status"},
		InsertValues: func(a *model.Account) []any {
			return []any{a.ID, a.Email, a.PasswordHash, string(a.Role), string(a.Status)}
		},
		UpdateColumns: []string{"password_hash", "role", "status"},
		UpdateValues: func(a *model.Account) []any {
			return []any{a.PasswordHash, string(a.Role), string(a.Status)}
		},
		IDValue: func(a *model.Account) any { return a.ID },
		Scan:    scanAccount,
	}
	return r
}

func (r *UserRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create inserts an account, mapping unique violations to ErrEmailTaken.
func (r *UserRepository) Create(ctx context.Context, a *model.Account) error {
	const query = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := r.querier(ctx).Exec(ctx, query, a.ID, a.Email, a.PasswordHash, string(a.Role), string(a.Status))
	if isUniqueViolation(err) {
		return domainerr.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// UpsertAdmin creates or updates the admin account.
func (r *UserRepository) UpsertAdmin(ctx context.Context, a *model.Account) error {
	const query = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, $2, $3, 'ADMIN', 'active')
		ON CONFLICT (email) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, role = 'ADMIN', status = 'active', updated_at = now()`
	if _, err := r.querier(ctx).Exec(ctx, query, a.ID, a.Email, a.PasswordHash); err != nil {
		return fmt.Errorf("upsert admin: %w", err)
	}
	return nil
}

// ByID loads an account by id.
func (r *UserRepository) ByID(ctx context.Context, id uuid.UUID) (*model.Account, error) {
	return r.Base.FindByID(ctx, id)
}

// ByEmail loads an account by normalized email.
func (r *UserRepository) ByEmail(ctx context.Context, email string) (*model.Account, error) {
	const query = `
		SELECT id, email, password_hash, role, status, created_at, updated_at
		FROM users WHERE email = $1`
	return scanAccount(r.querier(ctx).QueryRow(ctx, query, email))
}

// Activate sets a pending account to active.
func (r *UserRepository) Activate(ctx context.Context, id uuid.UUID) error {
	const query = `UPDATE users SET status = 'active', updated_at = now() WHERE id = $1`
	if _, err := r.querier(ctx).Exec(ctx, query, id); err != nil {
		return fmt.Errorf("activate user: %w", err)
	}
	return nil
}

// UpdatePassword replaces the password hash.
func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	const query = `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`
	if _, err := r.querier(ctx).Exec(ctx, query, id, passwordHash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

func scanAccount(row repository.Row) (*model.Account, error) {
	var (
		account model.Account
		role    string
		status  string
	)
	err := row.Scan(&account.ID, &account.Email, &account.PasswordHash, &role, &status, &account.CreatedAt, &account.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	account.Role = access.Role(role)
	account.Status = constant.Status(status)
	return &account, nil
}
