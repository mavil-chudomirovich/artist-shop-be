// Package repository declares the auth module's repository ports. Concrete
// implementations (infrastructure/implement/postgres) embed the generic
// share/repository.Base and satisfy these interfaces.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// UserRepository persists accounts.
type UserRepository interface {
	Create(ctx context.Context, account *model.Account) error
	UpsertAdmin(ctx context.Context, account *model.Account) error
	ByEmail(ctx context.Context, email string) (*model.Account, error)
	ByID(ctx context.Context, id uuid.UUID) (*model.Account, error)
	Activate(ctx context.Context, id uuid.UUID) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
}
