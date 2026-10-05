// Package model holds the auth module's entities and value objects.
package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// Account is a person who can authenticate.
type Account struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         access.Role
	Status       constant.Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NormalizeEmail trims and lowercases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Activate moves a pending account to active.
func (a *Account) Activate() error {
	if a.Status != constant.StatusPending {
		return domainerr.ErrInvalidTransition
	}
	a.Status = constant.StatusActive
	return nil
}

// Disable moves an active account to disabled.
func (a *Account) Disable() error {
	if a.Status != constant.StatusActive {
		return domainerr.ErrInvalidTransition
	}
	a.Status = constant.StatusDisabled
	return nil
}

// Enable moves a disabled account back to active.
func (a *Account) Enable() error {
	if a.Status != constant.StatusDisabled {
		return domainerr.ErrInvalidTransition
	}
	a.Status = constant.StatusActive
	return nil
}

// CanSignIn reports whether the account may authenticate.
func (a *Account) CanSignIn() error {
	switch a.Status {
	case constant.StatusActive:
		return nil
	case constant.StatusPending:
		return domainerr.ErrAccountPending
	case constant.StatusDisabled:
		return domainerr.ErrAccountDisabled
	default:
		return domainerr.ErrInvalidTransition
	}
}
