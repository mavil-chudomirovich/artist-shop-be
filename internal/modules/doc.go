// Package modules is the namespace for business capability modules (auth, user,
// product, order, commission, ...). Each module owns its data and exposes
// application services used by other modules through exported interfaces only.
//
// The platform foundation under internal/platform provides shared concerns
// (configuration, database, HTTP, middleware, audit, health); modules depend on
// the platform, never the reverse.
package modules
