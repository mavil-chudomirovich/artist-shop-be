//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	authtoken "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	carthideguard "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/hideguard"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	categoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	categorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	categoryauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/auditor"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	orderhideguard "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/hideguard"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	producthideguard "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/hideguard"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives the administrator surface through a whole lifecycle against
// real PostgreSQL: real migrations, the real repository adapter, the real audit
// writer and real access tokens from the auth module. After each step it checks
// what the public surface then shows and that the write left its audit row.

const (
	// categoryIntegrationSecret is a throwaway HS256 key for the test tokens.
	categoryIntegrationSecret = "0123456789abcdef0123456789abcdef"
	// categoryIntegrationTokenTTL is long enough that no token expires mid-test.
	categoryIntegrationTokenTTL = 15 * time.Minute
)

// maintenanceFixture is the module wired against real infrastructure.
type maintenanceFixture struct {
	handler       http.Handler
	pool          *pgxpool.Pool
	writer        *audit.Writer
	repo          *categorypostgres.CategoryRepository
	adminID       uuid.UUID
	customerID    uuid.UUID
	adminToken    string
	customerToken string
}

func newMaintenanceFixture(t *testing.T) *maintenanceFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

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

	writer := audit.NewWriter(audit.NewRepository(pool), testLogger, 64, 1, 1)
	writer.Start(ctx)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Stop(stopCtx)
	})

	repo := categorypostgres.NewCategoryRepository(pool)
	service := categoryimplement.New(categoryimplement.Service{
		Categories: repo,
		Audit:      categoryauditor.New(writer),
		Mapper:     categorymapper.New(),
	})
	handler := New(service, testLogger)

	adminID := seedCategoryAccount(t, pool, "catalogue-admin@example.com", access.RoleAdmin)
	customerID := seedCategoryAccount(t, pool, "catalogue-customer@example.com", access.RoleCustomer)

	hooks := categoryAuthHooks(t)
	root := chi.NewRouter()
	root.Mount(publicCategoriesPath, handler.Router(middleware.AuthHooks{}))
	root.Mount(adminCategoriesPath, handler.AdminRouter(hooks))

	adminToken, _, err := authtoken.NewAccessIssuer(categoryIntegrationSecret, categoryIntegrationTokenTTL).Issue(adminID, access.RoleAdmin)
	if err != nil {
		t.Fatalf("issue the administrator token: %v", err)
	}
	customerToken, _, err := authtoken.NewAccessIssuer(categoryIntegrationSecret, categoryIntegrationTokenTTL).Issue(customerID, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue the customer token: %v", err)
	}

	return &maintenanceFixture{
		handler:       root,
		pool:          pool,
		writer:        writer,
		repo:          repo,
		adminID:       adminID,
		customerID:    customerID,
		adminToken:    adminToken,
		customerToken: customerToken,
	}
}

// categoryAuthHooks verifies the bearer token with the auth module's own issuer
// and maps its failures the same way the composition root does, so this test sees
// the real 401 a tampered token produces.
func categoryAuthHooks(t *testing.T) middleware.AuthHooks {
	t.Helper()
	issuer := authtoken.NewAccessIssuer(categoryIntegrationSecret, categoryIntegrationTokenTTL)
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return nil, nil
			}
			claims, err := issuer.Parse(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
			if err != nil {
				if errors.Is(err, authdomainerr.ErrExpiredToken) {
					return nil, &httpx.AppError{Code: "AUTH_TOKEN_EXPIRED", Status: http.StatusUnauthorized, Message: "Access token has expired"}
				}
				return nil, &httpx.AppError{Code: "AUTH_TOKEN_INVALID", Status: http.StatusUnauthorized, Message: "Access token is not valid"}
			}
			return &middleware.Identity{Subject: claims.Subject.String(), Role: string(claims.Role), TokenID: claims.ID}, nil
		},
	}
}

