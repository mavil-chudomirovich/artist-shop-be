// Package auditor adapts the shared audit emitter to the order module's Auditor
// port. It delegates to the foundation's single writer rather than opening a
// second one (FR-023).
package auditor
