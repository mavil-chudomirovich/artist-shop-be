// Package auditor adapts the shared audit emitter to the inventory module's
// Auditor port. It delegates to the foundation's single writer rather than opening
// a second one (FR-006).
package auditor
