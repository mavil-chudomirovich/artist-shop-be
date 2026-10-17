// Package httpapi adapts the admin dashboard use case to HTTP. It carries no
// business rule: it reads the session, parses and validates the query, calls the
// use case and maps the answer onto the shared envelope (Constitution I).
package httpapi
