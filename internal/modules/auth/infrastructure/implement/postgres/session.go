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

// SessionRepository is the PostgreSQL implementation of the session port.
type SessionRepository struct {
	repository.Base[model.Session, uuid.UUID]
	pool *pgxpool.Pool
}

// NewSessionRepository creates a session repository.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	r := &SessionRepository{pool: pool}
	r.Base = repository.Base[model.Session, uuid.UUID]{
		Pool:          pool,
		Table:         "sessions",
		IDColumn:      "id",
		Columns:       []string{"id", "user_id", "refresh_token_hash", "expires_at", "revoked_at", "rotated_from", "user_agent", "ip", "created_at"},
		OrderBy:       "created_at DESC",
		InsertColumns: []string{"id", "user_id", "refresh_token_hash", "expires_at", "user_agent", "ip"},
		InsertValues: func(s *model.Session) []any {
			return []any{s.ID, s.UserID, s.RefreshTokenHash, s.ExpiresAt, nullable(s.UserAgent), nullable(s.IP)}
		},
		UpdateColumns: []string{"revoked_at", "user_agent", "ip"},
		UpdateValues: func(s *model.Session) []any {
			return []any{s.RevokedAt, nullable(s.UserAgent), nullable(s.IP)}
		},
		IDValue: func(s *model.Session) any { return s.ID },
		Scan:    scanSession,
	}
	return r
}

func (r *SessionRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create inserts a session.
func (r *SessionRepository) Create(ctx context.Context, s *model.Session) error {
	const query = `
		INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := r.querier(ctx).Exec(ctx, query, s.ID, s.UserID, s.RefreshTokenHash, s.ExpiresAt, nullable(s.UserAgent), nullable(s.IP)); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// ByTokenHash loads a session by its refresh-token hash.
func (r *SessionRepository) ByTokenHash(ctx context.Context, hash string) (*model.Session, error) {
	const query = `
		SELECT id, user_id, refresh_token_hash, expires_at, revoked_at, rotated_from, user_agent, ip, created_at
		FROM sessions WHERE refresh_token_hash = $1`
	return scanSession(r.querier(ctx).QueryRow(ctx, query, hash))
}

// Rotate revokes the old session and inserts its replacement. It runs inside the
// caller's transaction when the use case opened one through the UnitOfWork port.
func (r *SessionRepository) Rotate(ctx context.Context, oldID uuid.UUID, next *model.Session) error {
	q := r.querier(ctx)
	if _, err := q.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, oldID); err != nil {
		return fmt.Errorf("revoke rotated session: %w", err)
	}
	const query = `
		INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at, rotated_from, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := q.Exec(ctx, query, next.ID, next.UserID, next.RefreshTokenHash, next.ExpiresAt, oldID, nullable(next.UserAgent), nullable(next.IP)); err != nil {
		return fmt.Errorf("insert rotated session: %w", err)
	}
	return nil
}

// Revoke marks a session revoked.
func (r *SessionRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	if _, err := r.querier(ctx).Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// RevokeAllForUser revokes every session of an account.
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	const query = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
	if _, err := r.querier(ctx).Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

func scanSession(row repository.Row) (*model.Session, error) {
	var (
		session model.Session
		ua      *string
		ip      *string
	)
	err := row.Scan(&session.ID, &session.UserID, &session.RefreshTokenHash, &session.ExpiresAt, &session.RevokedAt, &session.RotatedFrom, &ua, &ip, &session.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	if ua != nil {
		session.UserAgent = *ua
	}
	if ip != nil {
		session.IP = *ip
	}
	return &session, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
