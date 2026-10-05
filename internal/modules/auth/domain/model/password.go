package model

import (
	"unicode"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

// ValidatePasswordPolicy requires at least 8 characters with a letter, a digit,
// and a special character (FR-003).
func ValidatePasswordPolicy(password string) error {
	if len(password) < 8 {
		return domainerr.ErrWeakPassword
	}
	var hasLetter, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPrint(r):
			hasSpecial = true
		}
	}
	if !hasLetter || !hasDigit || !hasSpecial {
		return domainerr.ErrWeakPassword
	}
	return nil
}
