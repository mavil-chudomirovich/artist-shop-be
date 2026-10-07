package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
)

// testNow is a fixed instant so timestamps can be asserted exactly.
var testNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// ptr returns a pointer to v, for building CategoryEdit members.
func ptr[T any](v T) *T { return &v }

// validDraft is a draft that every rule accepts.
func validDraft() CategoryDraft {
	return CategoryDraft{
		Name:        "Tranh sơn dầu",
		Slug:        "tranh-son-dau",
		Description: "Tranh vẽ tay trên vải.",
		Position:    3,
	}
}

// A newly created category is on display and carries a fresh identifier
// (FR-008, FR-015).
func TestNewCategoryStartsOnDisplay(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: unexpected error %v", err)
	}

	if category.ID == uuid.Nil {
		t.Errorf("a created category must receive an identifier")
	}
	if !category.IsVisible {
		t.Errorf("a created category must start on display (FR-008)")
	}
	if category.Name != "Tranh sơn dầu" {
		t.Errorf("Name = %q, want %q", category.Name, "Tranh sơn dầu")
	}
	if category.Slug != "tranh-son-dau" {
		t.Errorf("Slug = %q, want %q", category.Slug, "tranh-son-dau")
	}
	if category.Description != "Tranh vẽ tay trên vải." {
		t.Errorf("Description = %q", category.Description)
	}
	if category.Position != 3 {
		t.Errorf("Position = %d, want 3", category.Position)
	}
	if !category.CreatedAt.Equal(testNow) || !category.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps = %v / %v, want %v", category.CreatedAt, category.UpdatedAt, testNow)
	}
	assertFoldingKeys(t, category)
}

// The stored value is the trimmed one, which is what the uniqueness comparison
// and the length bound operate on (FR-016, FR-019).
func TestNewCategoryTrimsSurroundingWhitespace(t *testing.T) {
	draft := CategoryDraft{
		Name:        "  Tranh sơn dầu  ",
		Slug:        "  tranh-son-dau  ",
		Description: "  mô tả  ",
		Position:    0,
	}

	category, err := NewCategory(draft, testNow)
	if err != nil {
		t.Fatalf("NewCategory: unexpected error %v", err)
	}
	if category.Name != "Tranh sơn dầu" {
		t.Errorf("Name = %q, want the trimmed name", category.Name)
	}
	if category.Slug != "tranh-son-dau" {
		t.Errorf("Slug = %q, want the trimmed slug", category.Slug)
	}
	if category.Description != "mô tả" {
		t.Errorf("Description = %q, want the trimmed description", category.Description)
	}
	assertFoldingKeys(t, category)
}

