package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// This file publishes the read contracts the admin dashboard consumes. The admin
// module owns no data: it orchestrates a small, bounded set of reads, and each
// read is owned and implemented by the module that owns the table
// (Constitution I, FR-028). The DTOs here are pure data — no module internals
// cross the boundary (docs/system-design/contract-purity.md).

// TimeWindow is a half-open interval of storage instants: From is inclusive and
// To is exclusive. Callers build both endpoints in the shop timezone so a
// calendar day, week or month boundary is the operator's boundary, while the
// instants themselves stay UTC (FR-007, FR-008).
type TimeWindow struct {
	// From is the inclusive start of the window.
	From time.Time
	// To is the exclusive end of the window.
	To time.Time
}

// Money is an integer amount in a currency's minor unit plus the currency code.
// Money is never a float, so a total is exact at every layer (Constitution II,
// FR-005).
type Money struct {
	// Amount is the amount in the currency's minor unit. Never a float.
	Amount int64
	// Currency is the currency code the amount is denominated in.
	Currency string
}

// OrderKPIs answers the order and revenue figures the dashboard shows, in one
// read. Revenue counts only the orders that have actually earned money; the
// order counts include every state. The adapter sets Currency, so the
// application never invents one (FR-010, FR-012, FR-013, FR-014).
type OrderKPIs struct {
	// CurrentRevenue is the revenue of the current window.
	CurrentRevenue Money
	// LastRevenue is the revenue of the previous, comparable window.
	LastRevenue Money
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
	Amount Money
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
	Amount Money
}

// RecentOrder is the context the dashboard shows for one of the newest orders:
// its money, state and line summary, without resolving the customer (the
// application reads customer summaries separately, in one bulk call).
type RecentOrder struct {
	// ID identifies the order.
	ID uuid.UUID
	// CustomerID is the account that placed the order. It stays a loose
	// reference: the account may no longer exist.
	CustomerID uuid.UUID
	// Total is the order total.
	Total Money
	// Status is the order's stored state.
	Status string
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

// OrderStatistics answers the dashboard's order and revenue reads. The order
// module supplies the adapter at the composition root; each method is one
// bounded query, so the dashboard's read count does not grow with the number of
// orders (FR-032, Constitution I).
type OrderStatistics interface {
	// KPIs answers every order and revenue KPI in one read.
	KPIs(ctx context.Context, current, last TimeWindow) (OrderKPIs, error)
	// DailyRevenue answers the window's revenue per shop-timezone day.
	DailyRevenue(ctx context.Context, window TimeWindow, timeZone string) ([]DailyRevenue, error)
	// CategoryRevenue answers the window's revenue grouped by the category
	// snapshotted on each order line.
	CategoryRevenue(ctx context.Context, window TimeWindow) ([]CategoryRevenue, error)
	// RecentOrders answers at most limit newest orders, newest first.
	RecentOrders(ctx context.Context, limit int) ([]RecentOrder, error)
}

// CustomerKPIs answers the customer counts the dashboard shows, in one read
// (FR-015, FR-016).
type CustomerKPIs struct {
	// TotalUsers counts active CUSTOMER accounts, all-time.
	TotalUsers int64
	// NewUsersThisWeek counts active CUSTOMER accounts created in the window.
	NewUsersThisWeek int64
}

// CustomerSummary is the display label of an account, resolved for a recent
// order. DisplayName is empty when the account never set one; the account is
// absent from a read only when it no longer exists (FR-026).
type CustomerSummary struct {
	// ID identifies the account.
	ID uuid.UUID
	// DisplayName is the account's display name, empty when unset.
	DisplayName string
	// Email is the account's email.
	Email string
}

// CustomerStatistics answers the dashboard's customer reads. The user module
// supplies the adapter at the composition root (FR-028, Constitution I).
type CustomerStatistics interface {
	// KPIs answers both customer counts in one read.
	KPIs(ctx context.Context, week TimeWindow) (CustomerKPIs, error)
	// CustomerSummaries answers the summaries of the requested accounts, in one
	// bounded bulk read. An id no account carries is absent from the result.
	CustomerSummaries(ctx context.Context, ids []uuid.UUID) ([]CustomerSummary, error)
}

// LowStockVariant is a variant whose available stock falls below the threshold.
// Available is the physical stock minus the units held by active, unexpired
// holds, floored at zero (FR-017).
type LowStockVariant struct {
	// VariantID identifies the variant.
	VariantID uuid.UUID
	// Available is the sellable quantity after active holds.
	Available int64
}

// InventoryStatistics answers the dashboard's low-stock read. The inventory
// module supplies the adapter, using its own clock to decide which holds are
// still live (FR-017, FR-018, Constitution I).
type InventoryStatistics interface {
	// LowStockVariants answers every variant with a stock record whose available
	// quantity is below the threshold.
	LowStockVariants(ctx context.Context, threshold int64) ([]LowStockVariant, error)
}

// ProductVariantStates answers the state of a set of variants. The product
// module supplies the adapter, so the dashboard can drop discontinued variants
// without reading module 04's table (FR-018, Constitution I).
type ProductVariantStates interface {
	// DiscontinuedVariantIDs returns the subset of ids whose variant is
	// DISCONTINUED. An empty input answers an empty slice without a query.
	DiscontinuedVariantIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}
