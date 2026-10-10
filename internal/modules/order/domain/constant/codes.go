// Package constant holds the order module's business constants: the six order
// states, the machine-readable error codes, the list sort orders and the audit
// actions.
//
// A code or state value is part of the module's public contract: renaming one
// breaks every client that already handles it, so the values here mirror
// specs/010-order-confirmation/contracts/error-codes.md and
// specs/010-order-confirmation/contracts/openapi.yaml exactly. Feature 010
// supersedes specs/009-order's five-state set: PENDING_PAYMENT is renamed
// PAYMENT_PENDING and a new PENDING is added (research D1).
package constant

// Status is where an order is in its life. It is stored as constrained text in
// orders.status and is only ever changed through the order entity's transition
// methods (FR-001, FR-010).
type Status string

const (
	// StatusPending is an order awaiting the artist's confirmation. It holds no
	// goods and carries no deadline; every fresh order starts here (FR-001,
	// research D2).
	StatusPending Status = "PENDING"
	// StatusPaymentPending is an order the artist has confirmed: its goods are
	// held, all-or-nothing, while the customer pays within the payment window
	// (FR-005, FR-006, research D1, D3).
	StatusPaymentPending Status = "PAYMENT_PENDING"
	// StatusPaid is an order a confirmed payment has turned its hold into a
	// sale. Module 08 drives this transition (research D13 of 009).
	StatusPaid Status = "PAID"
	// StatusShipped is a paid order an administrator has marked shipped.
	StatusShipped Status = "SHIPPED"
	// StatusCompleted is a shipped order an administrator has marked completed.
	// It is terminal.
	StatusCompleted Status = "COMPLETED"
	// StatusCancelled is an order the customer cancelled, the artist declined, or
	// that expired unpaid. It is terminal.
	StatusCancelled Status = "CANCELLED"
)

// IsValidStatus reports whether value is one of the six order states. It mirrors
// the orders_status_ck check so the domain and the storage agree on the set.
func IsValidStatus(value Status) bool {
	switch value {
	case StatusPending, StatusPaymentPending, StatusPaid, StatusShipped, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

// OrderListSort is how an administrator order list is ordered. The zero value is
// SortNewest, the list's default; SortOldest is the FIFO confirmation queue
// (FR-026, research D11).
type OrderListSort string

const (
	// SortNewest returns the most recently placed orders first.
	SortNewest OrderListSort = "newest"
	// SortOldest returns the oldest orders first, so the confirmation queue is
	// FIFO by creation time.
	SortOldest OrderListSort = "oldest"
)

// Stable, machine-readable error codes of the order module. They mirror
// specs/009-order/contracts/error-codes.md and are mapped to HTTP status codes by
// presentation/http/errors.go. Clients branch on error.code, never on
// error.message.
const (
	// CodeOrderNotFound reports that no order carries the identifier, or that it
	// belongs to another customer. The two are one answer, so the route never
	// confirms another customer's order (FR-020, error-codes.md).
	CodeOrderNotFound = "ORDER_NOT_FOUND"
	// CodeCartEmpty reports that checkout was asked for an empty cart. An order
	// is never created from nothing (FR-006).
	CodeCartEmpty = "ORDER_CART_EMPTY"
	// CodeItemNotPurchasable reports that a cart line's product is off sale, out
	// of stock or removed, so it cannot be ordered (FR-004).
	CodeItemNotPurchasable = "ORDER_ITEM_NOT_PURCHASABLE"
	// CodeItemPriceChanged reports that a cart line's product is priced
	// differently from the price the customer was shown, so the whole checkout is
	// refused (FR-004).
	CodeItemPriceChanged = "ORDER_ITEM_PRICE_CHANGED"
	// CodeQuantityExceedsAvailable reports that a line asks for more than is
	// available, or that the goods could not be held (FR-005, FR-017).
	CodeQuantityExceedsAvailable = "ORDER_QUANTITY_EXCEEDS_AVAILABLE"
	// CodeNoAddress reports that the customer has no delivery address, so an order
	// has nowhere to go (FR-003).
	CodeNoAddress = "ORDER_NO_ADDRESS"
	// CodeStateTransitionInvalid reports that the requested move is not one the
	// order's current state allows. The message names the current state
	// (FR-011).
	CodeStateTransitionInvalid = "ORDER_STATE_TRANSITION_INVALID"
	// CodeNotTransferable reports that the order is not paid, so it cannot be
	// transferred (FR-024).
	CodeNotTransferable = "ORDER_NOT_TRANSFERABLE"
	// CodeTransferTargetNotFound reports that no account carries the email the
	// transfer names (FR-024 of 009).
	CodeTransferTargetNotFound = "ORDER_TRANSFER_TARGET_NOT_FOUND"
	// CodeNotEditable reports that the order is not in a state the customer may
	// change — it is paid or beyond. Only an order awaiting the artist or
	// awaiting payment can be edited (FR-012).
	CodeNotEditable = "ORDER_NOT_EDITABLE"
	// CodeEmptyOrder reports that an edit would leave the order with no line. An
	// order always carries at least one line; the customer cancels instead
	// (FR-015).
	CodeEmptyOrder = "ORDER_EMPTY"
)

// Audit actions emitted by the order module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package owns
// them so every module spells them the same way.
const (
	// AuditOrderConfirmed is recorded when an administrator confirms an order
	// awaiting the artist, holding the whole order (FR-024).
	AuditOrderConfirmed = "ORDER_CONFIRMED"
	// AuditOrderRejected is recorded when an administrator declines an order
	// awaiting the artist (FR-018).
	AuditOrderRejected = "ORDER_REJECTED"
	// AuditOrderShipped is recorded when an administrator marks a paid order
	// shipped (FR-022, FR-023).
	AuditOrderShipped = "ORDER_SHIPPED"
	// AuditOrderCompleted is recorded when an administrator marks a shipped order
	// completed (FR-022, FR-023).
	AuditOrderCompleted = "ORDER_COMPLETED"
	// AuditOrderTransferred is recorded when an administrator hands a paid order
	// to another account (FR-024).
	AuditOrderTransferred = "ORDER_TRANSFERRED"
)
