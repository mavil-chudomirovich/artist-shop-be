//go:build integration

package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	adminimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	adminmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/mapper"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	inventorystatistics "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/statistics"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	orderstatistics "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/statistics"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/variantstate"
	userimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	userpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file is the DB-backed half of SC-006 / FR-032 (T066b). It wires the real
// order, user, inventory and product adapters the composition root wires, seeds
// 10,000 orders and 1,000 variants, then measures the whole overview against the
// spec's 2-second target. The unit-level call-count assertion lives in
// internal/modules/admin/application/implement/overview_test.go; this file proves
// the wall-clock half on a real PostgreSQL. Docker is required; the file still
// compiles without a container under `go vet -tags integration`.

const (
	scenarioOrderCount    = 10_000
	scenarioVariantCount  = 1_000
	scenarioCustomerCount = 500
	scenarioTarget        = 2 * time.Second
)

// fixedClock is the dashboard clock the integration fixture injects; it is a real
// instant so the seeded rows fall inside the current windows.
type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

// noopAuditor satisfies the admin auditor port without a shared audit writer; the
// 10k fixture cares about read latency, not the audit record.
type noopAuditor struct{}

func (noopAuditor) Record(_ context.Context, _, _ string, _ *uuid.UUID, _, _, _ string, _ map[string]any) {
}

// dashboardFixture is one migrated PostgreSQL container plus the admin service
// assembled from the real adapters, exactly as cmd/api/main.go does.
type dashboardFixture struct {
	pool    *pgxpool.Pool
	service *adminimplement.Service
	now     time.Time
}

func newDashboardFixture(t *testing.T) *dashboardFixture {
	t.Helper()
	ctx := context.Background()
	dsn := testsupport.PostgresDSN(t)

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load the shop timezone: %v", err)
	}
	now := time.Now().UTC()

	orderRepo := orderpostgres.NewOrderRepository(pool)
	userService := userimplement.New(userimplement.Service{
		Directory: userpostgres.NewDirectoryRepository(pool),
	})
	service := adminimplement.New(adminimplement.Service{
		Orders:    orderstatistics.New(orderRepo),
		Customers: userService,
		Inventory: inventorystatistics.New(inventorypostgres.NewInventoryRepository(pool), fixedClock{now: now}),
		Variants:  variantstate.New(productpostgres.NewVariantRepository(pool)),
		Clock:     fixedClock{now: now},
		Auditor:   noopAuditor{},
		Config:    appinterface.Config{Location: loc},
		Mapper:    adminmapper.New(),
	})
	return &dashboardFixture{pool: pool, service: service, now: now}
}

