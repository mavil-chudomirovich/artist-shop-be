// Package account adapts the auth module's own repository to the
// contracts.AccountLookup port, so the order module can resolve a transfer
// recipient by email without reading the auth module's table. The order depends
// on that port; this adapter is supplied at the composition root (Constitution I,
// research D8).
package account

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/repository"
)

// Adapter resolves an account by email over the auth module's own repository, so
// the answer comes from the source of truth rather than a second copy. It never
// opens a transaction: the read joins the transaction the context already carries
// when there is one (Constitution I).
type Adapter struct {
	// Users is the auth module's user repository port.
	Users domainrepo.UserRepository
}

// New creates the account adapter over the auth user repository.
func New(users domainrepo.UserRepository) *Adapter {
	return &Adapter{Users: users}
}

// UserIDByEmail returns the identifier of the account whose email matches, or
// contracts.ErrAccountNotFound when none does (research D8).
//
// The email is normalised the same way the auth module stores it, so a transfer
// named with mixed case or stray whitespace resolves to the account the module
// already holds rather than answering not-found for a value that differs only in
// spelling (auth model.NormalizeEmail).
func (a *Adapter) UserIDByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	normalized := model.NormalizeEmail(email)
	account, err := a.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return uuid.Nil, contracts.ErrAccountNotFound
		}
		return uuid.Nil, err
	}
	return account.ID, nil
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, module 05's availability adapter).
var _ contracts.AccountLookup = (*Adapter)(nil)
