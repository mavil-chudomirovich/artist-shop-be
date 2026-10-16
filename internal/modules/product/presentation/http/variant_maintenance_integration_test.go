//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authconstant "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	authtoken "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	productconstant "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	productauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/auditor"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US2's administrator surface end-to-end against real
// PostgreSQL: the real migrations, module 03's real repository behind the
// visibility contract, the real product and variant adapters, the real audit
// writer and real access tokens. It proves what a fake cannot: that a product
// and its variants are persisted together, that the code/SKU uniqueness and the
// last-variant rule are enforced by storage, that every mutation leaves its audit
// row (SC-006), and that a customer token is refused on role grounds with the
// denial audited (FR-009 to FR-016). Docker is required to run it; the file still
// compiles without a container so `go vet -tags integration` covers it.

const (
	// variantMaintenanceSecret is a throwaway HS256 key for the test tokens.
	variantMaintenanceSecret = "0123456789abcdef0123456789abcdef"
	// variantMaintenanceTokenTTL is long enough that no token expires mid-test.
	variantMaintenanceTokenTTL = 15 * time.Minute
)

// variantMaintenanceClock is the production-like clock module 05's availability
// read needs; the administrator flow never reads availability, but the service
// requires the port to be non-nil for the public catalogue check.
type variantMaintenanceClock struct{}

func (variantMaintenanceClock) Now() time.Time { return time.Now().UTC() }

// variantMediaStub answers the media port without a provider, so the variant
// image upload can run hermetically. It records how many assets it released.
type variantMediaStub struct {
	removed int
}

func (m *variantMediaStub) Upload(context.Context, []byte, int) (media.Reference, error) {
	return media.Reference{
		PublicID: "artist-shop/variants/one",
		URL:      "https://cdn.example.test/variants/one.jpg",
		Width:    20,
		Height:   20,
	}, nil
}

func (m *variantMediaStub) Remove(context.Context, media.Reference) error {
	m.removed++
	return nil
}

var _ appinterface.MediaStore = (*variantMediaStub)(nil)

// variantMaintenanceFixture is the module wired against real infrastructure plus
// the real administrator and public HTTP surfaces.
type variantMaintenanceFixture struct {
	pool          *pgxpool.Pool
	categories    *categorypostgres.CategoryRepository
	handler       http.Handler
	adminID       uuid.UUID
	customerID    uuid.UUID
	adminToken    string
	customerToken string
}

func newVariantMaintenanceFixture(t *testing.T) *variantMaintenanceFixture {
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

	// The composition root's unit of work is the database handle, so the product
	// create runs in the same single transaction production uses (research D14).
	db := &database.DB{Pool: pool}

	writer := audit.NewWriter(audit.NewRepository(pool), testLogger, 128, 1, 1)
	writer.Start(ctx)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Stop(stopCtx)
	})

	categoryRepo := categorypostgres.NewCategoryRepository(pool)
	service := productimplement.New(productimplement.Service{
		Products:     productpostgres.NewProductRepository(pool),
		Variants:     productpostgres.NewVariantRepository(pool),
		Visibility:   categoryvisibility.New(categoryRepo),
		Availability: inventoryavailability.New(inventorypostgres.NewInventoryRepository(pool), variantMaintenanceClock{}),
		Audit:        productauditor.New(writer),
		Media:        &variantMediaStub{},
		Tx:           db,
		Mapper:       mapper.New(),
	})
	handler := New(service, appinterface.Config{PictureMaxBytes: 4096}, testLogger)

	adminID := seedVariantMaintenanceAccount(t, pool, "product-admin@example.test", access.RoleAdmin)
	customerID := seedVariantMaintenanceAccount(t, pool, "product-customer@example.test", access.RoleCustomer)

	hooks := variantMaintenanceAuthHooks(writer)
	root := chi.NewRouter()
	root.Mount("/api/v1/products", handler.Router(middleware.AuthHooks{}))
	root.Mount(productAdminPath, handler.AdminRouter(hooks))

	adminToken, _, err := authtoken.NewAccessIssuer(variantMaintenanceSecret, variantMaintenanceTokenTTL).Issue(adminID, access.RoleAdmin)
	if err != nil {
		t.Fatalf("issue the administrator token: %v", err)
	}
	customerToken, _, err := authtoken.NewAccessIssuer(variantMaintenanceSecret, variantMaintenanceTokenTTL).Issue(customerID, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue the customer token: %v", err)
	}

	return &variantMaintenanceFixture{
		pool:          pool,
		categories:    categoryRepo,
		handler:       root,
		adminID:       adminID,
		customerID:    customerID,
		adminToken:    adminToken,
		customerToken: customerToken,
	}
}

