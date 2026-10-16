package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// testNow is a fixed instant so timestamps can be asserted exactly.
var testNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// ptr returns a pointer to v, for building ProductEdit and VariantEdit members.
func ptr[T any](v T) *T { return &v }

// validDraft is a draft that every rule accepts: a product always carries at least
// one variant (FR-012).
func validDraft() ProductDraft {
	return ProductDraft{
		Name:        "Tranh sơn dầu",
		Code:        "ts-001",
		Description: "Tranh vẽ tay trên vải.",
		Price:       Price{Amount: 1999, Currency: "VND"},
		CategoryID:  uuid.New(),
		Position:    3,
		Variants: []VariantDraft{
			{Name: "Bản tiêu chuẩn", SKU: "TS-001-STD", Position: 0},
		},
	}
}

// A newly created product carries a fresh identifier, the operator-written code and
// the trimmed values; it has no sell state of its own (FR-011, research D2).
func TestNewProductCarriesTheCodeAndNoSellState(t *testing.T) {
	product, err := NewProduct(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewProduct: unexpected error %v", err)
	}

	if product.ID == uuid.Nil {
		t.Errorf("a created product must receive an identifier")
	}
	if product.Name != "Tranh sơn dầu" {
		t.Errorf("Name = %q", product.Name)
	}
	if product.Code != "ts-001" {
		t.Errorf("Code = %q, want ts-001", product.Code)
	}
	if product.Description != "Tranh vẽ tay trên vải." {
		t.Errorf("Description = %q", product.Description)
	}
	if product.Price.Amount != 1999 || product.Price.Currency != "VND" {
		t.Errorf("Price = %+v, want 1999 VND", product.Price)
	}
	if product.Position != 3 {
		t.Errorf("Position = %d, want 3", product.Position)
	}
	if !product.CreatedAt.Equal(testNow) || !product.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps = %v / %v, want %v", product.CreatedAt, product.UpdatedAt, testNow)
	}
}

// The stored values are the trimmed ones (FR-003, FR-011).
func TestNewProductTrimsSurroundingWhitespace(t *testing.T) {
	draft := validDraft()
	draft.Name = "  Tranh sơn dầu  "
	draft.Code = "  ts-001  "
	draft.Description = "  mô tả  "

	product, err := NewProduct(draft, testNow)
	if err != nil {
		t.Fatalf("NewProduct: unexpected error %v", err)
	}
	if product.Name != "Tranh sơn dầu" || product.Code != "ts-001" ||
		product.Description != "mô tả" {
		t.Errorf("values were not trimmed: %q / %q / %q",
			product.Name, product.Code, product.Description)
	}
}