// seed writes one category, 1,000 products with one variant each, a stock row per
// variant, 500 active customers and 10,000 orders with one line each, spread over
// the last 40 days so both month windows and the trend have work to do.
func (f *dashboardFixture) seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	categoryID := uuid.New()

	if _, err := f.pool.Exec(ctx,
		`INSERT INTO categories (id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		 VALUES ($1, 'Category', 'category', 'category', 'category', '', 0, true)`, categoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`INSERT INTO products (id, name, description, price_amount, currency, category_id, position, code)
		 SELECT gen_random_uuid(), 'Product ' || g, '', 1000, 'VND', $1, g, 'p-' || g
		 FROM generate_series(1, $2::int) g`, categoryID, scenarioVariantCount); err != nil {
		t.Fatalf("seed products: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO product_variants (id, product_id, name, sku, position, sell_state)
		 SELECT gen_random_uuid(), p.id, 'Variant', 'sku-' || p.code, 0, 'ACTIVE'
		 FROM products p`); err != nil {
		t.Fatalf("seed variants: %v", err)
	}
	// Every variant gets a stock row with quantity 1, so all 1,000 are candidates
	// below threshold 5 — the worst case for the low-stock path.
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO stock_levels (variant_id, quantity) SELECT id, 1 FROM product_variants`); err != nil {
		t.Fatalf("seed stock levels: %v", err)
	}
	// A tenth of them are discontinued, so the admin must filter them after the
	// product read.
	if _, err := f.pool.Exec(ctx,
		`UPDATE product_variants SET sell_state = 'DISCONTINUED' WHERE abs(hashtext(id::text)) % 10 = 0`); err != nil {
		t.Fatalf("seed discontinued variants: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`INSERT INTO users (email, password_hash, role, status, display_name, created_at)
		 SELECT 'cust' || g || '@example.com', 'x', 'CUSTOMER', 'active', 'Customer ' || g, $1
		 FROM generate_series(1, $2::int) g`, f.now, scenarioCustomerCount); err != nil {
		t.Fatalf("seed customers: %v", err)
	}

	// Orders are addressed by a deterministic UUID derived from g so the line
	// insert can reference them without a second round trip.
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO orders (
		     id, user_id, status, total_amount, currency,
		     recipient_name, recipient_phone, province_code, province_name,
		     ward_code, ward_name, street_address, created_at, updated_at
		 )
		 SELECT ('00000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		        gen_random_uuid(),
		        CASE WHEN g % 3 = 0 THEN 'PENDING' WHEN g % 3 = 1 THEN 'SHIPPING' ELSE 'PAID' END,
		        1000, 'VND', 'A', '0912345678', '01', 'Ha Noi', '00001', 'Phuc Xa', '1 street',
		        ($1::timestamptz - (g % 40) * interval '1 day'), $1::timestamptz
		 FROM generate_series(1, $2::int) g`, f.now, scenarioOrderCount); err != nil {
		t.Fatalf("seed orders: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO order_items (
		     id, order_id, product_id, name, unit_price_amount, currency, quantity, position,
		     created_at, variant_id, product_code, variant_name, sku, category_id, category_name
		 )
		 SELECT gen_random_uuid(),
		        ('00000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		        gen_random_uuid(), 'Product', 1000, 'VND', 1, 0,
		        $1::timestamptz, gen_random_uuid(), 'p-' || g, 'Variant', 'sku-' || g, $3, 'Category'
		 FROM generate_series(1, $2::int) g`, f.now, scenarioOrderCount, categoryID); err != nil {
		t.Fatalf("seed order items: %v", err)
	}
}

// rowCount is a tiny helper that proves the seed reached the promised volume.
func (f *dashboardFixture) rowCount(t *testing.T, table string) int {
	t.Helper()
	var count int
	// table is a compile-time constant in this file, never user input.
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// SC-006, FR-032, T066b: the whole overview over 10,000 orders and 1,000 variants
// completes within the 2-second target, and its response still carries every
// section. The measured duration is recorded either way.
func TestOverviewMeetsTheTwoSecondTargetAtScale(t *testing.T) {
	f := newDashboardFixture(t)
	f.seed(t)

	if got := f.rowCount(t, "orders"); got != scenarioOrderCount {
		t.Fatalf("seed: expected %d orders, got %d", scenarioOrderCount, got)
	}
	if got := f.rowCount(t, "product_variants"); got != scenarioVariantCount {
		t.Fatalf("seed: expected %d variants, got %d", scenarioVariantCount, got)
	}

	ctx := appinterface.WithActor(context.Background(), appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin})

	start := time.Now()
	out, err := f.service.Overview(ctx, appdto.OverviewInput{Days: 30, LowStockThreshold: 5})
	elapsed := time.Since(start)
	t.Logf("SC-006: full overview over %d orders / %d variants took %s (target < %s)",
		scenarioOrderCount, scenarioVariantCount, elapsed, scenarioTarget)

	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if elapsed >= scenarioTarget {
		t.Errorf("SC-006 violated: overview took %s, target is under %s", elapsed, scenarioTarget)
	}

	// The response is still complete at scale.
	if len(out.RevenueTrend) != 30 {
		t.Fatalf("expected a 30-day trend, got %d points", len(out.RevenueTrend))
	}
	if out.KPIs.Orders.Total != int64(scenarioOrderCount) {
		t.Fatalf("totalOrders = %d, want %d", out.KPIs.Orders.Total, scenarioOrderCount)
	}
	if out.KPIs.Inventory.Threshold != 5 {
		t.Fatalf("threshold echo = %d, want 5", out.KPIs.Inventory.Threshold)
	}
	if len(out.RecentOrders) > 5 {
		t.Fatalf("recent orders must be at most 5, got %d", len(out.RecentOrders))
	}
	if out.KPIs.Revenue.Current.Currency != "VND" {
		t.Fatalf("revenue currency = %q, want VND", out.KPIs.Revenue.Current.Currency)
	}
	// Low stock excludes the discontinued tenth; the read always returns the
	// candidate set, and the count must never exceed the variant total.
	if out.KPIs.Inventory.LowStockCount < 0 || out.KPIs.Inventory.LowStockCount > scenarioVariantCount {
		t.Fatalf("lowStockCount = %d, want 0..%d", out.KPIs.Inventory.LowStockCount, scenarioVariantCount)
	}
}

// The four read contracts are the only inputs; the empty fixture (migrations
// applied, nothing seeded) must still answer a valid zero response rather than an
// error, which is the DB-level counterpart of the unit empty-data test.
func TestOverviewOnAnEmptyDatabaseAnswersZeroes(t *testing.T) {
	f := newDashboardFixture(t)
	ctx := appinterface.WithActor(context.Background(), appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin})

	out, err := f.service.Overview(ctx, appdto.OverviewInput{Days: 7, LowStockThreshold: 5})
	if err != nil {
		t.Fatalf("Overview on empty database: %v", err)
	}
	if out.KPIs.Orders.Total != 0 || out.KPIs.Customers.TotalUsers != 0 {
		t.Fatalf("empty database must answer zero KPIs, got %+v", out.KPIs)
	}
	if len(out.RevenueTrend) != 7 {
		t.Fatalf("empty trend must still be zero-filled to 7, got %d", len(out.RevenueTrend))
	}
	if len(out.CategoryBreakdown) != 0 || len(out.RecentOrders) != 0 {
		t.Fatalf("empty sections must be empty, got categories=%d recent=%d", len(out.CategoryBreakdown), len(out.RecentOrders))
	}
}

// A compile-time guard that the service the fixture builds still satisfies the
// use-case interface the handler consumes.
var _ appinterface.OverviewService = (*adminimplement.Service)(nil)
