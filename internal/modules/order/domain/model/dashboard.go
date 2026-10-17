package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
)

// This file holds the order module's read models for the admin dashboard. They are
// projections over the order-owned tables, never stored entities: the dashboard
// assembles its response from them and the order module answers only what it owns
// (FR-028, research D7).

// TimeWindow is one half-open interval of storage instants: From is inclusive and
// To is exclusive. The caller builds both endpoints in the shop timezone, so a
// calendar boundary is the operator's boundary while the instants themselves stay
// UTC (FR-007, FR-008).
type TimeWindow struct {
	// From is the inclusive start of the window.
	From time.Time
	// To is the exclusive end of the window.
	To time.Time
}

// OrderKPIs is the order module's answer to the dashboard's revenue and order
// figures, read in one conditional-aggregate query (research D7). Revenue counts
// only orders that have earned money; the order counts include every state
// (FR-010, FR-012, FR-013, FR-014).
type OrderKPIs struct {
	// CurrentRevenue is the revenue of the current window.
	CurrentRevenue Price
	// LastRevenue is the revenue of the previous, comparable window.
	LastRevenue Price
	// CurrentOrders counts orders created in the current window, every state.
	CurrentOrders int64
	// LastOrders counts orders created in the previous window, every state.
	LastOrders int64
	// TotalOrders counts every order ever placed, every state.
	TotalOrders int64
	// PendingConfirmation counts orders awaiting the artist, all-time.
	PendingConfirmation int64
}

// DailyRevenue is one day's revenue in the shop timezone. Days the shop earned
// nothing are absent from the read; the application zero-fills them so the trend
// has a point per day (FR-019).
type DailyRevenue struct {
	// Date is the calendar day in the shop timezone, as YYYY-MM-DD.
	Date string
	// Amount is that day's revenue.
	Amount Price
}

// CategoryRevenue is the revenue of one category snapshot within a window.
// CategoryID is nil for a line whose category could not be resolved — the
// legacy/tombstone bucket the application names "Khác" (FR-021, FR-023).
type CategoryRevenue struct {
	// CategoryID is the snapshotted category, or nil when unresolved.
	CategoryID *uuid.UUID
	// CategoryName is the snapshotted category name, empty when unresolved.
	CategoryName string
	// Amount is the revenue attributed to the category in the window.
	Amount Price
}

// RecentOrder is the context the dashboard shows for one of the newest orders: its
// money, state and line summary, without resolving the customer (the application
// reads customer summaries separately, in one bulk call) (FR-025, FR-026).
type RecentOrder struct {
	// ID identifies the order.
	ID uuid.UUID
	// CustomerID is the account that placed the order. It stays a loose
	// reference: the account may no longer exist.
	CustomerID uuid.UUID
	// Total is the order total.
	Total Price
	// Status is the order's stored state.
	Status constant.Status
	// CreatedAt is when the order was placed.
	CreatedAt time.Time
	// FirstLineName is the first line's product-name snapshot, by position.
	FirstLineName string
	// FirstLineVariantName is the first line's variant-name snapshot. It is
	// empty for a legacy tombstone line.
	FirstLineVariantName string
	// FirstLineQuantity is the first line's quantity.
	FirstLineQuantity int64
	// LineCount is how many lines the order has.
	LineCount int64
	// TotalQuantity is the sum of the lines' quantities.
	TotalQuantity int64
}
