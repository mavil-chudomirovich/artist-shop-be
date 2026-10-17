package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// This file implements contracts.CustomerStatistics directly on the user service,
// as the module already implements CustomerLookupService. The dashboard reads the
// customer section through these two bounded reads, and the user module supplies
// the adapter at the composition root (specs/017-admin-dashboard research D8,
// Constitution I).
//
// Unlike LookupCustomer, these methods write no audit event: the dashboard records
// one ADMIN_DASHBOARD_VIEWED per request, and a per-customer audit would fill the
// trail with five PII reads on every refresh (FR-030, research D11).

// KPIs answers the dashboard's two customer counts in one read (FR-015, FR-016).
func (s *Service) KPIs(ctx context.Context, week contracts.TimeWindow) (contracts.CustomerKPIs, error) {
	kpis, err := s.Directory.KPIs(ctx, model.TimeWindow{From: week.From, To: week.To})
	if err != nil {
		return contracts.CustomerKPIs{}, err
	}
	return contracts.CustomerKPIs{
		TotalUsers:       kpis.TotalUsers,
		NewUsersThisWeek: kpis.NewUsersThisWeek,
	}, nil
}

// CustomerSummaries answers the display label of the requested accounts in one
// bulk read. An id no account carries is absent, which the admin maps to a nil
// customer on the recent order (FR-026, research D8).
func (s *Service) CustomerSummaries(ctx context.Context, ids []uuid.UUID) ([]contracts.CustomerSummary, error) {
	summaries, err := s.Directory.SummariesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.CustomerSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, contracts.CustomerSummary{
			ID:          summary.ID,
			DisplayName: summary.DisplayName,
			Email:       summary.Email,
		})
	}
	return out, nil
}

// The service must satisfy the cross-module contract. The assertion lives in the
// module's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module (Constitution I).
var _ contracts.CustomerStatistics = (*Service)(nil)
