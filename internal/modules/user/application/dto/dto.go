// Package dto defines the user module's use-case input/output types.
//
// Inputs carry the acting account id, which presentation always takes from the
// session and never from the request body or query (FR-006, research D5). A
// nil pointer in a partial-update input means "keep the current value", while a
// pointer to an empty string means "clear it" (FR-004).
package dto

import (
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// AvatarOutput is the stored avatar reference returned to clients. A nil value
// is the only signal that the customer has no photo.
type AvatarOutput struct {
	// PublicID is the media provider's opaque identifier.
	PublicID string
	// URL is the displayable link.
	URL string
	// Width and Height are the stored dimensions as reported by the provider.
	Width  int
	Height int
}

// ProfileOutput is the customer-facing profile.
type ProfileOutput struct {
	// ID identifies the account.
	ID uuid.UUID
	// Email is the account email, owned by the auth module.
	Email string
	// Role is the account role.
	Role access.Role
	// DisplayName may be empty when the customer never set one.
	DisplayName string
	// Phone is the normalised phone number, or nil when unset.
	Phone *string
	// Avatar is the stored avatar reference, or nil when the customer has no
	// photo. A media outage never fails a profile read (FR-021).
	Avatar *AvatarOutput
}

// UpdateProfileInput updates the display name and/or the phone independently.
// Both fields keep their current value when the corresponding pointer is nil.
type UpdateProfileInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// DisplayName is the new display name; nil keeps the current value and a
	// pointer to an empty string clears it (FR-004).
	DisplayName *string
	// Phone is the new phone number; nil keeps the current value and a pointer
	// to an empty string clears it. The domain normalises the value before it is
	// stored (research D9).
	Phone *string
}

// SetAvatarInput carries an uploaded avatar to the media service. The bytes are
// validated by the domain from their content, never from the client-declared file
// name, which is why no name is carried here at all (FR-014, research D7).
type SetAvatarInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// Content is the uploaded payload, already capped at the configured ceiling.
	Content []byte
}

// AddressOutput is one shipping address.
type AddressOutput struct {
	// ID identifies the address.
	ID uuid.UUID
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string
	// ProvinceCode and ProvinceName identify the first-level unit.
	ProvinceCode string
	ProvinceName string
	// WardCode and WardName identify the second-level unit.
	WardCode string
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
	// IsDefault reports whether this is the account's default address.
	IsDefault bool
	// DivisionNeedsReview is true when the stored province or ward code is no
	// longer in the official dataset. The captured names are still returned so
	// the customer sees what was saved (research D10).
	DivisionNeedsReview bool
}

// ListAddressesInput pages through one account's addresses.
type ListAddressesInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// Page is 1-based.
	Page int
	// PageSize is the number of addresses per page.
	PageSize int
}

// AddressPageOutput is one page of addresses.
type AddressPageOutput struct {
	// Addresses holds the page's addresses, default address first.
	Addresses []AddressOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of non-hidden addresses of the account.
	Total int64
}

// CreateAddressInput creates an address. The first address of an account
// becomes its default automatically (FR-009).
type CreateAddressInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the recipient phone number, normalised by the domain.
	RecipientPhone string
	// ProvinceCode and WardCode must exist in the official dataset and the ward
	// must belong to the province (FR-007a, FR-007b).
	ProvinceCode string
	// ProvinceName is optional; the current name is captured when omitted.
	ProvinceName string
	// WardCode must belong to ProvinceCode.
	WardCode string
	// WardName is optional; the current name is captured when omitted.
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
}

// UpdateAddressInput edits an address. Every field keeps its current value when
// the corresponding pointer is nil. The default flag is not settable here; the
// dedicated route owns it (FR-011).
type UpdateAddressInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// AddressID is the address to edit. It is always scoped to UserID.
	AddressID uuid.UUID
	// RecipientName is the new recipient name; nil keeps the current value.
	RecipientName *string
	// RecipientPhone is the new recipient phone; nil keeps the current value.
	RecipientPhone *string
	// ProvinceCode is the new province; nil keeps the current value. Changing it
	// requires a ward that belongs to it.
	ProvinceCode *string
	// ProvinceName is optional; the current name is recaptured when the province
	// changes.
	ProvinceName *string
	// WardCode is the new ward; nil keeps the current value.
	WardCode *string
	// WardName is optional; the current name is recaptured when the ward changes.
	WardName *string
	// StreetAddress is the new street address; nil keeps the current value.
	StreetAddress *string
}

// AddressRefInput targets one address of the acting account, for the routes
// that need nothing else: mark as default and hide.
type AddressRefInput struct {
	// UserID is the acting account, taken from the session.
	UserID uuid.UUID
	// AddressID is the targeted address. It is always scoped to UserID, so an
	// address of another customer is simply not found (FR-013).
	AddressID uuid.UUID
}
