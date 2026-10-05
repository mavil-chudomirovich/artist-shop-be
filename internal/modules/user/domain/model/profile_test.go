package model

import (
	"errors"
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
		profile.SetDisplayName(raw)
		if profile.DisplayName != want {
			t.Errorf("SetDisplayName(%q) stored %q, want %q", raw, profile.DisplayName, want)
		}
	}
}

// Clearing the display name is a valid outcome, not a rejection: the customer
// may deliberately remove it and the profile stays valid (FR-004).
func TestSetDisplayNameStoresAnEmptyName(t *testing.T) {
	profile := Profile{DisplayName: "Nguyen Van A"}

	profile.SetDisplayName("   ")

	if profile.DisplayName != "" {
		t.Fatalf("expected the display name to be cleared, got %q", profile.DisplayName)
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
