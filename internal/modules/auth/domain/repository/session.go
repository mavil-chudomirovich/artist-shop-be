package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// SessionRepository persists sessions.
type SessionRepository interface {
	Create(ctx context.Context, s *model.Session) error
	ByTokenHash(ctx context.Context, hash string) (*model.Session, error)
	Rotate(ctx context.Context, oldID uuid.UUID, next *model.Session) error
	Revoke(ctx context.Context, id uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
}
