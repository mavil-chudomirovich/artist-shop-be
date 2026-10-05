// Package appinterface declares the user module's use-case interface, the
// external-service ports the application depends on, and UnitOfWork.
package appinterface

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// MediaStore stores binary media outside the service and hands back a reference
// to keep in the database. It is named for the capability rather than the
// vendor, so swapping the provider touches only the adapter (ADR-005).
type MediaStore interface {
	// Upload stores content and returns the stored reference. targetWidth asks
	// the provider to resize during the upload; the provider, not this service,
	// is the system of record for the resulting dimensions.
	Upload(ctx context.Context, content []byte, targetWidth int) (MediaReference, error)
	// Remove releases a previously stored reference. Removing an unknown
	// reference must succeed so a retry never fails on an already-freed asset.
	Remove(ctx context.Context, ref MediaReference) error
}

// MediaReference identifies one stored media asset. Media bytes are never kept
// in the database; only this reference is (Constitution, Media constraint).
type MediaReference struct {
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width and Height are the stored pixel dimensions as reported by the
	// provider after resizing.
	Width  int
	Height int
}

// Divisions exposes the official Vietnamese administrative reference data
// (province -> ward, two levels) to the use cases. It is the only way the
// application layer reaches the dataset: the domain package cannot import
// internal/share/administrative, so existence checks are a use-case concern.
type Divisions interface {
	// Provinces returns every province for the first cascading select.
	Provinces(ctx context.Context) ([]Province, error)
	// Wards returns the wards of one province only. A ward list is always
	// scoped to a single province, so clients never receive the whole set.
	Wards(ctx context.Context, provinceCode string) ([]Ward, error)
	// ValidateAddressDivisions checks that the ward exists and belongs to the
	// province. It returns the sentinel errors of
	// internal/share/administrative (ErrUnknownProvince, ErrUnknownWard,
	// ErrWardProvinceMismatch) so presentation maps them to the USER_* codes
	// without this package depending on the shared dataset.
	ValidateAddressDivisions(ctx context.Context, provinceCode, wardCode string) error
}

// Province is one first-level administrative unit.
type Province struct {
	Code string
	Name string
}

// Ward is one second-level administrative unit, always tied to its province.
type Ward struct {
	Code         string
	Name         string
	ProvinceCode string
}

// Auditor records security-relevant and privacy-relevant events of the module:
// every profile, avatar and address change, and every administrator read of a
// customer's contact details (FR-019, FR-022a).
type Auditor interface {
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// UnitOfWork runs a function inside a single database transaction. The
// application layer owns every transaction boundary; repositories never open
// one themselves (Constitution I). The address default-flag transition needs it
// to make clearing the previous default and setting the new one indivisible
// (FR-010).
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Avatar defaults used when the composition leaves the Config values at zero, so
// a zero value still enforces the documented contract instead of accepting any
// upload.
//
// They are the domain's own ceilings rather than a second copy of the numbers:
// the domain is where FR-014 and FR-015 are enforced, and a literal repeated here
// could drift away from it without anything failing.
const (
	// DefaultAvatarMaxBytes is the 2 MB avatar ceiling of FR-014.
	DefaultAvatarMaxBytes int64 = model.MaxAvatarBytes
	// DefaultAvatarTargetWidth is the stored width ceiling of FR-015.
	DefaultAvatarTargetWidth = model.MaxAvatarWidth
)

// Config carries the composition settings the use cases cannot derive
// themselves. It is resolved once in the composition root.
type Config struct {
	// AvatarMaxBytes is the hard ceiling for an uploaded avatar (FR-014). Zero
	// means DefaultAvatarMaxBytes.
	AvatarMaxBytes int64
	// AvatarTargetWidth is the width the media provider is asked to resize to
	// during the upload; the provider is the system of record for the resulting
	// dimensions (research D6). Zero means DefaultAvatarTargetWidth.
	AvatarTargetWidth int
}

// AvatarMaxBytesOrDefault returns the configured ceiling, or the documented
// default when the composition left it unset.
func (c Config) AvatarMaxBytesOrDefault() int64 {
	if c.AvatarMaxBytes <= 0 {
		return DefaultAvatarMaxBytes
	}
	return c.AvatarMaxBytes
}

// AvatarTargetWidthOrDefault returns the configured target width, or the
// documented default when the composition left it unset.
func (c Config) AvatarTargetWidthOrDefault() int {
	if c.AvatarTargetWidth <= 0 {
		return DefaultAvatarTargetWidth
	}
	return c.AvatarTargetWidth
}

// UserService is the module's use-case surface. Every method takes the acting
// account from its input, and presentation fills that input from the
// authenticated session: no method accepts an owner identifier the client chose
// (FR-006, research D5). The single exception is the administrator lookup,
// where the account is the subject of the request rather than the actor.
type UserService interface {
	// GetProfile returns the signed-in customer's profile.
	GetProfile(ctx context.Context, userID uuid.UUID) (dto.ProfileOutput, error)
	// UpdateProfile changes the display name and/or the phone and returns the
	// stored profile.
	UpdateProfile(ctx context.Context, in dto.UpdateProfileInput) (dto.ProfileOutput, error)
	// SetAvatar validates the uploaded bytes, stores them through the MediaStore
	// and returns the profile carrying the new avatar. A rejected upload leaves
	// the current avatar untouched (FR-017).
	SetAvatar(ctx context.Context, in dto.SetAvatarInput) (dto.ProfileOutput, error)
	// RemoveAvatar releases the stored reference and returns the profile without
	// an avatar.
	RemoveAvatar(ctx context.Context, userID uuid.UUID) (dto.ProfileOutput, error)

	// ListAddresses returns one page of the account's non-hidden addresses,
	// default address first.
	ListAddresses(ctx context.Context, in dto.ListAddressesInput) (dto.AddressPageOutput, error)
	// CreateAddress stores a new address and returns it. The first address of an
	// account becomes its default (FR-009).
	CreateAddress(ctx context.Context, in dto.CreateAddressInput) (dto.AddressOutput, error)
	// UpdateAddress edits an address and returns it, preserving the default flag.
	UpdateAddress(ctx context.Context, in dto.UpdateAddressInput) (dto.AddressOutput, error)
	// DeleteAddress hides an address; the row survives for order history.
	DeleteAddress(ctx context.Context, in dto.AddressRefInput) error
	// SetDefaultAddress makes one address the account's single default.
	SetDefaultAddress(ctx context.Context, in dto.AddressRefInput) (dto.AddressOutput, error)

	// LookupCustomer returns a customer's contact details and addresses for an
	// operator. It is read-only, reports contracts.ErrCustomerNotFound for an
	// unknown account and audits every successful read (FR-022, FR-022a).
	//
	// It returns the cross-module contract DTO rather than an application DTO of
	// its own, because *implement.Service satisfies
	// internal/contracts.CustomerLookupService directly: a second shape would be a
	// parallel one that could drift without anything failing
	// (docs/system-design/contract-purity.md).
	//
	// The acting administrator is taken from the context that presentation filled
	// from the session (ActorFromContext); the userID is the subject of the read,
	// never the actor.
	LookupCustomer(ctx context.Context, userID uuid.UUID) (contracts.Customer, error)
}
