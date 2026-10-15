package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// validGroupDraft is a draft that every rule accepts.
func validGroupDraft() VariantGroupDraft {
	return VariantGroupDraft{
		Name:        "Card Genshin",
		Slug:        "card-genshin",
		Description: "Bộ card sưu tầm.",
		CategoryID:  uuid.New(),
		Position:    2,
		OptionName:  "Card",
	}
}

// A newly created group carries a fresh identifier, the trimmed values and both
// timestamps (FR-009).
func TestNewVariantGroupBuildsAValidGroup(t *testing.T) {
	group, err := NewVariantGroup(validGroupDraft(), testNow)
	if err != nil {
		t.Fatalf("NewVariantGroup: unexpected error %v", err)
	}

	if group.ID == uuid.Nil {
		t.Errorf("a created group must receive an identifier")
	}
	if group.Name != "Card Genshin" {
		t.Errorf("Name = %q", group.Name)
	}
	if group.Slug != "card-genshin" {
		t.Errorf("Slug = %q", group.Slug)
	}
	if group.Description != "Bộ card sưu tầm." {
		t.Errorf("Description = %q", group.Description)
	}
	if group.OptionName != "Card" {
		t.Errorf("OptionName = %q", group.OptionName)
	}
	if group.Position != 2 {
		t.Errorf("Position = %d, want 2", group.Position)
	}
	if !group.CreatedAt.Equal(testNow) || !group.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps = %v / %v, want %v", group.CreatedAt, group.UpdatedAt, testNow)
	}
}

// The stored free-text values are the trimmed ones (FR-020).
func TestNewVariantGroupTrimsSurroundingWhitespace(t *testing.T) {
	draft := validGroupDraft()
	draft.Name = "  Card Genshin  "
	draft.Slug = "  card-genshin  "
	draft.Description = "  mô tả  "
	draft.OptionName = "  Card  "

	group, err := NewVariantGroup(draft, testNow)
	if err != nil {
		t.Fatalf("NewVariantGroup: unexpected error %v", err)
	}
	if group.Name != "Card Genshin" || group.Slug != "card-genshin" ||
		group.Description != "mô tả" || group.OptionName != "Card" {
		t.Errorf("values were not trimmed: %q / %q / %q / %q",
			group.Name, group.Slug, group.Description, group.OptionName)
	}
}

// An empty description is a valid description (spec edge case).
func TestNewVariantGroupAcceptsAnEmptyDescription(t *testing.T) {
	draft := validGroupDraft()
	draft.Description = ""

	group, err := NewVariantGroup(draft, testNow)
	if err != nil {
		t.Fatalf("NewVariantGroup with an empty description: unexpected error %v", err)
	}
	if group.Description != "" {
		t.Errorf("Description = %q, want empty", group.Description)
	}
}

// A rejected draft yields no group at all and names the member (FR-009, FR-019,
// FR-020).
func TestNewVariantGroupRejectsInvalidValuesAndNamesTheField(t *testing.T) {
	overLongName := strings.Repeat("ệ", MaxNameRunes+1)
	overLongSlug := strings.Repeat("a", MaxSlugRunes+1)
	overLongDescription := strings.Repeat("ệ", MaxDescriptionRunes+1)

	cases := []struct {
		name  string
		field string
		draft VariantGroupDraft
	}{
		{"blank name", FieldName, VariantGroupDraft{Name: "  ", Slug: "a", OptionName: "Card"}},
		{"blank option name", FieldOptionName, VariantGroupDraft{Name: "a", Slug: "a", OptionName: "  "}},
		{"accented slug", FieldSlug, VariantGroupDraft{Name: "a", Slug: "card-genshin-đ", OptionName: "Card"}},
		{"over-long name", FieldName, VariantGroupDraft{Name: overLongName, Slug: "a", OptionName: "Card"}},
		{"over-long slug", FieldSlug, VariantGroupDraft{Name: "a", Slug: overLongSlug, OptionName: "Card"}},
		{"over-long description", FieldDescription, VariantGroupDraft{Name: "a", Slug: "b", Description: overLongDescription, OptionName: "Card"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group, err := NewVariantGroup(tc.draft, testNow)
			if group != nil {
				t.Errorf("a rejected draft must yield no group, got %+v", group)
			}
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
		})
	}
}

// Apply changes the editable members and bumps updated_at, without touching the
// identifier or the creation timestamp (FR-011).
func TestVariantGroupApplyChangesTheEditableFieldsAndBumpsUpdatedAt(t *testing.T) {
	group := mustGroup(t)
	id := group.ID
	categoryID := uuid.New()
	later := testNow.Add(time.Hour)

	edit := VariantGroupEdit{
		Name:        ptr("Card One Piece"),
		Slug:        ptr("card-one-piece"),
		Description: ptr("Mô tả mới."),
		CategoryID:  &categoryID,
		Position:    ptr(9),
		OptionName:  ptr("Nhân vật"),
	}
	if err := group.Apply(edit, later); err != nil {
		t.Fatalf("Apply: unexpected error %v", err)
	}

	if group.Name != "Card One Piece" || group.Slug != "card-one-piece" ||
		group.Description != "Mô tả mới." || group.OptionName != "Nhân vật" {
		t.Errorf("editable text was not applied: %+v", group)
	}
	if group.CategoryID != categoryID {
		t.Errorf("CategoryID = %s, want %s", group.CategoryID, categoryID)
	}
	if group.Position != 9 {
		t.Errorf("Position = %d, want 9", group.Position)
	}
	if !group.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", group.UpdatedAt, later)
	}
	if !group.CreatedAt.Equal(testNow) {
		t.Errorf("an edit must not change CreatedAt, got %v", group.CreatedAt)
	}
	if group.ID != id {
		t.Errorf("the identifier changed on edit (FR-009)")
	}
}

// A member the edit does not name keeps its current value (partial update).
func TestVariantGroupApplyKeepsOmittedFields(t *testing.T) {
	group := mustGroup(t)
	before := *group

	if err := group.Apply(VariantGroupEdit{Position: ptr(42)}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if group.Name != before.Name || group.Slug != before.Slug ||
		group.Description != before.Description || group.OptionName != before.OptionName ||
		group.CategoryID != before.CategoryID {
		t.Errorf("an omitted member must keep its value: %+v vs %+v", group, before)
	}
	if group.Position != 42 {
		t.Errorf("Position = %d, want 42", group.Position)
	}
}

// A rejected edit leaves the entity exactly as it was (FR-020).
func TestVariantGroupApplyRejectsInvalidChangeAndLeavesEntityUntouched(t *testing.T) {
	group := mustGroup(t)
	before := *group

	err := group.Apply(VariantGroupEdit{Slug: ptr("Card Sơn")}, testNow.Add(time.Hour))
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldSlug)

	if *group != before {
		t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", group, before)
	}
}

// A blank choice-dimension name is refused on edit too, naming the member.
func TestVariantGroupApplyRefusesABlankOptionName(t *testing.T) {
	group := mustGroup(t)
	before := *group

	err := group.Apply(VariantGroupEdit{OptionName: ptr("   ")}, testNow.Add(time.Hour))
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldOptionName)

	if *group != before {
		t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", group, before)
	}
}

// mustGroup builds a group from validGroupDraft for tests that only need a
// starting entity.
func mustGroup(t *testing.T) *VariantGroup {
	t.Helper()

	group, err := NewVariantGroup(validGroupDraft(), testNow)
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	return group
}
