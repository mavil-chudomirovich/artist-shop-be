//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	categorymodel "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	productmodel "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This test builds the public group against a real PostgreSQL container: real
// migrations, module 03's real repository behind the cross-module contract, the
// real product adapter and the real public use cases, with rows seeded through
// the repositories. It exists because the visibility predicate is what a fake
// cannot prove — a fake is written by the same hand that wrote the use case, so
// only a real query can show the filter is in SQL (research D1, SC-001).

const publicProductsPath = "/api/v1/products"

type productCatalogueFixture struct {
	pool     *pgxpool.Pool
	products *productpostgres.ProductRepository
	handler  http.Handler
}

func newProductCatalogueFixture(t *testing.T) *productCatalogueFixture {
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

	categoryRepo := categorypostgres.NewCategoryRepository(pool)
	products := productpostgres.NewProductRepository(pool)

	service := productimplement.New(productimplement.Service{
		Products:   products,
		Visibility: categoryvisibility.New(categoryRepo),
		Mapper:     mapper.New(),
	})
	handler := New(service, appinterface.Config{}, testLogger)

	root := chi.NewRouter()
	root.Use(productRequestID)
	root.Mount(publicProductsPath, handler.Router(middleware.AuthHooks{}))
	return &productCatalogueFixture{pool: pool, products: products, handler: root}
}

