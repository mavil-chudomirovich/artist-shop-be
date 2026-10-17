// Package dto holds the admin module's HTTP request and response shapes, separate
// from the application DTOs so a transport concern cannot leak into a use case.
//
// The member names are the wire contract: money is always an object, growth /
// customer / categoryId are nullable, and the recent order carries both the line
// count and the total quantity.
package dto

import (
	"time"

	"github.com/google/uuid"
)

// MoneyResponse is an integer amount in a currency's minor unit plus its
// currency. It is never a float on the wire (FR-005, SC-007).
type MoneyResponse struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code; VND for the MVP.
	Currency string `json:"currency"`
}

// RevenueKPIResponse is the revenue group of the response.
type RevenueKPIResponse struct {
	// CurrentMonthAmount is the current-window revenue.
	CurrentMonthAmount MoneyResponse `json:"currentMonthAmount"`
	// LastMonthAmount is the comparable previous-window revenue.
	LastMonthAmount MoneyResponse `json:"lastMonthAmount"`
	// GrowthPercent is null when the previous amount is zero.
	GrowthPercent *float64 `json:"growthPercent"`
}

// OrderKPIResponse is the order group of the response.
type OrderKPIResponse struct {
	// TotalOrders is every order ever placed, every state.
	TotalOrders int64 `json:"totalOrders"`
	// ThisMonthOrders counts the current-window orders.
	ThisMonthOrders int64 `json:"thisMonthOrders"`
	// LastMonthOrders counts the comparable previous-window orders.
	LastMonthOrders int64 `json:"lastMonthOrders"`
	// PendingConfirmationCount counts the orders awaiting the artist.
	PendingConfirmationCount int64 `json:"pendingConfirmationCount"`
	// GrowthPercent is null when the previous count is zero.
	GrowthPercent *float64 `json:"growthPercent"`
}

// CustomerKPIResponse is the customer group of the response.
type CustomerKPIResponse struct {
	// TotalUsers counts active customers all-time.
	TotalUsers int64 `json:"totalUsers"`
	// NewUsersThisWeek counts the active newcomers of the week.
	NewUsersThisWeek int64 `json:"newUsersThisWeek"`
}

// InventoryKPIResponse is the inventory group of the response.
type InventoryKPIResponse struct {
	// LowStockCount is the number of low-stock variants.
	LowStockCount int64 `json:"lowStockCount"`
	// Threshold echoes the requested threshold.
	Threshold int64 `json:"threshold"`
}

// KPIResponse groups the four KPI sections.
type KPIResponse struct {
	// Revenue is the revenue group.
	Revenue RevenueKPIResponse `json:"revenue"`
	// Orders is the order group.
	Orders OrderKPIResponse `json:"orders"`
	// Customers is the customer group.
	Customers CustomerKPIResponse `json:"customers"`
	// Inventory is the inventory group.
	Inventory InventoryKPIResponse `json:"inventory"`
}

// RevenuePointResponse is one day of the trend.
type RevenuePointResponse struct {
	// Date is the calendar day as YYYY-MM-DD.
	Date string `json:"date"`
	// DayLabel is the short label for the day.
	DayLabel string `json:"dayLabel"`
	// Amount is that day's revenue.
	Amount MoneyResponse `json:"amount"`
}

// CategoryShareResponse is one category's revenue and share.
type CategoryShareResponse struct {
	// CategoryID is null for an unresolved (legacy) category.
	CategoryID *uuid.UUID `json:"categoryId"`
	// CategoryName is the snapshotted name, or the unresolved label.
	CategoryName string `json:"categoryName"`
	// Amount is the revenue attributed to the category.
	Amount MoneyResponse `json:"amount"`
	// Percentage is the category's share of the window revenue.
	Percentage float64 `json:"percentage"`
}

// CustomerSummaryResponse is the display label of an order's account.
type CustomerSummaryResponse struct {
	// ID identifies the account.
	ID uuid.UUID `json:"id"`
	// DisplayName is empty when the account never set one.
	DisplayName string `json:"displayName"`
	// Email is the account's email.
	Email string `json:"email"`
}

// RecentOrderResponse is the context of one recent order.
type RecentOrderResponse struct {
	// ID identifies the order.
	ID uuid.UUID `json:"id"`
	// Customer is null when the account no longer exists.
	Customer *CustomerSummaryResponse `json:"customer"`
	// ItemSummary describes the first line, or the first line plus a remainder.
	ItemSummary string `json:"itemSummary"`
	// ItemCount is the number of lines.
	ItemCount int64 `json:"itemCount"`
	// ItemQuantity is the sum of the line quantities.
	ItemQuantity int64 `json:"itemQuantity"`
	// Total is the order total.
	Total MoneyResponse `json:"total"`
	// Status is the order's stored state.
	Status string `json:"status"`
	// CreatedAt is when the order was placed.
	CreatedAt time.Time `json:"createdAt"`
}

// OverviewResponse is the dashboard snapshot the response carries.
type OverviewResponse struct {
	// KPIs holds the four KPI groups.
	KPIs KPIResponse `json:"kpis"`
	// RevenueTrend is the daily revenue series, oldest first.
	RevenueTrend []RevenuePointResponse `json:"revenueTrend"`
	// CategoryBreakdown is the current-window revenue by category.
	CategoryBreakdown []CategoryShareResponse `json:"categoryBreakdown"`
	// RecentOrders is the newest orders.
	RecentOrders []RecentOrderResponse `json:"recentOrders"`
}
