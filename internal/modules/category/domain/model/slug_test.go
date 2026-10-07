package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
)

// The slug is the segment a public link is built from, so the accepted set is
// exactly lowercase unaccented letters, digits and single hyphens (FR-018).
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

// Every shape the operator could type that is not URL-safe must be refused, so a
// near-duplicate link can never be stored (FR-018, spec edge cases).
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
// whitespace. Note it does not collapse internal whitespace: that is research
// D11's deliberate narrow reading of FR-016.
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
// D3): Go folds Vietnamese uppercase letters using Unicode simple case mapping,
// which a collation-driven lower() under C would not. This test is the proof the
// spec demands and an ASCII-only test must not be counted as it (T033).
func TestFoldingKeyFoldsVietnameseUppercase(t *testing.T) {
	// Đ is U+0110; its lowercase is đ U+0111. It is not ASCII and is more than
	// one byte, so a byte-oriented or ASCII-only fold could not produce this.
	if got := FoldKey("Đàn bầu"); got != "đàn bầu" {
		t.Fatalf("FoldKey(%q) = %q, want %q", "Đàn bầu", got, "đàn bầu")
	}

	if got, want := FoldKey("ĐÀN BẦU"), FoldKey("đàn bầu"); got != want {
		t.Fatalf("a Vietnamese uppercase name must fold to the same key as its lowercase form: %q != %q", got, want)
	}

	// Establish that the letter really is non-ASCII and multi-byte, so the test
	// above could not pass under an ASCII-only fold.
	if len("Đ") == 1 {
		t.Fatalf("test setup: %q must be multi-byte", "Đ")
	}
}

// T033, FR-016, research D3: a name is refused when it differs from an existing
// one only by the case of a Vietnamese letter.
//
// The refusal is decided by the unique index on normalized_name, so proving the
// two names fold to one key proves the second is refused. The letter is
// Vietnamese — Đ (U+0110) whose lowercase is đ (U+0111) — and not an ASCII one,
// because that is the exact case a locale-driven fold passes: PostgreSQL's
// lower() under the C collation folds ASCII only, so "Đàn bầu" and "đàn bầu"
// would keep two different keys there, the index would accept both, and the
// catalogue would silently hold two names a human reads as the same. Go folds it
// with Unicode simple case mapping, which is why the fold lives in the
// application (research D3). The ASCII-only fold asserted below is there so this
// test cannot be satisfied by one.
func TestAVietnameseCaseOnlyDifferenceCollides(t *testing.T) {
	existing := "Đàn bầu"
	candidate := "đàn bầu"
	if existing == candidate {
		t.Fatal("test setup: the two names must differ")
	}

	if got, want := FoldKey(candidate), FoldKey(existing); got != want {
		t.Fatalf("a Vietnamese case-only difference must fold to one key: %q != %q", got, want)
	}

	// A locale- or ASCII-driven fold leaves the pair distinct, so this test is
	// not passable by one — the reason it uses a Vietnamese letter.
	if asciiOnlyFold(existing) == asciiOnlyFold(candidate) {
		t.Fatal("test setup: a locale-driven fold must NOT fold the Vietnamese pair, otherwise this test proves nothing about Unicode folding")
	}

	// The key a category stores is the fold of its name, so the two candidates
	// carry the same normalized_name and the unique index refuses the second.
	first, err := NewCategory(CategoryDraft{Name: existing, Slug: "dan-bau-mot"}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewCategory(%q): %v", existing, err)
	}
	second, err := NewCategory(CategoryDraft{Name: candidate, Slug: "dan-bau-hai"}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewCategory(%q): %v", candidate, err)
	}
	if first.NormalizedName != second.NormalizedName {
		t.Fatalf("the two names must collide on the stored key: %q != %q",
			first.NormalizedName, second.NormalizedName)
	}
}

