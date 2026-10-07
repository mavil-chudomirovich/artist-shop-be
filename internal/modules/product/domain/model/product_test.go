package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// testNow is a fixed instant so timestamps can be asserted exactly.
var testNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// ptr returns a pointer to v, for building ProductEdit members.
func ptr[T any](v T) *T { return &v }

// validDraft is a draft that every rule accepts.
func validDraft() ProductDraft {
	return ProductDraft{
		Name:        "Tranh sơn dầu",
		Slug:        "tranh-son-dau",
		Description: "Tranh vẽ tay trên vải.",
		Price:       Price{Amount: 1999, Currency: "VND"},
		CategoryID:  uuid.New(),
		Position:    3,
	}
}

// A newly created product is in COMING_SOON, carries a fresh identifier and is
// trimmed (FR-021, research D8).
func TestNewProductStartsInComingSoon(t *testing.T) {
	product, err := NewProduct(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewProduct: unexpected error %v", err)
	}

	if product.ID == uuid.Nil {
		t.Errorf("a created product must receive an identifier")
	}
	if product.SellState != constant.SellStateComingSoon {
		t.Errorf("SellState = %q, want %q", product.SellState, constant.SellStateComingSoon)
	}
	if product.Name != "Tranh sơn dầu" {
		t.Errorf("Name = %q", product.Name)
	}
	if product.Slug != "tranh-son-dau" {
		t.Errorf("Slug = %q", product.Slug)
	}
	if product.NormalizedSlug != FoldKey(product.Slug) {
		t.Errorf("NormalizedSlug = %q, want FoldKey(%q)", product.NormalizedSlug, product.Slug)
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

// The stored value is the trimmed one (FR-030).
func TestNewProductTrimsSurroundingWhitespace(t *testing.T) {
	draft := validDraft()
	draft.Name = "  Tranh sơn dầu  "
	draft.Slug = "  tranh-son-dau  "
	draft.Description = "  mô tả  "

	product, err := NewProduct(draft, testNow)
	if err != nil {
		t.Fatalf("NewProduct: unexpected error %v", err)
	}
	if product.Name != "Tranh sơn dầu" || product.Slug != "tranh-son-dau" || product.Description != "mô tả" {
		t.Errorf("values were not trimmed: %q / %q / %q", product.Name, product.Slug, product.Description)
	}
}

// A rejected draft yields no product at all and names the member (FR-028,
// FR-029, FR-030, FR-032).
func TestNewProductRejectsInvalidValuesAndNamesTheField(t *testing.T) {
	overLongName := strings.Repeat("ệ", MaxNameRunes+1)
	overLongSlug := strings.Repeat("a", MaxSlugRunes+1)
	overLongDescription := strings.Repeat("ệ", MaxDescriptionRunes+1)

	cases := []struct {
		name  string
		field string
		draft ProductDraft
	}{
		{"blank name", FieldName, ProductDraft{Name: "  ", Slug: "a", Price: Price{Amount: 1, Currency: "VND"}}},
		{"blank slug", FieldSlug, ProductDraft{Name: "a", Slug: "  ", Price: Price{Amount: 1, Currency: "VND"}}},
		{"accented slug", FieldSlug, ProductDraft{Name: "a", Slug: "tranh-sơn", Price: Price{Amount: 1, Currency: "VND"}}},
		{"over-long name", FieldName, ProductDraft{Name: overLongName, Slug: "a", Price: Price{Amount: 1, Currency: "VND"}}},
		{"over-long slug", FieldSlug, ProductDraft{Name: "a", Slug: overLongSlug, Price: Price{Amount: 1, Currency: "VND"}}},
		{"over-long description", FieldDescription, ProductDraft{Name: "a", Slug: "b", Description: overLongDescription, Price: Price{Amount: 1, Currency: "VND"}}},
		{"zero price", FieldPrice, ProductDraft{Name: "a", Slug: "c", Price: Price{Amount: 0, Currency: "VND"}}},
		{"negative price", FieldPrice, ProductDraft{Name: "a", Slug: "d", Price: Price{Amount: -5, Currency: "VND"}}},
		{"bad currency", FieldCurrency, ProductDraft{Name: "a", Slug: "e", Price: Price{Amount: 1, Currency: "vnd"}}},
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

// FR-021: COMING_SOON → ACTIVE.
func TestLaunchMovesComingSoonToActive(t *testing.T) {
	product := mustProduct(t)
	later := testNow.Add(time.Minute)

	if err := product.Launch(later); err != nil {
		t.Fatalf("Launch: unexpected error %v", err)
	}
	if product.SellState != constant.SellStateActive {
		t.Errorf("SellState = %q, want %q", product.SellState, constant.SellStateActive)
	}
	if !product.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", product.UpdatedAt, later)
	}
}

// Launch refuses any state but COMING_SOON and names the current one (FR-024).
func TestLaunchRefusesWhenNotComingSoon(t *testing.T) {
	product := mustProduct(t)
	if err := product.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	err := product.Launch(testNow.Add(time.Minute))
	if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
		t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
	}
	assertCurrentState(t, err, constant.SellStateActive)
	if product.SellState != constant.SellStateActive {
		t.Errorf("a refused transition must leave the product untouched, got %q", product.SellState)
	}
}

// FR-021: ACTIVE → OUT_OF_STOCK.
func TestSellOutMovesActiveToOutOfStock(t *testing.T) {
	product := mustProduct(t)
	if err := product.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if err := product.SellOut(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("SellOut: unexpected error %v", err)
	}
	if product.SellState != constant.SellStateOutOfStock {
		t.Errorf("SellState = %q, want %q", product.SellState, constant.SellStateOutOfStock)
	}
}

// SellOut refuses a product that is not on sale.
func TestSellOutRefusesWhenNotActive(t *testing.T) {
	product := mustProduct(t)

	err := product.SellOut(testNow)
	if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
		t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
	}
	assertCurrentState(t, err, constant.SellStateComingSoon)
}

// FR-021: OUT_OF_STOCK → ACTIVE (the restock direction research D8 adds).
func TestRestockReturnsOutOfStockToActive(t *testing.T) {
	product := mustProduct(t)
	if err := product.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := product.SellOut(testNow); err != nil {
		t.Fatalf("SellOut: %v", err)
	}

	if err := product.Restock(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Restock: unexpected error %v", err)
	}
	if product.SellState != constant.SellStateActive {
		t.Errorf("SellState = %q, want %q", product.SellState, constant.SellStateActive)
	}
}

// FR-021: any of the three non-terminal states → DISCONTINUED.
func TestRetireMovesAnyNonTerminalStateToDiscontinued(t *testing.T) {
	setups := []struct {
		name  string
		want  constant.SellState
		setup func(*testing.T, *Product)
	}{
		{
			name: "coming soon",
			want: constant.SellStateComingSoon,
		},
		{
			name: "active",
			want: constant.SellStateActive,
			setup: func(t *testing.T, p *Product) {
				t.Helper()
				if err := p.Launch(testNow); err != nil {
					t.Fatalf("set up active: %v", err)
				}
			},
		},
		{
			name: "out of stock",
			want: constant.SellStateOutOfStock,
			setup: func(t *testing.T, p *Product) {
				t.Helper()
				if err := p.Launch(testNow); err != nil {
					t.Fatalf("set up out of stock: %v", err)
				}
				if err := p.SellOut(testNow); err != nil {
					t.Fatalf("set up out of stock: %v", err)
				}
			},
		},
	}

	for _, tc := range setups {
		t.Run(tc.name, func(t *testing.T) {
			product := mustProduct(t)
			if tc.setup != nil {
				tc.setup(t, product)
			}
			if product.SellState != tc.want {
				t.Fatalf("set up %q failed, got %q", tc.want, product.SellState)
			}

			if err := product.Retire(testNow.Add(time.Minute)); err != nil {
				t.Fatalf("Retire from %q: unexpected error %v", tc.want, err)
			}
			if product.SellState != constant.SellStateDiscontinued {
				t.Errorf("SellState = %q, want %q", product.SellState, constant.SellStateDiscontinued)
			}
		})
	}
}

// FR-026: retiring is terminal, and a second retire is refused naming
// DISCONTINUED.
func TestRetireRefusesWhenAlreadyDiscontinued(t *testing.T) {
	product := mustProduct(t)
	if err := product.Retire(testNow); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	err := product.Retire(testNow.Add(time.Minute))
	if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
		t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
	}
	assertCurrentState(t, err, constant.SellStateDiscontinued)
}

