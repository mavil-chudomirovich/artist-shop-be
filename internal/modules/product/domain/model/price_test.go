package model

import (
	"errors"
	"math"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// FR-032: a price is positive; zero and negative amounts are refused and the
// price member is named.
func TestPriceRefusesZeroAndNegativeAmounts(t *testing.T) {
	cases := map[string]int64{
		"zero":          0,
		"negative":      -1,
		"very negative": math.MinInt64,
	}

	for name, amount := range cases {
		t.Run(name, func(t *testing.T) {
			price, err := NewPrice(amount, "VND")
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, FieldPrice)
			if price != (Price{}) {
				t.Errorf("a refused price must be the zero value, got %+v", price)
			}
		})
	}
}

// FR-030: the currency is exactly three uppercase letters.
func TestPriceRefusesMalformedCurrency(t *testing.T) {
	cases := map[string]string{
		"lowercase":   "vnd",
		"too short":   "VN",
		"too long":    "VNDD",
		"digits":      "12A",
		"mixed":       "VnD",
		"empty":       "",
		"with spaces": " VND ",
	}

	for name, currency := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewPrice(100, currency)
			if !errors.Is(err, domainerr.ErrProductInvalid) {
				t.Fatalf("expected ErrProductInvalid, got %v", err)
			}
			assertField(t, err, FieldCurrency)
		})
	}
}

// FR-031: the stored amount equals the amount supplied exactly, with no rounding
// and no floating point anywhere.
func TestPriceStoresTheAmountExactly(t *testing.T) {
	cases := []int64{1, 999, 1999, 1 << 40, math.MaxInt64}

	for _, amount := range cases {
		price, err := NewPrice(amount, "VND")
		if err != nil {
			t.Fatalf("NewPrice(%d): unexpected error %v", amount, err)
		}
		if price.Amount != amount {
			t.Errorf("Amount = %d, want exactly %d", price.Amount, amount)
		}
		if price.Currency != "VND" {
			t.Errorf("Currency = %q, want %q", price.Currency, "VND")
		}
	}
}

// The three-uppercase-letter shape is accepted for a range of real codes.
func TestPriceAcceptsThreeUppercaseLetters(t *testing.T) {
	for _, currency := range []string{"VND", "USD", "EUR", "JPY"} {
		if _, err := NewPrice(1, currency); err != nil {
			t.Errorf("NewPrice(1, %q) = %v, want nil", currency, err)
		}
	}
}
