package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// ErrCustomerNotFound is returned when no account carries the requested
// identifier. The providing module translates its own domain error into this
// sentinel so a consumer can branch on it without importing that module.
var ErrCustomerNotFound = errors.New("customer not found")

// CustomerLookupService exposes a customer's contact details and addresses to
// other modules for order and commission handling.
//
// The lookup is read-only by design: the user module offers no write path for
// customer data to an administrator (research D8). Every successful call is
// audited by the provider.
type CustomerLookupService interface {
	// LookupCustomer returns the customer, or ErrCustomerNotFound.
	LookupCustomer(ctx context.Context, userID uuid.UUID) (Customer, error)
}

// Customer is the contract DTO for a customer. It is declared here rather than
// reused from the user module's domain model so a storage change on the provider
// side cannot silently break every consumer (docs/system-design/
// contract-purity.md).
type Customer struct {
	// ID identifies the account.
	ID uuid.UUID
	// Email is the account's email, owned by the auth module.
	Email string
	// Role is the account role.
	Role access.Role
	// DisplayName may be empty when the customer never set one.
	DisplayName string
	// Phone is the normalised phone number, or nil when the customer has none.
	Phone *string
	// Addresses holds the customer's non-hidden addresses, default address
	// first and then most recently updated. Consumers may rely on that order to
	// offer the default first (FR-007d); it is never empty-by-lazy-default, an
	// account with no address carries a nil slice.
	Addresses []CustomerAddress
}

// CustomerAddress is one delivery address of a customer, as seen by a consumer
// module. Province and ward codes are the values stored on the address; the
// accompanying names were captured when the address was saved, so they stay
// truthful even if the official dataset later renames the unit (research D10).
type CustomerAddress struct {
	// ID identifies the address within the provider's module.
	ID uuid.UUID
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string
	// ProvinceCode and ProvinceName identify the first-level unit.
	ProvinceCode string
	ProvinceName string
	// WardCode and WardName identify the second-level unit, which always
	// belongs to ProvinceCode.
	WardCode string
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
	// IsDefault reports whether this is the customer's default address. At most
	// one address of the account is default.
	IsDefault bool
}