// A rejected draft yields no category at all, so a caller cannot persist half of
// one, and the rejection names the field (FR-018, FR-019, FR-020).
func TestNewCategoryRejectsInvalidValuesAndNamesTheField(t *testing.T) {
	cases := []struct {
		name  string
		field string
		draft CategoryDraft
	}{
		{"blank name", FieldName, CategoryDraft{Name: "  ", Slug: "a"}},
		{"blank slug", FieldSlug, CategoryDraft{Name: "a", Slug: "  "}},
		{"accented slug", FieldSlug, CategoryDraft{Name: "a", Slug: "tranh-sơn"}},
		{"uppercase slug", FieldSlug, CategoryDraft{Name: "a", Slug: "Tranh"}},
		{"space in slug", FieldSlug, CategoryDraft{Name: "a", Slug: "tranh son"}},
		{"leading hyphen", FieldSlug, CategoryDraft{Name: "a", Slug: "-tranh"}},
		{"trailing hyphen", FieldSlug, CategoryDraft{Name: "a", Slug: "tranh-"}},
		{"double hyphen", FieldSlug, CategoryDraft{Name: "a", Slug: "tranh--son"}},
		{"over-long name", FieldName, CategoryDraft{Name: strings.Repeat("ệ", MaxNameRunes+1), Slug: "a"}},
		{"over-long slug", FieldSlug, CategoryDraft{Name: "a", Slug: strings.Repeat("a", MaxSlugRunes+1)}},
		{"over-long description", FieldDescription, CategoryDraft{Name: "a", Slug: "a", Description: strings.Repeat("ệ", MaxDescriptionRunes+1)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			category, err := NewCategory(tc.draft, testNow)
			if category != nil {
				t.Errorf("a rejected draft must yield no category, got %+v", category)
			}
			if !errors.Is(err, domainerr.ErrCategoryInvalid) {
				t.Fatalf("expected ErrCategoryInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
		})
	}
}

// An empty description is a valid description, not an incomplete category
// (spec edge case).
func TestNewCategoryAcceptsAnEmptyDescription(t *testing.T) {
	draft := validDraft()
	draft.Description = ""

	category, err := NewCategory(draft, testNow)
	if err != nil {
		t.Fatalf("NewCategory with an empty description: unexpected error %v", err)
	}
	if category.Description != "" {
		t.Errorf("Description = %q, want empty", category.Description)
	}
}

// Apply changes the four editable members, recomputes the folding keys and bumps
// updated_at, without touching the identifier or the display state (FR-010).
func TestApplyChangesTheEditableFieldsAndBumpsUpdatedAt(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	id := category.ID
	later := testNow.Add(time.Hour)

	edit := CategoryEdit{
		Name:        ptr("Tranh khắc gỗ"),
		Slug:        ptr("tranh-khac-go"),
		Description: ptr("Mô tả mới."),
		Position:    ptr(9),
	}
	if err := category.Apply(edit, later); err != nil {
		t.Fatalf("Apply: unexpected error %v", err)
	}

	if category.Name != "Tranh khắc gỗ" {
		t.Errorf("Name = %q", category.Name)
	}
	if category.Slug != "tranh-khac-go" {
		t.Errorf("Slug = %q", category.Slug)
	}
	if category.Description != "Mô tả mới." {
		t.Errorf("Description = %q", category.Description)
	}
	if category.Position != 9 {
		t.Errorf("Position = %d, want 9", category.Position)
	}
	if !category.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", category.UpdatedAt, later)
	}
	if !category.CreatedAt.Equal(testNow) {
		t.Errorf("CreatedAt = %v, want it unchanged at %v", category.CreatedAt, testNow)
	}
	if category.ID != id {
		t.Errorf("the identifier changed on edit (FR-015, FR-023)")
	}
	if !category.IsVisible {
		t.Errorf("an edit must not change the display state")
	}
	assertFoldingKeys(t, category)
}

// A member the edit does not name keeps its current value (partial update).
func TestApplyKeepsOmittedFields(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	before := *category

	if err := category.Apply(CategoryEdit{Position: ptr(42)}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if category.Name != before.Name || category.Slug != before.Slug || category.Description != before.Description {
		t.Errorf("an omitted member must keep its value: %+v vs %+v", category, before)
	}
	if category.Position != 42 {
		t.Errorf("Position = %d, want 42", category.Position)
	}
}

// A rejected edit leaves the entity exactly as it was (FR-019, FR-020).
func TestApplyRejectsInvalidChangeAndLeavesEntityUntouched(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	before := *category

	err = category.Apply(CategoryEdit{Slug: ptr("Tranh Sơn")}, testNow.Add(time.Hour))
	if !errors.Is(err, domainerr.ErrCategoryInvalid) {
		t.Fatalf("expected ErrCategoryInvalid, got %v", err)
	}
	assertField(t, err, FieldSlug)

	if *category != before {
		t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", category, before)
	}
}

// An edit never resurrects a hidden category: display state moves only through
// Hide and Show (FR-011, research D9).
func TestApplyNeverChangesVisibility(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if !category.Hide(testNow.Add(time.Minute)) {
		t.Fatalf("hide must report a change")
	}

	if err := category.Apply(CategoryEdit{Name: ptr("Tên khác")}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if category.IsVisible {
		t.Errorf("an edit must not put a hidden category back on display")
	}
}

// Hide and Show are idempotent: a second call is a no-op, not an error, and does
// not touch updated_at (data-model.md transitions).
func TestHideAndShowAreIdempotentAndTouchUpdatedAtOnlyOnChange(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}

	hideAt := testNow.Add(time.Minute)
	if !category.Hide(hideAt) {
		t.Fatalf("the first hide must report a change")
	}
	if category.IsVisible {
		t.Fatalf("hide must take the category off display")
	}
	if !category.UpdatedAt.Equal(hideAt) {
		t.Errorf("UpdatedAt = %v, want %v", category.UpdatedAt, hideAt)
	}
	if category.Hide(hideAt.Add(time.Minute)) {
		t.Errorf("hiding an already hidden category must be a no-op")
	}
	if !category.UpdatedAt.Equal(hideAt) {
		t.Errorf("a no-op must not touch UpdatedAt, got %v", category.UpdatedAt)
	}

	showAt := hideAt.Add(time.Hour)
	if !category.Show(showAt) {
		t.Fatalf("show on a hidden category must report a change")
	}
	if !category.IsVisible {
		t.Fatalf("show must put the category back on display")
	}
	if !category.UpdatedAt.Equal(showAt) {
		t.Errorf("UpdatedAt = %v, want %v", category.UpdatedAt, showAt)
	}
	if category.Show(showAt.Add(time.Minute)) {
		t.Errorf("showing an already visible category must be a no-op")
	}
	if !category.UpdatedAt.Equal(showAt) {
		t.Errorf("a no-op must not touch UpdatedAt, got %v", category.UpdatedAt)
	}
}

// The folding keys are derived from the stored values, so they stay consistent
// through every transition — the invariant that keeps the uniqueness comparison
// honest (FR-016, FR-017).
func TestNormalisedKeysStayConsistentWithTheValues(t *testing.T) {
	category, err := NewCategory(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	assertFoldingKeys(t, category)

	if err := category.Apply(CategoryEdit{Name: ptr("Đàn bầu"), Slug: ptr("dan-bau")}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertFoldingKeys(t, category)
}

// assertFoldingKeys fails the test unless each folding key is FoldKey of its
// stored value.
func assertFoldingKeys(t *testing.T, category *Category) {
	t.Helper()

	if category.NormalizedName != FoldKey(category.Name) {
		t.Errorf("NormalizedName = %q, want FoldKey(%q) = %q", category.NormalizedName, category.Name, FoldKey(category.Name))
	}
	if category.NormalizedSlug != FoldKey(category.Slug) {
		t.Errorf("NormalizedSlug = %q, want FoldKey(%q) = %q", category.NormalizedSlug, category.Slug, FoldKey(category.Slug))
	}
}
