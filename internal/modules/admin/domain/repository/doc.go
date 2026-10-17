// Package repository is intentionally free of interfaces: the admin module owns
// no table and stores nothing, so its reads are the cross-module ports in
// internal/contracts rather than a storage repository (Constitution I; plan.md
// Complexity Tracking). The package exists so the module keeps the constitution's
// four-layer layout.
package repository
