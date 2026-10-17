package model

import (
	"time"

	"github.com/google/uuid"
)

// This file holds the user module's narrow read model for the admin dashboard. It
// is a projection over the accounts table, never a stored entity: the dashboard
// reads only the counts and summaries it needs, with no per-customer audit
// (research D8, Constitution I).

// TimeWindow is one half-open interval of storage instants: From is inclusive and
// To is exclusive. The caller builds both endpoints in the shop timezone, so a
// calendar boundary is the operator's boundary while the instants themselves stay
// UTC (FR-007, FR-009).
type TimeWindow struct {
	// From is the inclusive start of the window.
	From time.Time
	// To is the exclusive end of the window.
	To time.Time
}

// CustomerKPIs is the customer module's answer to the dashboard's customer counts
// (FR-015, FR-016).
type CustomerKPIs struct {
	// TotalUsers counts active CUSTOMER accounts, all-time.
	TotalUsers int64
	// NewUsersThisWeek counts active CUSTOMER accounts created in the window.
	NewUsersThisWeek int64
}

// CustomerSummary is the display label of an account, resolved for a recent
// order. DisplayName is empty when the account never set one; an account that no
// longer exists is absent from the read (FR-026).
type CustomerSummary struct {
	// ID identifies the account.
	ID uuid.UUID
	// DisplayName is the account's display name, empty when unset.
	DisplayName string
	// Email is the account's email.
	Email string
}