// seedCategoryAccount inserts an account the way the auth module does, so the
// token this test issues names a real account.
func seedCategoryAccount(t *testing.T, pool *pgxpool.Pool, email string, role access.Role) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, $2, 'not-used-by-this-test', $3, 'active')`
	if _, err := pool.Exec(context.Background(), query, id, email, string(role)); err != nil {
		t.Fatalf("seed account %s: %v", email, err)
	}
	return id
}

func (f *maintenanceFixture) call(method, path, body, token string) *httptest.ResponseRecorder {
	return performJSON(f.handler, method, path, body, token)
}

// publicList reads the public catalogue and returns the slugs it shows, so a test
// asserts what a customer sees after each write.
func (f *maintenanceFixture) publicList(t *testing.T) []string {
	t.Helper()
	rec := f.call(http.MethodGet, publicCategoriesPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCatalogueList(t, rec)
	out := make([]string, 0, len(body.Data))
	for _, entry := range body.Data {
		out = append(out, entry.Slug)
	}
	return out
}

func (f *maintenanceFixture) categoryRowCount(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM categories WHERE id = $1", id).Scan(&count); err != nil {
		t.Fatalf("count the category row: %v", err)
	}
	return count
}

// maintenanceAuditRow is one audit_logs row the module wrote.
type maintenanceAuditRow struct {
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	outcome    string
}

// waitForAuditRows waits until the given action has the expected number of
// persisted rows, so the assertion cannot race the asynchronous writer.
func (f *maintenanceFixture) waitForAuditRows(t *testing.T, action string, want int) []maintenanceAuditRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := f.auditRows(t, action)
		if len(rows) == want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d %s rows, got %d", want, action, len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *maintenanceFixture) auditRows(t *testing.T, action string) []maintenanceAuditRow {
	t.Helper()
	const query = `
		SELECT actor_id, actor_role, target_type, target_id, outcome
		FROM audit_logs WHERE action = $1 ORDER BY occurred_at`
	rows, err := f.pool.Query(context.Background(), query, action)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	defer rows.Close()
	var events []maintenanceAuditRow
	for rows.Next() {
		var event maintenanceAuditRow
		if err := rows.Scan(&event.actorID, &event.actorRole, &event.targetType, &event.targetID, &event.outcome); err != nil {
			t.Fatalf("scan an audit row: %v", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the audit trail: %v", err)
	}
	return events
}

// requireAudit asserts the one row of an action names the administrator, the
// category and a SUCCESS outcome.
func (f *maintenanceFixture) requireAudit(t *testing.T, action string, categoryID uuid.UUID) {
	t.Helper()
	rows := f.waitForAuditRows(t, action, 1)
	row := rows[0]
	if row.actorID == nil || *row.actorID != f.adminID {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, f.adminID, row.actorID)
	}
	if row.actorRole != string(access.RoleAdmin) {
		t.Fatalf("%s: expected the ADMIN role, got %q", action, row.actorRole)
	}
	if row.targetType != "category" || row.targetID != categoryID.String() {
		t.Fatalf("%s: expected the category as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// FR-008 to FR-013, FR-024, SC-005 against real PostgreSQL: create, edit, hide,
// show and remove, checking after each step what the public list shows and that
// the write left its audit row. A hidden category stays readable by the
// administrator while invisible to a customer.
func TestCategoryMaintenanceLifecycleAgainstPostgres(t *testing.T) {
	f := newMaintenanceFixture(t)

	// Create.
	createRec := f.call(http.MethodPost, adminCategoriesPath,
		`{"name":"Tranh sơn dầu","slug":"tranh-son-dau","description":"Tranh","position":1}`, f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeAdminCategory(t, createRec)
	if !created.IsVisible || created.Position != 1 || created.ID == uuid.Nil {
		t.Fatalf("unexpected created category: %+v", created)
	}
	if got := f.publicList(t); len(got) != 1 || got[0] != "tranh-son-dau" {
		t.Fatalf("after create the public list must show the category, got %v", got)
	}
	if f.categoryRowCount(t, created.ID) != 1 {
		t.Fatal("the created row must be persisted")
	}
	f.requireAudit(t, constant.AuditCategoryCreated, created.ID)

	// Edit: name, description and position.
	editRec := f.call(http.MethodPatch, adminCategoriesPath+"/"+created.ID.String(),
		`{"name":"Tranh sơn dầu mới","description":"Mô tả mới","position":5}`, f.adminToken)
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", editRec.Code, editRec.Body.String())
	}
	edited := decodeAdminCategory(t, editRec)
	if edited.Name != "Tranh sơn dầu mới" || edited.Description != "Mô tả mới" || edited.Position != 5 {
		t.Fatalf("the edit did not change every content field: %+v", edited)
	}
	// The customer-facing view reflects the change on the next request.
	rec := f.call(http.MethodGet, publicCategoriesPath+"/tranh-son-dau", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tranh sơn dầu mới") {
		t.Fatalf("the public detail must reflect the edit, got %d (%s)", rec.Code, rec.Body.String())
	}
	f.requireAudit(t, constant.AuditCategoryUpdated, created.ID)

	// Hide: it disappears from the customer list and the public detail, but the
	// administrator still reads it (FR-009).
	hideRec := f.call(http.MethodPatch, adminCategoriesPath+"/"+created.ID.String(), `{"isVisible":false}`, f.adminToken)
	if hideRec.Code != http.StatusOK {
		t.Fatalf("hide: expected 200, got %d (%s)", hideRec.Code, hideRec.Body.String())
	}
	if decodeAdminCategory(t, hideRec).IsVisible {
		t.Fatal("the category must be hidden")
	}
	if got := f.publicList(t); len(got) != 0 {
		t.Fatalf("a hidden category must be absent from the public list, got %v", got)
	}
	if rec := f.call(http.MethodGet, publicCategoriesPath+"/tranh-son-dau", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a hidden slug must answer 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	adminRead := f.call(http.MethodGet, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)
	if adminRead.Code != http.StatusOK {
		t.Fatalf("the administrator must still read a hidden category, got %d (%s)", adminRead.Code, adminRead.Body.String())
	}
	if decodeAdminCategory(t, adminRead).IsVisible {
		t.Fatal("the administrator read must report the category as hidden")
	}
	f.requireAudit(t, constant.AuditCategoryHidden, created.ID)

	// Show: it reappears in the public list.
	showRec := f.call(http.MethodPatch, adminCategoriesPath+"/"+created.ID.String(), `{"isVisible":true}`, f.adminToken)
	if showRec.Code != http.StatusOK {
		t.Fatalf("show: expected 200, got %d (%s)", showRec.Code, showRec.Body.String())
	}
	if !decodeAdminCategory(t, showRec).IsVisible {
		t.Fatal("the category must be visible again")
	}
	if got := f.publicList(t); len(got) != 1 || got[0] != "tranh-son-dau" {
		t.Fatalf("the shown category must reappear in the public list, got %v", got)
	}
	f.requireAudit(t, constant.AuditCategoryShown, created.ID)

	// Remove: the row goes, the catalogue has no trace of it, and the audit entry
	// stays (research D5).
	deleteRec := f.call(http.MethodDelete, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d (%s)", deleteRec.Code, deleteRec.Body.String())
	}
	if f.categoryRowCount(t, created.ID) != 0 {
		t.Fatal("the removed row must be gone")
	}
	if got := f.publicList(t); len(got) != 0 {
		t.Fatalf("a removed category must leave no trace, got %v", got)
	}
	if rec := f.call(http.MethodGet, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken); rec.Code != http.StatusNotFound {
		t.Fatalf("the removed category must be unreadable, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeAdminError(t, f.call(http.MethodGet, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)); body.Error.Code != "CATEGORY_NOT_FOUND" {
		t.Fatalf("expected CATEGORY_NOT_FOUND, got %s", body.Error.Code)
	}
	f.requireAudit(t, constant.AuditCategoryDeleted, created.ID)

	// A second removal is the same not-found rather than an internal failure.
	again := f.call(http.MethodDelete, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)
	if again.Code != http.StatusNotFound {
		t.Fatalf("a second removal: expected 404, got %d (%s)", again.Code, again.Body.String())
	}
	if body := decodeAdminError(t, again); body.Error.Code != "CATEGORY_NOT_FOUND" {
		t.Fatalf("a second removal must answer CATEGORY_NOT_FOUND, got %s", body.Error.Code)
	}
}

// FR-009 through real PostgreSQL: the administrator list carries the display
// state and includes the withheld categories the public list omits.
func TestTheAdministratorListIncludesHiddenCategoriesAgainstPostgres(t *testing.T) {
	f := newMaintenanceFixture(t)

	seed(t, f.repo, model.CategoryDraft{Name: "Công khai", Slug: "cong-khai", Position: 1}, time.Now().UTC().Add(-time.Hour))
	withheld := seed(t, f.repo, model.CategoryDraft{Name: "Nháp", Slug: "nhap", Position: 2}, time.Now().UTC().Add(-30*time.Minute))
	withheld.Hide(time.Now().UTC())
	if err := f.repo.Update(context.Background(), withheld); err != nil {
		t.Fatalf("hide the seeded category: %v", err)
	}

	adminRec := f.call(http.MethodGet, adminCategoriesPath, "", f.adminToken)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d (%s)", adminRec.Code, adminRec.Body.String())
	}
	if !strings.Contains(adminRec.Body.String(), `"isVisible":false`) {
		t.Fatalf("the administrator list must expose the hidden category: %s", adminRec.Body.String())
	}
	if !strings.Contains(adminRec.Body.String(), `"position"`) {
		t.Fatalf("the administrator list must expose the position: %s", adminRec.Body.String())
	}

	if got := f.publicList(t); len(got) != 1 || got[0] != "cong-khai" {
		t.Fatalf("the public list must hide the withheld category, got %v", got)
	}
}

// --- Hide-guard journey (specs/016-hide-guard US1, T010) ---
//
// These tests drive the guarded category hide against real PostgreSQL: real
// migrations, the real repositories, the real hide-guard adapters and the real
// audit writer. Raw SQL seeds across module boundaries on purpose — products,
// variants, orders and carts belong to other modules — to prove the guard and
// the cleanup end to end, not one module's unit.

// countingCategoryVariants wraps the real product hide-guard adapter and counts
// its calls, so a test proves the variant resolution stays one bulk read
// however many variants the category holds (spec SC-004).
type countingCategoryVariants struct {
	inner *producthideguard.Adapter
	mu    sync.Mutex
	calls int
}

func (c *countingCategoryVariants) VariantIDsByCategories(ctx context.Context, categoryIDs []uuid.UUID) ([]uuid.UUID, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.VariantIDsByCategories(ctx, categoryIDs)
}

func (c *countingCategoryVariants) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// countingOrderGuard wraps the real order hide-guard adapter and counts its
// calls, so a test proves the pending-order check stays one bulk read.
type countingOrderGuard struct {
	inner *orderhideguard.Adapter
	mu    sync.Mutex
	calls int
}

func (c *countingOrderGuard) PendingOrdersForVariants(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.PendingOrdersForVariants(ctx, variantIDs)
}

func (c *countingOrderGuard) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// countingCartCleanup wraps the real cart hide-guard adapter and counts its
// calls, so a test proves the cleanup stays one bulk delete.
type countingCartCleanup struct {
	inner *carthideguard.Adapter
	mu    sync.Mutex
	calls int
}

func (c *countingCartCleanup) RemoveVariantsFromAllCarts(ctx context.Context, variantIDs []uuid.UUID) (int64, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.RemoveVariantsFromAllCarts(ctx, variantIDs)
}

func (c *countingCartCleanup) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// hideGuardFixture is the category module wired with the real hide-guard
// adapters against real infrastructure, with call-counting wrappers around
// each adapter for the boundedness assertions.
type hideGuardFixture struct {
	handler  http.Handler
	pool     *pgxpool.Pool
	adminID  uuid.UUID
	adminTok string
	variants *countingCategoryVariants
	orders   *countingOrderGuard
	carts    *countingCartCleanup
}

func newHideGuardFixture(t *testing.T) *hideGuardFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

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

	writer := audit.NewWriter(audit.NewRepository(pool), testLogger, 64, 1, 1)
	writer.Start(ctx)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Stop(stopCtx)
	})

	categoryRepo := categorypostgres.NewCategoryRepository(pool)
	variants := &countingCategoryVariants{inner: producthideguard.New(productpostgres.NewProductRepository(pool))}
	guardOrders := &countingOrderGuard{inner: orderhideguard.New(orderpostgres.NewOrderRepository(pool))}
	cleanCarts := &countingCartCleanup{inner: carthideguard.New(cartpostgres.NewCartRepository(pool))}
	service := categoryimplement.New(categoryimplement.Service{
		Categories: categoryRepo,
		Audit:      categoryauditor.New(writer),
		Mapper:     categorymapper.New(),
		Tx:         &database.DB{Pool: pool},
		Orders:     guardOrders,
		Variants:   variants,
		Carts:      cleanCarts,
	})
	handler := New(service, testLogger)

	adminID := seedCategoryAccount(t, pool, "hideguard-admin@example.com", access.RoleAdmin)

	hooks := categoryAuthHooks(t)
	root := chi.NewRouter()
	root.Mount(publicCategoriesPath, handler.Router(middleware.AuthHooks{}))
	root.Mount(adminCategoriesPath, handler.AdminRouter(hooks))

	adminTok, _, err := authtoken.NewAccessIssuer(categoryIntegrationSecret, categoryIntegrationTokenTTL).Issue(adminID, access.RoleAdmin)
	if err != nil {
		t.Fatalf("issue the administrator token: %v", err)
	}

	return &hideGuardFixture{
		handler:  root,
		pool:     pool,
		adminID:  adminID,
		adminTok: adminTok,
		variants: variants,
		orders:   guardOrders,
		carts:    cleanCarts,
	}
}

func (f *hideGuardFixture) call(method, path, body string) *httptest.ResponseRecorder {
	return performJSON(f.handler, method, path, body, f.adminTok)
}

// seedHideGuardProduct inserts a product referencing the category, the way the
// product module would. Raw SQL is deliberate: the product belongs to another
// module, and this test seeds across that boundary on purpose.
func (f *hideGuardFixture) seedHideGuardProduct(t *testing.T, categoryID uuid.UUID, code string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO products
			(id, name, code, description, price_amount, currency, category_id, position)
		VALUES ($1, $2, $3, '', 100, 'VND', $4, 1)`
	if _, err := f.pool.Exec(context.Background(), query, id, "Product "+code, code, categoryID); err != nil {
		t.Fatalf("seed product %s: %v", code, err)
	}
	return id
}