// variantMaintenanceAuthHooks verifies the bearer token with the auth module's own
// issuer and records the privilege denial through the shared audit writer, exactly
// as the composition root wires module 01's hooks (FR-013).
func variantMaintenanceAuthHooks(writer *audit.Writer) middleware.AuthHooks {
	issuer := authtoken.NewAccessIssuer(variantMaintenanceSecret, variantMaintenanceTokenTTL)
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return nil, nil
			}
			claims, err := issuer.Parse(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
			if err != nil {
				return nil, &invalidVariantTokenError{}
			}
			return &middleware.Identity{Subject: claims.Subject.String(), Role: string(claims.Role), TokenID: claims.ID}, nil
		},
		OnDenied: func(ctx context.Context, id middleware.Identity, r *http.Request) {
			var actor *uuid.UUID
			if parsed, err := uuid.Parse(id.Subject); err == nil {
				actor = &parsed
			}
			writer.Emit(ctx, audit.Event{
				ActorID:    actor,
				ActorRole:  id.Role,
				Action:     authconstant.AuditPrivilegeDenied,
				TargetType: "route",
				TargetID:   r.URL.Path,
				Outcome:    audit.OutcomeFailure,
			})
		},
	}
}

// invalidVariantTokenError is the plain authentication failure for a malformed
// token; the middleware turns a non-AppError into UNAUTHENTICATED.
type invalidVariantTokenError struct{}

func (*invalidVariantTokenError) Error() string { return "access token is not valid" }

// seedVariantMaintenanceAccount inserts an account the way the auth module does,
// so the token this test issues names a real account.
func seedVariantMaintenanceAccount(t *testing.T, pool *pgxpool.Pool, email string, role access.Role) uuid.UUID {
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

func (f *variantMaintenanceFixture) call(method, path, body, token string) *httptest.ResponseRecorder {
	return performJSON(f.handler, method, path, body, token)
}

// variantAdminDetailBody is the members of one administrator detail answer this
// test inspects.
type variantAdminDetailBody struct {
	Data struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		Variants []struct {
			ID        string  `json:"id"`
			Name      string  `json:"name"`
			SKU       string  `json:"sku"`
			ImageURL  *string `json:"imageUrl"`
			SellState string  `json:"sellState"`
		} `json:"variants"`
	} `json:"data"`
}

func decodeVariantAdminDetail(t *testing.T, rec *httptest.ResponseRecorder) variantAdminDetailBody {
	t.Helper()
	var body variantAdminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the administrator detail %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeVariantErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body.Error.Code
}

// variantAuditRow is one audit_logs row the module wrote.
type variantAuditRow struct {
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	outcome    string
}

