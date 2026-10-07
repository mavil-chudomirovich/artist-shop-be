package model

import (
	"errors"
	"strings"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// The slug is the segment a public link is built from, so the accepted set is
// exactly lowercase unaccented letters, digits and single hyphens (FR-029).
func TestSlugShapeAcceptsURLSafeSegments(t *testing.T) {
	valid := []string{
		"tranh-son-dau",
		"a",
		"a1",
		"abc123",
		"a-b-c",
		"1-2",
		"tranh-son-dau-2024",
		"x-y-z-0",
	}

	for _, slug := range valid {
		if !IsValidSlug(slug) {
			t.Errorf("IsValidSlug(%q) = false, want true", slug)
		}
	}
}

// Every shape the operator could type that is not URL-safe must be refused
// (FR-029, spec edge cases).
func TestSlugShapeRejectsAnythingThatIsNotURLSafe(t *testing.T) {
	invalid := map[string]string{
		"accented letters":  "tranh-sơn-dầu",
		"uppercase letters": "Tranh-Son-Dau",
		"spaces":            "tranh son dau",
		"leading hyphen":    "-tranh-son-dau",
		"trailing hyphen":   "tranh-son-dau-",
		"double hyphen":     "tranh--son-dau",
		"underscore":        "tranh_son",
		"dot":               "tranh.son",
		"slash":             "tranh/son",
		"empty":             "",
		"a single hyphen":   "-",
		"whitespace only":   " ",
	}

	for name, slug := range invalid {
		t.Run(name, func(t *testing.T) {
			if IsValidSlug(slug) {
				t.Errorf("IsValidSlug(%q) = true, want false", slug)
			}
		})
	}
}

// The folding key is what makes uniqueness ignore letter case and surrounding
// whitespace (FR-028, research D4).
func TestFoldingKeyIgnoresCaseAndSurroundingWhitespace(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"Tranh", "tranh"},
		{"tranh", "tranh"},
		{"TrAnH", "tranh"},
		{"  Tranh sơn dầu  ", "tranh sơn dầu"},
		{"Tranh sơn dầu", "tranh sơn dầu"},
	}

	for _, tc := range cases {
		if got := FoldKey(tc.raw); got != tc.want {
			t.Errorf("FoldKey(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// The reason the application owns the fold rather than the database (research
// D4): Go folds Vietnamese uppercase letters using Unicode simple case mapping,
// which a collation-driven lower() under C would not.
func TestFoldingKeyFoldsVietnameseUppercase(t *testing.T) {
	// Đ is U+0110; its lowercase is đ U+0111. It is not ASCII and is more than one
	// byte, so a byte-oriented or ASCII-only fold could not produce this.
	if got := FoldKey("Đàn bầu"); got != "đàn bầu" {
		t.Fatalf("FoldKey(%q) = %q, want %q", "Đàn bầu", got, "đàn bầu")
	}

	if got, want := FoldKey("ĐÀN BẦU"), FoldKey("đàn bầu"); got != want {
		t.Fatalf("a Vietnamese uppercase name must fold to the same key as its lowercase form: %q != %q", got, want)
	}

	if len("Đ") == 1 {
		t.Fatalf("test setup: %q must be multi-byte", "Đ")
	}
}

// The same Vietnamese case-only difference must fold to one key, so a second
// product cannot slip past the unique index (FR-028, research D4).
func TestAVietnameseCaseOnlyDifferenceCollides(t *testing.T) {
	existing := "Đàn bầu"
	candidate := "đàn bầu"
	if existing == candidate {
		t.Fatal("test setup: the two names must differ")
	}

	if got, want := FoldKey(candidate), FoldKey(existing); got != want {
		t.Fatalf("a Vietnamese case-only difference must fold to one key: %q != %q", got, want)
	}

	if asciiOnlyFold(existing) == asciiOnlyFold(candidate) {
		t.Fatal("test setup: a locale-driven fold must NOT fold the Vietnamese pair")
	}
}

// asciiOnlyFold is what a collation-driven fold gives under the C collation: it
// lowercases the ASCII range and leaves everything else untouched. It exists only
// so TestAVietnameseCaseOnlyDifferenceCollides can show the Vietnamese pair
// survives it.
func asciiOnlyFold(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}

// Research D4: internal runs of whitespace are deliberately not collapsed, so two
// names that look alike to a human can be two products.
func TestFoldingKeyDoesNotCollapseInternalWhitespace(t *testing.T) {
	if FoldKey("Tranh  sơn  dầu") == FoldKey("Tranh sơn dầu") {
		t.Errorf("internal whitespace must not be collapsed")
	}
}

// FR-030: the name bound counts characters, not bytes. A 120-character Vietnamese
// name is about 360 bytes.
func TestNameBoundCountsVietnameseCharactersNotBytes(t *testing.T) {
	name := strings.Repeat("ệ", MaxNameRunes)
	if len(name) <= MaxNameRunes {
		t.Fatalf("test setup: %d characters of %q must occupy more than %d bytes, got %d", MaxNameRunes, "ệ", MaxNameRunes, len(name))
	}
	if err := ValidateName(name); err != nil {
		t.Errorf("a name of exactly %d Vietnamese characters must be accepted: %v", MaxNameRunes, err)
	}

	err := ValidateName(name + "ệ")
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("a name of %d characters must be refused with ErrProductInvalid, got %v", MaxNameRunes+1, err)
	}
	assertField(t, err, FieldName)
}

// FR-030: the slug bound is 140 characters.
func TestSlugBoundIsOneHundredAndFortyCharacters(t *testing.T) {
	atLimit := strings.Repeat("a", MaxSlugRunes)
	if err := ValidateSlug(atLimit); err != nil {
		t.Errorf("a slug of exactly %d characters must be accepted: %v", MaxSlugRunes, err)
	}

	err := ValidateSlug(atLimit + "a")
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("a slug of %d characters must be refused with ErrProductInvalid, got %v", MaxSlugRunes+1, err)
	}
	assertField(t, err, FieldSlug)
}

// FR-030: the description bound is 5000 characters, counted in characters.
func TestDescriptionBoundIsFiveThousandCharacters(t *testing.T) {
	atLimit := strings.Repeat("ệ", MaxDescriptionRunes)
	if len(atLimit) <= MaxDescriptionRunes {
		t.Fatalf("test setup: the Vietnamese description must be longer in bytes than the bound")
	}
	if err := ValidateDescription(atLimit); err != nil {
		t.Errorf("a description of exactly %d characters must be accepted: %v", MaxDescriptionRunes, err)
	}

	err := ValidateDescription(atLimit + "ệ")
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("a description of %d characters must be refused with ErrProductInvalid, got %v", MaxDescriptionRunes+1, err)
	}
	assertField(t, err, FieldDescription)
}

// Every field-level rejection must name the offending member (FR-028, FR-029,
// FR-030).
func TestValidationNamesTheOffendingField(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		field string
	}{
		{"blank name", ValidateName(""), FieldName},
		{"over-long name", ValidateName(strings.Repeat("a", MaxNameRunes+1)), FieldName},
		{"blank slug", ValidateSlug(""), FieldSlug},
		{"accented slug", ValidateSlug("tranh-sơn"), FieldSlug},
		{"uppercase slug", ValidateSlug("Tranh"), FieldSlug},
		{"trailing-hyphen slug", ValidateSlug("tranh-"), FieldSlug},
		{"double-hyphen slug", ValidateSlug("tranh--son"), FieldSlug},
		{"over-long slug", ValidateSlug(strings.Repeat("a", MaxSlugRunes+1)), FieldSlug},
		{"over-long description", ValidateDescription(strings.Repeat("a", MaxDescriptionRunes+1)), FieldDescription},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", tc.err)
			}
			assertField(t, tc.err, tc.field)
		})
	}
}
