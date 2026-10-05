package model

import (
	"errors"
	"strings"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// Customers type the same number several ways; all of them mean one number and
// all of them must end up stored the same way (research D9).
func TestNormalizePhoneAcceptsEverySpellingOfTheSameNumber(t *testing.T) {
	cases := map[string]string{
		"0912345678":       "0912345678",
		"0912 345 678":     "0912345678",
		"0912-345-678":     "0912345678",
		"0912.345.678":     "0912345678",
		"(0912) 345 678":   "0912345678",
		"+84912345678":     "0912345678",
		"+84 912 345 678":  "0912345678",
		"  +84912345678  ": "0912345678",
		"0912-345-678\n":   "0912345678",
		"0901234567":       "0901234567",
		"0988 765 432":     "0988765432",
	}

	for raw, want := range cases {
		got, err := NormalizePhone(raw)
		if err != nil {
			t.Errorf("NormalizePhone(%q): unexpected error %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizePhoneRejectsAnythingThatIsNotAVietnameseMobileNumber(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"too short", "12345"},
		{"nine digits", "912345678"},
		{"nine digits after a country code", "+841234567"},
		{"eleven digits", "09123456789"},
		{"country code without the plus", "84912345678"},
		{"foreign country code", "+85912345678"},
		{"letters", "09123456a8"},
		{"letters only", "abcdefghij"},
		{"a formatted number with letters", "0912 345 67a"},
		{"no leading zero", "9123456780"},
		{"empty", ""},
		{"separators only", "  - . ( )  "},
		{"internal punctuation left over", "0912-345.678(9)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizePhone(tc.raw)
			if !errors.Is(err, domainerr.ErrInvalidPhone) {
				t.Fatalf("expected ErrInvalidPhone for %q, got %q / %v", tc.raw, got, err)
			}
			if got != "" {
				t.Fatalf("a rejected number must yield no value, got %q", got)
			}
		})
	}
}

// The stored form is the point of the value object: one representation in the
// database, so equality checks and a future SMS integration never have to
// handle four spellings of one person.
func TestNormalizePhoneAlwaysStoresTenDigits(t *testing.T) {
	for _, raw := range []string{"0912345678", "+84912345678", "  0912-345-678  "} {
		got, err := NormalizePhone(raw)
		if err != nil {
			t.Fatalf("NormalizePhone(%q): %v", raw, err)
		}
		if len(got) != 10 {
			t.Fatalf("NormalizePhone(%q) = %q, want exactly ten digits", raw, got)
		}
		if !strings.HasPrefix(got, "0") {
			t.Fatalf("NormalizePhone(%q) = %q, want a number starting with 0", raw, got)
		}
		for _, r := range got {
			if r < '0' || r > '9' {
				t.Fatalf("NormalizePhone(%q) = %q, want digits only", raw, got)
			}
		}
	}
}

// Normalising an already-normalised value is a no-op, so a stored value read
// back can be re-validated without changing it.
func TestNormalizePhoneIsIdempotent(t *testing.T) {
	once, err := NormalizePhone("+84912345678")
	if err != nil {
		t.Fatalf("first normalisation: %v", err)
	}
	twice, err := NormalizePhone(once)
	if err != nil {
		t.Fatalf("second normalisation: %v", err)
	}
	if once != twice {
		t.Fatalf("normalisation is not idempotent: %q then %q", once, twice)
	}
}
