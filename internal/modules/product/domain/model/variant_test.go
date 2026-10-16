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

// variantProduct is the product a test variant belongs to.
var variantProduct = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// validVariantDraft is a draft that every rule accepts.
func validVariantDraft() VariantDraft {
	return VariantDraft{
		Name:     "Bản tiêu chuẩn",
		SKU:      "TS-001-STD",
		Position: 0,
	}
}

// A newly created variant is in COMING_SOON, belongs to its product and carries a
// fresh identifier (FR-003, FR-015).
func TestNewVariantStartsInComingSoon(t *testing.T) {
	variant, err := NewVariant(variantProduct, validVariantDraft(), testNow)
	if err != nil {
		t.Fatalf("NewVariant: unexpected error %v", err)
	}

	if variant.ID == uuid.Nil {
		t.Errorf("a created variant must receive an identifier")
	}
	if variant.ProductID != variantProduct {
		t.Errorf("ProductID = %s, want %s", variant.ProductID, variantProduct)
	}
	if variant.SellState != constant.SellStateComingSoon {
		t.Errorf("SellState = %q, want %q", variant.SellState, constant.SellStateComingSoon)
	}
	if variant.PriceAmount != nil {
		t.Errorf("PriceAmount = %v, want nil (fall back to the product's default)", variant.PriceAmount)
	}
	if !variant.CreatedAt.Equal(testNow) || !variant.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps = %v / %v, want %v", variant.CreatedAt, variant.UpdatedAt, testNow)
	}
}

// The stored name and SKU are the trimmed ones (FR-003, FR-011).
func TestNewVariantTrimsNameAndSKU(t *testing.T) {
	draft := validVariantDraft()
	draft.Name = "  Bản đặc biệt  "
	draft.SKU = "  TS-001-SPC  "

	variant, err := NewVariant(variantProduct, draft, testNow)
	if err != nil {
		t.Fatalf("NewVariant: unexpected error %v", err)
	}
	if variant.Name != "Bản đặc biệt" || variant.SKU != "TS-001-SPC" {
		t.Errorf("values were not trimmed: %q / %q", variant.Name, variant.SKU)
	}
}

// A rejected draft yields no variant at all and names the member (FR-003, FR-011,
// FR-016, FR-017).
func TestNewVariantRejectsInvalidValuesAndNamesTheField(t *testing.T) {
	overLongName := strings.Repeat("ệ", MaxNameRunes+1)
	zero := int64(0)
	negative := int64(-5)
	badImage := &VariantImage{PublicID: "p", URL: "http://example.test/a.jpg", Width: 10, Height: 10}

	cases := []struct {
		name  string
		field string
		draft VariantDraft
	}{
		{"blank name", FieldName, VariantDraft{Name: "  ", SKU: "S"}},
		{"over-long name", FieldName, VariantDraft{Name: overLongName, SKU: "S"}},
		{"blank sku", FieldSKU, VariantDraft{Name: "a", SKU: "  "}},
		{"zero price", FieldPrice, VariantDraft{Name: "a", SKU: "S", PriceAmount: &zero}},
		{"negative price", FieldPrice, VariantDraft{Name: "a", SKU: "S", PriceAmount: &negative}},
		{"non-https image", FieldVariantImage, VariantDraft{Name: "a", SKU: "S", Image: badImage}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			variant, err := NewVariant(variantProduct, tc.draft, testNow)
			if variant != nil {
				t.Errorf("a rejected draft must yield no variant, got %+v", variant)
			}
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
		})
	}
}

// An explicit price is copied, so the entity never aliases the caller's value
// (FR-017).
func TestNewVariantCopiesTheOwnPrice(t *testing.T) {
	amount := int64(2500)
	draft := validVariantDraft()
	draft.PriceAmount = &amount

	variant, err := NewVariant(variantProduct, draft, testNow)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	if variant.PriceAmount == nil || *variant.PriceAmount != 2500 {
		t.Fatalf("PriceAmount = %v, want 2500", variant.PriceAmount)
	}
	amount = 9999
	if *variant.PriceAmount != 2500 {
		t.Errorf("the entity aliased the caller's amount: %d", *variant.PriceAmount)
	}
}

