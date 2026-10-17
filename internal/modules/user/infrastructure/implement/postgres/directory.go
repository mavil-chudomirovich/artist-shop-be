package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
)

// DirectoryRepository answers the dashboard's customer read model over the users
// table. It is a separate narrow adapter rather than a method on
// ProfileRepository: it reads aggregate counts and bulk labels, never a profile,
// and it writes no audit event (specs/017-admin-dashboard research D8).
type DirectoryRepository struct {
	pool *pgxpool.Pool
}

// NewDirectoryRepository creates the customer directory repository.
func NewDirectoryRepository(pool *pgxpool.Pool) *DirectoryRepository {
	return &DirectoryRepository{pool: pool}
}

var _ domainrepo.DirectoryRepository = (*DirectoryRepository)(nil)

// querier resolves the transaction-aware querier, so a read joins a caller's
// transaction without this adapter ever opening one itself (Constitution I).
func (r *DirectoryRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// KPIs answers both customer counts in one conditional-aggregate query: active
// CUSTOMER accounts all-time and those created in the given window. ADMIN,
// pending and disabled accounts are excluded by the storage-level predicate, not
// by the caller (FR-015, FR-016).
func (r *DirectoryRepository) KPIs(ctx context.Context, week model.TimeWindow) (model.CustomerKPIs, error) {
	const query = `
		SELECT
			count(*) FILTER (WHERE role = 'CUSTOMER' AND status = 'active')::bigint,
			count(*) FILTER (
				WHERE role = 'CUSTOMER' AND status = 'active'
				  AND created_at >= $1 AND created_at < $2
			)::bigint
		FROM users`
	var kpis model.CustomerKPIs
	if err := r.querier(ctx).QueryRow(ctx, query, week.From, week.To).Scan(
		&kpis.TotalUsers,
		&kpis.NewUsersThisWeek,
	); err != nil {
		return model.CustomerKPIs{}, fmt.Errorf("read customer kpis: %w", err)
	}
	return kpis, nil
}

// SummariesByIDs answers the display label of every requested account that still
// exists, in one bounded bulk read. There is no role/status filter: a disabled
// account still labels its old order, exactly as the dashboard's recent orders
// require. A missing id is absent from the result (FR-026, research D8).
func (r *DirectoryRepository) SummariesByIDs(ctx context.Context, ids []uuid.UUID) ([]model.CustomerSummary, error) {
	if len(ids) == 0 {
		return []model.CustomerSummary{}, nil
	}
	const query = `
		SELECT id, COALESCE(display_name, ''), email
		FROM users
		WHERE id = ANY($1)
		ORDER BY id`
	rows, err := r.querier(ctx).Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("read customer summaries: %w", err)
	}
	defer rows.Close()

	out := make([]model.CustomerSummary, 0, len(ids))
	for rows.Next() {
		var summary model.CustomerSummary
		if err := rows.Scan(&summary.ID, &summary.DisplayName, &summary.Email); err != nil {
			return nil, fmt.Errorf("scan customer summary: %w", err)
		}
		out = append(out, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
