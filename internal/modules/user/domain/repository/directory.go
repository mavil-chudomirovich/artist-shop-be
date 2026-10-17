package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// DirectoryRepository is the narrow customer read port behind the cross-module
// CustomerStatistics contract (specs/017-admin-dashboard research D8). It is
// deliberately separate from ProfileRepository: the dashboard needs aggregate
// counts and a bulk display label, not a profile, and it must not perform the
// audited per-customer lookup the operator surface uses.
//
// No method opens a transaction: every read joins the transaction the context
// already carries (Constitution I). Summary reads carry no role/status filter, so a
// disabled account still labels its old order; a missing id is simply absent.
type DirectoryRepository interface {
	// KPIs answers both customer counts in one read: active CUSTOMER accounts
	// all-time and active CUSTOMER accounts created in the given window
	// (FR-015, FR-016).
	KPIs(ctx context.Context, week model.TimeWindow) (model.CustomerKPIs, error)

	// SummariesByIDs answers the display label of every requested account that
	// still exists, in one bounded bulk read. An id no account carries is absent
	// from the result rather than an error (FR-026).
	SummariesByIDs(ctx context.Context, ids []uuid.UUID) ([]model.CustomerSummary, error)
}
