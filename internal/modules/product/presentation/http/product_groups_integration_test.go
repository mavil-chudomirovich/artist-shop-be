//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authtoken "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	productappinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	productauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/auditor"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives User Story 2 end to end against a real PostgreSQL container:
// the real variant-group adapter, the real product adapter, the real audit writer
// and real access tokens. It proves what a fake cannot — the whole administrator
// group lifecycle (create, list, read, edit, attach/detach options, gallery and
// removal with the restricting foreign key), the role refusal on a customer
// session, and that every mutation left its audit row (FR-009 to FR-015, SC-006).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// productGroupsFixture is the module wired against real infrastructure, with both
// administrator route groups the feature adds.
type productGroupsFixture struct {
	handler       http.Handler
	pool          *pgxpool.Pool
	writer        *audit.Writer
	categoryRepo  *categorypostgres.CategoryRepository
	adminID       uuid.UUID
	customerID    uuid.UUID
	adminToken    string
	customerToken string
}

func newProductGroupsFixture(t *testing.T) *productGroupsFixture {
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
	products := productpostgres.NewProductRepository(pool)
	config := productappinterface.Config{}
	service := productimplement.New(productimplement.Service{
		Products:   products,
		Groups:     productpostgres.NewVariantGroupRepository(pool),
		Visibility: categoryvisibility.New(categoryRepo),
		Audit:      productauditor.New(writer),
		Media:      newProductMediaStub(),
		Tx:         &database.DB{Pool: pool},
		Config:     config,
		Mapper:     mapper.New(),
	})
	handler := New(service, config, testLogger)

	adminID := seedProductAccount(t, pool, "product-group-admin@example.com", access.RoleAdmin)
	customerID := seedProductAccount(t, pool, "product-group-customer@example.com", access.RoleCustomer)

	hooks := productAuthHooks(t)
	root := chi.NewRouter()
	root.Mount(adminProductsPath, handler.AdminRouter(hooks))
	root.Mount(adminProductGroupsPath, handler.GroupAdminRouter(hooks))

	adminToken, _, err := authtoken.NewAccessIssuer(productIntegrationSecret, productIntegrationTokenTTL).Issue(adminID, access.RoleAdmin)
	if err != nil {
		t.Fatalf("issue the administrator token: %v", err)
	}
	customerToken, _, err := authtoken.NewAccessIssuer(productIntegrationSecret, productIntegrationTokenTTL).Issue(customerID, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue the customer token: %v", err)
	}

	return &productGroupsFixture{
		handler:       root,
		pool:          pool,
		writer:        writer,
		categoryRepo:  categoryRepo,
		adminID:       adminID,
		customerID:    customerID,
		adminToken:    adminToken,
		customerToken: customerToken,
	}
}

// groupDetailBody is the administrator group detail as the JSON contract declares
// it. Only the members this test asserts are declared.
type groupDetailBody struct {
	Data struct {
		ID         uuid.UUID `json:"id"`
		Name       string    `json:"name"`
		Slug       string    `json:"slug"`
		OptionName string    `json:"optionName"`
		Images     []struct {
			ID        uuid.UUID `json:"id"`
			IsPrimary bool      `json:"isPrimary"`
		} `json:"images"`
		Options []struct {
			Label string `json:"label"`
			Slug  string `json:"slug"`
		} `json:"options"`
	} `json:"data"`
}

func (f *productGroupsFixture) call(method, path, body, token string) *httptest.ResponseRecorder {
	return performJSON(f.handler, method, path, body, token)
}

func decodeGroupDetail(t *testing.T, rec *httptest.ResponseRecorder) groupDetailBody {
	t.Helper()
	var body groupDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the group detail %q: %v", rec.Body.String(), err)
	}
	return body
}