// seedHideGuardVariant inserts one on-sale variant of the product.
func (f *hideGuardFixture) seedHideGuardVariant(t *testing.T, productID uuid.UUID, sku string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount, position, sell_state)
		VALUES ($1, $2, $3, $4, 100, 1, 'ACTIVE')`
	if _, err := f.pool.Exec(context.Background(), query, id, productID, "Variant "+sku, sku); err != nil {
		t.Fatalf("seed variant %s: %v", sku, err)
	}
	return id
}

// seedHideGuardOrder inserts an order in the given status holding the variant,
// the way checkout would. The line snapshots mirror what the order module
// stores; only the status and the variant reference matter to the guard.
func (f *hideGuardFixture) seedHideGuardOrder(t *testing.T, ownerID uuid.UUID, status string, productID, variantID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const orderQuery = `
		INSERT INTO orders
			(id, user_id, status, total_amount, currency,
			 recipient_name, recipient_phone,
			 province_code, province_name, ward_code, ward_name, street_address,
			 payment_expires_at, order_version, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, $3, 100, 'VND', 'Buyer', '0123456789',
			'P', 'Province', 'W', 'Ward', 'Street',
			NULL, 1, NULL, now(), now())`
	if _, err := f.pool.Exec(context.Background(), orderQuery, id, ownerID, status); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	const lineQuery = `
		INSERT INTO order_items
			(order_id, product_id, variant_id, name, product_code, variant_name, sku,
			 unit_price_amount, currency, quantity, position, created_at)
		VALUES ($1, $2, $3, 'Product', 'bulk-prod', 'Variant', $4, 100, 'VND', 1, 1, now())`
	if _, err := f.pool.Exec(context.Background(), lineQuery, id, productID, variantID, "SKU-"+id.String()[:8]); err != nil {
		t.Fatalf("seed order line: %v", err)
	}
	return id
}

// seedHideGuardCartLine inserts a cart holding the variant for the owner,
// creating the cart lazily the way the cart module would.
func (f *hideGuardFixture) seedHideGuardCartLine(t *testing.T, ownerID, variantID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	cartID := uuid.New()
	const cartQuery = `INSERT INTO carts (id, user_id) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`
	if _, err := f.pool.Exec(ctx, cartQuery, cartID, ownerID); err != nil {
		t.Fatalf("seed cart: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT id FROM carts WHERE user_id = $1`, ownerID).Scan(&cartID); err != nil {
		t.Fatalf("read the seeded cart: %v", err)
	}
	const lineQuery = `
		INSERT INTO cart_items (cart_id, variant_id, quantity, unit_price_amount, currency)
		VALUES ($1, $2, 1, 100, 'VND')`
	if _, err := f.pool.Exec(ctx, lineQuery, cartID, variantID); err != nil {
		t.Fatalf("seed cart line: %v", err)
	}
}