// FR-026: DISCONTINUED has no outgoing edge, through any transition.
func TestDiscontinuedHasNoOutgoingEdge(t *testing.T) {
	moves := map[string]func(*Product, time.Time) error{
		"launch":   (*Product).Launch,
		"sell out": (*Product).SellOut,
		"restock":  (*Product).Restock,
		"retire":   (*Product).Retire,
		"change sell state": func(p *Product, now time.Time) error {
			return p.ChangeSellState(constant.SellStateComingSoon, now)
		},
	}

	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			product := mustProduct(t)
			if err := product.Retire(testNow); err != nil {
				t.Fatalf("Retire: %v", err)
			}

			err := move(product, testNow.Add(time.Minute))
			if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
				t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
			}
			assertCurrentState(t, err, constant.SellStateDiscontinued)
			if product.SellState != constant.SellStateDiscontinued {
				t.Errorf("a refused transition must leave the product retired, got %q", product.SellState)
			}
		})
	}
}

// FR-022: an edge the table does not have is refused and the current state is
// named.
func TestChangeSellStateRefusesAnEdgeTheTableDoesNotHave(t *testing.T) {
	product := mustProduct(t)

	err := product.ChangeSellState(constant.SellStateOutOfStock, testNow)
	if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
		t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
	}
	assertCurrentState(t, err, constant.SellStateComingSoon)
}

