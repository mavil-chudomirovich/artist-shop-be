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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	categorymodel "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
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

// This file drives US1's public catalogue against a real PostgreSQL container: the
// real migrations, module 03's real repository behind the visibility contract, the
// real product and variant adapters, module 05's real availability read and the
// real public use cases. Its centre of gravity is what a fake cannot prove: the
// whole visibility predicate and the one-entry-per-product collapse run in SQL
// (FR-001, FR-002, FR-004, SC-002). Docker is required to run it; the file still
// compiles without a container so `go vet -tags integration` covers it.

const skuCataloguePath = "/api/v1/products"

// skuClock is the production-like clock module 05's availability read needs.
type skuClock struct{}

func (skuClock) Now() time.Time { return time.Now().UTC() }

// catalogueSKUFixture is one migrated PostgreSQL container plus the real public
// HTTP surface, so the catalogue can be driven through the routes a customer
// reaches.
type catalogueSKUFixture struct {
	pool       *pgxpool.Pool
	categories *categorypostgres.CategoryRepository
	products   *productpostgres.ProductRepository
	variants   *productpostgres.VariantRepository
	handler    http.Handler
}

func newCatalogueSKUFixture(t *testing.T) *catalogueSKUFixture {
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
	variants := productpostgres.NewVariantRepository(pool)
	service := productimplement.New(productimplement.Service{
		Products:     products,
		Variants:     variants,
		Visibility:   categoryvisibility.New(categoryRepo),
		Availability: inventoryavailability.New(inventorypostgres.NewInventoryRepository(pool), skuClock{}),
		Mapper:       mapper.New(),
	})
	handler := New(service, appinterface.Config{}, testLogger)

	root := chi.NewRouter()
	root.Use(skuRequestID)
	root.Mount(skuCataloguePath, handler.Router(middleware.AuthHooks{}))
	return &catalogueSKUFixture{pool: pool, categories: categoryRepo, products: products, variants: variants, handler: root}
}

