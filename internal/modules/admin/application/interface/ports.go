package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
)

// The admin application consumes the dashboard's read model through these four
// cross-module contracts. The aliases keep this package's vocabulary in one place;
// the contracts themselves are declared in internal/contracts and provided by the
// owning modules at the composition root (Constitution I, research D1).
type (
	// OrderStatistics answers the order and revenue reads.
	OrderStatistics = contracts.OrderStatistics
	// CustomerStatistics answers the customer counts and summaries.
	CustomerStatistics = contracts.CustomerStatistics
	// InventoryStatistics answers the low-stock candidates.
	InventoryStatistics = contracts.InventoryStatistics
	// ProductVariantStates answers the DISCONTINUED subset.
	ProductVariantStates = contracts.ProductVariantStates
	// Money is an integer minor-unit amount plus currency.
	Money = contracts.Money
	// TimeWindow is a half-open interval of storage instants.
	TimeWindow = contracts.TimeWindow
)

// Clock reads the current instant. The dashboard reads it exactly once per request
// so every window, the trend's "today" and the audit agree on "now".
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Auditor records the dashboard's single view event. It carries no PII.
type Auditor interface {
	// Record emits one audit event. Emission never blocks the request and never
	// fails it.
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}
