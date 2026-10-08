//go:build integration

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	productappinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	productmodel "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	productauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/auditor"
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
// what the public surface then shows and that the write left its audit row. The
// concurrency tests then prove the two guarantees a fake cannot: the single main
// picture and the ten-picture ceiling (research D6, quickstart 10i, 10j).

const (
	// productIntegrationSecret is a throwaway HS256 key for the test tokens.
	productIntegrationSecret = "0123456789abcdef0123456789abcdef"
	// productIntegrationTokenTTL is long enough that no token expires mid-test.
	productIntegrationTokenTTL = 15 * time.Minute
)

// productMaintenanceFixture is the module wired against real infrastructure.
type productMaintenanceFixture struct {
	handler       http.Handler
	pool          *pgxpool.Pool
	writer        *audit.Writer
	media         *productMediaStub
	products      *productpostgres.ProductRepository
	categoryRepo  *categorypostgres.CategoryRepository
	adminID       uuid.UUID
	customerID    uuid.UUID
	adminToken    string
	customerToken string
}

func newProductMaintenanceFixture(t *testing.T) *productMaintenanceFixture {
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
	media := newProductMediaStub()
	config := productappinterface.Config{}
	service := productimplement.New(productimplement.Service{
		Products:   products,
		Visibility: categoryvisibility.New(categoryRepo),
		Audit:      productauditor.New(writer),
		Media:      media,
		Tx:         &database.DB{Pool: pool},
		Config:     config,
		Mapper:     mapper.New(),
	})
	handler := New(service, config, testLogger)

	adminID := seedProductAccount(t, pool, "product-admin@example.com", access.RoleAdmin)
	customerID := seedProductAccount(t, pool, "product-customer@example.com", access.RoleCustomer)

	hooks := productAuthHooks(t)
	root := chi.NewRouter()
	root.Mount(publicProductsPath, handler.Router(middleware.AuthHooks{}))
	root.Mount(adminProductsPath, handler.AdminRouter(hooks))

	adminToken, _, err := authtoken.NewAccessIssuer(productIntegrationSecret, productIntegrationTokenTTL).Issue(adminID, access.RoleAdmin)
	if err != nil {
		t.Fatalf("issue the administrator token: %v", err)
	}
	customerToken, _, err := authtoken.NewAccessIssuer(productIntegrationSecret, productIntegrationTokenTTL).Issue(customerID, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue the customer token: %v", err)
	}

	return &productMaintenanceFixture{
		handler:       root,
		pool:          pool,
		writer:        writer,
		media:         media,
		products:      products,
		categoryRepo:  categoryRepo,
		adminID:       adminID,
		customerID:    customerID,
		adminToken:    adminToken,
		customerToken: customerToken,
	}
}