// FR-017: a variant with no own price uses the product's default price, in the
// product's currency.
func TestEffectivePriceFallsBackToTheProductDefault(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	productPrice := Price{Amount: 1999, Currency: "VND"}

	if got := variant.EffectivePrice(productPrice); got != productPrice {
		t.Errorf("EffectivePrice() = %+v, want the product default %+v", got, productPrice)
	}
}

// FR-017: a variant's own price overrides the product's default, but the currency
// stays the product's (FR-024).
func TestEffectivePriceUsesTheVariantsOwnAmount(t *testing.T) {
	amount := int64(2500)
	draft := validVariantDraft()
	draft.PriceAmount = &amount
	variant, err := NewVariant(variantProduct, draft, testNow)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}

	got := variant.EffectivePrice(Price{Amount: 1999, Currency: "VND"})
	if got.Amount != 2500 {
		t.Errorf("EffectivePrice().Amount = %d, want 2500", got.Amount)
	}
	if got.Currency != "VND" {
		t.Errorf("EffectivePrice().Currency = %q, want the product's VND (FR-024)", got.Currency)
	}
}

// FR-015: COMING_SOON -> ACTIVE.
func TestVariantLaunchMovesComingSoonToActive(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	later := testNow.Add(time.Minute)

	if err := variant.Launch(later); err != nil {
		t.Fatalf("Launch: unexpected error %v", err)
	}
	if variant.SellState != constant.SellStateActive {
		t.Errorf("SellState = %q, want %q", variant.SellState, constant.SellStateActive)
	}
	if !variant.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", variant.UpdatedAt, later)
	}
}

// Launch refuses any state but COMING_SOON and names the current one (FR-015).
func TestVariantLaunchRefusesWhenNotComingSoon(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	if err := variant.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	err := variant.Launch(testNow.Add(time.Minute))
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	assertVariantCurrentState(t, err, constant.SellStateActive)
	if variant.SellState != constant.SellStateActive {
		t.Errorf("a refused transition must leave the variant untouched, got %q", variant.SellState)
	}
}

// FR-015: ACTIVE -> OUT_OF_STOCK.
func TestVariantSellOutMovesActiveToOutOfStock(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	if err := variant.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if err := variant.SellOut(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("SellOut: unexpected error %v", err)
	}
	if variant.SellState != constant.SellStateOutOfStock {
		t.Errorf("SellState = %q, want %q", variant.SellState, constant.SellStateOutOfStock)
	}
}

// SellOut refuses a variant that is not on sale.
func TestVariantSellOutRefusesWhenNotActive(t *testing.T) {
	variant := mustVariant(t, variantProduct)

	err := variant.SellOut(testNow)
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	assertVariantCurrentState(t, err, constant.SellStateComingSoon)
}

// FR-015: OUT_OF_STOCK -> ACTIVE (the restock direction).
func TestVariantRestockReturnsOutOfStockToActive(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	if err := variant.Launch(testNow); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := variant.SellOut(testNow); err != nil {
		t.Fatalf("SellOut: %v", err)
	}

	if err := variant.Restock(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Restock: unexpected error %v", err)
	}
	if variant.SellState != constant.SellStateActive {
		t.Errorf("SellState = %q, want %q", variant.SellState, constant.SellStateActive)
	}
}

// Restock refuses a variant that is not out of stock, so a restock cannot force an
// illegal edge.
func TestVariantRestockRefusesWhenNotOutOfStock(t *testing.T) {
	variant := mustVariant(t, variantProduct)

	err := variant.Restock(testNow)
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	assertVariantCurrentState(t, err, constant.SellStateComingSoon)
}

