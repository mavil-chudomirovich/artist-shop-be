package model

import (
	"time"

	"github.com/google/uuid"
)

// Address is one shipping address of an account.
//
// ProvinceCode and WardCode are the values that join to the official dataset;
// ProvinceName and WardName are the display names captured when the address was
// saved, so history stays truthful after the government renames a unit
// (research D10). The dataset itself is invisible here: whether a code still
// exists is answered in the application layer through the Divisions port,
// because the domain may not import internal/share/administrative
// (Constitution I).
//
// A non-nil DeletedAt means the address is hidden: it disappears from every
// customer-facing list and can never be the default, but the row survives so
// past orders keep the address text they used (research D4).
//
// The transitions — create, edit, mark default, hide — are added with the
// address use cases (T034, T038); this struct is the persisted shape the
// repository adapter and the mapper read.
type Address struct {
	// ID identifies the address.
	ID uuid.UUID
	// UserID is the owning account. Every query filters on it, so one customer
	// can never reach another customer's address (FR-006).
	UserID uuid.UUID
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string
	// ProvinceCode is the stored first-level administrative unit.
	ProvinceCode string
	// ProvinceName is the display name captured at save time.
	ProvinceName string
	// WardCode is the stored second-level administrative unit; it always
	// belongs to ProvinceCode.
	WardCode string
	// WardName is the display name captured at save time.
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
	// IsDefault reports whether this is the account's default address. At most
	// one non-hidden address of an account is default (FR-008).
	IsDefault bool
	// DeletedAt is when the address was hidden, or nil while it is visible.
	DeletedAt *time.Time
	// CreatedAt is when the address was created.
	CreatedAt time.Time
	// UpdatedAt is when the address was last written.
	UpdatedAt time.Time
}