// FR-039: launching clears the pre-order label, because a product that is on sale
// is not "coming soon".
func TestLaunchClearsThePreorderLabel(t *testing.T) {
	date := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := validDraft()
	draft.IsPreorder = true
	draft.PreorderExpectedAt = &date

	product, err := NewProduct(draft, testNow)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if !product.IsPreorder || product.PreorderExpectedAt == nil {
		t.Fatalf("a pre-order draft must keep its label and date")
	}

	if err := product.Launch(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if product.IsPreorder {
		t.Errorf("launching must clear the pre-order label (FR-039)")
	}
	if product.PreorderExpectedAt != nil {
		t.Errorf("launching must clear the expected date (FR-039)")
	}
}

// FR-040: an expected date without the label belongs to nothing and is refused.
func TestNewProductRefusesAPreorderDateWithoutTheLabel(t *testing.T) {
	date := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := validDraft()
	draft.IsPreorder = false
	draft.PreorderExpectedAt = &date

	product, err := NewProduct(draft, testNow)
	if product != nil {
		t.Errorf("a refused draft must yield no product")
	}
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldPreorderExpectedAt)
}

// Apply changes the editable members, recomputes the folding key and bumps
// updated_at, without touching the identifier or the sell state (FR-012).
func TestApplyChangesTheEditableFieldsAndBumpsUpdatedAt(t *testing.T) {
	product := mustProduct(t)
	id := product.ID
	categoryID := uuid.New()
	later := testNow.Add(time.Hour)

	edit := ProductEdit{
		Name:        ptr("Tranh khắc gỗ"),
		Slug:        ptr("tranh-khac-go"),
		Description: ptr("Mô tả mới."),
		Price:       ptr(Price{Amount: 250, Currency: "USD"}),
		CategoryID:  &categoryID,
		Position:    ptr(9),
	}
	if err := product.Apply(edit, later); err != nil {
		t.Fatalf("Apply: unexpected error %v", err)
	}

	if product.Name != "Tranh khắc gỗ" || product.Slug != "tranh-khac-go" || product.Description != "Mô tả mới." {
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
	if product.NormalizedSlug != FoldKey(product.Slug) {
		t.Errorf("NormalizedSlug = %q, want FoldKey(%q)", product.NormalizedSlug, product.Slug)
	}
	if !product.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", product.UpdatedAt, later)
	}
	if product.ID != id {
		t.Errorf("the identifier changed on edit (FR-016)")
	}
	if product.SellState != constant.SellStateComingSoon {
		t.Errorf("an edit must not change the sell state, got %q", product.SellState)
	}
}

// A member the edit does not name keeps its current value (partial update).
func TestApplyKeepsOmittedFields(t *testing.T) {
	product := mustProduct(t)
	before := *product

	if err := product.Apply(ProductEdit{Position: ptr(42)}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if product.Name != before.Name || product.Slug != before.Slug ||
		product.Description != before.Description || product.CategoryID != before.CategoryID {
		t.Errorf("an omitted member must keep its value: %+v vs %+v", product, before)
	}
	if product.Position != 42 {
		t.Errorf("Position = %d, want 42", product.Position)
	}
}

// A rejected edit leaves the entity exactly as it was (FR-029).
func TestApplyRejectsInvalidChangeAndLeavesEntityUntouched(t *testing.T) {
	product := mustProduct(t)
	before := *product

	err := product.Apply(ProductEdit{Slug: ptr("Tranh Sơn")}, testNow.Add(time.Hour))
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldSlug)

	if *product != before {
		t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", product, before)
	}
}

// FR-040: an on-sale product is not "coming soon", so it cannot be marked a
// pre-order.
func TestApplyRefusesThePreorderLabelOnAnActiveProduct(t *testing.T) {
	product := mustProduct(t)
	if err := product.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	err := product.Apply(ProductEdit{IsPreorder: ptr(true)}, testNow.Add(time.Minute))
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	assertField(t, err, FieldIsPreorder)
	if product.IsPreorder {
		t.Errorf("a refused edit must not change the label")
	}
}

// FR-040: dropping the pre-order label also drops the expected date.
func TestApplyClearsTheDateWhenTheLabelIsDropped(t *testing.T) {
	date := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := validDraft()
	draft.IsPreorder = true
	draft.PreorderExpectedAt = &date
	product, err := NewProduct(draft, testNow)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	if err := product.Apply(ProductEdit{IsPreorder: ptr(false)}, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if product.IsPreorder || product.PreorderExpectedAt != nil {
		t.Errorf("dropping the label must clear the date, got isPreorder=%v date=%v", product.IsPreorder, product.PreorderExpectedAt)
	}
}

// assertCurrentState fails the test unless err carries the state the refused move
// started from (FR-024).
func assertCurrentState(t *testing.T, err error, want constant.SellState) {
	t.Helper()

	var transitionErr *domainerr.StateTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("expected a *StateTransitionError, got %T", err)
	}
	if transitionErr.Current != want {
		t.Errorf("refusal names current state %q, want %q", transitionErr.Current, want)
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

// mustProduct builds a product from validDraft for tests that only need a
// starting entity.
func mustProduct(t *testing.T) *Product {
	t.Helper()

	product, err := NewProduct(validDraft(), testNow)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	return product
}
