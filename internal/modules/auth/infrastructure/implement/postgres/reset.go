package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// ResetRepository is the PostgreSQL implementation of the reset port.
type ResetRepository struct {
	repository.Base[model.ResetRequest, uuid.UUID]
	pool *pgxpool.Pool
}

// NewResetRepository creates a password-reset repository.
func NewResetRepository(pool *pgxpool.Pool) *ResetRepository {
	r := &ResetRepository{pool: pool}
	r.Base = repository.Base[model.ResetRequest, uuid.UUID]{
		Pool:          pool,
		Table:         "password_reset_requests",
		IDColumn:      "id",
		OrderBy:       "created_at DESC",
		InsertColumns: []string{"id", "user_id", "token_hash", "expires_at"},
		InsertValues: func(r *model.ResetRequest) []any {
			return []any{r.ID, r.UserID, r.TokenHash, r.ExpiresAt}
		},
		UpdateColumns: []string{"used_at"},
		UpdateValues: func(r *model.ResetRequest) []any {
			return []any{r.UsedAt}
		},
		IDValue: func(r *model.ResetRequest) any { return r.ID },
		Scan:    scanReset,
	}
	return r
}

func (r *ResetRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create invalidates open requests and inserts a new one in the caller's
// transaction (application-owned via UnitOfWork).
func (r *ResetRepository) Create(ctx context.Context, req *model.ResetRequest) error {
	q := r.querier(ctx)
	if _, err := q.Exec(ctx, `UPDATE password_reset_requests SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, req.UserID); err != nil {
		return fmt.Errorf("invalidate open resets: %w", err)
	}
	const query = `INSERT INTO password_reset_requests (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`
	if _, err := q.Exec(ctx, query, req.ID, req.UserID, req.TokenHash, req.ExpiresAt); err != nil {
		return fmt.Errorf("insert reset request: %w", err)
	}
	return nil
}

// ByTokenHash loads a reset request by token hash.
func (r *ResetRepository) ByTokenHash(ctx context.Context, tokenHash string) (*model.ResetRequest, error) {
	const query = `
		SELECT id, user_id, token_hash, expires_at, used_at, created_at
		FROM password_reset_requests WHERE token_hash = $1`
	return scanReset(r.querier(ctx).QueryRow(ctx, query, tokenHash))
}

// Consume marks a reset request as used.
func (r *ResetRepository) Consume(ctx context.Context, id uuid.UUID) error {
	if _, err := r.querier(ctx).Exec(ctx, `UPDATE password_reset_requests SET used_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("consume reset request: %w", err)
	}
	return nil
}

func scanReset(row repository.Row) (*model.ResetRequest, error) {
	var req model.ResetRequest
	err := row.Scan(&req.ID, &req.UserID, &req.TokenHash, &req.ExpiresAt, &req.UsedAt, &req.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrResetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan reset request: %w", err)
	}
	return &req, nil
}