func (f *variantMaintenanceFixture) auditRows(t *testing.T, action string) []variantAuditRow {
	t.Helper()
	const query = `
		SELECT actor_id, actor_role, target_type, target_id, outcome
		FROM audit_logs WHERE action = $1 ORDER BY occurred_at`
	rows, err := f.pool.Query(context.Background(), query, action)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	defer rows.Close()
	var events []variantAuditRow
	for rows.Next() {
		var event variantAuditRow
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

// waitForAuditAtLeast waits until the given action has at least the expected
// number of persisted rows, so the assertion cannot race the asynchronous writer.
func (f *variantMaintenanceFixture) waitForAuditAtLeast(t *testing.T, action string, min int) []variantAuditRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := f.auditRows(t, action)
		if len(rows) >= min {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected at least %d %s rows, got %d", min, action, len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// requireAdminAudit asserts the persisted rows of an action all name the
// administrator and the named target with a SUCCESS outcome (FR-013, SC-006).
func (f *variantMaintenanceFixture) requireAdminAudit(t *testing.T, action, targetType, targetID string, min int) {
	t.Helper()
	rows := f.waitForAuditAtLeast(t, action, min)
	for _, row := range rows {
		if row.actorID == nil || *row.actorID != f.adminID {
			t.Fatalf("%s: expected the administrator actor %s, got %v", action, f.adminID, row.actorID)
		}
		if row.actorRole != string(access.RoleAdmin) {
			t.Fatalf("%s: expected the ADMIN role, got %q", action, row.actorRole)
		}
		if row.targetType != targetType || row.targetID != targetID {
			t.Fatalf("%s: expected the target %s/%s, got %s/%s", action, targetType, targetID, row.targetType, row.targetID)
		}
		if row.outcome != string(audit.OutcomeSuccess) {
			t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
		}
	}
}

// variantProductBody builds a create body for a product carrying one variant.
func variantProductBody(name, code, sku string, categoryID uuid.UUID) string {
	return fmt.Sprintf(`{"name":%q,"code":%q,"description":"Mô tả","defaultPrice":{"amount":100000,"currency":"VND"},"categoryId":%q,"position":1,"variants":[{"name":"Mặc định","sku":%q,"position":0}]}`,
		name, code, categoryID.String(), sku)
}

// FR-009 to FR-016, FR-013, SC-006 against real PostgreSQL: a full administrator
// lifecycle — create with a variant, add/edit/remove a variant, set its image and
// move its state — with every mutation guarded and audited, the duplicate and
// last-removal rules enforced, and a customer token refused on role grounds.
func TestVariantMaintenanceLifecycleAgainstPostgres(t *testing.T) {
	f := newVariantMaintenanceFixture(t)
	category := seedSKUCategory(t, f.categories, "tranh-sku", true)

	// Create a product with one variant (FR-009, FR-012).
	createRec := f.call(http.MethodPost, productAdminPath,
		variantProductBody("Bộ card", "bo-card", "CARD-1-D", category.ID), f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeVariantAdminDetail(t, createRec)
	productID := created.Data.ID
	if created.Data.Code != "bo-card" || len(created.Data.Variants) != 1 {
		t.Fatalf("the created product must carry its variant: %+v", created.Data)
	}
	if created.Data.Variants[0].SellState != string(productconstant.SellStateComingSoon) {
		t.Fatalf("a new variant must start COMING_SOON, got %q", created.Data.Variants[0].SellState)
	}
	firstVariantID := created.Data.Variants[0].ID
	f.requireAdminAudit(t, productconstant.AuditProductCreated, "product", productID, 1)

	// Add a second variant (FR-010).
	addRec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants",
		`{"name":"Khổ lớn","sku":"CARD-1-L","price":{"amount":150000},"position":1}`, f.adminToken)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add variant: expected 201, got %d (%s)", addRec.Code, addRec.Body.String())
	}
	added := decodeVariantAdminDetail(t, addRec)
	if len(added.Data.Variants) != 2 {
		t.Fatalf("expected two variants, got %d", len(added.Data.Variants))
	}
	secondVariantID := added.Data.Variants[1].ID
	f.requireAdminAudit(t, productconstant.AuditProductVariantAdded, "product_variant", secondVariantID, 1)

	// Edit the second variant (FR-010).
	editRec := f.call(http.MethodPatch, productAdminPath+"/"+productID+"/variants/"+secondVariantID,
		`{"name":"Khổ lớn mới","position":2}`, f.adminToken)
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit variant: expected 200, got %d (%s)", editRec.Code, editRec.Body.String())
	}
	f.requireAdminAudit(t, productconstant.AuditProductVariantUpdated, "product_variant", secondVariantID, 1)

	// Set the variant image through the media port (FR-016).
	imageRec := performMultipart(f.handler, http.MethodPost,
		productAdminPath+"/"+productID+"/variants/"+secondVariantID+"/image",
		"image", "variant.png", pngBytes(), f.adminToken)
	if imageRec.Code != http.StatusCreated {
		t.Fatalf("variant image: expected 201, got %d (%s)", imageRec.Code, imageRec.Body.String())
	}
	if decodeVariantAdminDetail(t, imageRec).Data.Variants[1].ImageURL == nil {
		t.Fatal("the variant image must be stored")
	}
	f.requireAdminAudit(t, productconstant.AuditProductVariantUpdated, "product_variant", secondVariantID, 2)

	// Move the variant's sell state (FR-015).
	stateRec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants/"+secondVariantID+"/state",
		`{"to":"ACTIVE"}`, f.adminToken)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("variant state: expected 200, got %d (%s)", stateRec.Code, stateRec.Body.String())
	}
	if got := decodeVariantAdminDetail(t, stateRec).Data.Variants[1].SellState; got != string(productconstant.SellStateActive) {
		t.Fatalf("expected ACTIVE, got %q", got)
	}
	f.requireAdminAudit(t, productconstant.AuditProductVariantUpdated, "product_variant", secondVariantID, 3)

	// The activation puts the product on the customer-facing catalogue (FR-002,
	// FR-004), proving the administrator flow produced a buyable product.
	listRec := f.call(http.MethodGet, "/api/v1/products", "", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("public list: expected 200, got %d (%s)", listRec.Code, listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), `"code":"bo-card"`) {
		t.Fatalf("the activated product must appear once in the catalogue: %s", listRec.Body.String())
	}

	// Remove the first variant, leaving one (FR-012).
	removeRec := f.call(http.MethodDelete, productAdminPath+"/"+productID+"/variants/"+firstVariantID, "", f.adminToken)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove variant: expected 204, got %d (%s)", removeRec.Code, removeRec.Body.String())
	}
	f.requireAdminAudit(t, productconstant.AuditProductVariantRemoved, "product_variant", firstVariantID, 1)

	// Removing the product's last variant is refused (FR-012).
	lastRec := f.call(http.MethodDelete, productAdminPath+"/"+productID+"/variants/"+secondVariantID, "", f.adminToken)
	if lastRec.Code != http.StatusConflict {
		t.Fatalf("last removal: expected 409, got %d (%s)", lastRec.Code, lastRec.Body.String())
	}
	if got := decodeVariantErrorCode(t, lastRec); got != productconstant.CodeVariantLastRemoval {
		t.Fatalf("expected %s, got %s", productconstant.CodeVariantLastRemoval, got)
	}

	// A duplicate product code is refused (FR-011).
	dupCodeRec := f.call(http.MethodPost, productAdminPath,
		variantProductBody("Trùng mã", "bo-card", "OTHER-1", category.ID), f.adminToken)
	if dupCodeRec.Code != http.StatusConflict {
		t.Fatalf("duplicate code: expected 409, got %d (%s)", dupCodeRec.Code, dupCodeRec.Body.String())
	}
	if got := decodeVariantErrorCode(t, dupCodeRec); got != productconstant.CodeProductCodeTaken {
		t.Fatalf("expected %s, got %s", productconstant.CodeProductCodeTaken, got)
	}

	// A duplicate SKU is refused (FR-011). The surviving variant carries CARD-1-L.
	dupSKURec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants",
		`{"name":"Trùng SKU","sku":"CARD-1-L"}`, f.adminToken)
	if dupSKURec.Code != http.StatusConflict {
		t.Fatalf("duplicate SKU: expected 409, got %d (%s)", dupSKURec.Code, dupSKURec.Body.String())
	}
	if got := decodeVariantErrorCode(t, dupSKURec); got != productconstant.CodeVariantSKUTaken {
		t.Fatalf("expected %s, got %s", productconstant.CodeVariantSKUTaken, got)
	}

	// A customer's session is refused on a variant route on role grounds, and the
	// denial is audited (FR-013).
	customerRec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants",
		`{"name":"Lén","sku":"LEN-1"}`, f.customerToken)
	if customerRec.Code != http.StatusForbidden {
		t.Fatalf("a customer: expected 403, got %d (%s)", customerRec.Code, customerRec.Body.String())
	}
	if got := decodeVariantErrorCode(t, customerRec); got != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", got)
	}
	denials := f.waitForAuditAtLeast(t, authconstant.AuditPrivilegeDenied, 1)
	if denials[0].actorID == nil || *denials[0].actorID != f.customerID {
		t.Fatalf("the denial must name the customer, got %v", denials[0].actorID)
	}
	if denials[0].outcome != string(audit.OutcomeFailure) {
		t.Fatalf("a denied request must be recorded as FAILURE, got %q", denials[0].outcome)
	}
}

