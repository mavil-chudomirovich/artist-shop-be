// Package modules is the namespace for business capability modules (auth, user,
// product, order, commission, ...). Each module owns its data and exposes
// application services used by other modules through exported interfaces only.
//
// The shared foundation under internal/share provides cross-cutting concerns
// (configuration, database, HTTP, middleware, audit, health); modules depend on
// the foundation, never the reverse.
package modules