// A rejected draft yields no product at all and names the member (FR-009, FR-011,
// FR-024).
func TestNewProductRejectsInvalidValuesAndNamesTheField(t *testing.T) {
	overLongName := strings.Repeat("ệ", MaxNameRunes+1)
	overLongCode := strings.Repeat("a", MaxCodeRunes+1)
	overLongDescription := strings.Repeat("ệ", MaxDescriptionRunes+1)

	cases := []struct {
		name  string
		field string
		draft ProductDraft
	}{
		{"blank name", FieldName, ProductDraft{Name: "  ", Code: "a", Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"blank code", FieldCode, ProductDraft{Name: "a", Code: "  ", Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"accented code", FieldCode, ProductDraft{Name: "a", Code: "tranh-sơn", Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"over-long name", FieldName, ProductDraft{Name: overLongName, Code: "a", Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"over-long code", FieldCode, ProductDraft{Name: "a", Code: overLongCode, Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"over-long description", FieldDescription, ProductDraft{Name: "a", Code: "b", Description: overLongDescription, Price: Price{Amount: 1, Currency: "VND"}, Variants: validDraft().Variants}},
		{"zero price", FieldPrice, ProductDraft{Name: "a", Code: "c", Price: Price{Amount: 0, Currency: "VND"}, Variants: validDraft().Variants}},
		{"negative price", FieldPrice, ProductDraft{Name: "a", Code: "d", Price: Price{Amount: -5, Currency: "VND"}, Variants: validDraft().Variants}},
		{"bad currency", FieldCurrency, ProductDraft{Name: "a", Code: "e", Price: Price{Amount: 1, Currency: "vnd"}, Variants: validDraft().Variants}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			product, err := NewProduct(tc.draft, testNow)
			if product != nil {
				t.Errorf("a rejected draft must yield no product, got %+v", product)
			}
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
		})
	}
}

// FR-012: a product is created with at least one variant, so a draft with none is
// refused naming the variants member (research D14).
func TestNewProductRefusesAnEmptyVariantList(t *testing.T) {
	draft := validDraft()
	draft.Variants = nil

	product, err := NewProduct(draft, testNow)
	if product != nil {
		t.Errorf("a product with no variant must not be created, got %+v", product)
	}
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldVariants)
}

// An empty description is a valid description (spec edge case).
func TestNewProductAcceptsAnEmptyDescription(t *testing.T) {
	draft := validDraft()
	draft.Description = ""

	product, err := NewProduct(draft, testNow)
	if err != nil {
		t.Fatalf("NewProduct with an empty description: unexpected error %v", err)
	}
	if product.Description != "" {
		t.Errorf("Description = %q, want empty", product.Description)
	}
}

// Apply changes the editable members, recomputes the folding key and bumps
// updated_at, without touching the identifier (FR-009, FR-010).
func TestApplyChangesTheEditableFieldsAndBumpsUpdatedAt(t *testing.T) {
	product := mustProduct(t)
	id := product.ID
	categoryID := uuid.New()
	later := testNow.Add(time.Hour)

	edit := ProductEdit{
		Name:        ptr("Tranh khắc gỗ"),
		Code:        ptr("tk-002"),
		Description: ptr("Mô tả mới."),
		Price:       ptr(Price{Amount: 250, Currency: "USD"}),
		CategoryID:  &categoryID,
		Position:    ptr(9),
	}
	if err := product.Apply(edit, later); err != nil {
		t.Fatalf("Apply: unexpected error %v", err)
	}

	if product.Name != "Tranh khắc gỗ" || product.Code != "tk-002" ||
		product.Description != "Mô tả mới." {
		t.Errorf("editable text was not applied: %+v", product)
	}
	if product.Price.Amount != 250 || product.Price.Currency != "USD" {
		t.Errorf("Price = %+v, want 250 USD", product.Price)
	}
	if product.CategoryID != categoryID {
		t.Errorf("CategoryID = %s, want %s", product.CategoryID, categoryID)
	}
	if product.Position != 9 {
		t.Errorf("Position = %d, want 9", product.Position)
	}
	if !product.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", product.UpdatedAt, later)
	}
	if product.ID != id {
		t.Errorf("the identifier changed on edit (FR-009)")
	}
}

// A member the edit does not name keeps its current value (partial update).
func TestApplyKeepsOmittedFields(t *testing.T) {
	product := mustProduct(t)
	before := *product

	if err := product.Apply(ProductEdit{Position: ptr(42)}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if product.Name != before.Name || product.Code != before.Code ||
		product.Description != before.Description || product.CategoryID != before.CategoryID {
		t.Errorf("an omitted member must keep its value: %+v vs %+v", product, before)
	}
	if product.Position != 42 {
		t.Errorf("Position = %d, want 42", product.Position)
	}
}

// A rejected edit leaves the entity exactly as it was (FR-009, FR-011).
func TestApplyRejectsInvalidChangeAndLeavesEntityUntouched(t *testing.T) {
	cases := []struct {
		name  string
		field string
		edit  ProductEdit
	}{
		{"invalid code", FieldCode, ProductEdit{Code: ptr("Tranh Sơn")}},
		{"blank code", FieldCode, ProductEdit{Code: ptr("  ")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			product := mustProduct(t)
			before := *product

			err := product.Apply(tc.edit, testNow.Add(time.Hour))
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
			if *product != before {
				t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", product, before)
			}
		})
	}
}

// assertField fails the test unless err carries a ProductFieldError naming want.
func assertField(t *testing.T, err error, want string) {
	t.Helper()

	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("expected a *ProductFieldError, got %T", err)
	}
	if fieldErr.Field != want {
		t.Errorf("rejection names field %q, want %q", fieldErr.Field, want)
	}
}

// mustProduct builds a product from validDraft for tests that only need a starting
// entity.
func mustProduct(t *testing.T) *Product {
	t.Helper()

	product, err := NewProduct(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	return product
}