// productRequestID fills the correlation id from the request header, so the two
// not-found answers compared below carry distinct request identifiers and the
// comparison that ignores requestId is not vacuously true.
func productRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := reqctx.WithCorrelation(r.Context(), r.Header.Get("X-Request-Id"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func seedCatalogueCategory(t *testing.T, repo *categorypostgres.CategoryRepository, slug string, visible bool) *categorymodel.Category {
	t.Helper()
	category, err := categorymodel.NewCategory(categorymodel.CategoryDraft{
		Name: "Danh mục " + slug,
		Slug: slug,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewCategory(%q): %v", slug, err)
	}
	if !visible {
		category.Hide(time.Now().UTC())
	}
	if err := repo.Create(context.Background(), category); err != nil {
		t.Fatalf("Create category %q: %v", slug, err)
	}
	return category
}

// seedCatalogueProduct inserts a product through the adapter, applying an
// optional sell-state transition first so the row is stored in the state the
// test needs.
func seedCatalogueProduct(t *testing.T, repo *productpostgres.ProductRepository, draft productmodel.ProductDraft, transition func(*productmodel.Product, time.Time) error) *productmodel.Product {
	t.Helper()
	product, err := productmodel.NewProduct(draft, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Slug, err)
	}
	if transition != nil {
		if err := transition(product, time.Now().UTC()); err != nil {
			t.Fatalf("transition %q: %v", draft.Slug, err)
		}
	}
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("Create product %q: %v", draft.Slug, err)
	}
	return product
}

func productPrice(amount int64) productmodel.Price {
	return productmodel.Price{Amount: amount, Currency: "VND"}
}

func getProductWithRequestID(t *testing.T, handler http.Handler, path, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type productCatalogueListBody struct {
	Data []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
		Price struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"price"`
		ImageURL   *string `json:"imageUrl"`
		IsPreorder bool    `json:"isPreorder"`
	} `json:"data"`
	Meta struct {
		RequestID string `json:"requestId"`
		Page      int    `json:"page"`
		PageSize  int    `json:"pageSize"`
		Total     int64  `json:"total"`
	} `json:"meta"`
}

func decodeProductCatalogueList(t *testing.T, rec *httptest.ResponseRecorder) productCatalogueListBody {
	t.Helper()
	var body productCatalogueListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeProductEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-002, FR-026, SC-001, SC-003, quickstart 16e against real PostgreSQL: a
// product in a hidden category, a COMING_SOON product and an OUT_OF_STOCK one
// are absent from the list and from their own route, a pre-order is present,
// meta.total counts only what is visible, the page is not short when hidden rows
// exist, and the hidden answer matches the never-existed answer apart from the
// request identifier.
func TestPublicProductCatalogueAgainstPostgres(t *testing.T) {
	fixture := newProductCatalogueFixture(t)

	// Categories: one on display, one the operator has hidden. The hidden one is
	// seeded with a valid slug so CategoryIDBySlug still resolves it; only the
	// visibility predicate excludes its products.
	categoryRepo := categorypostgres.NewCategoryRepository(fixture.pool)
	visibleCategory := seedCatalogueCategory(t, categoryRepo, "tranh", true)
	hiddenCategory := seedCatalogueCategory(t, categoryRepo, "nhap", false)

	// Visible: an on-sale product and a pre-order, at positions after the hidden
	// rows, so an unfiltered page of two would be hidden rows (SC-001, 16e).
	onSale := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "Acrylic stand", Slug: "acrylic-stand", Description: "Acrylic stand 15cm",
		Price: productPrice(120000), CategoryID: visibleCategory.ID, Position: 10,
	}, (*productmodel.Product).Launch)
	preorder := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "Pre-order", Slug: "pre-order", Price: productPrice(300000),
		CategoryID: visibleCategory.ID, Position: 11, IsPreorder: true,
	}, nil)

	// Hidden for their own reason, each at a position that would sort first.
	announced := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "Announced", Slug: "announced", Price: productPrice(200000),
		CategoryID: visibleCategory.ID, Position: 0,
	}, nil)
	soldOut := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "Sold out", Slug: "sold-out", Price: productPrice(210000),
		CategoryID: visibleCategory.ID, Position: 1,
	}, func(product *productmodel.Product, now time.Time) error {
		if err := product.Launch(now); err != nil {
			return err
		}
		return product.SellOut(now)
	})
	retired := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "Retired", Slug: "retired", Price: productPrice(220000),
		CategoryID: visibleCategory.ID, Position: 2,
	}, func(product *productmodel.Product, now time.Time) error {
		if err := product.Launch(now); err != nil {
			return err
		}
		return product.Retire(now)
	})
	hiddenCategoryProduct := seedCatalogueProduct(t, fixture.products, productmodel.ProductDraft{
		Name: "In a hidden category", Slug: "hidden-category", Price: productPrice(230000),
		CategoryID: hiddenCategory.ID, Position: 3,
	}, (*productmodel.Product).Launch)

	// The list carries exactly the visible two, in position order, and the total
	// counts only them.
	listRec := getProductWithRequestID(t, fixture.handler, publicProductsPath, "list-1")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listRec.Code, listRec.Body.String())
	}
	list := decodeProductCatalogueList(t, listRec)
	if list.Meta.Total != 2 {
		t.Fatalf("meta.total must count only the visible products, got %d", list.Meta.Total)
	}
	if len(list.Data) != 2 {
		t.Fatalf("the page must not be short when hidden rows exist, got %d entries", len(list.Data))
	}
	if list.Data[0].Slug != onSale.Slug || list.Data[1].Slug != preorder.Slug {
		t.Fatalf("expected the visible order [%s %s], got [%s %s]",
			onSale.Slug, preorder.Slug, list.Data[0].Slug, list.Data[1].Slug)
	}
	if !list.Data[1].IsPreorder {
		t.Fatalf("the pre-order must be present and marked isPreorder")
	}
	if list.Data[0].Price.Amount != 120000 || list.Data[0].Price.Currency != "VND" {
		t.Fatalf("the price must round-trip exactly, got %+v", list.Data[0].Price)
	}

	hiddenSlugs := map[string]string{
		"announced":       announced.Slug,
		"sold out":        soldOut.Slug,
		"retired":         retired.Slug,
		"hidden category": hiddenCategoryProduct.Slug,
	}
	for reason, slug := range hiddenSlugs {
		rec := getProductWithRequestID(t, fixture.handler, publicProductsPath+"/"+slug, "detail-"+slug)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s product %q must answer 404, got %d (%s)", reason, slug, rec.Code, rec.Body.String())
		}
	}

	// The visible ones resolve on their own route.
	if rec := getProductWithRequestID(t, fixture.handler, publicProductsPath+"/"+onSale.Slug, "detail-onsale"); rec.Code != http.StatusOK {
		t.Fatalf("the on-sale product must resolve, got %d (%s)", rec.Code, rec.Body.String())
	}
	preorderRec := getProductWithRequestID(t, fixture.handler, publicProductsPath+"/"+preorder.Slug, "detail-preorder")
	if preorderRec.Code != http.StatusOK {
		t.Fatalf("the pre-order must resolve, got %d (%s)", preorderRec.Code, preorderRec.Body.String())
	}
	var preorderBody struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(preorderRec.Body.Bytes(), &preorderBody); err != nil {
		t.Fatalf("decode the pre-order detail: %v", err)
	}
	if string(preorderBody.Data["isPreorder"]) != "true" {
		t.Fatalf("the pre-order detail must report isPreorder true, got %s", preorderBody.Data["isPreorder"])
	}

	// SC-003: the hidden answer and the never-existed answer are identical apart
	// from the request identifier.
	hiddenRec := getProductWithRequestID(t, fixture.handler, publicProductsPath+"/"+hiddenCategoryProduct.Slug, "probe-hidden")
	unknownRec := getProductWithRequestID(t, fixture.handler, publicProductsPath+"/never-existed", "probe-unknown")
	if hiddenRec.Code != http.StatusNotFound || unknownRec.Code != http.StatusNotFound {
		t.Fatalf("expected both 404, got %d and %d", hiddenRec.Code, unknownRec.Code)
	}
	hiddenEnvelope := decodeProductEnvelope(t, hiddenRec)
	unknownEnvelope := decodeProductEnvelope(t, unknownRec)

	hiddenErr, ok := hiddenEnvelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("the hidden answer is not an error envelope: %s", hiddenRec.Body.String())
	}
	unknownErr, ok := unknownEnvelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown answer is not an error envelope: %s", unknownRec.Body.String())
	}
	if hiddenErr["code"] != "PRODUCT_NOT_FOUND" || unknownErr["code"] != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND for both, got %v and %v", hiddenErr["code"], unknownErr["code"])
	}
	hiddenRequestID, _ := hiddenErr["requestId"].(string)
	unknownRequestID, _ := unknownErr["requestId"].(string)
	if hiddenRequestID == unknownRequestID {
		t.Fatalf("expected distinct request identifiers so the comparison is meaningful, got %q", hiddenRequestID)
	}
	delete(hiddenErr, "requestId")
	delete(unknownErr, "requestId")
	if !reflect.DeepEqual(hiddenEnvelope, unknownEnvelope) {
		t.Fatalf("the hidden and unknown answers differ beyond the request identifier:\n hidden %s\n unknown  %s",
			hiddenRec.Body.String(), unknownRec.Body.String())
	}
}