// FR-015: any of the three non-terminal states -> DISCONTINUED.
func TestVariantRetireMovesAnyNonTerminalStateToDiscontinued(t *testing.T) {
	setups := []struct {
		name  string
		want  constant.SellState
		setup func(*testing.T, *Variant)
	}{
		{name: "coming soon", want: constant.SellStateComingSoon},
		{
			name: "active",
			want: constant.SellStateActive,
			setup: func(t *testing.T, v *Variant) {
				t.Helper()
				if err := v.Launch(testNow); err != nil {
					t.Fatalf("set up active: %v", err)
				}
			},
		},
		{
			name: "out of stock",
			want: constant.SellStateOutOfStock,
			setup: func(t *testing.T, v *Variant) {
				t.Helper()
				if err := v.Launch(testNow); err != nil {
					t.Fatalf("set up out of stock: %v", err)
				}
				if err := v.SellOut(testNow); err != nil {
					t.Fatalf("set up out of stock: %v", err)
				}
			},
		},
	}

	for _, tc := range setups {
		t.Run(tc.name, func(t *testing.T) {
			variant := mustVariant(t, variantProduct)
			if tc.setup != nil {
				tc.setup(t, variant)
			}
			if variant.SellState != tc.want {
				t.Fatalf("set up %q failed, got %q", tc.want, variant.SellState)
			}

			if err := variant.Retire(testNow.Add(time.Minute)); err != nil {
				t.Fatalf("Retire from %q: unexpected error %v", tc.want, err)
			}
			if variant.SellState != constant.SellStateDiscontinued {
				t.Errorf("SellState = %q, want %q", variant.SellState, constant.SellStateDiscontinued)
			}
		})
	}
}

// FR-015: retiring is terminal, and a second retire is refused naming
// DISCONTINUED.
func TestVariantRetireRefusesWhenAlreadyDiscontinued(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	if err := variant.Retire(testNow); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	err := variant.Retire(testNow.Add(time.Minute))
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	assertVariantCurrentState(t, err, constant.SellStateDiscontinued)
}

// FR-015: DISCONTINUED has no outgoing edge, through any transition.
func TestVariantDiscontinuedHasNoOutgoingEdge(t *testing.T) {
	moves := map[string]func(*Variant, time.Time) error{
		"launch":   (*Variant).Launch,
		"sell out": (*Variant).SellOut,
		"restock":  (*Variant).Restock,
		"retire":   (*Variant).Retire,
		"change sell state": func(v *Variant, now time.Time) error {
			return v.ChangeSellState(constant.SellStateComingSoon, now)
		},
	}

	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			variant := mustVariant(t, variantProduct)
			if err := variant.Retire(testNow); err != nil {
				t.Fatalf("Retire: %v", err)
			}

			err := move(variant, testNow.Add(time.Minute))
			if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
				t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
			}
			assertVariantCurrentState(t, err, constant.SellStateDiscontinued)
			if variant.SellState != constant.SellStateDiscontinued {
				t.Errorf("a refused transition must leave the variant retired, got %q", variant.SellState)
			}
		})
	}
}

// FR-015: an edge the table does not have is refused and the current state is
// named.
func TestVariantChangeSellStateRefusesAnEdgeTheTableDoesNotHave(t *testing.T) {
	variant := mustVariant(t, variantProduct)

	err := variant.ChangeSellState(constant.SellStateOutOfStock, testNow)
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	assertVariantCurrentState(t, err, constant.SellStateComingSoon)
}

// FR-005, FR-007: only an on-sale or out-of-stock variant is visible, and only an
// on-sale one is buyable, judged from the state alone whatever wrote the row.
func TestVariantVisibilityAndBuyableFollowTheState(t *testing.T) {
	cases := []struct {
		name    string
		state   constant.SellState
		visible bool
		buyable bool
	}{
		{"coming soon", constant.SellStateComingSoon, false, false},
		{"on sale", constant.SellStateActive, true, true},
		{"out of stock", constant.SellStateOutOfStock, true, false},
		{"retired", constant.SellStateDiscontinued, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			variant := Variant{SellState: tc.state}

			if got := variant.VisibleToCustomers(); got != tc.visible {
				t.Errorf("VisibleToCustomers() = %v, want %v for %s", got, tc.visible, tc.state)
			}
			if got := variant.Buyable(); got != tc.buyable {
				t.Errorf("Buyable() = %v, want %v for %s", got, tc.buyable, tc.state)
			}
		})
	}
}