// skuRequestID fills the correlation id from the request header, so two not-found
// answers compared below carry distinct request identifiers and the comparison is
// not vacuously true.
func skuRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := reqctx.WithCorrelation(r.Context(), r.Header.Get("X-Request-Id"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// seedSKUCategory creates one category, hidden when visible is false (FR-004).
func seedSKUCategory(t *testing.T, repo *categorypostgres.CategoryRepository, slug string, visible bool) *categorymodel.Category {
	t.Helper()
	category, err := categorymodel.NewCategory(categorymodel.CategoryDraft{Name: "Danh mục " + slug, Slug: slug}, time.Now().UTC())
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

// seedSKUProduct inserts one product and each of its variants through the real
// adapters, applying an optional transition per variant so a row is stored in the
// sell state the test needs. transitions[i] is applied to variant i before its
// insert; a nil entry leaves the variant in COMING_SOON.
func (f *catalogueSKUFixture) seedSKUProduct(
	t *testing.T,
	draft productmodel.ProductDraft,
	transitions map[int]func(*productmodel.Variant, time.Time) error,
) (productmodel.Product, []productmodel.Variant) {
	t.Helper()
	now := time.Now().UTC()
	product, err := productmodel.NewProduct(draft, now)
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Code, err)
	}
	if err := f.products.Create(context.Background(), product); err != nil {
		t.Fatalf("Create product %q: %v", draft.Code, err)
	}
	variants := make([]productmodel.Variant, 0, len(draft.Variants))
	for i, variantDraft := range draft.Variants {
		variant, err := productmodel.NewVariant(product.ID, variantDraft, now)
		if err != nil {
			t.Fatalf("NewVariant(%q): %v", variantDraft.SKU, err)
		}
		if transition := transitions[i]; transition != nil {
			if err := transition(variant, now); err != nil {
				t.Fatalf("transition %q: %v", variantDraft.SKU, err)
			}
		}
		if err := f.variants.Create(context.Background(), variant); err != nil {
			t.Fatalf("Create variant %q: %v", variantDraft.SKU, err)
		}
		variants = append(variants, *variant)
	}
	return *product, variants
}

// setSKUStock gives a variant a physical quantity, so the detail's stock status has
// a number behind it.
func (f *catalogueSKUFixture) setSKUStock(t *testing.T, variantID uuid.UUID, quantity int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO stock_levels (variant_id, quantity) VALUES ($1, $2)`, variantID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
}

// addSKUGalleryPicture stores one product gallery picture through the adapter, so a
// card-image fallback has a real primary picture to fall back to.
func (f *catalogueSKUFixture) addSKUGalleryPicture(t *testing.T, productID uuid.UUID, position int, primary bool) {
	t.Helper()
	picture := &productmodel.Picture{
		ID: uuid.New(), ProductID: productID, PublicID: "demo/gallery",
		URL: "https://cdn.example.test/gallery.jpg", Width: 800, Height: 600,
		Position: position, IsPrimary: primary, CreatedAt: time.Now().UTC(),
	}
	if err := f.products.AddPicture(context.Background(), picture); err != nil {
		t.Fatalf("AddPicture: %v", err)
	}
}

func skuProductPrice(amount int64) productmodel.Price {
	return productmodel.Price{Amount: amount, Currency: "VND"}
}

func variantImage(publicID, url string) *productmodel.VariantImage {
	return &productmodel.VariantImage{PublicID: publicID, URL: url, Width: 10, Height: 10}
}

// getSKU sends one GET with an optional request id, so the two not-found answers
// can be told apart.
func getSKU(t *testing.T, handler http.Handler, path, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type skuCatalogueListBody struct {
	Data []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Code  string `json:"code"`
		Price struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"price"`
		ImageURL *string `json:"imageUrl"`
	} `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

type skuProductDetailBody struct {
	Data struct {
		Code  string `json:"code"`
		Price struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"price"`
		ImageURL *string `json:"imageUrl"`
		Variants []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			SellState string `json:"sellState"`
			InStock   bool   `json:"inStock"`
			Price     struct {
				Amount int64 `json:"amount"`
			} `json:"price"`
		} `json:"variants"`
	} `json:"data"`
}