// readAuditRows reads every audit row of one action, in the order it was written.
func (f *productGroupsFixture) readAuditRows(t *testing.T, action string) []productAuditRow {
	t.Helper()
	const query = `
		SELECT actor_id, actor_role, target_type, target_id, outcome
		FROM audit_logs WHERE action = $1 ORDER BY occurred_at`
	rows, err := f.pool.Query(context.Background(), query, action)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	defer rows.Close()
	var events []productAuditRow
	for rows.Next() {
		var event productAuditRow
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

// waitForAuditRows waits for the writer's asynchronous queue to deliver the rows
// of one action.
func (f *productGroupsFixture) waitForAuditRows(t *testing.T, action string, want int) []productAuditRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := f.readAuditRows(t, action)
		if len(rows) == want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d %s rows, got %d", want, action, len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// requireGroupAudit asserts one audit row of an action names the administrator, the
// group as the target and a SUCCESS outcome (FR-014, SC-006).
func (f *productGroupsFixture) requireGroupAudit(t *testing.T, action string, groupID uuid.UUID) {
	t.Helper()
	row := f.waitForAuditRows(t, action, 1)[0]
	if row.actorID == nil || *row.actorID != f.adminID {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, f.adminID, row.actorID)
	}
	if row.actorRole != string(access.RoleAdmin) {
		t.Fatalf("%s: expected the ADMIN role, got %q", action, row.actorRole)
	}
	if row.targetType != "variant_group" || row.targetID != groupID.String() {
		t.Fatalf("%s: expected the group as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// requireProductAudit asserts one audit row of an action names the administrator
// and the product as the target.
func (f *productGroupsFixture) requireProductAudit(t *testing.T, action string, productID uuid.UUID) {
	t.Helper()
	row := f.waitForAuditRows(t, action, 1)[0]
	if row.actorID == nil || *row.actorID != f.adminID {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, f.adminID, row.actorID)
	}
	if row.targetType != "product" || row.targetID != productID.String() {
		t.Fatalf("%s: expected the product as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// createGroup posts a group and returns its decoded detail.
func (f *productGroupsFixture) createGroup(t *testing.T, categoryID uuid.UUID, slug string) groupDetailBody {
	t.Helper()
	rec := f.call(http.MethodPost, adminProductGroupsPath,
		`{"name":"Card Genshin","slug":"`+slug+`","description":"Bộ card Genshin","categoryId":"`+categoryID.String()+`","position":1,"optionName":"Card"}`,
		f.adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeGroupDetail(t, rec)
}

const groupProductBasePath = "/api/v1/admin/products"

// FR-010, FR-011, FR-014, FR-018, SC-006 against real PostgreSQL: the whole group
// lifecycle — create, list, read, edit, attach and detach options, add/promote/
// remove pictures, and the restricting removal — with an audit row after each
// mutation and the options surviving the group's removal.
func TestVariantGroupMaintenanceLifecycleAgainstPostgres(t *testing.T) {
	f := newProductGroupsFixture(t)
	category := seedCatalogueCategory(t, f.categoryRepo, "card-shop", true)

	// Create: the group carries the operator's content.
	created := f.createGroup(t, category.ID, "card-genshin")
	groupID := created.Data.ID
	if groupID == uuid.Nil || created.Data.OptionName != "Card" {
		t.Fatalf("unexpected created group: %+v", created.Data)
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupCreated, groupID)

	// List: the administrator list carries the created group.
	listRec := f.call(http.MethodGet, adminProductGroupsPath, "", f.adminToken)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "card-genshin") {
		t.Fatalf("the administrator list must include the created group, got %d (%s)", listRec.Code, listRec.Body.String())
	}

	// Two ordinary products, then attach both as labelled options.
	flins := createOrdinaryProduct(t, f, category.ID, "card-flins")
	dottore := createOrdinaryProduct(t, f, category.ID, "card-dottore")
	attach := func(productID uuid.UUID, label string) {
		rec := f.call(http.MethodPatch, groupProductBasePath+"/"+productID.String(),
			`{"variantGroupId":"`+groupID.String()+`","variantLabel":"`+label+`"}`, f.adminToken)
		if rec.Code != http.StatusOK {
			t.Fatalf("attach %s: expected 200, got %d (%s)", label, rec.Code, rec.Body.String())
		}
	}
	attach(flins, "Flins")
	f.requireProductAudit(t, constant.AuditProductVariantAttached, flins)
	attach(dottore, "Dottore")
	f.requireProductAudit(t, constant.AuditProductVariantAttached, dottore)

	// The administrator group detail lists every option with its own label.
	detailRec := f.call(http.MethodGet, adminProductGroupsPath+"/"+groupID.String(), "", f.adminToken)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("read group: expected 200, got %d (%s)", detailRec.Code, detailRec.Body.String())
	}
	detail := decodeGroupDetail(t, detailRec)
	labels := make([]string, 0, len(detail.Data.Options))
	for _, option := range detail.Data.Options {
		labels = append(labels, option.Label)
	}
	if len(labels) != 2 || !containsSlug(labels, "Flins") || !containsSlug(labels, "Dottore") {
		t.Fatalf("the group detail must list both labelled options, got %v", labels)
	}

	// Gallery: the first picture becomes the main one; the second can be promoted.
	imagePath := adminProductGroupsPath + "/" + groupID.String() + "/images"
	firstRec := postImage(f.handler, imagePath, "one.png", "image/png", pngBytes(), f.adminToken)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("add first group picture: expected 201, got %d (%s)", firstRec.Code, firstRec.Body.String())
	}
	first := decodeGroupDetail(t, firstRec)
	if len(first.Data.Images) != 1 || !first.Data.Images[0].IsPrimary {
		t.Fatalf("the first group picture must be the main one: %+v", first.Data.Images)
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupImageAdded, groupID)

	secondRec := postImage(f.handler, imagePath, "two.png", "image/png", pngBytes(), f.adminToken)
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("add second group picture: expected 201, got %d (%s)", secondRec.Code, secondRec.Body.String())
	}
	second := decodeGroupDetail(t, secondRec)
	if len(second.Data.Images) != 2 {
		t.Fatalf("expected two group pictures, got %+v", second.Data.Images)
	}
	f.waitForAuditRows(t, constant.AuditVariantGroupImageAdded, 2)

	promoteRec := f.call(http.MethodPost, imagePath+"/"+second.Data.Images[1].ID.String()+"/primary", "", f.adminToken)
	if promoteRec.Code != http.StatusOK {
		t.Fatalf("promote group picture: expected 200, got %d (%s)", promoteRec.Code, promoteRec.Body.String())
	}
	promoted := decodeGroupDetail(t, promoteRec)
	if !promoted.Data.Images[1].IsPrimary || promoted.Data.Images[0].IsPrimary {
		t.Fatalf("the promoted group picture must be the only main one: %+v", promoted.Data.Images)
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupImagePrimarySet, groupID)

	removeRec := f.call(http.MethodDelete, imagePath+"/"+second.Data.Images[1].ID.String(), "", f.adminToken)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove group picture: expected 204, got %d (%s)", removeRec.Code, removeRec.Body.String())
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupImageRemoved, groupID)

	// Edit: the change is reflected on the next read.
	editRec := f.call(http.MethodPatch, adminProductGroupsPath+"/"+groupID.String(), `{"name":"Card Genshin v2"}`, f.adminToken)
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit group: expected 200, got %d (%s)", editRec.Code, editRec.Body.String())
	}
	if decodeGroupDetail(t, editRec).Data.Name != "Card Genshin v2" {
		t.Fatalf("the edit was not applied: %s", editRec.Body.String())
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupUpdated, groupID)

	// Remove: refused while options remain, so the operator detaches them first.
	refused := f.call(http.MethodDelete, adminProductGroupsPath+"/"+groupID.String(), "", f.adminToken)
	if refused.Code != http.StatusConflict {
		t.Fatalf("removing a group with options: expected 409, got %d (%s)", refused.Code, refused.Body.String())
	}
	if body := decodeAdminError(t, refused); body.Error.Code != "VARIANT_GROUP_HAS_OPTIONS" {
		t.Fatalf("expected VARIANT_GROUP_HAS_OPTIONS, got %s", body.Error.Code)
	}

	detach := func(productID uuid.UUID) {
		rec := f.call(http.MethodPatch, groupProductBasePath+"/"+productID.String(),
			`{"variantGroupId":null,"variantLabel":null}`, f.adminToken)
		if rec.Code != http.StatusOK {
			t.Fatalf("detach: expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
	detach(flins)
	f.requireProductAudit(t, constant.AuditProductVariantDetached, flins)
	detach(dottore)
	f.requireProductAudit(t, constant.AuditProductVariantDetached, dottore)

	removed := f.call(http.MethodDelete, adminProductGroupsPath+"/"+groupID.String(), "", f.adminToken)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("remove empty group: expected 204, got %d (%s)", removed.Code, removed.Body.String())
	}
	f.requireGroupAudit(t, constant.AuditVariantGroupDeleted, groupID)

	// The options survive: detaching made them ordinary products, and removing the
	// group touched neither.
	for _, productID := range []uuid.UUID{flins, dottore} {
		if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, productID); got != 1 {
			t.Fatalf("removing a group must not remove its options, product %s count is %d", productID, got)
		}
		var group *uuid.UUID
		if err := f.pool.QueryRow(context.Background(), `SELECT variant_group_id FROM products WHERE id = $1`, productID).Scan(&group); err != nil {
			t.Fatalf("read the detached option: %v", err)
		}
		if group != nil {
			t.Fatalf("a detached option must have no group, got %s", *group)
		}
	}
}

// createOrdinaryProduct creates one standalone product through the administrator
// route and returns its identifier.
func createOrdinaryProduct(t *testing.T, f *productGroupsFixture, categoryID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	rec := f.call(http.MethodPost, groupProductBasePath,
		`{"name":"`+slug+`","slug":"`+slug+`","description":"","price":{"amount":120000,"currency":"VND"},"categoryId":"`+categoryID.String()+`","position":1}`,
		f.adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create product %s: expected 201, got %d (%s)", slug, rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the created product %q: %v", rec.Body.String(), err)
	}
	return body.Data.ID
}

// count reads a scalar count for the assertions that check what storage holds.
func (f *productGroupsFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var total int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&total); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return total
}

