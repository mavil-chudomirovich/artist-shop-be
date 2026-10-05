package model

import (
	"errors"
	"strings"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

func TestSetDisplayNameTrimsTheValue(t *testing.T) {
	cases := map[string]string{
		"Nguyen Van A":      "Nguyen Van A",
		"  Nguyen Van A  ":  "Nguyen Van A",
		"\tNguyen Van A\n":  "Nguyen Van A",
		"Nguyen  Van   A":   "Nguyen  Van   A",
		"  ":                "",
		"":                  "",
		"Nguyễn Văn A":      "Nguyễn Văn A",
		"   Nguyễn  Văn A ": "Nguyễn  Văn A",
	}

	for raw, want := range cases {
		profile := Profile{}
		if err := profile.SetDisplayName(raw); err != nil {
			t.Errorf("SetDisplayName(%q): unexpected error %v", raw, err)
			continue
		}
		if profile.DisplayName != want {
			t.Errorf("SetDisplayName(%q) stored %q, want %q", raw, profile.DisplayName, want)
		}
	}
}

// Clearing the display name is a valid outcome, not a rejection: the customer
// may deliberately remove it and the profile stays valid (FR-004).
func TestSetDisplayNameStoresAnEmptyName(t *testing.T) {
	profile := Profile{DisplayName: "Nguyen Van A"}

	if err := profile.SetDisplayName("   "); err != nil {
		t.Fatalf("clearing must not be an error, got %v", err)
	}

	if profile.DisplayName != "" {
		t.Fatalf("expected the display name to be cleared, got %q", profile.DisplayName)
	}
}

// The ceiling is inclusive and the stored value is the trimmed one, so a name of
// exactly the advertised length is accepted and stored without its surrounding
// whitespace (FR-020).
func TestSetDisplayNameAcceptsExactlyTheContractCeiling(t *testing.T) {
	name := strings.Repeat("a", 120)
	profile := Profile{}

	if err := profile.SetDisplayName("  " + name + "  "); err != nil {
		t.Fatalf("a name of exactly the advertised ceiling must be accepted, got %v", err)
	}

	if profile.DisplayName != name {
		t.Fatalf("expected the trimmed name of %d characters, got %d",
			len([]rune(name)), len([]rune(profile.DisplayName)))
	}
}

// One character past the ceiling is refused outright rather than truncated, and
// the response must name the member the customer has to shorten. The stored name
// is left untouched, so a refused update cannot lose a value the customer did not
// touch (FR-020).
func TestSetDisplayNameRefusesOneCharacterOverTheContractCeiling(t *testing.T) {
	profile := Profile{DisplayName: "Nguyen Van A"}

	err := profile.SetDisplayName(strings.Repeat("a", 121))

	if !errors.Is(err, domainerr.ErrAddressInvalid) {
		t.Fatalf("expected the module's field-error sentinel, got %v", err)
	}
	var carrier *domainerr.AddressFieldError
	if !errors.As(err, &carrier) {
		t.Fatalf("expected a named field error, got %v", err)
	}
	if carrier.Field != FieldDisplayName {
		t.Fatalf("expected the rejection to name %q, got %q", FieldDisplayName, carrier.Field)
	}
	if profile.DisplayName != "Nguyen Van A" {
		t.Fatalf("a refused name changed the stored one to %q", profile.DisplayName)
	}
}

// The ceiling counts characters, not bytes: a Vietnamese name of 120 multi-byte
// characters is 360 bytes and must still pass, because the contract advertises
// maxLength 120 to a client sending UTF-8 (FR-020).
func TestSetDisplayNameCountsMultiByteCharactersNotBytes(t *testing.T) {
	profile := Profile{}

	if err := profile.SetDisplayName(strings.Repeat("Ữ", 120)); err != nil {
		t.Fatalf("120 multi-byte characters are 120 characters, got %v", err)
	}
	if profile.DisplayName != strings.Repeat("Ữ", 120) {
		t.Fatalf("expected the multi-byte name to survive unchanged, got %q", profile.DisplayName)
	}

	if err := profile.SetDisplayName(strings.Repeat("Ữ", 121)); err == nil {
		t.Fatal("121 characters must be refused even though the rule counts characters")
	}
}

// Whitespace around a name the entity would have trimmed anyway does not count
// against the ceiling, and does not rescue a name that is genuinely too long
// either: the rule measures the value that gets stored.
func TestSetDisplayNameMeasuresTheCeilingOnTheTrimmedValue(t *testing.T) {
	profile := Profile{}

	if err := profile.SetDisplayName(strings.Repeat("a", 120) + "   \t "); err != nil {
		t.Fatalf("padding the trimmed value must not count against the ceiling, got %v", err)
	}
	if err := profile.SetDisplayName(strings.Repeat("a", 121) + "   "); err == nil {
		t.Fatal("padding must not bring an over-long value back under the ceiling")
	}
}

// The ceiling is the exact number contracts/openapi.yaml advertises as maxLength on
// UpdateProfileRequest.displayName.
//
// This test exists because every other test in this file measures against the
// constant rather than against literals: a constant widened to the wrong number
// would otherwise keep the whole suite green while the server enforced a limit
// different from the one it advertises — exactly the gap this work exists to close.
func TestTheDisplayNameCeilingMatchesTheAdvertisedMaxLength(t *testing.T) {
	if maxDisplayNameRunes != 120 {
		t.Errorf("displayName: the contract advertises maxLength 120, the entity enforces %d",
			maxDisplayNameRunes)
	}
}

// The field key the entity reports is the contract's member name, so presentation
// can put it straight into the field detail without a second vocabulary that could
// drift (FR-020).
func TestTheDisplayNameFieldKeyMatchesTheContractMemberName(t *testing.T) {
	if FieldDisplayName != "displayName" {
		t.Errorf("field key %q does not match the contract member name %q", FieldDisplayName, "displayName")
	}
}

func TestSetPhoneNormalisesAndStores(t *testing.T) {
	cases := map[string]string{
		"0912345678":      "0912345678",
		"  0912 345 678":  "0912345678",
		"+84912345678":    "0912345678",
		"0912-345-678":    "0912345678",
		"(0912)345.678":   "0912345678",
		"\t+84 912345678": "0912345678",
	}

	for raw, want := range cases {
		profile := Profile{}
		if err := profile.SetPhone(raw); err != nil {
			t.Errorf("SetPhone(%q): unexpected error %v", raw, err)
			continue
		}
		if profile.Phone == nil {
			t.Errorf("SetPhone(%q) stored nothing", raw)
			continue
		}
		if *profile.Phone != want {
			t.Errorf("SetPhone(%q) stored %q, want %q", raw, *profile.Phone, want)
		}
	}
}

func TestSetPhoneClearsTheNumberOnAnEmptyValue(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		existing := "0912345678"
		profile := Profile{Phone: &existing}

		if err := profile.SetPhone(raw); err != nil {
			t.Fatalf("SetPhone(%q): clearing must not be an error, got %v", raw, err)
		}
		if profile.Phone != nil {
			t.Fatalf("SetPhone(%q) kept %q, want the number cleared", raw, *profile.Phone)
		}
	}
}