func decodeSKUCatalogueList(t *testing.T, rec *httptest.ResponseRecorder) skuCatalogueListBody {
	t.Helper()
	var body skuCatalogueListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the catalogue body %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeSKUDetail(t *testing.T, rec *httptest.ResponseRecorder) skuProductDetailBody {
	t.Helper()
	var body skuProductDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the detail body %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeSKUEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-001, FR-002, FR-004, SC-002 against real PostgreSQL: a product with several
// variants appears exactly once, carrying the lowest on-sale variant price and the
// product's primary gallery picture (falling back to the default variant's image);
// a product with no on-sale variant is absent from the catalogue and from its own
// route.
func TestCatalogueCollapsesVariantsToOneEntryAgainstPostgres(t *testing.T) {
	f := newCatalogueSKUFixture(t)
	category := seedSKUCategory(t, f.categories, "tranh", true)

	// Two on-sale variants at different prices, the default (position 0) with its
	// own image and the product carrying a primary gallery picture the primary
	// gallery picture must win over.
	_, cardVariants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Card set", Code: "card-set", Price: skuProductPrice(150000),
		CategoryID: category.ID, Position: 1,
		Variants: []productmodel.VariantDraft{
			{Name: "Small", SKU: "CARD-1-S", PriceAmount: skuPtr[int64](120000), Position: 0, Image: variantImage("v/s", "https://cdn.test/small.jpg")},
			{Name: "Large", SKU: "CARD-1-L", PriceAmount: skuPtr[int64](180000), Position: 1, Image: variantImage("v/l", "https://cdn.test/large.jpg")},
		},
	}, map[int]func(*productmodel.Variant, time.Time) error{
		0: (*productmodel.Variant).Launch,
		1: (*productmodel.Variant).Launch,
	})
	productID := cardVariants[0].ProductID
	f.addSKUGalleryPicture(t, productID, 0, true)
	f.setSKUStock(t, cardVariants[0].ID, 5)
	f.setSKUStock(t, cardVariants[1].ID, 5)

	// A product whose only variant has no image: the card image is the product's
	// primary gallery picture.
	_, fallbackVariants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Fallback", Code: "fallback", Price: skuProductPrice(50000),
		CategoryID: category.ID, Position: 2,
		Variants: []productmodel.VariantDraft{{Name: "Mặc định", SKU: "FB-1-D", Position: 0}},
	}, map[int]func(*productmodel.Variant, time.Time) error{0: (*productmodel.Variant).Launch})
	f.addSKUGalleryPicture(t, fallbackVariants[0].ProductID, 0, true)

	// Every variant out of stock or not yet on sale: the product leaves the
	// catalogue entirely (FR-004).
	f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Sold out", Code: "sold-out", Price: skuProductPrice(90000),
		CategoryID: category.ID, Position: 3,
		Variants: []productmodel.VariantDraft{{Name: "Hết", SKU: "SO-1-D", Position: 0}},
	}, map[int]func(*productmodel.Variant, time.Time) error{
		0: func(v *productmodel.Variant, now time.Time) error {
			if err := v.Launch(now); err != nil {
				return err
			}
			return v.SellOut(now)
		},
	})
	f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Coming soon", Code: "coming-soon", Price: skuProductPrice(70000),
		CategoryID: category.ID, Position: 4,
		Variants: []productmodel.VariantDraft{{Name: "Sắp ra mắt", SKU: "CS-1-D", Position: 0}},
	}, nil)

	rec := getSKU(t, f.handler, skuCataloguePath, "list-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	list := decodeSKUCatalogueList(t, rec)
	if list.Meta.Total != 2 || len(list.Data) != 2 {
		t.Fatalf("the catalogue must collapse to one entry per on-sale product, got total %d entries %+v", list.Meta.Total, list.Data)
	}
	if list.Data[0].Code != "card-set" || list.Data[1].Code != "fallback" {
		t.Fatalf("unexpected catalogue order: %+v", list.Data)
	}
	if list.Data[0].Price.Amount != 120000 {
		t.Fatalf("the card price must be the lowest on-sale variant price, got %d", list.Data[0].Price.Amount)
	}
	if list.Data[0].ImageURL == nil || *list.Data[0].ImageURL != "https://cdn.example.test/gallery.jpg" {
		t.Fatalf("the card image must be the primary gallery picture, got %+v", list.Data[0].ImageURL)
	}
	if list.Data[1].Price.Amount != 50000 {
		t.Fatalf("the fallback product's card price must be its default price, got %d", list.Data[1].Price.Amount)
	}
	if list.Data[1].ImageURL == nil || *list.Data[1].ImageURL != "https://cdn.example.test/gallery.jpg" {
		t.Fatalf("the card image must be the primary gallery picture, got %+v", list.Data[1].ImageURL)
	}

	// FR-008: a product with no on-sale variant answers the same not-found as an
	// unknown slug.
	for _, slug := range []string{"sold-out", "coming-soon", "never-existed"} {
		if rec := getSKU(t, f.handler, skuCataloguePath+"/"+slug, "probe-"+slug); rec.Code != http.StatusNotFound {
			t.Fatalf("%q must answer 404, got %d (%s)", slug, rec.Code, rec.Body.String())
		}
	}
}