// cartLineCount counts every cart line holding the variant across all carts.
func (f *hideGuardFixture) cartLineCount(t *testing.T, variantID uuid.UUID) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cart_items WHERE variant_id = $1`, variantID).Scan(&count); err != nil {
		t.Fatalf("count the cart lines: %v", err)
	}
	return count
}

// waitForHideAudit waits until the hide action has a persisted audit row, so
// the assertion cannot race the asynchronous writer.
func (f *hideGuardFixture) waitForHideAudit(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var count int
		if err := f.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM audit_logs WHERE action = $1`, constant.AuditCategoryHidden).Scan(&count); err != nil {
			t.Fatalf("count the hide audit rows: %v", err)
		}
		if count == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 1 %s row, got %d", constant.AuditCategoryHidden, count)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hideAuditMetadata reads the hide event's metadata after it is persisted.
func (f *hideGuardFixture) hideAuditMetadata(t *testing.T) map[string]any {
	t.Helper()
	f.waitForHideAudit(t)
	var raw []byte
	if err := f.pool.QueryRow(context.Background(),
		`SELECT metadata FROM audit_logs WHERE action = $1`, constant.AuditCategoryHidden).Scan(&raw); err != nil {
		t.Fatalf("read the hide audit metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("decode the hide audit metadata %q: %v", raw, err)
	}
	return metadata
}

// hideAuditCount counts the persisted hide audit rows right now. A refusal
// records nothing, so after a short settle delay this must be zero.
func (f *hideGuardFixture) hideAuditCount(t *testing.T) int {
	t.Helper()
	time.Sleep(500 * time.Millisecond)
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_logs WHERE action = $1`, constant.AuditCategoryHidden).Scan(&count); err != nil {
		t.Fatalf("count the hide audit rows: %v", err)
	}
	return count
}

// createHideGuardCategory creates a category through the administrator route.
func (f *hideGuardFixture) createHideGuardCategory(t *testing.T, name, slug string) uuid.UUID {
	t.Helper()
	rec := f.call(http.MethodPost, adminCategoriesPath,
		fmt.Sprintf(`{"name":%q,"slug":%q,"position":1}`, name, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAdminCategory(t, rec).ID
}

// Spec US1, FR-001/FR-004/FR-007 against real PostgreSQL: a category with a
// PENDING order holding one of its variants refuses the hide with the blocking
// order named and nothing changes; resolving the order lets the hide succeed
// and removes the cart lines in the same transaction.
func TestCategoryHideGuardJourneyAgainstPostgres(t *testing.T) {
	f := newHideGuardFixture(t)

	categoryID := f.createHideGuardCategory(t, "Tranh guard", "tranh-guard")
	productID := f.seedHideGuardProduct(t, categoryID, "guard-prod")
	variantID := f.seedHideGuardVariant(t, productID, "GUARD-SKU-1")

	buyerID := seedCategoryAccount(t, f.pool, "hideguard-buyer@example.com", access.RoleCustomer)
	blockingID := f.seedHideGuardOrder(t, buyerID, "PENDING", productID, variantID)
	f.seedHideGuardCartLine(t, buyerID, variantID)

	before := f.call(http.MethodGet, adminCategoriesPath+"/"+categoryID.String(), "")
	if before.Code != http.StatusOK {
		t.Fatalf("read before hide: expected 200, got %d (%s)", before.Code, before.Body.String())
	}

	// Attempt the hide: refusal names the blocking order.
	rec := f.call(http.MethodPatch, adminCategoriesPath+"/"+categoryID.String(), `{"isVisible":false}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("blocked hide: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeAdminError(t, rec)
	if body.Error.Code != constant.CodeCategoryHasPendingOrders {
		t.Fatalf("expected %s, got %s", constant.CodeCategoryHasPendingOrders, body.Error.Code)
	}
	named := false
	for _, detail := range body.Error.Details {
		if detail.Field == "orderId" && detail.Issue == blockingID.String() {
			named = true
		}
	}
	if !named {
		t.Fatalf("the refusal must name the blocking order %s, got %+v", blockingID, body.Error.Details)
	}

	// Nothing changes: the category is byte-for-byte the same, the cart line is
	// intact, and no hide is audited.
	after := f.call(http.MethodGet, adminCategoriesPath+"/"+categoryID.String(), "")
	if after.Code != http.StatusOK {
		t.Fatalf("read after refused hide: expected 200, got %d (%s)", after.Code, after.Body.String())
	}
	if after.Body.String() != before.Body.String() {
		t.Fatalf("a refused hide must leave the category unchanged:\nbefore %s\nafter  %s",
			before.Body.String(), after.Body.String())
	}
	if got := f.cartLineCount(t, variantID); got != 1 {
		t.Fatalf("a refused hide must leave carts untouched, got %d lines", got)
	}
	if got := f.hideAuditCount(t); got != 0 {
		t.Fatalf("a refused hide must record nothing, got %d hide rows", got)
	}

	// Resolve the order out of the pending states: the hide now succeeds.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET status = 'PAID' WHERE id = $1`, blockingID); err != nil {
		t.Fatalf("resolve the blocking order: %v", err)
	}
	hide := f.call(http.MethodPatch, adminCategoriesPath+"/"+categoryID.String(), `{"isVisible":false}`)
	if hide.Code != http.StatusOK {
		t.Fatalf("clean hide: expected 200, got %d (%s)", hide.Code, hide.Body.String())
	}
	if decodeAdminCategory(t, hide).IsVisible {
		t.Fatal("the category must be hidden")
	}
	if got := f.cartLineCount(t, variantID); got != 0 {
		t.Fatalf("a successful hide must remove the hidden variant from every cart, got %d lines", got)
	}
	metadata := f.hideAuditMetadata(t)
	if got, ok := metadata["removedCartLines"]; !ok || got != float64(1) {
		t.Fatalf("the hide event must carry the removed cart-line count, got %v", metadata)
	}
	if f.variants.callCount() == 0 || f.orders.callCount() == 0 || f.carts.callCount() == 0 {
		t.Fatalf("the guard and the cleanup must run, got variant=%d order=%d cart=%d calls",
			f.variants.callCount(), f.orders.callCount(), f.carts.callCount())
	}
}

// Spec SC-004, FR-008 against real PostgreSQL: hiding a category holding
// hundreds of variants completes as bounded bulk statements — one resolution,
// one check and one cleanup — with cart lines of other categories untouched.
func TestCategoryHideGuardStaysBoundedForHundredsOfVariantsAgainstPostgres(t *testing.T) {
	f := newHideGuardFixture(t)

	categoryID := f.createHideGuardCategory(t, "Tranh bulk", "tranh-bulk")
	productID := f.seedHideGuardProduct(t, categoryID, "bulk-prod")

	const variantCount = 200
	variantIDs := make([]uuid.UUID, 0, variantCount)
	for i := 0; i < variantCount; i++ {
		variantIDs = append(variantIDs, f.seedHideGuardVariant(t, productID, fmt.Sprintf("BULK-SKU-%04d", i)))
	}

	// Spread cart lines across three buyers, plus one line of another category
	// that the cleanup must leave alone.
	buyers := []uuid.UUID{
		seedCategoryAccount(t, f.pool, "hideguard-bulk-1@example.com", access.RoleCustomer),
		seedCategoryAccount(t, f.pool, "hideguard-bulk-2@example.com", access.RoleCustomer),
		seedCategoryAccount(t, f.pool, "hideguard-bulk-3@example.com", access.RoleCustomer),
	}
	for i, variantID := range variantIDs[:60] {
		f.seedHideGuardCartLine(t, buyers[i%len(buyers)], variantID)
	}
	otherCategoryID := f.createHideGuardCategory(t, "Tranh other", "tranh-other")
	otherProductID := f.seedHideGuardProduct(t, otherCategoryID, "other-prod")
	otherVariantID := f.seedHideGuardVariant(t, otherProductID, "OTHER-SKU-1")
	f.seedHideGuardCartLine(t, buyers[0], otherVariantID)

	hide := f.call(http.MethodPatch, adminCategoriesPath+"/"+categoryID.String(), `{"isVisible":false}`)
	if hide.Code != http.StatusOK {
		t.Fatalf("bulk hide: expected 200, got %d (%s)", hide.Code, hide.Body.String())
	}

	// Bounded: exactly one resolution, one check and one cleanup for all two
	// hundred variants — no per-variant round trips.
	if got := f.variants.callCount(); got != 1 {
		t.Fatalf("the variant resolution must be one bulk read, got %d calls", got)
	}
	if got := f.orders.callCount(); got != 1 {
		t.Fatalf("the pending-order check must be one bulk read, got %d calls", got)
	}
	if got := f.carts.callCount(); got != 1 {
		t.Fatalf("the cart cleanup must be one bulk delete, got %d calls", got)
	}

	var remaining int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cart_items WHERE variant_id = ANY($1)`, variantIDs).Scan(&remaining); err != nil {
		t.Fatalf("count the remaining bulk lines: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("a successful hide must remove every hidden line, got %d remaining", remaining)
	}
	if got := f.cartLineCount(t, otherVariantID); got != 1 {
		t.Fatalf("the cleanup must leave other categories' lines alone, got %d", got)
	}
}

// seedCategoryProduct inserts a product that references the category, the way
// module 04's own adapter would, so the restricting foreign key has a live row to
// refuse against. Raw SQL is deliberate: this test seeds across a module boundary
// on purpose to prove the storage guarantee, not the product module.
func seedCategoryProduct(t *testing.T, pool *pgxpool.Pool, categoryID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	code := "tranh-thu-" + id.String()[:8]
	const query = `
		INSERT INTO products
			(id, name, code, description, price_amount, currency, category_id, position)
		VALUES ($1, $2, $3, '', 100, 'VND', $4, 1)`
	if _, err := pool.Exec(context.Background(), query, id, "Tranh thử", code, categoryID); err != nil {
		t.Fatalf("seed a product referencing the category: %v", err)
	}
	return id
}

// FR-035, FR-036, SC-005 against real PostgreSQL: with a product referencing the
// category the removal is refused, and the refusal is produced by the storage
// layer — the database itself reports a foreign-key violation (SQLSTATE 23503)
// before module 03 translates it into CATEGORY_IN_USE. After the product is
// removed the removal succeeds. This is the debt feature 005 left and this
// scenario is what closes it (quickstart.md 12).
func TestRemovingACategoryWithProductsIsRefusedByTheForeignKeyAgainstPostgres(t *testing.T) {
	f := newMaintenanceFixture(t)

	createRec := f.call(http.MethodPost, adminCategoriesPath,
		`{"name":"Tranh sơn dầu","slug":"tranh-son-dau","position":1}`, f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeAdminCategory(t, createRec)

	productID := seedCategoryProduct(t, f.pool, created.ID)

	// The storage layer itself refuses the removal: before any translation, the
	// database reports a foreign-key violation. This is the check an
	// application-level guard would fail, and the reason the rule cannot be
	// bypassed by a code path that skips the check.
	var pgErr *pgconn.PgError
	_, storageErr := f.pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, created.ID)
	if !errors.As(storageErr, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("the database must refuse the removal with a foreign-key violation, got %v", storageErr)
	}

	// The operator's answer is the module code, not an unexplained server failure.
	rec := f.call(http.MethodDelete, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeAdminError(t, rec); body.Error.Code != constant.CodeCategoryInUse {
		t.Fatalf("expected %s, got %s", constant.CodeCategoryInUse, body.Error.Code)
	}

	// The category and its product are both still there.
	if f.categoryRowCount(t, created.ID) != 1 {
		t.Fatal("a refused removal must leave the category in place")
	}
	var products int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM products WHERE id = $1", productID).Scan(&products); err != nil {
		t.Fatalf("count the referencing product: %v", err)
	}
	if products != 1 {
		t.Fatal("a refused removal must leave the products in place")
	}

	// Removing the product frees the category: the removal then succeeds.
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, productID); err != nil {
		t.Fatalf("remove the product: %v", err)
	}
	afterRec := f.call(http.MethodDelete, adminCategoriesPath+"/"+created.ID.String(), "", f.adminToken)
	if afterRec.Code != http.StatusNoContent {
		t.Fatalf("after the product is gone the removal must succeed, got %d (%s)", afterRec.Code, afterRec.Body.String())
	}
	if f.categoryRowCount(t, created.ID) != 0 {
		t.Fatal("the category must be gone once nothing references it")
	}
}

// reorderUUIDList renders a UUID slice as the JSON array a reorder body carries,
// so a test can build the body without hand-writing each identifier.
func reorderUUIDList(ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%q", id.String()))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Spec 018 US1 (T009) against real PostgreSQL: the category reorder stores the
// complete submitted order with the adapter's single bulk UNNEST statement
// (research D1), the customer-facing catalogue reflects it, and every refusal
// (duplicate, incomplete, unknown) leaves the stored order unchanged.
func TestCategoryReorderAgainstPostgres(t *testing.T) {
	f := newMaintenanceFixture(t)
	base := time.Now().UTC().Add(-time.Hour)
	alpha := seed(t, f.repo, model.CategoryDraft{Name: "Alpha", Slug: "alpha", Position: 1}, base)
	beta := seed(t, f.repo, model.CategoryDraft{Name: "Beta", Slug: "beta", Position: 2}, base.Add(time.Minute))
	gamma := seed(t, f.repo, model.CategoryDraft{Name: "Gamma", Slug: "gamma", Position: 3}, base.Add(2*time.Minute))

	seeded := []string{"alpha", "beta", "gamma"}
	if got := f.publicList(t); !reflect.DeepEqual(got, seeded) {
		t.Fatalf("the seeded order must be the stored one, got %v", got)
	}

	// Every refusal leaves the stored order untouched (FR-004, SC-001).
	refusals := []struct {
		name string
		body string
		want int
		code string
	}{
		{"duplicate", `{"categoryIds":` + reorderUUIDList([]uuid.UUID{gamma.ID, alpha.ID, gamma.ID}) + `}`,
			http.StatusBadRequest, constant.CodeCategoryReorderDuplicateID},
		{"incomplete", `{"categoryIds":` + reorderUUIDList([]uuid.UUID{alpha.ID, beta.ID}) + `}`,
			http.StatusBadRequest, "VALIDATION_ERROR"},
		{"unknown", `{"categoryIds":` + reorderUUIDList([]uuid.UUID{alpha.ID, beta.ID, uuid.New()}) + `}`,
			http.StatusNotFound, "CATEGORY_NOT_FOUND"},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			rec := f.call(http.MethodPut, adminCategoriesPath+"/reorder", refusal.body, f.adminToken)
			if rec.Code != refusal.want {
				t.Fatalf("expected %d, got %d (%s)", refusal.want, rec.Code, rec.Body.String())
			}
			if body := decodeAdminError(t, rec); body.Error.Code != refusal.code {
				t.Fatalf("expected %s, got %s", refusal.code, body.Error.Code)
			}
			if got := f.publicList(t); !reflect.DeepEqual(got, seeded) {
				t.Fatalf("a refused reorder must leave the order unchanged, got %v", got)
			}
		})
	}

	// The successful reorder stores the submitted order in one bulk write: the
	// affected-row count equals the submitted size (research D1).
	order := []uuid.UUID{gamma.ID, alpha.ID, beta.ID}
	rec := f.call(http.MethodPut, adminCategoriesPath+"/reorder",
		`{"categoryIds":`+reorderUUIDList(order)+`}`, f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			UpdatedCount int64 `json:"updatedCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the reorder answer %q: %v", rec.Body.String(), err)
	}
	if body.Data.UpdatedCount != 3 {
		t.Fatalf("expected the bulk write to touch 3 rows, got %d", body.Data.UpdatedCount)
	}

	for wantPosition, id := range order {
		stored, err := f.repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatalf("read category %s: %v", id, err)
		}
		if stored.Position != wantPosition {
			t.Fatalf("stored position of %s = %d, want %d", id, stored.Position, wantPosition)
		}
	}
	if got := f.publicList(t); !reflect.DeepEqual(got, []string{"gamma", "alpha", "beta"}) {
		t.Fatalf("the customer-facing order must reflect the reorder, got %v", got)
	}

	rows := f.waitForAuditRows(t, constant.AuditCategoryReordered, 1)
	if rows[0].actorID == nil || *rows[0].actorID != f.adminID {
		t.Fatalf("the reorder must name the administrator, got %v", rows[0].actorID)
	}
	if rows[0].targetType != "category" || rows[0].targetID != "" {
		t.Fatalf("a bulk reorder targets the scope with no single row, got %q/%q", rows[0].targetType, rows[0].targetID)
	}
	if rows[0].outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("expected a SUCCESS outcome, got %q", rows[0].outcome)
	}
}

