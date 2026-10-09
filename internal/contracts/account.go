package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrAccountNotFound is returned when no account carries the requested email. The
// providing module translates its own domain error into this sentinel so a
// consumer can branch on it without importing that module, exactly as
// CustomerLookupService does with ErrCustomerNotFound (research D8).
var ErrAccountNotFound = errors.New("account not found")

// AccountLookup resolves an account by its email, so the order module can name a
// transfer recipient without reading the auth module's table. The auth module
// (01) supplies an adapter at the composition root; the order module (07) is the
// consumer (Constitution I, research D8).
//
// It is a separate contract rather than an extension of CustomerLookupService:
// that service is keyed by identifier and does not own the email, which is the
// auth module's fact (contracts.Customer.Email says so).
type AccountLookup interface {
	// UserIDByEmail returns the identifier of the account whose email matches, or
	// ErrAccountNotFound when none does. The email is matched as the auth module
	// stores it: normalised to lowercase and trimmed (research D8).
	UserIDByEmail(ctx context.Context, email string) (uuid.UUID, error)
}