// FR-015: a customer session is refused on the group administrator routes, so no
// variant-group maintenance action is reachable without the ADMIN role.
func TestTheGroupAdminSurfaceRefusesACustomerSessionAgainstPostgres(t *testing.T) {
	f := newProductGroupsFixture(t)
	category := seedCatalogueCategory(t, f.categoryRepo, "card-refusal", true)
	created := f.createGroup(t, category.ID, "card-refused")
	groupID := created.Data.ID

	routes := []adminRoute{
		{http.MethodGet, adminProductGroupsPath, ""},
		{http.MethodGet, adminProductGroupsPath + "/" + groupID.String(), ""},
		{http.MethodPatch, adminProductGroupsPath + "/" + groupID.String(), `{"name":"B"}`},
		{http.MethodDelete, adminProductGroupsPath + "/" + groupID.String(), ""},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			noToken := f.call(route.method, route.path, route.body, "")
			if noToken.Code != http.StatusUnauthorized {
				t.Fatalf("no token: expected 401, got %d (%s)", noToken.Code, noToken.Body.String())
			}
			customer := f.call(route.method, route.path, route.body, f.customerToken)
			if customer.Code != http.StatusForbidden {
				t.Fatalf("customer token: expected 403, got %d (%s)", customer.Code, customer.Body.String())
			}
			if body := decodeAdminError(t, customer); body.Error.Code != "FORBIDDEN" {
				t.Fatalf("customer token: expected FORBIDDEN, got %s", body.Error.Code)
			}
		})
	}
}