// adminCategoriesListBody is the members of one administrator list answer this
// test inspects.
type adminCategoriesListBody struct {
	Data []struct {
		Slug string `json:"slug"`
	} `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

func decodeAdminCategoriesList(t *testing.T, rec *httptest.ResponseRecorder) adminCategoriesListBody {
	t.Helper()
	var body adminCategoriesListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the administrator list %q: %v", rec.Body.String(), err)
	}
	return body
}

// Spec 018 US3 (T017) against real PostgreSQL: `all=true` returns the complete
// ordered list beyond one page, while the ordinary request still pages.
func TestCategoryListAllSelectorAgainstPostgres(t *testing.T) {
	f := newMaintenanceFixture(t)
	base := time.Now().UTC().Add(-time.Hour)
	const total = defaultPageSize + 5
	for i := 0; i < total; i++ {
		seed(t, f.repo, model.CategoryDraft{
			Name:     fmt.Sprintf("Mục %02d", i),
			Slug:     fmt.Sprintf("muc-%02d", i),
			Position: i,
		}, base.Add(time.Duration(i)*time.Second))
	}

	// The ordinary request is paged: it cannot return everything.
	paged := f.call(http.MethodGet, adminCategoriesPath, "", f.adminToken)
	if paged.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d (%s)", paged.Code, paged.Body.String())
	}
	if body := decodeAdminCategoriesList(t, paged); len(body.Data) != defaultPageSize || body.Meta.Total != total {
		t.Fatalf("a paged request must return one page, got %d of %d", len(body.Data), body.Meta.Total)
	}

	// `all=true` returns the whole ordered list.
	all := f.call(http.MethodGet, adminCategoriesPath+"?all=true", "", f.adminToken)
	if all.Code != http.StatusOK {
		t.Fatalf("all list: expected 200, got %d (%s)", all.Code, all.Body.String())
	}
	body := decodeAdminCategoriesList(t, all)
	if len(body.Data) != total || body.Meta.Total != total {
		t.Fatalf("all=true must return the whole list, got %d of %d", len(body.Data), body.Meta.Total)
	}
	for i, entry := range body.Data {
		if want := fmt.Sprintf("muc-%02d", i); entry.Slug != want {
			t.Fatalf("all=true must preserve the stored order: entry %d = %q, want %q", i, entry.Slug, want)
		}
	}
}
