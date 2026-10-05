package model

import (
	"strings"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// phoneDigits is the length of a Vietnamese mobile number once normalised: ten
// digits starting with a zero (spec clarification, FR-003).
const phoneDigits = 10

// phoneSeparators are the characters a customer types to group digits. They
// carry no information, so they are removed rather than rejected.
const phoneSeparators = " \t.-()"

// countryPrefix is the international form of the domestic prefix: +84 912 345 678
// is the same number as 0912345678.
const countryPrefix = "+84"

// NormalizePhone turns any spelling of a Vietnamese mobile number into the one
// stored form: ten digits starting with a zero.
//
// It trims the value, removes the grouping characters a customer types, rewrites
// a leading +84 as the domestic 0, and then requires exactly ten digits whose
// first is a zero. It reports domainerr.ErrInvalidPhone for anything else and
// returns an empty string, so a rejected value can never be mistaken for a
// usable one.
//
// The rule lives in the domain rather than in a handler so every entry point —
// HTTP today, a CLI or a worker tomorrow — normalises the same way and the
// database holds exactly one representation of a person (research D9). It is a
// pure function: no I/O, no clock, no external dependency.
func NormalizePhone(raw string) (string, error) {
	compact := stripPhoneSeparators(strings.TrimSpace(raw))
	if strings.HasPrefix(compact, countryPrefix) {
		compact = "0" + strings.TrimPrefix(compact, countryPrefix)
	}
	if !isDomesticPhone(compact) {
		return "", domainerr.ErrInvalidPhone
	}
	return compact, nil
}

// stripPhoneSeparators removes the grouping characters from a raw value. Every
// other character is kept, so a letter or an unexpected symbol survives and is
// rejected by the digit check instead of being silently discarded.
func stripPhoneSeparators(raw string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(phoneSeparators, r) {
			return -1
		}
		return r
	}, raw)
}

// isDomesticPhone reports whether a value is exactly ten digits starting with a
// zero.
func isDomesticPhone(value string) bool {
	if len(value) != phoneDigits || value[0] != '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
