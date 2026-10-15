//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	cartimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/implement"
	cartmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	carthttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/presentation/http"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	productappinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	productmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives User Story 1 end to end against a real PostgreSQL container:
// the real variant-group adapter, the real product adapter behind the merged
// list and the group route, and the real cart over module 04's product facts. It
// proves what a fake cannot: the merged list is one SQL statement that shows a
// group once and its options never as standalone entries, the group detail and
// the direct option link resolve, and a customer buys the chosen option at its
// own price while an off-sale sibling is refused (FR-001, FR-004, FR-007).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

const (
	variantsProductsPath      = "/api/v1/products"
	variantsProductGroupsPath = "/api/v1/product-groups"
	variantsCartPath          = "/api/v1/cart"
)

// variantsClock is the production-like clock for the cart in the integration run.
type variantsClock struct{}

func (variantsClock) Now() time.Time { return time.Now().UTC() }

// variantsFixture is one migrated PostgreSQL container plus the real HTTP
// surface a customer reaches: the public catalogue, the public group route and
// the cart.
type variantsFixture struct {
	pool *pgxpool.Pool
	root http.Handler
}

func newVariantsFixture(t *testing.T) *variantsFixture {
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
	productRepo := productpostgres.NewProductRepository(pool)

	productService := productimplement.New(productimplement.Service{
		Products:   productRepo,
		Groups:     productpostgres.NewVariantGroupRepository(pool),
		Visibility: categoryvisibility.New(categoryRepo),
		Mapper:     productmapper.New(),
	})
	productHandler := New(productService, productappinterface.Config{}, testLogger)

	cartService := cartimplement.New(cartimplement.Service{
		Carts:        cartpostgres.NewCartRepository(pool),
		Products:     productcatalog.New(productRepo),
		Availability: inventoryavailability.New(inventorypostgres.NewInventoryRepository(pool), variantsClock{}),
		Tx:           &database.DB{Pool: pool},
		Clock:        variantsClock{},
		Mapper:       cartmapper.New(),
	})
	cartHandler := carthttp.New(cartService, testLogger)

	root := chi.NewRouter()
	root.Mount(variantsProductsPath, productHandler.Router(middleware.AuthHooks{}))
	root.Mount(variantsProductGroupsPath, productHandler.GroupRouter(middleware.AuthHooks{}))
	root.Mount(variantsCartPath, cartHandler.Router(maintenanceHooks(nil)))
	return &variantsFixture{pool: pool, root: root}
}

// seedVariantsUser writes the signed-in account the test hooks resolve, so the
// carts table has a real owner to reference.
func seedVariantsUser(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'hash')`,
		testCustomerID, "variants-customer@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

// variantsSeed names the rows a test needs: the group, its on-sale option and
// its off-sale sibling.
type variantsSeed struct {
	groupSlug   string
	activeID    uuid.UUID
	activeSlug  string
	activePrice int64
	offID       uuid.UUID
	offSlug     string
}

// seedVariantsCatalogue writes one visible category, one group with two options
// — one on sale with stock, one discontinued — straight into the database, so
// the real merged read has both kinds to merge.
func seedVariantsCatalogue(t *testing.T, pool *pgxpool.Pool) variantsSeed {
	t.Helper()
	ctx := context.Background()

	categoryID := uuid.New()
	suffix := categoryID.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`,
		categoryID, "Danh mục "+suffix, "danh-muc-"+suffix, "danh-muc-"+suffix, "danh-muc-"+suffix); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	groupID := uuid.New()
	const groupSlug = "card-genshin"
	if _, err := pool.Exec(ctx, `
		INSERT INTO product_variant_groups
			(id, name, slug, description, category_id, position, option_name)
		VALUES ($1, $2, $3, $4, $5, 1, 'Card')`,
		groupID, "Card Genshin", groupSlug, "Bộ card Genshin", categoryID); err != nil {
		t.Fatalf("insert group: %v", err)
	}

	activeID := uuid.New()
	const activeSlug = "card-flins"
	const activePrice int64 = 120000
	if _, err := pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder, variant_group_id, variant_label)
		VALUES ($1, $2, $3, $4, '', $5, 'VND', $6, 1, 'ACTIVE', false, false, $7, 'Flins')`,
		activeID, "Card Flins", activeSlug, activeSlug, activePrice, categoryID, groupID); err != nil {
		t.Fatalf("insert active option: %v", err)
	}

	offID := uuid.New()
	const offSlug = "card-dottore"
	if _, err := pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder, variant_group_id, variant_label)
		VALUES ($1, $2, $3, $4, '', $5, 'VND', $6, 2, 'DISCONTINUED', false, false, $7, 'Dottore')`,
		offID, "Card Dottore", offSlug, offSlug, 150000, categoryID, groupID); err != nil {
		t.Fatalf("insert off-sale option: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO stock_levels (product_id, quantity) VALUES ($1, 10)`, activeID); err != nil {
		t.Fatalf("insert stock: %v", err)
	}

	return variantsSeed{
		groupSlug:   groupSlug,
		activeID:    activeID,
		activeSlug:  activeSlug,
		activePrice: activePrice,
		offID:       offID,
		offSlug:     offSlug,
	}
}

