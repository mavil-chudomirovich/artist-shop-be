package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
)

// assertField fails the test unless err is an InvalidValueError naming field.
func assertField(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *domainerr.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected an InvalidValueError, got %v", err)
	}
	if invalid.Field != field {
		t.Fatalf("rejection named field %q, want %q", invalid.Field, field)
	}
}

// FR-009: money is integer minor units, so the line total is exactly quantity
// times the captured price, with no rounding.
func TestLineTotalIsQuantityTimesCapturedPrice(t *testing.T) {
	line, err := NewCartLine(uuid.New(), 3, Price{Amount: 120000, Currency: "VND"})
	if err != nil {
		t.Fatalf("NewCartLine: %v", err)
	}

	total := line.LineTotal()
	if total.Amount != 360000 {
		t.Fatalf("line total = %d, want 360000", total.Amount)
	}
	if total.Currency != "VND" {
		t.Fatalf("line total currency = %q, want VND", total.Currency)
	}
}

// FR-009, SC-005: the subtotal is the exact sum of the line totals, computed in
// whole minor units. A total carried through a float would round; the exact
// integer round-trip is what proves it did not.
func TestSubtotalSumsLineTotalsExactly(t *testing.T) {
	cart := Cart{Lines: []CartLine{
		{ProductID: uuid.New(), Quantity: 2, UnitPrice: Price{Amount: 199999, Currency: "VND"}},
		{ProductID: uuid.New(), Quantity: 7, UnitPrice: Price{Amount: 1234567, Currency: "VND"}},
		{ProductID: uuid.New(), Quantity: 1, UnitPrice: Price{Amount: 1, Currency: "VND"}},
	}}

	subtotal, ok := cart.Subtotal()
	if !ok {
		t.Fatal("a cart with lines must have a subtotal")
	}
	const want = 2*199999 + 7*1234567 + 1
	if subtotal.Amount != want {
		t.Fatalf("subtotal = %d, want %d", subtotal.Amount, want)
	}
	if subtotal.Currency != "VND" {
		t.Fatalf("subtotal currency = %q, want VND", subtotal.Currency)
	}
}

// FR-004, spec Assumptions: an empty cart carries no money, so it has no subtotal
// rather than a zero in an invented currency.
func TestEmptyCartHasNoSubtotal(t *testing.T) {
	cart := Cart{}

	if !cart.Empty() {
		t.Fatal("a cart with no lines must report empty")
	}
	if _, ok := cart.Subtotal(); ok {
		t.Fatal("an empty cart must not carry a subtotal")
	}
}

// FR-006: a line quantity is a positive whole number; zero and negatives are
// refused naming `quantity`. A fractional value cannot reach this rule: the
// parameter is an int64 and presentation parses the raw number into one first, so
// `1.5` is a request-shape error there, exactly as module 05 handles it.
func TestQuantityRuleRejectsNonPositiveNamingTheField(t *testing.T) {
	for _, quantity := range []int64{0, -1, -42} {
		err := ValidateQuantity(quantity)
		if !errors.Is(err, domainerr.ErrInvalidValue) {
			t.Fatalf("ValidateQuantity(%d): expected ErrInvalidValue, got %v", quantity, err)
		}
		assertField(t, err, FieldQuantity)
	}

	if err := ValidateQuantity(1); err != nil {
		t.Fatalf("ValidateQuantity(1) must be accepted, got %v", err)
	}
}

// FR-006: a line is refused when its quantity is not positive, and a rejected
// line yields no line at all. The price is taken as given: it comes from the
// product module and the cart_items money checks enforce it (FR-009).
func TestNewCartLineRejectsANonPositiveQuantity(t *testing.T) {
	for _, quantity := range []int64{0, -1} {
		line, err := NewCartLine(uuid.New(), quantity, Price{Amount: 1000, Currency: "VND"})
		if !errors.Is(err, domainerr.ErrInvalidValue) {
			t.Fatalf("quantity %d: expected ErrInvalidValue, got %v", quantity, err)
		}
		if line != (CartLine{}) {
			t.Errorf("a rejected line must be zero, got %+v", line)
		}
		assertField(t, err, FieldQuantity)
	}
}

// FR-012, research D10: a line is buyable only when the product is on sale and at
// least the line's quantity is available.
func TestBuyableRequiresOnSaleAndEnoughAvailable(t *testing.T) {
	line := CartLine{ProductID: uuid.New(), Quantity: 3, UnitPrice: Price{Amount: 1000, Currency: "VND"}}

	cases := []struct {
		name  string
		facts ProductFacts
		want  bool
	}{
		{"on sale and exactly enough", ProductFacts{OnSale: true, Available: 3}, true},
		{"on sale and more than enough", ProductFacts{OnSale: true, Available: 10}, true},
		{"on sale but short", ProductFacts{OnSale: true, Available: 2}, false},
		{"on sale but zero available", ProductFacts{OnSale: true, Available: 0}, false},
		{"off sale but plenty available", ProductFacts{OnSale: false, Available: 100}, false},
		{"gone and no availability", ProductFacts{OnSale: false, Available: 0}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := line.Buyable(tc.facts); got != tc.want {
				t.Fatalf("Buyable(%+v) = %v, want %v", tc.facts, got, tc.want)
			}
		})
	}
}

// FR-012, research D10: a line that cannot be bought because the product is on
// sale but short reports the currently available quantity, including zero, so the
// customer can reduce the line to a buyable amount.
func TestShortLineReportsTheAvailableQuantity(t *testing.T) {
	line := CartLine{ProductID: uuid.New(), Quantity: 2, UnitPrice: Price{Amount: 1000, Currency: "VND"}}

	available, reported := line.ReportedAvailability(ProductFacts{OnSale: true, Available: 1})
	if !reported || available != 1 {
		t.Fatalf("a short line must report available=1, got available=%d reported=%v", available, reported)
	}

	available, reported = line.ReportedAvailability(ProductFacts{OnSale: true, Available: 0})
	if !reported || available != 0 {
		t.Fatalf("a zero-available line must report available=0, got available=%d reported=%v", available, reported)
	}

	// A line that is fully available reports nothing: there is no limit to show.
	if _, reported := line.ReportedAvailability(ProductFacts{OnSale: true, Available: 5}); reported {
		t.Fatal("a fully available line must not report an available quantity")
	}
}

// FR-012, research D10: an off-sale or removed product reports no quantity — there
// is no number to give, only the truth that the line cannot be bought.
func TestOffSaleOrGoneLineReportsNoQuantity(t *testing.T) {
	line := CartLine{ProductID: uuid.New(), Quantity: 2, UnitPrice: Price{Amount: 1000, Currency: "VND"}}

	for _, facts := range []ProductFacts{
		{OnSale: false, Available: 100}, // off sale, plenty in stock
		{OnSale: false, Available: 0},   // removed
	} {
		available, reported := line.ReportedAvailability(facts)
		if reported {
			t.Fatalf("an off-sale/gone line must report no quantity, got available=%d for %+v", available, facts)
		}
		if line.Buyable(facts) {
			t.Fatalf("an off-sale/gone line must not be buyable: %+v", facts)
		}
	}
}