func TestSetPhoneStartsFromNoNumber(t *testing.T) {
	profile := Profile{}

	if err := profile.SetPhone(""); err != nil {
		t.Fatalf("clearing an unset phone: %v", err)
	}
	if profile.Phone != nil {
		t.Fatalf("expected no phone, got %q", *profile.Phone)
	}
}

// A rejected phone must leave the stored number exactly as it was, otherwise a
// partial update would lose a valid value the customer did not touch (FR-003).
func TestSetPhoneRejectsAnInvalidNumberAndKeepsTheStoredOne(t *testing.T) {
	existing := "0912345678"
	profile := Profile{Phone: &existing}

	for _, raw := range []string{"12345", "09123456a8", "09123456789", "+8491234567", "0912 345"} {
		err := profile.SetPhone(raw)

		if !errors.Is(err, domainerr.ErrInvalidPhone) {
			t.Fatalf("SetPhone(%q): expected ErrInvalidPhone, got %v", raw, err)
		}
		if profile.Phone == nil || *profile.Phone != existing {
			t.Fatalf("SetPhone(%q) changed the stored number to %v, want it untouched", raw, profile.Phone)
		}
	}
}

// The four avatar columns are stored as one unit, so the entity accepts either
// no reference at all or a complete one. A half-filled reference would leave a
// profile the client cannot render (FR-016).
func TestSetAvatarRefusesAnIncompleteReference(t *testing.T) {
	complete := AvatarReference{PublicID: "users/a/avatar", URL: "https://cdn.test/a.png", Width: 512, Height: 512}

	cases := map[string]*AvatarReference{
		"no public id":     {URL: "https://cdn.test/a.png", Width: 512, Height: 512},
		"no url":           {PublicID: "users/a/avatar", Width: 512, Height: 512},
		"no width":         {PublicID: "users/a/avatar", URL: "https://cdn.test/a.png", Height: 512},
		"no height":        {PublicID: "users/a/avatar", URL: "https://cdn.test/a.png", Width: 512},
		"a zero width":     {PublicID: "users/a/avatar", URL: "https://cdn.test/a.png", Height: 512},
		"a negative size":  {PublicID: "users/a/avatar", URL: "https://cdn.test/a.png", Width: -1, Height: 512},
		"only a blank url": {PublicID: "users/a/avatar", URL: "  ", Width: 512, Height: 512},
	}

	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			profile := Profile{}
			if err := profile.SetAvatar(ref); !errors.Is(err, domainerr.ErrIncompleteAvatar) {
				t.Fatalf("expected ErrIncompleteAvatar, got %v", err)
			}
			if profile.Avatar != nil {
				t.Fatalf("a refused reference must not be attached, got %+v", profile.Avatar)
			}
		})
	}

	profile := Profile{}
	if err := profile.SetAvatar(&complete); err != nil {
		t.Fatalf("a complete reference must be accepted: %v", err)
	}
	if profile.Avatar == nil || profile.Avatar.PublicID != complete.PublicID {
		t.Fatalf("expected the reference to be attached, got %+v", profile.Avatar)
	}
}

func TestSetAvatarClearsEveryColumnTogether(t *testing.T) {
	profile := Profile{Avatar: &AvatarReference{
		PublicID: "users/a/avatar",
		URL:      "https://cdn.test/a.png",
		Width:    512,
		Height:   512,
	}}

	if err := profile.SetAvatar(nil); err != nil {
		t.Fatalf("clearing the avatar must not be an error, got %v", err)
	}
	if profile.Avatar != nil {
		t.Fatalf("expected no avatar, got %+v", profile.Avatar)
	}
}

func TestCompleteAvatarReportsWhetherEveryStoredValueIsPresent(t *testing.T) {
	complete := &AvatarReference{PublicID: "id", URL: "url", Width: 1, Height: 1}
	if !complete.IsComplete() {
		t.Fatal("a reference carrying every stored value must be complete")
	}
	if (&AvatarReference{PublicID: "id", URL: "url", Width: 1}).IsComplete() {
		t.Fatal("a reference without a height must not be complete")
	}
	var absent *AvatarReference
	if absent.IsComplete() {
		t.Fatal("no reference must not be complete")
	}
}