// productAuthHooks verifies the bearer token with the auth module's own issuer
// and maps its failures the same way the composition root does.
func productAuthHooks(t *testing.T) middleware.AuthHooks {
	t.Helper()
	issuer := authtoken.NewAccessIssuer(productIntegrationSecret, productIntegrationTokenTTL)
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

// seedProductAccount inserts an account the way the auth module does, so the
// token this test issues names a real account.
func seedProductAccount(t *testing.T, pool *pgxpool.Pool, email string, role access.Role) uuid.UUID {
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

func (f *productMaintenanceFixture) call(method, path, body, token string) *httptest.ResponseRecorder {
	return performJSON(f.handler, method, path, body, token)
}

// publicSlugs reads the public catalogue and returns the slugs it shows.
func (f *productMaintenanceFixture) publicSlugs(t *testing.T) []string {
	t.Helper()
	rec := getProductWithRequestID(t, f.handler, publicProductsPath, "list")
	if rec.Code != http.StatusOK {
		t.Fatalf("public list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeProductCatalogueList(t, rec)
	out := make([]string, 0, len(body.Data))
	for _, entry := range body.Data {
		out = append(out, entry.Slug)
	}
	return out
}

func (f *productMaintenanceFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var total int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&total); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return total
}

// productAuditRow is one audit_logs row the module wrote.
type productAuditRow struct {
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	outcome    string
}

func (f *productMaintenanceFixture) auditRows(t *testing.T, action string) []productAuditRow {
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

func (f *productMaintenanceFixture) waitForAuditRows(t *testing.T, action string, want int) []productAuditRow {
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

// requireAudit asserts the one row of an action names the administrator, the
// product and a SUCCESS outcome.
func (f *productMaintenanceFixture) requireAudit(t *testing.T, action string, productID uuid.UUID) {
	t.Helper()
	rows := f.waitForAuditRows(t, action, 1)
	row := rows[0]
	if row.actorID == nil || *row.actorID != f.adminID {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, f.adminID, row.actorID)
	}
	if row.actorRole != string(access.RoleAdmin) {
		t.Fatalf("%s: expected the ADMIN role, got %q", action, row.actorRole)
	}
	if row.targetType != "product" || row.targetID != productID.String() {
		t.Fatalf("%s: expected the product as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// seedActiveProduct stores one ACTIVE product directly, so a concurrency test
// starts from a visible catalogue row.
func seedActiveProduct(t *testing.T, repo *productpostgres.ProductRepository, categoryID uuid.UUID, slug string) *productmodel.Product {
	t.Helper()
	product, err := productmodel.NewProduct(productmodel.ProductDraft{
		Name: "Product " + slug, Slug: slug, Price: productPrice(1000),
		CategoryID: categoryID, Position: 1,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", slug, err)
	}
	if err := product.Launch(time.Now().UTC()); err != nil {
		t.Fatalf("Launch(%q): %v", slug, err)
	}
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("Create(%q): %v", slug, err)
	}
	return product
}

// insertPictureViaRepo stores one picture through the adapter, so a test can seed
// a product with pictures without going through the upload endpoint.
func insertPictureViaRepo(t *testing.T, repo *productpostgres.ProductRepository, productID uuid.UUID, position int, primary bool) uuid.UUID {
	t.Helper()
	picture := &productmodel.Picture{
		ID: uuid.New(), ProductID: productID, PublicID: "demo/seed",
		URL: "https://cdn.example.test/seed.jpg", Width: 800, Height: 600,
		Position: position, IsPrimary: primary, CreatedAt: time.Now().UTC(),
	}
	if err := repo.AddPicture(context.Background(), picture); err != nil {
		t.Fatalf("AddPicture: %v", err)
	}
	return picture.ID
}

// FR-010 to FR-014, SC-006, SC-008 against real PostgreSQL: create, edit, add
// pictures, promote one, remove one and remove the product, checking after each
// step what the public list shows and that the write left its audit row. A
// product's pictures and membership rows are gone after the product is.
func TestProductMaintenanceLifecycleAgainstPostgres(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	ctx := context.Background()
	category := seedCatalogueCategory(t, f.categoryRepo, "tranh", true)

	// Create: the product starts in COMING_SOON and no customer-facing route
	// reveals it.
	createRec := f.call(http.MethodPost, adminProductsPath,
		`{"name":"Acrylic stand","slug":"acrylic-stand","description":"Acrylic stand 15cm","price":{"amount":120000,"currency":"VND"},"categoryId":"`+category.ID.String()+`","position":10}`,
		f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeAdminProduct(t, createRec)
	if created.SellState != string(constant.SellStateComingSoon) || created.ID == uuid.Nil {
		t.Fatalf("unexpected created product: %+v", created)
	}
	if got := f.publicSlugs(t); len(got) != 0 {
		t.Fatalf("a COMING_SOON product must not appear in the public list, got %v", got)
	}
	adminList := f.call(http.MethodGet, adminProductsPath, "", f.adminToken)
	if adminList.Code != http.StatusOK || !strings.Contains(adminList.Body.String(), "acrylic-stand") {
		t.Fatalf("the administrator list must include the created product, got %d (%s)", adminList.Code, adminList.Body.String())
	}
	f.requireAudit(t, constant.AuditProductCreated, created.ID)

	// Launch directly: the state route is US3, so the public list only becomes
	// meaningful once the product is on sale.
	view, err := f.products.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := view.Product.Launch(time.Now().UTC()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := f.products.Update(ctx, &view.Product); err != nil {
		t.Fatalf("Update after launch: %v", err)
	}
	if got := f.publicSlugs(t); len(got) != 1 || got[0] != "acrylic-stand" {
		t.Fatalf("an on-sale product must appear in the public list, got %v", got)
	}

	// Edit: the customer-facing view reflects the change on the next request.
	editRec := f.call(http.MethodPatch, adminProductsPath+"/"+created.ID.String(),
		`{"name":"Acrylic stand mới","price":{"amount":130000,"currency":"VND"}}`, f.adminToken)
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", editRec.Code, editRec.Body.String())
	}
	edited := decodeAdminProduct(t, editRec)
	if edited.Name != "Acrylic stand mới" || edited.Price.Amount != 130000 {
		t.Fatalf("the edit did not change the fields it carried: %+v", edited)
	}
	publicDetail := getProductWithRequestID(t, f.handler, publicProductsPath+"/acrylic-stand", "detail")
	if publicDetail.Code != http.StatusOK || !strings.Contains(publicDetail.Body.String(), "Acrylic stand mới") {
		t.Fatalf("the public detail must reflect the edit, got %d (%s)", publicDetail.Code, publicDetail.Body.String())
	}
	f.requireAudit(t, constant.AuditProductUpdated, created.ID)

	// Add two pictures: the first becomes the main one.
	firstRec := postImage(f.handler, adminProductsPath+"/"+created.ID.String()+"/images", "one.png", "image/png", pngBytes(), f.adminToken)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("add first picture: expected 201, got %d (%s)", firstRec.Code, firstRec.Body.String())
	}
	first := decodeAdminProduct(t, firstRec)
	if len(first.Images) != 1 || !first.Images[0].IsPrimary {
		t.Fatalf("the first picture must be the main one: %+v", first.Images)
	}
	secondRec := postImage(f.handler, adminProductsPath+"/"+created.ID.String()+"/images", "two.png", "image/png", pngBytes(), f.adminToken)
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("add second picture: expected 201, got %d (%s)", secondRec.Code, secondRec.Body.String())
	}
	second := decodeAdminProduct(t, secondRec)
	if len(second.Images) != 2 || second.Images[0].IsPrimary == second.Images[1].IsPrimary {
		t.Fatalf("exactly one of the two pictures must be the main one: %+v", second.Images)
	}
	f.waitForAuditRows(t, constant.AuditProductImageAdded, 2)

	// Promote the second picture.
	promoteRec := f.call(http.MethodPost, adminProductsPath+"/"+created.ID.String()+"/images/"+second.Images[1].ID.String()+"/primary", "", f.adminToken)
	if promoteRec.Code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d (%s)", promoteRec.Code, promoteRec.Body.String())
	}
	promoted := decodeAdminProduct(t, promoteRec)
	if !promoted.Images[1].IsPrimary || promoted.Images[0].IsPrimary {
		t.Fatalf("the promoted picture must be the only main one: %+v", promoted.Images)
	}
	f.requireAudit(t, constant.AuditProductImagePrimarySet, created.ID)

	// Remove the main picture: the next by position is promoted.
	removeRec := f.call(http.MethodDelete, adminProductsPath+"/"+created.ID.String()+"/images/"+second.Images[1].ID.String(), "", f.adminToken)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove picture: expected 204, got %d (%s)", removeRec.Code, removeRec.Body.String())
	}
	afterRemove := f.call(http.MethodGet, adminProductsPath+"/"+created.ID.String(), "", f.adminToken)
	if afterRemove.Code != http.StatusOK {
		t.Fatalf("read after removing a picture: %d (%s)", afterRemove.Code, afterRemove.Body.String())
	}
	remaining := decodeAdminProduct(t, afterRemove)
	if len(remaining.Images) != 1 || !remaining.Images[0].IsPrimary {
		t.Fatalf("removing the main picture must promote the next one: %+v", remaining.Images)
	}
	f.requireAudit(t, constant.AuditProductImageRemoved, created.ID)

	// Membership rows: record the product as a set and confirm the cascade
	// removes them without touching the member.
	member := seedActiveProduct(t, f.products, category.ID, "member-product")
	if err := f.products.ReplaceSetMembers(ctx, created.ID, []uuid.UUID{member.ID}); err != nil {
		t.Fatalf("ReplaceSetMembers: %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1`, created.ID); got != 1 {
		t.Fatalf("expected one membership row, got %d", got)
	}

	// Remove the product: the row goes, cascading to its pictures and membership
	// rows, and the member survives.
	deleteRec := f.call(http.MethodDelete, adminProductsPath+"/"+created.ID.String(), "", f.adminToken)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("remove product: expected 204, got %d (%s)", deleteRec.Code, deleteRec.Body.String())
	}
	if got := f.publicSlugs(t); containsSlug(got, "acrylic-stand") {
		t.Fatalf("a removed product must leave no trace, got %v", got)
	}
	if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, created.ID); got != 0 {
		t.Fatalf("the removed product row survived, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1`, created.ID); got != 0 {
		t.Fatalf("the removed product's pictures survived the cascade, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1`, created.ID); got != 0 {
		t.Fatalf("the removed product's membership rows survived the cascade, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, member.ID); got != 1 {
		t.Fatalf("removing a set must not remove its member, member count is %d", got)
	}
	f.requireAudit(t, constant.AuditProductDeleted, created.ID)

	// A second removal is the same not-found rather than an internal failure.
	again := f.call(http.MethodDelete, adminProductsPath+"/"+created.ID.String(), "", f.adminToken)
	if again.Code != http.StatusNotFound {
		t.Fatalf("a second removal: expected 404, got %d (%s)", again.Code, again.Body.String())
	}
}

// research D6, quickstart 10i and 10j against real PostgreSQL: two concurrent
// promotions leave exactly one main picture, and two concurrent uploads at the
// ceiling admit at most ten.
func TestConcurrentPictureOperationsRespectTheSinglePrimaryAndTheCeiling(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	ctx := context.Background()
	category := seedCatalogueCategory(t, f.categoryRepo, "tranh-concurrency", true)

	// Two concurrent promotions leave exactly one main picture.
	promotedProduct := seedActiveProduct(t, f.products, category.ID, "promote-product")
	first := insertPictureViaRepo(t, f.products, promotedProduct.ID, 0, true)
	second := insertPictureViaRepo(t, f.products, promotedProduct.ID, 1, false)

	var promotions sync.WaitGroup
	for _, pictureID := range []uuid.UUID{first, second} {
		promotions.Add(1)
		go func(id uuid.UUID) {
			defer promotions.Done()
			rec := f.call(http.MethodPost,
				adminProductsPath+"/"+promotedProduct.ID.String()+"/images/"+id.String()+"/primary", "", f.adminToken)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent promotion: expected 200, got %d (%s)", rec.Code, rec.Body.String())
			}
		}(pictureID)
	}
	promotions.Wait()
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1 AND is_primary`, promotedProduct.ID); got != 1 {
		t.Fatalf("two concurrent promotions must leave exactly one main picture, got %d", got)
	}
	_ = ctx

	// Two concurrent uploads at the ceiling admit at most ten. The media stub
	// holds both uploads until they have both passed the pre-upload count check,
	// so the insert transaction is the only thing that can serialise them.
	ceilingProduct := seedActiveProduct(t, f.products, category.ID, "ceiling-product")
	for i := 0; i < productmodel.MaxProductPictures-1; i++ {
		insertPictureViaRepo(t, f.products, ceilingProduct.ID, i, i == 0)
	}

	var arrived sync.WaitGroup
	arrived.Add(2)
	f.media.beforeUpload = func() {
		arrived.Done()
		arrived.Wait()
	}
	defer func() { f.media.beforeUpload = nil }()

	var uploads sync.WaitGroup
	for i := 0; i < 2; i++ {
		uploads.Add(1)
		go func() {
			defer uploads.Done()
			postImage(f.handler, adminProductsPath+"/"+ceilingProduct.ID.String()+"/images", "x.png", "image/png", pngBytes(), f.adminToken)
		}()
	}
	uploads.Wait()

	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1`, ceilingProduct.ID); got > productmodel.MaxProductPictures {
		t.Fatalf("two concurrent uploads at the ceiling admitted %d pictures, want at most %d", got, productmodel.MaxProductPictures)
	}
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1 AND is_primary`, ceilingProduct.ID); got != 1 {
		t.Fatalf("the ceiling product must still have exactly one main picture, got %d", got)
	}
}

// T045 negative control: the partial unique index is what refuses a second main
// picture from a writer that bypasses the repository. Dropping it inside a
// rollback-only transaction lets the same insert succeed, which is the evidence
// that the promotion guarantee is the index and not an application check.
func TestThePartialUniqueIndexRefusesASecondPrimaryAgainstPostgres(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	ctx := context.Background()
	category := seedCatalogueCategory(t, f.categoryRepo, "tranh-index", true)
	product := seedActiveProduct(t, f.products, category.ID, "index-product")

	if _, err := f.rawInsertPrimary(ctx, product.ID); err != nil {
		t.Fatalf("the first main picture must be accepted, got %v", err)
	}
	if _, err := f.rawInsertPrimary(ctx, product.ID); err == nil {
		t.Fatal("the storage layer accepted a second main picture")
	} else if name := constraintNameOf(err); name != "product_images_one_primary_key" {
		t.Fatalf("expected the one-primary index to refuse the row, got %v", err)
	}

	// Negative control: without the index the same row is storable.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DROP INDEX product_images_one_primary_key`); err != nil {
		t.Fatalf("drop the index inside the transaction: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO product_images (id, product_id, public_id, secure_url, width, height, position, is_primary)
		 VALUES ($1, $2, 'demo/negative', 'https://example.test/negative.jpg', 10, 10, 5, true)`,
		uuid.New(), product.ID); err != nil {
		t.Fatalf("without the index the second main picture must be storable, got %v", err)
	}
}

// rawInsertPrimary writes one main picture straight into the table, bypassing the
// adapter and the domain, so the index is the only thing that can refuse it.
func (f *productMaintenanceFixture) rawInsertPrimary(ctx context.Context, productID uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	_, err := f.pool.Exec(ctx,
		`INSERT INTO product_images (id, product_id, public_id, secure_url, width, height, position, is_primary)
		 VALUES ($1, $2, $3, 'https://example.test/raw.jpg', 10, 10, 0, true)`,
		id, productID, "demo/raw/"+id.String())
	return id, err
}

// constraintNameOf returns the constraint a PostgreSQL error names, or "".
func constraintNameOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// containsSlug reports whether the public list shows the given slug.
func containsSlug(slugs []string, want string) bool {
	for _, slug := range slugs {
		if slug == want {
			return true
		}
	}
	return false
}

// FR-039, US5 scenarios 1-3 against real PostgreSQL: the membership rows are
// written and read in order, removing a member drops it from the set it was in
// while leaving the others, and removing the set cascades its membership rows and
// leaves the members alone.
func TestSetMembershipAgainstPostgres(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	category := seedCatalogueCategory(t, f.categoryRepo, "combo", true)
	first := seedActiveProduct(t, f.products, category.ID, "combo-member-first")
	second := seedActiveProduct(t, f.products, category.ID, "combo-member-second")

	create := f.call(http.MethodPost, adminProductsPath, setBody(category.ID, "combo-aki", 300000, []uuid.UUID{first.ID, second.ID}), f.adminToken)
	if create.Code != http.StatusCreated {
		t.Fatalf("create set: expected 201, got %d (%s)", create.Code, create.Body.String())
	}
	set := decodeAdminProduct(t, create)
	if !set.IsSet || set.Price.Amount != 300000 {
		t.Fatalf("the set must keep its own price rather than the members' sum: %+v", set)
	}
	if len(set.Members) != 2 || set.Members[0].ID != first.ID || set.Members[1].ID != second.ID {
		t.Fatalf("the members must be written and read in order, got %+v", set.Members)
	}

	// The rows themselves exist in the stored order.
	rows, err := f.pool.Query(context.Background(),
		`SELECT member_product_id FROM product_set_items WHERE set_product_id = $1 ORDER BY position`, set.ID)
	if err != nil {
		t.Fatalf("read membership rows: %v", err)
	}
	defer rows.Close()
	ordered := make([]uuid.UUID, 0, 2)
	for rows.Next() {
		var memberID uuid.UUID
		if err := rows.Scan(&memberID); err != nil {
			t.Fatalf("scan membership row: %v", err)
		}
		ordered = append(ordered, memberID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate membership rows: %v", err)
	}
	if len(ordered) != 2 || ordered[0] != first.ID || ordered[1] != second.ID {
		t.Fatalf("the membership rows must be stored in order, got %v", ordered)
	}

	// A self-referencing member reaches the storage check and is reported as a
	// validation error naming the member field, not as a 500, and the failed
	// transaction leaves the stored members untouched (quickstart 11g).
	selfRef := f.call(http.MethodPatch, adminProductsPath+"/"+set.ID.String(),
		`{"memberProductIds":["`+set.ID.String()+`"]}`, f.adminToken)
	if selfRef.Code != http.StatusBadRequest {
		t.Fatalf("a self-referencing member: expected 400, got %d (%s)", selfRef.Code, selfRef.Body.String())
	}
	if body := decodeAdminError(t, selfRef); body.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("a self-referencing member: expected VALIDATION_ERROR, got %s", body.Error.Code)
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1`, set.ID); got != 2 {
		t.Fatalf("a refused self-reference must leave the members untouched, got %d", got)
	}

	// Removing a member removes it from the set it was in and leaves the other.
	removeMember := f.call(http.MethodDelete, adminProductsPath+"/"+first.ID.String(), "", f.adminToken)
	if removeMember.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d (%s)", removeMember.Code, removeMember.Body.String())
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1 AND member_product_id = $2`, set.ID, first.ID); got != 0 {
		t.Fatalf("removing a member must drop it from the set, got %d membership rows", got)
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1 AND member_product_id = $2`, set.ID, second.ID); got != 1 {
		t.Fatalf("removing one member must leave the other in the set, got %d membership rows", got)
	}

	// Removing the set cascades its membership rows and leaves the member.
	removeSet := f.call(http.MethodDelete, adminProductsPath+"/"+set.ID.String(), "", f.adminToken)
	if removeSet.Code != http.StatusNoContent {
		t.Fatalf("remove set: expected 204, got %d (%s)", removeSet.Code, removeSet.Body.String())
	}
	if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1`, set.ID); got != 0 {
		t.Fatalf("removing a set must cascade its membership rows, got %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, second.ID); got != 1 {
		t.Fatalf("removing a set must leave its member, product count is %d", got)
	}
}

// T060 negative control: a self-referencing membership is refused by the
// storage check product_set_items_not_self_ck, not by an application check.
// Dropping the constraint inside a rollback-only transaction lets the same row
// through, which is the evidence that the guarantee lives in storage.
func TestTheSelfReferenceCheckRefusesASelfMemberAgainstPostgres(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	ctx := context.Background()
	category := seedCatalogueCategory(t, f.categoryRepo, "self-check", true)
	product := seedActiveProduct(t, f.products, category.ID, "self-product")

	if err := f.rawInsertSelfMember(ctx, product.ID); err == nil {
		t.Fatal("the storage layer accepted a self-referencing membership")
	} else if name := constraintNameOf(err); name != "product_set_items_not_self_ck" {
		t.Fatalf("expected product_set_items_not_self_ck to refuse the row, got %v", err)
	}

	// Negative control: without the check the same row is storable.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `ALTER TABLE product_set_items DROP CONSTRAINT product_set_items_not_self_ck`); err != nil {
		t.Fatalf("drop the check inside the transaction: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO product_set_items (set_product_id, member_product_id, position) VALUES ($1, $1, 0)`,
		product.ID); err != nil {
		t.Fatalf("without the check a self-referencing membership must be storable, got %v", err)
	}
}

// rawInsertSelfMember writes a self-referencing membership straight into the
// table, bypassing the adapter and the domain, so the check constraint is the
// only thing that can refuse it.
func (f *productMaintenanceFixture) rawInsertSelfMember(ctx context.Context, productID uuid.UUID) error {
	_, err := f.pool.Exec(ctx,
		`INSERT INTO product_set_items (set_product_id, member_product_id, position) VALUES ($1, $1, 0)`,
		productID)
	return err
}
