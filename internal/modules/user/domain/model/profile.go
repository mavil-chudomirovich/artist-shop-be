package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
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
// The administrative dataset is deliberately invisible to this entity: whether a
// province or ward code still exists is answered in the application layer
// through the Divisions port, because the domain may not import
// internal/share/administrative (Constitution I, research D1).
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
	//
	// Neither timestamp is projected by the repository adapter: no profile
	// response carries them, so both stay at the zero value on a profile this
	// module reads. Auth owns both columns (data-model.md §2).
	UpdatedAt time.Time
}

// SetDisplayName stores the trimmed display name.
//
// An empty name is a valid outcome rather than a rejection: a customer who has
// not set a name yet has none, and one who clears it must end up with an empty
// name instead of keeping the old value (FR-002, FR-004).
func (p *Profile) SetDisplayName(name string) {
	p.DisplayName = strings.TrimSpace(name)
}

// SetPhone normalises and stores the phone number. An empty value clears it.
//
// A value that is not a Vietnamese mobile number is refused with
// domainerr.ErrInvalidPhone and the stored number is left exactly as it was, so
// a rejected update can never lose a value the customer did not touch (FR-003).
func (p *Profile) SetPhone(raw string) error {
	if strings.TrimSpace(raw) == "" {
		p.Phone = nil
		return nil
	}
	normalized, err := NormalizePhone(raw)
	if err != nil {
		return err
	}
	p.Phone = &normalized
	return nil
}

// SetAvatar attaches an avatar reference or, with nil, removes it.
//
// The four avatar columns are written as one unit, so the entity accepts only
// the two representable states: no reference at all, or a complete one. A
// half-filled reference would leave a profile the client cannot render, so it is
// refused here and the same rule is a CHECK constraint on the row (FR-016).
func (p *Profile) SetAvatar(ref *AvatarReference) error {
	if ref != nil && !ref.IsComplete() {
		return domainerr.ErrIncompleteAvatar
	}
	p.Avatar = ref
	return nil
}
