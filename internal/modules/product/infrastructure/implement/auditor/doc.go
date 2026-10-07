// Package auditor adapts the shared audit emitter to the product module's Auditor
// port. It delegates to the foundation's single writer rather than opening a second
// one (FR-014).
package auditor
