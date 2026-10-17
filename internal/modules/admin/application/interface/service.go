package appinterface

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
)

// OverviewService is the admin module's use-case surface. It has exactly one
// operation: assemble the whole dashboard in one read-only request. The acting
// administrator is taken from the context the session filled (ActorFromContext),
// never from the request.
type OverviewService interface {
	// Overview assembles the dashboard snapshot for the acting administrator and
	// records the single ADMIN_DASHBOARD_VIEWED audit event (FR-002, FR-030).
	Overview(ctx context.Context, in dto.OverviewInput) (dto.Overview, error)
}
