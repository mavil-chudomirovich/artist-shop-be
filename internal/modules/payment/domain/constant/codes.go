package constant

// Status is where a payment is in its life. The set is deliberately small — every
// value in it is reachable (research D6) — and is stored as constrained text in
// payments.status, changed only through the payment entity's transition methods
// (FR-011, Constitution III).
type Status string

const (
	// StatusPending is a live payment: the provider payment request exists and the
	// customer may pay it. It is the only non-terminal state (research D6).
	StatusPending Status = "PENDING"
	// StatusPaid is a payment a verified confirmation applied. It is terminal.
	StatusPaid Status = "PAID"
	// StatusCancelled is a payment whose order was cancelled (its provider link
	// cancelled too). It is terminal.
	StatusCancelled Status = "CANCELLED"
	// StatusUnmatched is a verified confirmation that could not be applied — a
	// wrong amount, or an order whose content changed or is no longer awaiting
	// payment. It carries the reason the operator reconciles; it is terminal and
	// the order is never moved by it (research D8).
	StatusUnmatched Status = "UNMATCHED"
)

// IsValidStatus reports whether value is one of the four payment states. It
// mirrors the payments_status_ck check so the domain and the storage agree on the
// set.
func IsValidStatus(value Status) bool {
	switch value {
	case StatusPending, StatusPaid, StatusCancelled, StatusUnmatched:
		return true
	default:
		return false
	}
}

// ProviderPayOS is the provider name stored on every payment row: the module
// integrates PayOS only (ADR 015 §5, research D1).
const ProviderPayOS = "payos"

// MaxOrderCode is the largest value the PayOS orderCode may take: a positive
// 53-bit integer (research D4). The generator in the domain never exceeds it.
const MaxOrderCode int64 = 1<<53 - 1

// Stable, machine-readable error codes of the payment module. They mirror
// specs/011-order-payment/contracts/error-codes.md and are mapped to HTTP status
// codes by presentation/http/errors.go. Clients branch on error.code, never on
// error.message.
const (
	// CodeOrderNotFound reports that no order carries the identifier, or that it
	// belongs to another customer. The two are one answer, so the route never
	// starts a payment for another customer's order (FR-017, error-codes.md).
	CodeOrderNotFound = "PAYMENT_ORDER_NOT_FOUND"
	// CodeOrderNotPayable reports that the order is not awaiting payment —
	// already paid, cancelled, or still awaiting the artist's confirmation. A
	// payment can only start for an order awaiting payment (FR-002).
	CodeOrderNotPayable = "PAYMENT_ORDER_NOT_PAYABLE"
	// CodeNotFound reports that the webhook carries an order code no payment
	// carries. Nothing is recorded — there is no payment to attach a record to —
	// and the module logs a warning for the operator (research D7).
	CodeNotFound = "PAYMENT_NOT_FOUND"
	// CodeAlreadyLive reports that a live payment exists for the order but its
	// link can no longer be returned, so a new one cannot be created while the
	// stale row stands (FR-003).
	CodeAlreadyLive = "PAYMENT_ALREADY_LIVE"
	// CodeProviderError reports that the provider refused to create the link or
	// was unreachable. Nothing was created and the order is unchanged (FR-002).
	CodeProviderError = "PAYMENT_PROVIDER_ERROR"
	// CodeWebhookInvalidSignature reports that the webhook's signature does not
	// verify, so the event is not from the provider. Nothing is applied
	// (FR-019).
	CodeWebhookInvalidSignature = "PAYMENT_WEBHOOK_INVALID_SIGNATURE"
)

// Audit actions emitted by the payment module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package owns
// them so every module spells them the same way.
const (
	// AuditPaymentCreated is recorded when a payment request is created (FR-022).
	AuditPaymentCreated = "PAYMENT_CREATED"
	// AuditPaymentApplied is recorded when a verified confirmation is applied
	// (FR-022).
	AuditPaymentApplied = "PAYMENT_APPLIED"
	// AuditPaymentCancelled is recorded when a cancelled order's payment is
	// cancelled (FR-022).
	AuditPaymentCancelled = "PAYMENT_CANCELLED"
	// AuditPaymentUnmatched is recorded when a verified confirmation cannot be
	// applied and is kept for the operator (FR-022).
	AuditPaymentUnmatched = "PAYMENT_UNMATCHED"
)
