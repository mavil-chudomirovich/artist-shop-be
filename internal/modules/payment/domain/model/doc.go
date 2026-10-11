// Package model holds the payment module's entity and its four-state machine: a
// payment, the states PENDING, PAID, CANCELLED and UNMATCHED, the allowed
// transitions and the order-code generator. The domain imports only the standard
// library, internal/share/access and github.com/google/uuid (Constitution I).
package model
