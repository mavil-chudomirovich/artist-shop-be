package model

import (
	"time"

	"github.com/google/uuid"
)

// EditSnapshot is the content of an order at one moment: its delivery address and
// its snapshot lines. An edit records the content before and after it, so the shop
// can see what changed even after the order has moved on (FR-017, research D14).
type EditSnapshot struct {
	// Address is the delivery address at that moment.
	Address Address
	// Lines are the order's snapshot lines at that moment, in position order.
	Lines []OrderLine
}

// EditHistory is one accepted edit: the order it belongs to, the content version
// the edit produced, the account that made it, and the before/after content
// snapshots (FR-017, research D14). It is an audit record: read whole, never
// queried by field, which is why the snapshots are stored as JSON documents rather
// than normalized rows (Complexity Tracking).
type EditHistory struct {
	// ID identifies the record.
	ID uuid.UUID
	// OrderID is the order that was edited.
	OrderID uuid.UUID
	// Version is the order's content version after the edit.
	Version int64
	// ActorID is the account that edited the order. A loose reference: the record
	// outlives the account it names.
	ActorID uuid.UUID
	// Before is the order's content before the edit.
	Before EditSnapshot
	// After is the order's content after the edit.
	After EditSnapshot
	// CreatedAt is when the edit was accepted. Set once.
	CreatedAt time.Time
}
