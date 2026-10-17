// Package domainerr is intentionally free of sentinels: the admin module owns no
// table and defines no business transition or business error, so request
// rejection is a request-level concern handled in presentation/http
// (Constitution I; plan.md Complexity Tracking). The package exists so the module
// keeps the constitution's four-layer layout.
package domainerr
