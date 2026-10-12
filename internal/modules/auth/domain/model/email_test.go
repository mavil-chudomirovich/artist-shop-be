package model

import (
	"errors"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

// TestValidateEmailAcceptsWellFormedAddresses pins the positive path: a plain,
// well-formed address is accepted, including one that still carries the
// surrounding whitespace registration trims (FR-001).
func TestValidateEmailAcceptsWellFormedAddresses(t *testing.T) {
	valid := []string{
		"user@example.com",
		"New@Example.com",
		"  user@example.com  ",
		"first.last+tag@sub.example.co.uk",
	}
	for _, email := range valid {
		if err := ValidateEmail(email); err != nil {
			t.Fatalf("ValidateEmail(%q): expected nil, got %v", email, err)
		}
	}
}

// TestValidateEmailRejectsMalformedAddresses pins the negative path: an empty
// value, a missing part, a domain without a dot and the display-name form are
// all refused (FR-001).
func TestValidateEmailRejectsMalformedAddresses(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"not-an-email",
		"user@",
		"@example.com",
		"user@example",
		"user example@example.com",
		"Name <user@example.com>",
	}
	for _, email := range invalid {
		if err := ValidateEmail(email); !errors.Is(err, domainerr.ErrInvalidEmail) {
			t.Fatalf("ValidateEmail(%q): expected ErrInvalidEmail, got %v", email, err)
		}
	}
}
