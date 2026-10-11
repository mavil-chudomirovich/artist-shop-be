// Package payos adapts the PayOS Merchant API to the payment module's provider
// port with the standard library only — net/http for the calls and crypto/hmac +
// crypto/sha256 for the signatures — so no dependency is added (ADR 015 §5 D4).
// A log-only provider stands in when the settings are empty, so every
// non-provider path still runs without credentials.
package payos