func getVariants(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

type variantsListBody struct {
	Data []struct {
		Type  string `json:"type"`
		Slug  string `json:"slug"`
		Price struct {
			Amount int64 `json:"amount"`
		} `json:"price"`
	} `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

type variantsGroupDetailBody struct {
	Data struct {
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		OptionName string `json:"optionName"`
		Options    []struct {
			Slug  string `json:"slug"`
			Label string `json:"label"`
			Price struct {
				Amount int64 `json:"amount"`
			} `json:"price"`
		} `json:"options"`
	} `json:"data"`
}

// FR-001, FR-002, FR-005, FR-007: against real PostgreSQL the merged list shows
// the group once as `variant-group` with the lowest visible option's price and
// never lists the options as entries; the group detail carries the shared
// content and its visible option only; the direct option link carries the
// selector; an unknown group answers the module not-found.
func TestCatalogueVariantsMergedListGroupDetailAndSelectorAgainstPostgres(t *testing.T) {
	f := newVariantsFixture(t)
	seed := seedVariantsCatalogue(t, f.pool)

	listRec := getVariants(t, f.root, variantsProductsPath)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (%s)", listRec.Code, listRec.Body.String())
	}
	var list variantsListBody
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode the list body %q: %v", listRec.Body.String(), err)
	}
	if list.Meta.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("expected exactly the group entry, got %d entries and a total of %d", len(list.Data), list.Meta.Total)
	}
	entry := list.Data[0]
	if entry.Type != "variant-group" || entry.Slug != seed.groupSlug {
		t.Fatalf("expected the group as type variant-group, got %+v", entry)
	}
	if entry.Price.Amount != seed.activePrice {
		t.Fatalf("the group's price must be its lowest visible option's, got %d", entry.Price.Amount)
	}
	if len(list.Data) != 1 {
		t.Fatalf("a group must appear exactly once, never once per option, got %d entries", len(list.Data))
	}

	groupRec := getVariants(t, f.root, variantsProductGroupsPath+"/"+seed.groupSlug)
	if groupRec.Code != http.StatusOK {
		t.Fatalf("group detail: expected 200, got %d (%s)", groupRec.Code, groupRec.Body.String())
	}
	var group variantsGroupDetailBody
	if err := json.Unmarshal(groupRec.Body.Bytes(), &group); err != nil {
		t.Fatalf("decode the group detail %q: %v", groupRec.Body.String(), err)
	}
	if group.Data.Name != "Card Genshin" || group.Data.OptionName != "Card" {
		t.Fatalf("the group detail must carry the shared content, got %+v", group.Data)
	}
	if len(group.Data.Options) != 1 || group.Data.Options[0].Slug != seed.activeSlug || group.Data.Options[0].Label != "Flins" {
		t.Fatalf("the group detail must list only the visible option, got %+v", group.Data.Options)
	}
	if group.Data.Options[0].Price.Amount != seed.activePrice {
		t.Fatalf("the option must carry its own price, got %d", group.Data.Options[0].Price.Amount)
	}

	optionRec := getVariants(t, f.root, variantsProductsPath+"/"+seed.activeSlug)
	if optionRec.Code != http.StatusOK {
		t.Fatalf("option link: expected 200, got %d (%s)", optionRec.Code, optionRec.Body.String())
	}
	var optionBody struct {
		Data struct {
			Slug         string `json:"slug"`
			VariantGroup *struct {
				ID      uuid.UUID `json:"id"`
				Slug    string    `json:"slug"`
				Name    string    `json:"name"`
				Options []struct {
					Slug string `json:"slug"`
				} `json:"options"`
			} `json:"variantGroup"`
		} `json:"data"`
	}
	if err := json.Unmarshal(optionRec.Body.Bytes(), &optionBody); err != nil {
		t.Fatalf("decode the option detail %q: %v", optionRec.Body.String(), err)
	}
	if optionBody.Data.VariantGroup == nil {
		t.Fatalf("the direct option link must carry the variantGroup selector: %s", optionRec.Body.String())
	}
	if optionBody.Data.VariantGroup.Slug != seed.groupSlug || optionBody.Data.VariantGroup.Name != "Card Genshin" {
		t.Fatalf("the selector must carry the group's shared content, got %+v", optionBody.Data.VariantGroup)
	}
	if len(optionBody.Data.VariantGroup.Options) != 1 || optionBody.Data.VariantGroup.Options[0].Slug != seed.activeSlug {
		t.Fatalf("the selector must list only the visible sibling, got %+v", optionBody.Data.VariantGroup.Options)
	}

	unknown := getVariants(t, f.root, variantsProductGroupsPath+"/never-existed")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("an unknown group slug: expected 404, got %d (%s)", unknown.Code, unknown.Body.String())
	}
	if body := decodeAdminError(t, unknown); body.Error.Code != "VARIANT_GROUP_NOT_FOUND" {
		t.Fatalf("expected VARIANT_GROUP_NOT_FOUND, got %s", body.Error.Code)
	}
}

type variantsCartBody struct {
	Data struct {
		Lines []struct {
			ProductID uuid.UUID `json:"productId"`
			Quantity  int64     `json:"quantity"`
			UnitPrice struct {
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
			} `json:"unitPrice"`
		} `json:"lines"`
	} `json:"data"`
}

func variantsAddBody(productID uuid.UUID, quantity int64) string {
	return `{"productId":"` + productID.String() + `","quantity":` + strconv.FormatInt(quantity, 10) + `}`
}

// FR-003, FR-004: a customer adds the chosen option to the cart at the option's
// own price, while its off-sale sibling is refused 409 and adds nothing.
func TestCustomerBuysTheOptionAtItsOwnPriceAgainstPostgres(t *testing.T) {
	f := newVariantsFixture(t)
	seed := seedVariantsCatalogue(t, f.pool)
	seedVariantsUser(t, f.pool)

	added := performJSON(f.root, http.MethodPost, variantsCartPath+"/items", variantsAddBody(seed.activeID, 1), "customer-token")
	if added.Code != http.StatusOK {
		t.Fatalf("adding the on-sale option: expected 200, got %d (%s)", added.Code, added.Body.String())
	}
	var cart variantsCartBody
	if err := json.Unmarshal(added.Body.Bytes(), &cart); err != nil {
		t.Fatalf("decode the cart body %q: %v", added.Body.String(), err)
	}
	if len(cart.Data.Lines) != 1 {
		t.Fatalf("expected one cart line, got %+v", cart.Data.Lines)
	}
	line := cart.Data.Lines[0]
	if line.ProductID != seed.activeID {
		t.Fatalf("the cart must hold the option itself, got %s", line.ProductID)
	}
	if line.UnitPrice.Amount != seed.activePrice || line.UnitPrice.Currency != "VND" {
		t.Fatalf("the line must hold the option's own price %d, got %+v", seed.activePrice, line.UnitPrice)
	}

	off := performJSON(f.root, http.MethodPost, variantsCartPath+"/items", variantsAddBody(seed.offID, 1), "customer-token")
	if off.Code != http.StatusConflict {
		t.Fatalf("adding the off-sale option: expected 409, got %d (%s)", off.Code, off.Body.String())
	}
	if body := decodeAdminError(t, off); body.Error.Code != "CART_PRODUCT_NOT_PURCHASABLE" {
		t.Fatalf("expected CART_PRODUCT_NOT_PURCHASABLE, got %s", body.Error.Code)
	}

	// The refused sibling changed nothing: the on-sale option is still the only line.
	current := performJSON(f.root, http.MethodGet, variantsCartPath, "", "customer-token")
	if current.Code != http.StatusOK {
		t.Fatalf("read the cart: expected 200, got %d (%s)", current.Code, current.Body.String())
	}
	if err := json.Unmarshal(current.Body.Bytes(), &cart); err != nil {
		t.Fatalf("decode the cart body %q: %v", current.Body.String(), err)
	}
	if len(cart.Data.Lines) != 1 || cart.Data.Lines[0].ProductID != seed.activeID {
		t.Fatalf("the off-sale refusal must leave the on-sale line alone, got %+v", cart.Data.Lines)
	}
}
