package model

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// SlugPattern is the URL-safe shape of a product slug: lowercase unaccented
// letters, digits and single hyphens, with no leading or trailing hyphen
// (FR-029).
//
// It is the one home of the rule; the database check in
// migrations/00006_product.sql and specs/006-product-catalog/contracts/openapi.yaml
// carry the same pattern, and the three must keep agreeing. A product slug follows
// exactly the rule a category slug follows (research D4), so the operator learns
// the shape once.
const SlugPattern = `^[a-z0-9]+(-[a-z0-9]+)*$`

// slugPattern compiles SlugPattern once, at package init, so validation does not
// recompile it on every call.
var slugPattern = regexp.MustCompile(SlugPattern)

// Character bounds of the three operator-written values (FR-030).
//
// They count characters (runes), not bytes: a 120-character Vietnamese name is up
// to about 360 bytes, so a byte-counted bound would refuse it at roughly a third of
// its allowance. The database's length() counts characters too, which is what the
// matching checks rely on. Name and slug take the same 120/140 bounds module 03
// uses; the description is wider because a product description carries material,
// dimensions and condition.
const (
	// MaxNameRunes is the ceiling on a product name.
	MaxNameRunes = 120
	// MaxSlugRunes is the ceiling on a product slug.
	MaxSlugRunes = 140
	// MaxDescriptionRunes is the ceiling on a product description.
	MaxDescriptionRunes = 5000
)

// Explanations attached to a field-level rejection. They are short and
// non-sensitive: the response puts the member name beside one of these and the
// operator acts on it.
const (
	requiredIssue  = "is required"
	slugShapeIssue = "must be lowercase letters, digits and single hyphens, with no leading or trailing hyphen"
)

// limitIssue explains a rejection caused by a character bound.
func limitIssue(limit int) string {
	return fmt.Sprintf("must be at most %d characters", limit)
}

// IsValidSlug reports whether slug matches the URL-safe shape. The value is
// expected to be already trimmed; the shape rule itself does not trim, so a
// surrounding space is a rejection rather than something quietly discarded.
func IsValidSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

// FoldKey returns the folding key a value is compared by for catalogue-wide
// uniqueness (FR-028).
//
// It trims the surrounding whitespace and case-folds with strings.ToLower, which
// applies Unicode simple case mapping rather than a locale. That is the whole point
// of computing the key in the application (research D4): PostgreSQL's lower()
// follows the database collation and under the C collation folds ASCII only, so a
// Vietnamese uppercase letter such as Đ (U+0110) would not fold and the catalogue
// would silently accept two slugs differing only by its case.
//
// It deliberately does not collapse internal runs of whitespace: FR-028 scopes
// sameness to letter case and *surrounding* whitespace.
func FoldKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ValidateName refuses a blank name or one over its character bound, naming the
// member so the client can fix the exact input (FR-030).
func ValidateName(name string) error {
	if name == "" {
		return domainerr.InvalidProductField(FieldName, requiredIssue)
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return domainerr.InvalidProductField(FieldName, limitIssue(MaxNameRunes))
	}
	return nil
}

// ValidateSlug refuses a blank slug, one over its character bound, or one that is
// not URL-safe, naming the member (FR-029, FR-030).
//
// The bound is checked before the shape so a URL-safe slug that is merely too long
// is reported as a length problem; a slug that fails both is reported by whichever
// rule it trips first, and either answer names the same member.
func ValidateSlug(slug string) error {
	if slug == "" {
		return domainerr.InvalidProductField(FieldSlug, requiredIssue)
	}
	if utf8.RuneCountInString(slug) > MaxSlugRunes {
		return domainerr.InvalidProductField(FieldSlug, limitIssue(MaxSlugRunes))
	}
	if !IsValidSlug(slug) {
		return domainerr.InvalidProductField(FieldSlug, slugShapeIssue)
	}
	return nil
}

// ValidateDescription refuses a description over its character bound, naming the
// member (FR-030). An empty description is valid, so it is not refused for being
// incomplete.
func ValidateDescription(description string) error {
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return domainerr.InvalidProductField(FieldDescription, limitIssue(MaxDescriptionRunes))
	}
	return nil
}
