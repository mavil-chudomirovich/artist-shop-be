// Package httpapi exposes the user module over HTTP. It owns the /users and
// /divisions routes and carries no business rules: handlers translate HTTP to
// application DTOs and map sentinel errors to the shared error envelope.
package httpapi
