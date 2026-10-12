package model

import (
	"net/mail"
	"strings"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

// ValidateEmail rejects a submitted address that is not a plain, well-formed
// email. It parses the value with net/mail and then applies the minimum sanity
// checks the parser does not enforce: a non-empty local part and domain, and a
// dot in the domain, so an address such as "user@localhost" is refused
// (FR-001). Validation runs against the raw submitted value, before the address
// is normalized, so a malformed input is rejected before any temporary or
// durable state is written.
func ValidateEmail(email string) error {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return domainerr.ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return domainerr.ErrInvalidEmail
	}
	// ParseAddress also accepts a display-name form ("Name <user@example.com>").
	// Only a bare address is valid here, so the parsed address must be the whole
	// input.
	if addr.Address != trimmed {
		return domainerr.ErrInvalidEmail
	}
	at := strings.LastIndex(addr.Address, "@")
	if at <= 0 || at == len(addr.Address)-1 {
		return domainerr.ErrInvalidEmail
	}
	if !strings.Contains(addr.Address[at+1:], ".") {
		return domainerr.ErrInvalidEmail
	}
	return nil
}
