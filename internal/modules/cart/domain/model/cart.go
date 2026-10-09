// Package model holds the cart module's entities and value objects.
//
// The domain owns the money rule and the "can this be bought" decision. It
// validates structure and the invariant that a line holds a positive whole
// quantity; whether a product is on sale and how many are available are facts
// only other modules own, so a line is judged against facts it is handed rather
// than facts it reads (Constitution I, research D1, D2).
package model

import (
	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
)

// Contract member names used when a rejection has to say which input is wrong.
// They are the exact member names of specs/008-cart/contracts/openapi.yaml, so
// presentation can put one straight into the response detail without a second
// vocabulary that could drift away from the contract.
const (
	// FieldProductID is the request member carrying a product identifier.
	FieldProductID = "productId"
	// FieldQuantity is the request member carrying a line quantity.
	FieldQuantity = "quantity"
)

// Price is money as an integer amount in a currency's minor unit together with
// that currency (FR-009).
//
// Money is never a float and never a decimal string in this service: the amount is
// an integer of minor units, stored as an integer and returned as an integer, so
// the stored amount equals the amount supplied exactly. The currency travels with
// the amount because the constitution requires money to carry its own currency.
type Price struct {
	// Amount is the amount in the currency's minor unit. It is positive.
	Amount int64
	// Currency is the currency code: exactly three uppercase letters.
	Currency string
}

// CartLine is one product the customer intends to buy: its quantity and the unit
// price captured when it was added (FR-008, research D4).
//
// The captured price is a record of what the customer was shown, not a promise:
// the checkout re-checks it, so this line never silently charges a price the
// customer did not see.
type CartLine struct {
	// ProductID identifies the product the line is for. A cart holds at most one
	// line per product (FR-002).
	ProductID uuid.UUID
	// Quantity is how many the customer chose. A whole number, at least 1.
	Quantity int64
	// UnitPrice is the price captured when the product was added.
	UnitPrice Price
}

// NewCartLine builds a line, refusing a quantity that is not a positive whole
// number. A rejected line yields no line at all, so a caller cannot persist half
// of one. The captured price is taken as given: it comes from the product module,
// whose own rule and the cart_items money checks already guarantee it is positive
// money, so the cart does not re-decide a fact it does not own.
func NewCartLine(productID uuid.UUID, quantity int64, unitPrice Price) (CartLine, error) {
	if err := ValidateQuantity(quantity); err != nil {
		return CartLine{}, err
	}
	return CartLine{ProductID: productID, Quantity: quantity, UnitPrice: unitPrice}, nil
}

// ValidateQuantity enforces FR-006: a line quantity is a positive whole number.
// Zero, a negative value, or a fraction is refused naming `quantity`. Because the
// parameter is an int64, a fractional value cannot reach this rule at all: the
// presentation layer parses the raw JSON number into an int64 first, so a
// fractional quantity is a request-shape error there, exactly as module 05 does.
func ValidateQuantity(quantity int64) error {
	if quantity < 1 {
		return domainerr.InvalidValue(FieldQuantity, "must be a positive whole number")
	}
	return nil
}

// LineTotal is the line's contribution to the cart: quantity times the captured
// unit price, in the same currency (FR-009).
func (l CartLine) LineTotal() Price {
	return Price{Amount: l.Quantity * l.UnitPrice.Amount, Currency: l.UnitPrice.Currency}
}

// ProductFacts are the live facts a read re-checks a line against: whether the
// product may be sold and how many units are available right now. They are read
// across the module boundary, never stored on the line, so the cart reflects
// today's reality rather than the moment the line was added (FR-012, research D5).
type ProductFacts struct {
	// OnSale reports whether the product may currently be sold.
	OnSale bool
	// Available is the quantity a customer can take right now.
	Available int64
}

// Buyable reports whether the line can be bought as it stands: the product is on
// sale and at least the line's quantity is available (FR-012, research D10).
func (l CartLine) Buyable(facts ProductFacts) bool {
	return facts.OnSale && facts.Available >= l.Quantity
}

// ReportedAvailability returns the currently available quantity and whether it is
// reported for this line. It is reported only when the line cannot be bought
// because it is short — the product is on sale but holds fewer units than the line
// asks for, including zero — so the customer can reduce the line to a buyable
// amount. An off-sale or removed product reports no quantity: there is no number
// to give, only the truth that it cannot be bought (FR-012, research D10).
func (l CartLine) ReportedAvailability(facts ProductFacts) (int64, bool) {
	if !facts.OnSale || facts.Available >= l.Quantity {
		return 0, false
	}
	return facts.Available, true
}

// Cart is the one working basket of a signed-in customer. It belongs to exactly
// one account and is the source of the subtotal (FR-001, research D8).
type Cart struct {
	// ID identifies the cart. It is generated once and never exposed to the
	// client: the cart is addressed as /cart, not by identifier (research D7).
	ID uuid.UUID
	// UserID is the one account the cart belongs to.
	UserID uuid.UUID
	// Lines are the products the customer intends to buy, at most one per
	// product.
	Lines []CartLine
}

// NewCart builds a cart for one owner with a fresh identifier.
func NewCart(userID uuid.UUID) *Cart {
	return &Cart{ID: uuid.New(), UserID: userID}
}

// Empty reports whether the cart holds no line. An empty cart answers an empty
// cart, not an error (FR-004).
func (c Cart) Empty() bool { return len(c.Lines) == 0 }

// Subtotal is the sum of the line totals, computed as int64 minor units with no
// rounding or approximation (FR-009). The second result reports whether a subtotal
// exists at all: an empty cart carries no line and therefore no money, so it has
// no subtotal rather than a zero in an invented currency (spec, Assumptions).
//
// The MVP shop is single-currency, so every line in one cart shares a currency and
// the first line's currency is authoritative; the cart performs no conversion and
// a mismatch never arises (spec, Assumptions).
func (c Cart) Subtotal() (Price, bool) {
	if len(c.Lines) == 0 {
		return Price{}, false
	}
	currency := c.Lines[0].UnitPrice.Currency
	var total int64
	for _, line := range c.Lines {
		total += line.LineTotal().Amount
	}
	return Price{Amount: total, Currency: currency}, true
}
