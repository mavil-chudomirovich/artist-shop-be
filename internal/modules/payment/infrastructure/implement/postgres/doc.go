// Package postgres adapts PostgreSQL to the payment module's repository port. It
// joins the transaction the application put in the context and never opens one
// itself (Constitution I).
package postgres
