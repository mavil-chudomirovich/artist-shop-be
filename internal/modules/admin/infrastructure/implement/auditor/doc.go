// Package auditor adapts the shared audit emitter to the admin module's Auditor
// port. It delegates to the foundation's single writer rather than opening a
// second one, and the dashboard's audit event carries no PII (FR-030,
// Constitution VI).
package auditor
