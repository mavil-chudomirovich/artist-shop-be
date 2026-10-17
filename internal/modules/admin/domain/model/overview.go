package model

import (
	"time"

	"github.com/google/uuid"
)

// This file holds the admin module's read models for the dashboard overview. They
// are assembled in memory from the cross-module read contracts and are never
// stored: the admin module owns no table (FR-028, Constitution I). It imports
// only the standard library and github.com/google/uuid.

// Money is an integer amount in a currency's minor unit plus the currency code.
// It is never a float, so a total is exact at every layer (Constitution II,
// FR-005).
type Money struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency the amount is denominated in.
	Currency string
}

// RevenueKPI is the revenue group: the current-month figure, the comparable
// previous-month figure and their growth. Growth is a pointer because a zero base
// has no meaningful percentage (FR-011).
type RevenueKPI struct {
	// Current is the revenue of the current window.
	Current Money
	// Last is the revenue of the comparable previous window.
	Last Money
	// GrowthPercent is (Current-Last)/Last*100, or nil when Last is zero.
	GrowthPercent *float64
}

// OrderKPI is the order group: all-time and per-window counts, the awaiting-
// confirmation queue and the window growth (FR-012 to FR-014).
type OrderKPI struct {
	// Total is every order ever placed, every state.
	Total int64
	// ThisMonth counts orders created in the current window, every state.
	ThisMonth int64
	// LastMonth counts orders created in the comparable previous window.
	LastMonth int64
	// PendingConfirmation counts orders awaiting the artist, all-time.
	PendingConfirmation int64
	// GrowthPercent is ThisMonth vs LastMonth, or nil when LastMonth is zero.
	GrowthPercent *float64
}

// CustomerKPI is the customer group: active customers all-time and the active
// newcomers of the current week (FR-015, FR-016).
type CustomerKPI struct {
	// TotalUsers counts active CUSTOMER accounts, all-time.
	TotalUsers int64
	// NewUsersThisWeek counts active CUSTOMER accounts created this week.
	NewUsersThisWeek int64
}

// InventoryKPI is the inventory group: how many variants are low on stock and the
// threshold that produced that count (FR-018).
type InventoryKPI struct {
	// LowStockCount is the number of variants below the threshold.
	LowStockCount int64
	// Threshold echoes the requested threshold.
	Threshold int64
}

// KPIs groups the four KPI sections of the overview.
type KPIs struct {
	// Revenue is the revenue group.
	Revenue RevenueKPI
	// Orders is the order group.
	Orders OrderKPI
	// Customers is the customer group.
	Customers CustomerKPI
	// Inventory is the inventory group.
	Inventory InventoryKPI
}

// RevenuePoint is one day of the trend. The day label the response shows is a
// presentation concern the mapper derives; the read model carries the date and
// the money (FR-019).
type RevenuePoint struct {
	// Date is the calendar day in the shop timezone, as YYYY-MM-DD.
	Date string
	// Amount is that day's revenue.
	Amount Money
}

// CategoryShare is one category's revenue in the current window. CategoryID is
// nil for a line whose category could not be resolved — the bucket the mapper
// names "Khác" (FR-021, FR-023).
type CategoryShare struct {
	// CategoryID is the snapshotted category, or nil when unresolved.
	CategoryID *uuid.UUID
	// CategoryName is the snapshotted name, empty when unresolved.
	CategoryName string
	// Amount is the revenue attributed to the category.
	Amount Money
	// Percentage is the category's share of the window's revenue.
	Percentage float64
}

// CustomerSummary is the label of an account resolved for a recent order.
// DisplayName is empty when the account never set one.
type CustomerSummary struct {
	// ID identifies the account.
	ID uuid.UUID
	// DisplayName is the account's display name, empty when unset.
	DisplayName string
	// Email is the account's email.
	Email string
}

// RecentOrder is the context of one of the newest orders. The line facts are
// carried raw so the mapper owns the summary wording (FR-025 to FR-027); the
// customer is nil when the account no longer exists.
type RecentOrder struct {
	// ID identifies the order.
	ID uuid.UUID
	// Customer is the resolved account, or nil when it no longer exists.
	Customer *CustomerSummary
	// Total is the order total.
	Total Money
	// Status is the order's stored state.
	Status string
	// CreatedAt is when the order was placed.
	CreatedAt time.Time
	// FirstLineName is the first line's product-name snapshot, by position.
	FirstLineName string
	// FirstLineVariantName is the first line's variant-name snapshot, empty for
	// a legacy tombstone line.
	FirstLineVariantName string
	// FirstLineQuantity is the first line's quantity.
	FirstLineQuantity int64
	// LineCount is how many lines the order has.
	LineCount int64
	// TotalQuantity is the sum of the lines' quantities.
	TotalQuantity int64
}

// Overview is the whole dashboard snapshot: the four KPI groups, the trend, the
// category breakdown and the newest orders, assembled from the read contracts in
// one request (FR-002).
type Overview struct {
	// KPIs holds the four KPI groups.
	KPIs KPIs
	// RevenueTrend is the daily revenue series, oldest first.
	RevenueTrend []RevenuePoint
	// CategoryBreakdown is current-window revenue by category.
	CategoryBreakdown []CategoryShare
	// RecentOrders is the newest orders.
	RecentOrders []RecentOrder
}
