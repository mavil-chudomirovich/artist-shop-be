package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
)

// OrderSummary is one row of an order list: what a customer's and the operator's
// list project without loading the lines, plus the line count (data-model.md,
// "What each read path projects"). It is a read model, distinct from Order, so a
// list read never carries a partly-loaded entity whose lines and count could
// disagree.
type OrderSummary struct {
	// ID identifies the order.
	ID uuid.UUID
	// UserID is the account that owns the order. The customer's list does not
	// expose it; the operator's does (FR-018, FR-021).
	UserID uuid.UUID
	// Status is where the order is in its life.
	Status constant.Status
	// Total is the committed total in the currency's minor unit.
	Total Price
	// ItemCount is how many lines the order carries.
	ItemCount int64
	// CreatedAt is when the order was placed; lists are newest first.
	CreatedAt time.Time
}
