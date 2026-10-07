// Package auditor adapts the shared audit emitter to the category module's
// Auditor port. It delegates to the foundation's single writer rather than
// opening a second one (FR-013).
package auditor
