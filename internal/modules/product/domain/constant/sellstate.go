// Package constant holds the product module's business constants.
package constant

// SellState is the point a product has reached in its selling life (FR-022,
// research D8). It is stored as a constrained text column and is only ever changed
// through the product entity's transition methods.
type SellState string

// The four sell states of FR-022. They are the exact values the database's
// products_sell_state_ck accepts and the SellState enum of
// contracts/openapi.yaml declares, so a state this package can name is a state the
// database can store and the API can return.
const (
	// SellStateComingSoon is a product announced but not yet on sale. It is hidden
	// from customers unless the pre-order label is set (FR-002).
	SellStateComingSoon SellState = "COMING_SOON"
	// SellStateActive is a product on sale: visible and buyable (FR-025).
	SellStateActive SellState = "ACTIVE"
	// SellStateOutOfStock is a product that cannot currently be bought. It is
	// hidden from customers (FR-002).
	SellStateOutOfStock SellState = "OUT_OF_STOCK"
	// SellStateDiscontinued is the terminal state. It has no outgoing edge, so a
	// retired product never returns to sale (FR-026).
	SellStateDiscontinued SellState = "DISCONTINUED"
)

// transition is one edge of the sell-state machine.
type transition struct {
	from SellState
	to   SellState
}

// allowedTransitions is the transition table as data (FR-022, research D8): the
// edges are declared here rather than as a chain of conditionals, so a reader can
// check them against the spec at a glance.
//
// The set is exactly: announced → on sale; on sale ↔ out of stock in both
// directions, so a restock returns a sold-out product to sale; any of those three
// → retired. DISCONTINUED never appears as a `from`, which is what makes it
// terminal.
var allowedTransitions = []transition{
	{from: SellStateComingSoon, to: SellStateActive},
	{from: SellStateActive, to: SellStateOutOfStock},
	{from: SellStateOutOfStock, to: SellStateActive},
	{from: SellStateComingSoon, to: SellStateDiscontinued},
	{from: SellStateActive, to: SellStateDiscontinued},
	{from: SellStateOutOfStock, to: SellStateDiscontinued},
}

// IsValidSellState reports whether value is one of the four sell states.
func IsValidSellState(value SellState) bool {
	switch value {
	case SellStateComingSoon, SellStateActive, SellStateOutOfStock, SellStateDiscontinued:
		return true
	default:
		return false
	}
}

// IsAllowedTransition reports whether the transition table has an edge from
// `from` to `to` (FR-022). A state with no outgoing edge (DISCONTINUED) answers
// false for every target, which is how the terminal state is enforced.
func IsAllowedTransition(from, to SellState) bool {
	for _, edge := range allowedTransitions {
		if edge.from == from && edge.to == to {
			return true
		}
	}
	return false
}
