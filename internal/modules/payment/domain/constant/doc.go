// Package constant holds the payment module's business constants: the four
// payment states, the machine-readable error codes and the audit actions.
//
// A code or state value is part of the module's public contract: renaming one
// breaks every client that already handles it, so the values here mirror
// specs/011-order-payment/contracts/error-codes.md and contracts/openapi.yaml
// exactly.
package constant