// FR-003, FR-005 against real PostgreSQL: the product detail lists the shared
// content plus every visible variant (on sale or out of stock) with its own price
// and stock status, default first, and omits the not-yet-on-sale and retired ones.
func TestProductDetailListsVisibleVariantsWithStatusAgainstPostgres(t *testing.T) {
	f := newCatalogueSKUFixture(t)
	category := seedSKUCategory(t, f.categories, "tranh", true)

	_, variants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Card set", Code: "card-set", Description: "Bộ card",
		Price: skuProductPrice(100000), CategoryID: category.ID, Position: 1,
		Variants: []productmodel.VariantDraft{
			{Name: "Default", SKU: "D", PriceAmount: skuPtr[int64](120000), Position: 0},
			{Name: "Sold out", SKU: "S", Position: 1},
			{Name: "Coming", SKU: "N", Position: 2},
			{Name: "Retired", SKU: "R", Position: 3},
		},
	}, map[int]func(*productmodel.Variant, time.Time) error{
		0: (*productmodel.Variant).Launch,
		1: func(v *productmodel.Variant, now time.Time) error {
			if err := v.Launch(now); err != nil {
				return err
			}
			return v.SellOut(now)
		},
		3: func(v *productmodel.Variant, now time.Time) error {
			if err := v.Launch(now); err != nil {
				return err
			}
			return v.Retire(now)
		},
	})
	f.setSKUStock(t, variants[0].ID, 5)
	f.setSKUStock(t, variants[1].ID, 0)

	rec := getSKU(t, f.handler, skuCataloguePath+"/card-set", "detail")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	detail := decodeSKUDetail(t, rec)
	if detail.Data.Code != "card-set" {
		t.Fatalf("unexpected detail: %+v", detail.Data)
	}
	if len(detail.Data.Variants) != 2 {
		t.Fatalf("only the on-sale and out-of-stock variants are visible, got %+v", detail.Data.Variants)
	}
	if detail.Data.Variants[0].Name != "Default" || !detail.Data.Variants[0].InStock || detail.Data.Variants[0].Price.Amount != 120000 {
		t.Fatalf("the default (first) variant must be on sale with its own price, got %+v", detail.Data.Variants[0])
	}
	if detail.Data.Variants[1].Name != "Sold out" || detail.Data.Variants[1].InStock || detail.Data.Variants[1].SellState != "OUT_OF_STOCK" {
		t.Fatalf("the sold-out variant must be listed marked out of stock, got %+v", detail.Data.Variants[1])
	}
}

// FR-008 against real PostgreSQL: a product in a hidden category answers exactly
// the same not-found as an unknown slug, so the response never confirms that a
// hidden product exists.
func TestHiddenAndUnknownProductAnswerTheSameNotFoundAgainstPostgres(t *testing.T) {
	f := newCatalogueSKUFixture(t)
	hiddenCategory := seedSKUCategory(t, f.categories, "nhap", false)

	_, hiddenVariants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Hidden", Code: "hidden", Price: skuProductPrice(100000),
		CategoryID: hiddenCategory.ID, Position: 1,
		Variants: []productmodel.VariantDraft{{Name: "Mặc định", SKU: "HID-1-D", Position: 0}},
	}, map[int]func(*productmodel.Variant, time.Time) error{0: (*productmodel.Variant).Launch})
	f.setSKUStock(t, hiddenVariants[0].ID, 5)

	hiddenRec := getSKU(t, f.handler, skuCataloguePath+"/hidden", "probe-hidden")
	unknownRec := getSKU(t, f.handler, skuCataloguePath+"/never-existed", "probe-unknown")
	if hiddenRec.Code != http.StatusNotFound || unknownRec.Code != http.StatusNotFound {
		t.Fatalf("expected both 404, got %d and %d", hiddenRec.Code, unknownRec.Code)
	}

	hiddenEnvelope := decodeSKUEnvelope(t, hiddenRec)
	unknownEnvelope := decodeSKUEnvelope(t, unknownRec)
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
	if hiddenErr["requestId"] == unknownErr["requestId"] {
		t.Fatalf("expected distinct request identifiers so the comparison is meaningful, got %v", hiddenErr["requestId"])
	}
	delete(hiddenErr, "requestId")
	delete(unknownErr, "requestId")
	if !reflect.DeepEqual(hiddenEnvelope, unknownEnvelope) {
		t.Fatalf("the hidden and unknown answers differ beyond the request identifier:\n hidden %s\n unknown  %s",
			hiddenRec.Body.String(), unknownRec.Body.String())
	}
}

