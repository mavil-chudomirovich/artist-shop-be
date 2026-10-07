//go:build integration

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	authtoken "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	categoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	categorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	categoryauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/auditor"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
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
