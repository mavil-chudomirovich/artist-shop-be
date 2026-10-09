package constant

// MovementKind is the kind of a physical stock change, stored as constrained
// text in inventory_transactions.kind and never changed once written (FR-004,
// research D9). The values are the exact ones the database's
// inventory_transactions_kind_ck accepts and the MovementKind enum of
// specs/007-inventory-tracking/contracts/openapi.yaml declares.
type MovementKind string

const (
	// MovementRestock is goods arriving: an increase.
	MovementRestock MovementKind = "RESTOCK"
	// MovementDamage is goods lost: a decrease.
	MovementDamage MovementKind = "DAMAGE"
	// MovementAdjustment is a correction to a counted value: a signed increase or
	// decrease, recorded as its difference (research D9).
	MovementAdjustment MovementKind = "ADJUSTMENT"
	// MovementSale is a paid hold becoming a decrease. It is distinct from DAMAGE
	// because the two are different events with different sources — a payment
	// versus a person — and the ledger is read by operators explaining a quantity
	// (research D9).
	MovementSale MovementKind = "SALE"
)

// IsValidMovementKind reports whether value is one of the four movement kinds.
func IsValidMovementKind(value MovementKind) bool {
	switch value {
	case MovementRestock, MovementDamage, MovementAdjustment, MovementSale:
		return true
	default:
		return false
	}
}
