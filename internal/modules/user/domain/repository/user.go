package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// ProfileRepository persists the six profile columns this module owns on the
// accounts table (display name, phone and the four avatar columns).
//
// It deliberately does not embed share/repository.Repository[model.Profile,
// uuid.UUID]: a profile is a column group of a row owned by the auth module, so
// Create, Delete and an unscoped FindAll have no meaning here — FindAll would in
// fact enumerate every customer's profile, which this module must never do. The
// adapter still embeds share/repository.Base for its CRUD plumbing and sets its
// Columns projection to exactly the columns its scan function reads.
type ProfileRepository interface {
	// ByID returns the account's profile. It reports
	// domainerr.ErrUserNotFound when no account carries the identifier, which
	// only the administrator lookup can observe.
	ByID(ctx context.Context, id uuid.UUID) (*model.Profile, error)
	// Save writes the six profile columns of the account named by the profile's
	// identifier. Email, role, password and status are never written by this
	// module.
	Save(ctx context.Context, profile *model.Profile) error
}