// Apply changes the editable members and bumps updated_at, without touching the
// identifier, the product or the sell state (FR-010).
func TestVariantApplyChangesEditableFields(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	id := variant.ID
	later := testNow.Add(time.Hour)

	if err := variant.Apply(VariantEdit{
		Name:     ptr("Bản đặc biệt"),
		SKU:      ptr("TS-001-SPC"),
		Position: ptr(4),
	}, later); err != nil {
		t.Fatalf("Apply: unexpected error %v", err)
	}

	if variant.Name != "Bản đặc biệt" || variant.SKU != "TS-001-SPC" || variant.Position != 4 {
		t.Errorf("editable fields were not applied: %+v", variant)
	}
	if !variant.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", variant.UpdatedAt, later)
	}
	if variant.ID != id || variant.ProductID != variantProduct {
		t.Errorf("the identifier or the product changed on edit")
	}
	if variant.SellState != constant.SellStateComingSoon {
		t.Errorf("an edit must not change the sell state, got %q", variant.SellState)
	}
}

// A member the edit does not name keeps its current value (partial update).
func TestVariantApplyKeepsOmittedFields(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	before := *variant

	if err := variant.Apply(VariantEdit{Position: ptr(42)}, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if variant.Name != before.Name || variant.SKU != before.SKU {
		t.Errorf("an omitted member must keep its value: %+v vs %+v", variant, before)
	}
	if variant.Position != 42 {
		t.Errorf("Position = %d, want 42", variant.Position)
	}
}

// FR-017: an edit can set an own price and can clear it back to the product's
// default.
func TestVariantApplySetsAndClearsTheOwnPrice(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	amount := int64(3000)

	if err := variant.Apply(VariantEdit{PriceAmount: &amount, PriceAmountSet: true}, testNow); err != nil {
		t.Fatalf("Apply set: %v", err)
	}
	if variant.PriceAmount == nil || *variant.PriceAmount != 3000 {
		t.Fatalf("PriceAmount = %v, want 3000", variant.PriceAmount)
	}

	if err := variant.Apply(VariantEdit{PriceAmountSet: true}, testNow); err != nil {
		t.Fatalf("Apply clear: %v", err)
	}
	if variant.PriceAmount != nil {
		t.Errorf("clearing the price must fall back to the product default, got %v", variant.PriceAmount)
	}
}

// A rejected edit leaves the entity exactly as it was (FR-011, FR-017).
func TestVariantApplyRejectsInvalidChangeAndLeavesEntityUntouched(t *testing.T) {
	variant := mustVariant(t, variantProduct)
	before := *variant
	zero := int64(0)

	cases := []struct {
		name  string
		field string
		edit  VariantEdit
	}{
		{"blank sku", FieldSKU, VariantEdit{SKU: ptr("  ")}},
		{"blank name", FieldName, VariantEdit{Name: ptr("  ")}},
		{"zero price", FieldPrice, VariantEdit{PriceAmount: &zero, PriceAmountSet: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := variant.Apply(tc.edit, testNow.Add(time.Hour))
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, tc.field)
			if *variant != before {
				t.Errorf("a rejected edit must leave the entity untouched: %+v vs %+v", variant, before)
			}
		})
	}
}

// assertVariantCurrentState fails the test unless err carries the variant state the
// refused move started from (FR-015).
func assertVariantCurrentState(t *testing.T, err error, want constant.SellState) {
	t.Helper()

	var transitionErr *domainerr.VariantStateTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("expected a *VariantStateTransitionError, got %T", err)
	}
	if transitionErr.Current != want {
		t.Errorf("refusal names current state %q, want %q", transitionErr.Current, want)
	}
}

// mustVariant builds a variant from validVariantDraft for tests that only need a
// starting entity.
func mustVariant(t *testing.T, productID uuid.UUID) *Variant {
	t.Helper()

	variant, err := NewVariant(productID, validVariantDraft(), testNow)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	return variant
}
