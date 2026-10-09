// Package httpapi adapts the cart module's use cases to HTTP. It carries no
// business rule: it reads the session, parses the request, calls the use case and
// maps the answer onto the shared envelope (Constitution I).
package httpapi
