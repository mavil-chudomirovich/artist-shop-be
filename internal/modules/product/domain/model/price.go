package model

import (
	"regexp"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// currencyPattern is the shape a currency code must have: exactly three uppercase
// ASCII letters (FR-030). It is the same pattern the database check
// products_currency_ck carries, so a value the domain accepts is a value the
// database stores and vice versa.
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Price is an amount in a currency's minor unit together with that currency
// (FR-031, research D3).
//
// Money is never a float and never a decimal string in this service: the amount is
// an integer of minor units, parsed as an integer, stored as an integer and
// returned as an integer, so the stored amount equals the amount supplied exactly.
// The currency travels with the amount because the constitution requires money to
// carry its own currency rather than a service-wide constant.
type Price struct {
	// Amount is the price in the currency's minor unit. It is positive.
	Amount int64
	// Currency is the currency code: exactly three uppercase letters.
	Currency string
}

// NewPrice builds a price, refusing an amount or a currency that fails its rule.
// It is the constructor every stored price passes through.
func NewPrice(amount int64, currency string) (Price, error) {
	price := Price{Amount: amount, Currency: currency}
	if err := price.Validate(); err != nil {
		return Price{}, err
	}
	return price, nil
}

// Validate refuses a non-positive amount or a currency that is not three uppercase
// letters, naming the offending field (FR-030, FR-032).
func (p Price) Validate() error {
	if p.Amount <= 0 {
		return domainerr.InvalidProductField(
			FieldPrice, "must be a positive amount in the currency's minor unit")
	}
	if !currencyPattern.MatchString(p.Currency) {
		return domainerr.InvalidProductField(
			FieldCurrency, "must be three uppercase letters")
	}
	return nil
}
