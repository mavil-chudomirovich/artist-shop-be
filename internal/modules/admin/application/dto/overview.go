// Package dto defines the admin module's use-case input and output types. They
// are separate from the HTTP shapes: these carry what a use case computes, while
// presentation owns what a response looks like.
package dto

import (
	"time"

	"github.com/google/uuid"
)

// OverviewInput is the validated request the overview use case consumes. Days is
// the trend length; LowStockThreshold is the inventory warning threshold.
type OverviewInput struct {
	// Days is the number of trailing calendar days in the trend.
	Days int
	// LowStockThreshold is the available-quantity ceiling that marks a variant
	// low on stock.
	LowStockThreshold int64
}

// Money is an integer amount in a currency's minor unit plus the currency code.
// It is never a float (Constitution II, FR-005).
type Money struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency the amount is denominated in.
	Currency string
}

// RevenueKPI is the revenue group of the overview (FR-011).
type RevenueKPI struct {
	// Current is the current-window revenue.
	Current Money
	// Last is the comparable previous-window revenue.
	Last Money
	// GrowthPercent is nil when the previous amount is zero.
	GrowthPercent *float64
}

// OrderKPI is the order group of the overview (FR-012 to FR-014).
type OrderKPI struct {
	// Total is every order ever placed.
	Total int64
	// ThisMonth counts the current-window orders.
	ThisMonth int64
	// LastMonth counts the comparable previous-window orders.
	LastMonth int64
	// PendingConfirmation counts the orders awaiting the artist.
	PendingConfirmation int64
	// GrowthPercent is nil when the previous count is zero.
	GrowthPercent *float64
}

// CustomerKPI is the customer group of the overview (FR-015, FR-016).
type CustomerKPI struct {
	// TotalUsers counts active customers all-time.
	TotalUsers int64
	// NewUsersThisWeek counts the active newcomers of the week.
	NewUsersThisWeek int64
}

// InventoryKPI is the inventory group of the overview (FR-018).
type InventoryKPI struct {
	// LowStockCount is the number of low-stock variants.
	LowStockCount int64
	// Threshold echoes the requested threshold.
	Threshold int64
}

// KPIs groups the four KPI sections.
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

// RevenuePoint is one day of the trend, with the day label the response shows
// (FR-019, FR-020).
type RevenuePoint struct {
	// Date is the calendar day as YYYY-MM-DD.
	Date string
	// DayLabel is the short label the response shows for the day.
	DayLabel string
	// Amount is that day's revenue.
	Amount Money
}

// CategoryShare is one category's revenue and share of the window (FR-021,
// FR-024).
type CategoryShare struct {
	// CategoryID is nil for an unresolved (legacy) category.
	CategoryID *uuid.UUID
	// CategoryName is the snapshotted name, or the unresolved label.
	CategoryName string
	// Amount is the revenue attributed to the category.
	Amount Money
	// Percentage is the category's share of the window revenue.
	Percentage float64
}

// CustomerSummary is the display label of an account on a recent order.
type CustomerSummary struct {
	// ID identifies the account.
	ID uuid.UUID
	// DisplayName is empty when the account never set one.
	DisplayName string
	// Email is the account's email.
	Email string
}

// RecentOrder is the context of one recent order, with the summary and counts the
// response shows (FR-026, FR-027).
type RecentOrder struct {
	// ID identifies the order.
	ID uuid.UUID
	// Customer is nil when the account no longer exists.
	Customer *CustomerSummary
	// ItemSummary describes the first line, or the first line plus a remainder.
	ItemSummary string
	// ItemCount is the number of lines.
	ItemCount int64
	// ItemQuantity is the sum of the line quantities.
	ItemQuantity int64
	// Total is the order total.
	Total Money
	// Status is the order's stored state.
	Status string
	// CreatedAt is when the order was placed.
	CreatedAt time.Time
}

// Overview is the whole dashboard snapshot.
type Overview struct {
	// KPIs holds the four KPI groups.
	KPIs KPIs
	// RevenueTrend is the daily revenue series, oldest first.
	RevenueTrend []RevenuePoint
	// CategoryBreakdown is the current-window revenue by category.
	CategoryBreakdown []CategoryShare
	// RecentOrders is the newest orders.
	RecentOrders []RecentOrder
}