// specs/016-hide-guard US3 (T019) against real PostgreSQL: a product whose own
// visibility flag is off is absent from the catalogue browse, the category
// filter and the direct link — like a product with no on-sale variant — while
// a visible product in the same category still answers every read.
func TestFlagOffProductLeavesTheCatalogueAgainstPostgres(t *testing.T) {
	f := newCatalogueSKUFixture(t)
	category := seedSKUCategory(t, f.categories, "tranh", true)

	visibleProduct, visibleVariants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Visible", Code: "visible", Price: skuProductPrice(100000),
		CategoryID: category.ID, Position: 1,
		Variants: []productmodel.VariantDraft{{Name: "Mặc định", SKU: "VIS-1-D", Position: 0}},
	}, map[int]func(*productmodel.Variant, time.Time) error{0: (*productmodel.Variant).Launch})
	hiddenProduct, hiddenVariants := f.seedSKUProduct(t, productmodel.ProductDraft{
		Name: "Hidden flag", Code: "hidden-flag", Price: skuProductPrice(90000),
		CategoryID: category.ID, Position: 2,
		Variants: []productmodel.VariantDraft{{Name: "Mặc định", SKU: "HID-1-D", Position: 0}},
	}, map[int]func(*productmodel.Variant, time.Time) error{0: (*productmodel.Variant).Launch})
	f.setSKUStock(t, visibleVariants[0].ID, 5)
	f.setSKUStock(t, hiddenVariants[0].ID, 5)

	// Turn the flag off through the real adapter, so the persistence path is
	// exercised together with the reads below.
	view, err := f.products.FindByID(context.Background(), hiddenProduct.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !view.Product.IsVisible {
		t.Fatal("a seeded product must start visible")
	}
	flagOff := view.Product
	if !flagOff.Hide(time.Now().UTC()) {
		t.Fatal("Hide must take a visible product off display")
	}
	if err := f.products.Update(context.Background(), &flagOff); err != nil {
		t.Fatalf("persist the flag edit: %v", err)
	}

	// Browse carries only the visible product.
	rec := getSKU(t, f.handler, skuCataloguePath, "browse")
	if rec.Code != http.StatusOK {
		t.Fatalf("browse: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	browse := decodeSKUCatalogueList(t, rec)
	if browse.Meta.Total != 1 || len(browse.Data) != 1 || browse.Data[0].Code != visibleProduct.Code {
		t.Fatalf("the browse must carry only the visible product, got %+v", browse.Data)
	}

	// The category filter agrees: same category narrowed, same single entry.
	rec = getSKU(t, f.handler, skuCataloguePath+"?category=tranh", "filter")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	filtered := decodeSKUCatalogueList(t, rec)
	if filtered.Meta.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].Code != visibleProduct.Code {
		t.Fatalf("the filter must carry only the visible product, got %+v", filtered.Data)
	}

	// The direct link to the hidden code answers not-found, like an unknown one.
	rec = getSKU(t, f.handler, skuCataloguePath+"/"+hiddenProduct.Code, "direct-hidden")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the hidden direct link must answer 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	envelope := decodeSKUEnvelope(t, rec)
	errBody, ok := envelope["error"].(map[string]any)
	if !ok || errBody["code"] != "PRODUCT_NOT_FOUND" {
		t.Fatalf("the hidden direct link must answer PRODUCT_NOT_FOUND, got %s", rec.Body.String())
	}

	// The visible product in the same category still answers its detail.
	rec = getSKU(t, f.handler, skuCataloguePath+"/"+visibleProduct.Code, "direct-visible")
	if rec.Code != http.StatusOK {
		t.Fatalf("the visible direct link must answer 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if detail := decodeSKUDetail(t, rec); detail.Data.Code != visibleProduct.Code {
		t.Fatalf("unexpected detail: %+v", detail.Data)
	}
}

func skuPtr[T any](value T) *T { return &value }