// asciiOnlyFold is what a collation-driven fold gives under the C collation:
// it lowercases the ASCII range and leaves everything else untouched. It exists
// only so TestAVietnameseCaseOnlyDifferenceCollides can show the Vietnamese pair
// survives it.
func asciiOnlyFold(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}

// Research D11: internal runs of whitespace are deliberately not collapsed, so
// two names that look alike to a human can be two categories.
func TestFoldingKeyDoesNotCollapseInternalWhitespace(t *testing.T) {
	if FoldKey("Tranh  sơn  dầu") == FoldKey("Tranh sơn dầu") {
		t.Errorf("internal whitespace must not be collapsed; research D11 is explicit")
	}
}

// FR-019: the name bound counts characters, not bytes. A 120-character
// Vietnamese name is about 360 bytes, and a byte-counted bound would refuse it
// at a third of its allowance while every ASCII test still passed.
func TestNameBoundCountsVietnameseCharactersNotBytes(t *testing.T) {
	// "ệ" is U+1EC7, three bytes in UTF-8.
	name := strings.Repeat("ệ", MaxNameRunes)
	if len(name) <= MaxNameRunes {
		t.Fatalf("test setup: %d characters of %q must occupy more than %d bytes, got %d", MaxNameRunes, "ệ", MaxNameRunes, len(name))
	}
	if err := ValidateName(name); err != nil {
		t.Errorf("a name of exactly %d Vietnamese characters must be accepted: %v", MaxNameRunes, err)
	}

	over := name + "ệ"
	err := ValidateName(over)
	if !errors.Is(err, domainerr.ErrCategoryInvalid) {
		t.Fatalf("a name of %d characters must be refused with ErrCategoryInvalid, got %v", MaxNameRunes+1, err)
	}
	assertField(t, err, FieldName)
}

func TestSlugBoundIsOneHundredAndFortyCharacters(t *testing.T) {
	atLimit := strings.Repeat("a", MaxSlugRunes)
	if err := ValidateSlug(atLimit); err != nil {
		t.Errorf("a slug of exactly %d characters must be accepted: %v", MaxSlugRunes, err)
	}

	err := ValidateSlug(atLimit + "a")
	if !errors.Is(err, domainerr.ErrCategoryInvalid) {
		t.Fatalf("a slug of %d characters must be refused with ErrCategoryInvalid, got %v", MaxSlugRunes+1, err)
	}
	assertField(t, err, FieldSlug)
}

func TestDescriptionBoundIsTwoThousandCharacters(t *testing.T) {
	atLimit := strings.Repeat("ệ", MaxDescriptionRunes)
	if len(atLimit) <= MaxDescriptionRunes {
		t.Fatalf("test setup: the Vietnamese description must be longer in bytes than the bound")
	}
	if err := ValidateDescription(atLimit); err != nil {
		t.Errorf("a description of exactly %d characters must be accepted: %v", MaxDescriptionRunes, err)
	}

	err := ValidateDescription(atLimit + "ệ")
	if !errors.Is(err, domainerr.ErrCategoryInvalid) {
		t.Fatalf("a description of %d characters must be refused with ErrCategoryInvalid, got %v", MaxDescriptionRunes+1, err)
	}
	assertField(t, err, FieldDescription)
}

// Every field-level rejection must name the offending member so the operator can
// correct it in one step (FR-018, FR-019, FR-020).
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
			if !errors.Is(tc.err, domainerr.ErrCategoryInvalid) {
				t.Fatalf("expected ErrCategoryInvalid, got %v", tc.err)
			}
			assertField(t, tc.err, tc.field)
		})
	}
}

// assertField fails the test unless err carries a CategoryFieldError naming want.
func assertField(t *testing.T, err error, want string) {
	t.Helper()

	var fieldErr *domainerr.CategoryFieldError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("expected a *CategoryFieldError, got %T", err)
	}
	if fieldErr.Field != want {
		t.Errorf("rejection names field %q, want %q", fieldErr.Field, want)
	}
}
