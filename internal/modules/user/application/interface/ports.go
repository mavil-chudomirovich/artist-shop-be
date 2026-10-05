// Package appinterface declares the user module's use-case interface, the
// external-service ports the application depends on, and UnitOfWork.
package appinterface

import "context"

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
