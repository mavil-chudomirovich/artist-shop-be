// Package postgres implements the category module's repository port with pgx.
//
// The adapter embeds share/repository.Base for its write plumbing and sets an
// explicit Columns projection; there is deliberately no `SELECT *` fallback, so
// a column added to the physical table later cannot change a scan's arity.
package postgres