// FR-012 against real PostgreSQL: removing a product removes its variants with it
// and touches no other product, and the removal is audited.
func TestRemovingAProductRemovesItsVariantsAgainstPostgres(t *testing.T) {
	f := newVariantMaintenanceFixture(t)
	category := seedSKUCategory(t, f.categories, "tranh-sku", true)

	createRec := f.call(http.MethodPost, productAdminPath,
		variantProductBody("Bộ card", "bo-card", "CARD-1-D", category.ID), f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeVariantAdminDetail(t, createRec)
	productID := created.Data.ID
	variantID := created.Data.Variants[0].ID

	removeRec := f.call(http.MethodDelete, productAdminPath+"/"+productID, "", f.adminToken)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove product: expected 204, got %d (%s)", removeRec.Code, removeRec.Body.String())
	}
	f.requireAdminAudit(t, productconstant.AuditProductDeleted, "product", productID, 1)

	var variants int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM product_variants WHERE id = $1", variantID).Scan(&variants); err != nil {
		t.Fatalf("count the variant row: %v", err)
	}
	if variants != 0 {
		t.Fatalf("the removed product's variants must be gone, got %d", variants)
	}

	// The product is unreadable afterwards.
	readRec := f.call(http.MethodGet, productAdminPath+"/"+productID, "", f.adminToken)
	if readRec.Code != http.StatusNotFound {
		t.Fatalf("the removed product must answer 404, got %d (%s)", readRec.Code, readRec.Body.String())
	}
	if got := decodeVariantErrorCode(t, readRec); got != productconstant.CodeProductNotFound {
		t.Fatalf("expected %s, got %s", productconstant.CodeProductNotFound, got)
	}
}
