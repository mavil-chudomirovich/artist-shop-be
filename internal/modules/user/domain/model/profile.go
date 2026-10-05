package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// Profile is the customer-facing part of an account. It is not an entity of its
// own: it is the group of profile columns on the row the auth module created,
// keyed by that row's identifier, so ID is also the account id
// (data-model.md §2).
//
// Email and Role belong to the auth module. They are read here because the
// read-only administrator lookup returns them (research D8); this module never
// writes them.
//
// Display name and phone rules (trimming, clearing, phone normalisation) are
// added with the profile use cases (T028, T029); the struct is the persisted
// shape the repository adapter and the mapper read.
type Profile struct {
	// ID identifies the account and is the profile's key.
	ID uuid.UUID
	// Email is the account email, owned by the auth module.
	Email string
	// Role is the account role, owned by the auth module.
	Role access.Role
	// DisplayName is the customer-facing name. Empty means the customer never
	// set one, which is a valid state rather than an error.
	DisplayName string
	// Phone is the normalised phone number, or nil when the customer has none.
	Phone *string
	// Avatar is the stored avatar reference, or nil when the customer has no
	// photo.
	Avatar *AvatarReference
	// CreatedAt is when the account was created; auth owns the column.
	CreatedAt time.Time
	// UpdatedAt is the row timestamp, refreshed by every write to the row.
	UpdatedAt time.Time
}
