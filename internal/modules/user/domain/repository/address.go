package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// AddressRepository persists shipping addresses.
//
// It does not embed share/repository.Repository[model.Address, uuid.UUID]: the
// generic FindAll would list every customer's addresses and the generic Delete
// is a hard delete, while this module hides addresses so past orders keep the
// text they used (research D4). Every method is therefore scoped to the owning
// account, and the adapter embeds share/repository.Base only for the writes it
// shares, with an explicit Columns projection.
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port, which is what makes SetDefault and
// ClearDefault a single indivisible outcome (FR-010).
type AddressRepository interface {
	// Create inserts a new address for an account.
	Create(ctx context.Context, address *model.Address) error
	// Update writes the editable columns of the address named by its identifier,
	// preserving the default flag (FR-011).
	Update(ctx context.Context, address *model.Address) error
	// FindByOwner returns one non-hidden address of the given account. An
	// address that is unknown, hidden or owned by somebody else is reported the
	// same way, as domainerr.ErrAddressNotFound, so the response never confirms
	// that another customer's address exists (FR-013).
	FindByOwner(ctx context.Context, userID, addressID uuid.UUID) (*model.Address, error)
	// ListByOwner returns a page of the account's non-hidden addresses, default
	// address first and then most recently updated, plus the total number of
	// non-hidden addresses.
	ListByOwner(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]model.Address, int64, error)
	// ClearDefault removes the default flag from every non-hidden address of the
	// account.
	ClearDefault(ctx context.Context, userID uuid.UUID) error
	// SetDefault marks one address as the account's default. The caller has
	// already resolved ownership and MUST run it in the same transaction as the
	// preceding ClearDefault; the partial unique index on (user_id) makes a
	// second default impossible at the storage layer (ADR-003).
	SetDefault(ctx context.Context, addressID uuid.UUID) error
	// Hide stamps the address with hiddenAt instead of deleting the row.
	Hide(ctx context.Context, userID, addressID uuid.UUID, hiddenAt time.Time) error
}
