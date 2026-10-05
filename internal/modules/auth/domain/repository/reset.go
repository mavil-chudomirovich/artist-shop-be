package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// ResetRepository persists password reset requests.
type ResetRepository interface {
	Create(ctx context.Context, r *model.ResetRequest) error
	ByTokenHash(ctx context.Context, tokenHash string) (*model.ResetRequest, error)
	Consume(ctx context.Context, id uuid.UUID) error
}
