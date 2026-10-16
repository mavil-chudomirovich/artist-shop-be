//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the product adapter against a real PostgreSQL container.
//
// Its centre of gravity is what a fake repository cannot prove: that the
// migration applies to a fresh database, that the public catalogue collapses a
// product's variants into one entry with the lowest on-sale price and the default
// variant's image, that the detail read carries only the visible variants, and
// that the storage layer refuses a duplicate code/SKU, a malformed value and the
// removal of a category that still has products — even when the application is
// bypassed. Most constraint tests write the tables directly, with no domain
// validation and no use case, because a guard against a *future* writer can only
// be proven by pretending to be that writer.
//
// Every rejection asserts both the SQLSTATE and the constraint name, so a test
// fails if the constraint it names is removed from the migration.

// SQLSTATE codes the constraint tests distinguish.
const (
	sqlStateUnique     = "23505"
	sqlStateForeignKey = "23503"
	sqlStateCheck      = "23514"
)

// The constraint and index names migrations/00006_product.sql and
// migrations/00014_product_sku_model.sql must create (data-model.md).
const (
	idxNormalizedSlug     = "products_normalized_slug_key"
	ckNormalizedSlugShape = "products_normalized_slug_shape_ck"
	ckSlugFormat          = "products_slug_format_ck"
	ckNameLength          = "products_name_length_ck"
	ckSlugLength          = "products_slug_length_ck"
	ckDescriptionLength   = "products_description_length_ck"
	ckPriceAmount         = "products_price_amount_ck"
	ckCurrency            = "products_currency_ck"
	fkCategory            = "products_category_fk"
	idxOrdering           = "products_ordering_idx"
	idxCategory           = "products_category_idx"

	idxCode          = "products_code_key"
	ckCodeLength     = "products_code_length_ck"
	ckCodeShape      = "products_code_shape_ck"
	fkProductVariant = "product_variants_product_fk"
	ckVariantPrice   = "product_variants_price_ck"
	ckVariantState   = "product_variants_sell_state_ck"
	ckVariantName    = "product_variants_name_length_ck"
	ckVariantURL     = "product_variants_image_url_ck"
	ckVariantWidth   = "product_variants_image_width_ck"
	ckVariantHeight  = "product_variants_image_height_ck"
	ckVariantImage   = "product_variants_image_ck"
	idxVariantSKU    = "product_variants_sku_key"
	idxVariantOrder  = "product_variants_product_idx"

	fkImageProduct = "product_images_product_fk"
	ckImageWidth   = "product_images_width_ck"
	ckImageHeight  = "product_images_height_ck"
	ckImageHTTPS   = "product_images_secure_url_ck"
	idxOnePrimary  = "product_images_one_primary_key"
	idxImageOrder  = "product_images_ordering_idx"
)

// productFixture is one migrated PostgreSQL container plus the adapters under
// test.
type productFixture struct {
	pool     *pgxpool.Pool
	repo     *ProductRepository
	variants *VariantRepository
}

func newProductFixture(t *testing.T) *productFixture {
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
	return &productFixture{
		pool:     pool,
		repo:     NewProductRepository(pool),
		variants: NewVariantRepository(pool),
	}
}

// insertCategory writes one visible category straight into module 03's table, so
// the product foreign key has a real row to reference. The test may do this
// because it is not module 04's production code: the boundary the constitution
// protects is between modules, not between a test and a fixture.
func (f *productFixture) insertCategory(ctx context.Context, slug string) (uuid.UUID, error) {
	id := uuid.New()
	name := "Category " + slug
	const query = `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`
	_, err := f.pool.Exec(ctx, query, id, name, strings.ToLower(name), slug, strings.ToLower(slug))
	return id, err
}

func (f *productFixture) requireCategory(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	id, err := f.insertCategory(context.Background(), slug)
	if err != nil {
		t.Fatalf("insert category %q: %v", slug, err)
	}
	return id
}

// rawProduct carries the columns a raw product insert writes, deliberately
// skipping the domain and the adapter.
type rawProduct struct {
	CategoryID     uuid.UUID
	Name           string
	Code           string
	Slug           string
	NormalizedSlug string
	Description    string
	PriceAmount    int64
	Currency       string
	Position       int
}

// validRaw returns a product row every constraint accepts, so a subtest changes
// only the value it is proving is refused.
func validRaw(categoryID uuid.UUID, slug string) rawProduct {
	return rawProduct{
		CategoryID:     categoryID,
		Name:           "Product " + slug,
		Code:           "code-" + slug,
		Slug:           slug,
		NormalizedSlug: slug,
		Description:    "a description",
		PriceAmount:    1000,
		Currency:       "VND",
		Position:       0,
	}
}

func (f *productFixture) rawInsertProduct(ctx context.Context, p rawProduct) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO products
			(id, name, code, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), now())`
	_, err := f.pool.Exec(ctx, query, id, p.Name, p.Code, p.Slug, p.NormalizedSlug, p.Description,
		p.PriceAmount, p.Currency, p.CategoryID, p.Position)
	return id, err
}

// rawVariant carries the columns a raw variant insert writes.
type rawVariant struct {
	ProductID   uuid.UUID
	Name        string
	SKU         string
	PriceAmount *int64
	ImagePublic *string
	ImageURL    *string
	ImageWidth  *int
	ImageHeight *int
	Position    int
	SellState   string
}

// validRawVariant returns a variant row every constraint accepts. It is on sale,
// which is the state a catalogue test needs unless it changes it.
func validRawVariant(productID uuid.UUID, sku string) rawVariant {
	return rawVariant{
		ProductID: productID,
		Name:      "Variant " + sku,
		SKU:       sku,
		Position:  0,
		SellState: "ACTIVE",
	}
}

func (f *productFixture) rawInsertVariant(ctx context.Context, v rawVariant) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount,
			 image_public_id, image_secure_url, image_width, image_height,
			 position, sell_state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())`
	_, err := f.pool.Exec(ctx, query, id, v.ProductID, v.Name, v.SKU, v.PriceAmount,
		v.ImagePublic, v.ImageURL, v.ImageWidth, v.ImageHeight, v.Position, v.SellState)
	return id, err
}

func (f *productFixture) rawInsertPicture(ctx context.Context, productID uuid.UUID, primary bool) (uuid.UUID, error) {
	return f.rawInsertPictureURL(ctx, productID, "https://example.test/image.jpg", primary)
}

func (f *productFixture) rawInsertPictureURL(ctx context.Context, productID uuid.UUID, url string, primary bool) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO product_images
			(id, product_id, public_id, secure_url, width, height, position, is_primary)
		VALUES ($1, $2, $3, $4, 10, 10, 0, $5)`
	_, err := f.pool.Exec(ctx, query, id, productID, "demo/"+id.String(), url, primary)
	return id, err
}

func (f *productFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var total int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&total); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return total
}

// newProduct builds a domain-valid product with one default variant.
func newProduct(t *testing.T, draft model.ProductDraft) *model.Product {
	t.Helper()
	product, err := model.NewProduct(draft, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Slug, err)
	}
	return product
}

// buildProduct constructs a domain-valid product whose code is derived from the
// slug, so a subtest can insert it through the adapter.
func buildProduct(t *testing.T, categoryID uuid.UUID, slug string) *model.Product {
	t.Helper()
	price, err := model.NewPrice(1000, "VND")
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	return newProduct(t, model.ProductDraft{
		Name:       "Product " + slug,
		Code:       "code-" + slug,
		Slug:       slug,
		Price:      price,
		CategoryID: categoryID,
		Position:   0,
		Variants: []model.VariantDraft{{
			Name: "Variant " + slug, SKU: "sku-" + slug, Position: 0,
		}},
	})
}

// buildVariant constructs a domain-valid variant of a product.
func buildVariant(t *testing.T, productID uuid.UUID, name, sku string, amount *int64, position int) *model.Variant {
	t.Helper()
	variant, err := model.NewVariant(productID, model.VariantDraft{
		Name: name, SKU: sku, PriceAmount: amount, Position: position,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariant(%q): %v", sku, err)
	}
	return variant
}

// sqlState returns the PostgreSQL SQLSTATE an error carries, or "" for a
// non-database error.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// constraintName returns the constraint an error names, or "".
func constraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// requireConstraint fails unless err is exactly the SQLSTATE and constraint
// expected. Asserting the name, not just the code, is what lets each test be
// mutation-checked against a single constraint.
func requireConstraint(t *testing.T, err error, wantState, wantConstraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the storage layer accepted a row that %s must reject", wantConstraint)
	}
	if got := sqlState(err); got != wantState {
		t.Fatalf("expected SQLSTATE %s (%s), got %q: %v", wantState, wantConstraint, got, err)
	}
	if got := constraintName(err); got != wantConstraint {
		t.Fatalf("expected constraint %s, got %q: %v", wantConstraint, got, err)
	}
}

// FR-011, FR-012: the migration is applied to a brand new container database —
// proof that it applies to a fresh database — and every constraint and index it
// must have created is checked by name, while feature 014's grouping tables are
// gone (research D12).
func TestTheProductMigrationAppliesAndCreatesEveryConstraint(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()

	for _, table := range []string{"products", "product_images", "product_variants"} {
		var exists bool
		if err := f.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("look up table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("the migration did not create the %s table", table)
		}
	}
	for _, table := range []string{"product_variant_groups", "product_variant_group_images", "product_set_items"} {
		var exists bool
		if err := f.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("look up dropped table %s: %v", table, err)
		}
		if exists {
			t.Errorf("the migration did not drop feature 014's %s table", table)
		}
	}

	checks := []string{
		ckNormalizedSlugShape, ckSlugFormat, ckNameLength, ckSlugLength,
		ckDescriptionLength, ckPriceAmount, ckCurrency, fkCategory,
		ckCodeLength, ckCodeShape, fkProductVariant, ckVariantPrice,
		ckVariantState, ckVariantName, ckVariantURL, ckVariantWidth,
		ckVariantHeight, ckVariantImage,
		ckImageWidth, ckImageHeight, ckImageHTTPS, fkImageProduct,
	}
	const constraintQuery = `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			WHERE t.relname = $1 AND c.conname = $2
		)`
	for _, name := range checks {
		var exists bool
		if err := f.pool.QueryRow(ctx, constraintQuery, tableForConstraint(name), name).Scan(&exists); err != nil {
			t.Fatalf("look up constraint %s: %v", name, err)
		}
		if !exists {
			t.Errorf("the migration did not create constraint %s", name)
		}
	}

	indexes := []string{
		idxNormalizedSlug, idxOrdering, idxCategory, idxCode,
		idxVariantSKU, idxVariantOrder, idxOnePrimary, idxImageOrder,
	}
	const indexQuery = `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`
	for _, name := range indexes {
		var exists bool
		if err := f.pool.QueryRow(ctx, indexQuery, name).Scan(&exists); err != nil {
			t.Fatalf("look up index %s: %v", name, err)
		}
		if !exists {
			t.Errorf("the migration did not create index %s", name)
		}
	}
}

// tableForConstraint maps a constraint name to the table that carries it, so the
// presence check above can look it up.
func tableForConstraint(name string) string {
	switch {
	case strings.HasPrefix(name, "product_images_"):
		return "product_images"
	case strings.HasPrefix(name, "product_variants_"):
		return "product_variants"
	default:
		return "products"
	}
}

// FR-011, FR-030: every product-level storage rule holds against a writer that
// bypassed the application entirely. Each subtest names the one constraint it
// expects, so removing that constraint from the migration makes exactly this
// subtest fail.
func TestStorageConstraintsRejectWhatTheApplicationWouldPrevent(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-for-constraints")

	insert := func(t *testing.T, mutate func(*rawProduct), slug string) error {
		t.Helper()
		p := validRaw(categoryID, slug)
		mutate(&p)
		_, err := f.rawInsertProduct(ctx, p)
		return err
	}

	t.Run("duplicate normalised slug", func(t *testing.T) {
		if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, "dup-slug-one")); err != nil {
			t.Fatalf("seed the first row: %v", err)
		}
		p := validRaw(categoryID, "dup-slug-two")
		p.NormalizedSlug = "dup-slug-one"
		_, err := f.rawInsertProduct(ctx, p)
		requireConstraint(t, err, sqlStateUnique, idxNormalizedSlug)
	})

	t.Run("duplicate code ignoring case and whitespace", func(t *testing.T) {
		if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, "code-one")); err != nil {
			t.Fatalf("seed the first row: %v", err)
		}
		p := validRaw(categoryID, "code-two")
		p.Code = "  CODE-ONE  "
		_, err := f.rawInsertProduct(ctx, p)
		requireConstraint(t, err, sqlStateUnique, idxCode)
	})

	t.Run("un-normalised normalised_slug", func(t *testing.T) {
		// The slug itself stays valid so only the shape check rejects the row.
		err := insert(t, func(p *rawProduct) {
			p.NormalizedSlug = "Shape-One"
		}, "shape-one")
		requireConstraint(t, err, sqlStateCheck, ckNormalizedSlugShape)
	})

	t.Run("slug format", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) {
			p.Slug = "Tranh_Son_Dau"
			p.NormalizedSlug = "tranh_son_dau"
		}, "unused")
		requireConstraint(t, err, sqlStateCheck, ckSlugFormat)
	})

	t.Run("name length", func(t *testing.T) {
		long := strings.Repeat("a", 121)
		err := insert(t, func(p *rawProduct) { p.Name = long }, "name-length")
		requireConstraint(t, err, sqlStateCheck, ckNameLength)
	})

	t.Run("slug length", func(t *testing.T) {
		long := strings.Repeat("a", 141)
		err := insert(t, func(p *rawProduct) {
			p.Slug = long
			p.NormalizedSlug = long
		}, "slug-length")
		requireConstraint(t, err, sqlStateCheck, ckSlugLength)
	})

	t.Run("code length", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) { p.Code = strings.Repeat("a", 141) }, "code-length")
		requireConstraint(t, err, sqlStateCheck, ckCodeLength)
	})

	t.Run("code shape", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) { p.Code = "  padded  " }, "code-shape")
		requireConstraint(t, err, sqlStateCheck, ckCodeShape)
	})

	t.Run("description length", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) { p.Description = strings.Repeat("a", 5001) }, "description-length")
		requireConstraint(t, err, sqlStateCheck, ckDescriptionLength)
	})

	t.Run("non-positive price", func(t *testing.T) {
		for _, amount := range []int64{0, -1} {
			err := insert(t, func(p *rawProduct) { p.PriceAmount = amount }, "price-zero")
			requireConstraint(t, err, sqlStateCheck, ckPriceAmount)
		}
	})

	t.Run("malformed currency", func(t *testing.T) {
		for _, currency := range []string{"usd", "US", "V1D"} {
			err := insert(t, func(p *rawProduct) { p.Currency = currency }, "currency-bad")
			requireConstraint(t, err, sqlStateCheck, ckCurrency)
		}
	})
}

// FR-011, FR-015, FR-016, FR-017: every variant-level storage rule holds against
// a raw writer.
func TestVariantStorageConstraintsRejectWhatTheApplicationWouldPrevent(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-variant-constraints")
	productID, err := f.rawInsertProduct(ctx, validRaw(categoryID, "variant-constraint-product"))
	if err != nil {
		t.Fatalf("insert the product: %v", err)
	}

	insert := func(t *testing.T, mutate func(*rawVariant), sku string) error {
		t.Helper()
		v := validRawVariant(productID, sku)
		mutate(&v)
		_, err := f.rawInsertVariant(ctx, v)
		return err
	}

	t.Run("duplicate sku ignoring case and whitespace", func(t *testing.T) {
		if _, err := f.rawInsertVariant(ctx, validRawVariant(productID, "sku-one")); err != nil {
			t.Fatalf("seed the first row: %v", err)
		}
		v := validRawVariant(productID, "sku-two")
		v.SKU = "  SKU-ONE  "
		_, err := f.rawInsertVariant(ctx, v)
		requireConstraint(t, err, sqlStateUnique, idxVariantSKU)
	})

	t.Run("unknown product", func(t *testing.T) {
		v := validRawVariant(uuid.New(), "sku-orphan")
		_, err := f.rawInsertVariant(ctx, v)
		requireConstraint(t, err, sqlStateForeignKey, fkProductVariant)
	})

	t.Run("non-positive price", func(t *testing.T) {
		zero := int64(0)
		err := insert(t, func(v *rawVariant) { v.PriceAmount = &zero }, "sku-price")
		requireConstraint(t, err, sqlStateCheck, ckVariantPrice)
	})

	t.Run("unknown sell state", func(t *testing.T) {
		err := insert(t, func(v *rawVariant) { v.SellState = "ARCHIVED" }, "sku-state")
		requireConstraint(t, err, sqlStateCheck, ckVariantState)
	})

	t.Run("name length", func(t *testing.T) {
		err := insert(t, func(v *rawVariant) { v.Name = strings.Repeat("a", 121) }, "sku-name")
		requireConstraint(t, err, sqlStateCheck, ckVariantName)
	})

	t.Run("non-https image url", func(t *testing.T) {
		public, url := "demo/x", "http://example.test/x.jpg"
		width, height := 10, 10
		err := insert(t, func(v *rawVariant) {
			v.ImagePublic, v.ImageURL, v.ImageWidth, v.ImageHeight = &public, &url, &width, &height
		}, "sku-url")
		requireConstraint(t, err, sqlStateCheck, ckVariantURL)
	})

	t.Run("non-positive image width", func(t *testing.T) {
		public, url := "demo/x", "https://example.test/x.jpg"
		width, height := 0, 10
		err := insert(t, func(v *rawVariant) {
			v.ImagePublic, v.ImageURL, v.ImageWidth, v.ImageHeight = &public, &url, &width, &height
		}, "sku-width")
		requireConstraint(t, err, sqlStateCheck, ckVariantWidth)
	})

	t.Run("half-present image", func(t *testing.T) {
		public := "demo/x"
		err := insert(t, func(v *rawVariant) { v.ImagePublic = &public }, "sku-half-image")
		requireConstraint(t, err, sqlStateCheck, ckVariantImage)
	})
}

// FR-034, FR-035: the restricting foreign key refuses the removal of a category
// that still has products, from the storage layer, so no code path can leave a
// product pointing at a category that is gone.
func TestTheRestrictingForeignKeyRefusesDeletingACategoryWithProducts(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-in-use")

	if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, "product-in-category")); err != nil {
		t.Fatalf("insert the product: %v", err)
	}

	_, err := f.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID)
	requireConstraint(t, err, sqlStateForeignKey, fkCategory)

	// The refused delete must not have removed anything: the category and the
	// product that references it both survive.
	if got := f.count(t, `SELECT count(*) FROM categories WHERE id = $1`, categoryID); got != 1 {
		t.Fatalf("the category was removed despite the refusal, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM products WHERE category_id = $1`, categoryID); got != 1 {
		t.Fatalf("the product was removed despite the refusal, count is %d", got)
	}

	// Once the product is gone, the same delete succeeds.
	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE category_id = $1`, categoryID); err != nil {
		t.Fatalf("remove the product: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID); err != nil {
		t.Fatalf("the category must be removable once it has no products, got %v", err)
	}
}

// FR-016: "at most one main picture" is a uniqueness property the partial index
// enforces, not an application check. A second primary is refused, while any
// number of non-primary pictures is allowed.
func TestThePartialUniqueIndexRefusesASecondMainPicture(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-pictures")
	productID, err := f.rawInsertProduct(ctx, validRaw(categoryID, "product-pictures"))
	if err != nil {
		t.Fatalf("insert the product: %v", err)
	}

	if _, err := f.rawInsertPicture(ctx, productID, true); err != nil {
		t.Fatalf("the first main picture must be accepted, got %v", err)
	}
	_, err = f.rawInsertPicture(ctx, productID, true)
	requireConstraint(t, err, sqlStateUnique, idxOnePrimary)

	nonPrimary, err := f.rawInsertPicture(ctx, productID, false)
	if err != nil {
		t.Fatalf("a non-primary picture must be accepted, got %v", err)
	}

	// Promoting the non-primary picture must leave exactly one primary.
	if err := f.repo.SetPrimaryPicture(ctx, productID, nonPrimary); err != nil {
		t.Fatalf("SetPrimaryPicture: %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1 AND is_primary`, productID); got != 1 {
		t.Fatalf("expected exactly one main picture after promotion, got %d", got)
	}
	var primaryID uuid.UUID
	if err := f.pool.QueryRow(ctx,
		`SELECT id FROM product_images WHERE product_id = $1 AND is_primary`, productID).Scan(&primaryID); err != nil {
		t.Fatalf("read the main picture: %v", err)
	}
	if primaryID != nonPrimary {
		t.Fatalf("the promoted picture is not the main one: got %s, want %s", primaryID, nonPrimary)
	}

	// A picture that does not belong to the product is not found.
	if err := f.repo.SetPrimaryPicture(ctx, productID, uuid.New()); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound for a foreign picture, got %v", err)
	}
}

// FR-012: removing a product cascades its pictures and its variants, while the
// variants of a removed product do not survive it.
func TestCascadingDeletesRemovePicturesAndVariants(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-cascade")

	productID, err := f.rawInsertProduct(ctx, validRaw(categoryID, "cascade-product"))
	if err != nil {
		t.Fatalf("insert the product: %v", err)
	}
	if _, err := f.rawInsertVariant(ctx, validRawVariant(productID, "cascade-sku")); err != nil {
		t.Fatalf("insert the variant: %v", err)
	}
	if _, err := f.rawInsertPicture(ctx, productID, true); err != nil {
		t.Fatalf("insert the picture: %v", err)
	}

	if err := f.repo.Delete(ctx, productID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1`, productID); got != 0 {
		t.Errorf("the product's pictures survived the cascade, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM product_variants WHERE product_id = $1`, productID); got != 0 {
		t.Errorf("the product's variants survived the cascade, count is %d", got)
	}

	// Adding a picture to an unknown product answers not-found.
	picture := &model.Picture{
		ID: uuid.New(), ProductID: uuid.New(), PublicID: "demo/orphan",
		URL: "https://example.test/orphan.jpg", Width: 10, Height: 10,
		CreatedAt: time.Now().UTC(),
	}
	if err := f.repo.AddPicture(ctx, picture); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// FR-009, SC-008: the identifier never changes, and no update path can write it,
// while the folding key is recomputed from the slug the adapter writes.
func TestTheIdentifierSurvivesEveryUpdateAndNoPathRewritesIt(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-stable-id")

	product := buildProduct(t, categoryID, "stable-id")
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create: %v", err)
	}
	original := product.ID

	loaded, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if loaded.Product.ID != original {
		t.Fatalf("the read returned id %s, want %s", loaded.Product.ID, original)
	}
	newName := "Product renamed"
	newSlug := "stable-id-renamed"
	newCode := "code-renamed"
	newDescription := "changed"
	newPosition := 7
	edited := loaded.Product
	if err := edited.Apply(model.ProductEdit{
		Name:        &newName,
		Code:        &newCode,
		Slug:        &newSlug,
		Description: &newDescription,
		Position:    &newPosition,
	}, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.repo.Update(ctx, &edited); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterEdit, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after edit: %v", err)
	}
	if afterEdit.Product.ID != original {
		t.Fatalf("the identifier moved after an edit: %s", afterEdit.Product.ID)
	}
	if afterEdit.Product.Name != newName || afterEdit.Product.Slug != newSlug ||
		afterEdit.Product.Code != newCode || afterEdit.Product.Description != newDescription ||
		afterEdit.Product.Position != newPosition {
		t.Fatalf("the edit was not written: %+v", afterEdit.Product)
	}

	// The folding key is recomputed by the adapter from the slug it writes.
	var storedKey string
	if err := f.pool.QueryRow(ctx, `SELECT normalized_slug FROM products WHERE id = $1`, original).Scan(&storedKey); err != nil {
		t.Fatalf("read the folding key: %v", err)
	}
	if storedKey != model.FoldKey(newSlug) {
		t.Fatalf("the update stored folding key %q, want %q", storedKey, model.FoldKey(newSlug))
	}

	// An update carrying a different identifier must not rewrite or create a row.
	forged := edited
	forged.ID = uuid.New()
	if err := f.repo.Update(ctx, &forged); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound for a forged identifier, got %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM products`); got != 1 {
		t.Fatalf("a forged-identifier update created or removed a row, count is %d", got)
	}
}

// FR-009, SC-009: with several products sharing a position, two identical reads
// return the same order, and the immutable identifier breaks the remaining tie.
func TestPageOrderingIsStableAcrossTwoReads(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-ordering")
	fixed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)

	for _, slug := range []string{"order-alpha", "order-beta", "order-gamma"} {
		if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, slug)); err != nil {
			t.Fatalf("insert %q: %v", slug, err)
		}
		if _, err := f.pool.Exec(ctx,
			`UPDATE products SET position = 5, created_at = $2 WHERE slug = $1`, slug, fixed); err != nil {
			t.Fatalf("pin %q: %v", slug, err)
		}
	}
	if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, "order-first")); err != nil {
		t.Fatalf("insert order-first: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE products SET position = 1, created_at = $1 WHERE slug = 'order-first'`, fixed); err != nil {
		t.Fatalf("pin order-first: %v", err)
	}

	first, total, err := f.repo.ListAll(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if total != 4 || len(first) != 4 {
		t.Fatalf("expected 4 products, got %d of %d", len(first), total)
	}
	if first[0].Product.Position != 1 {
		t.Fatalf("expected the lowest position first, got position %d", first[0].Product.Position)
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range first {
		if seen[item.Product.ID] {
			t.Fatalf("product %s appeared twice on one page", item.Product.ID)
		}
		seen[item.Product.ID] = true
	}

	second, _, err := f.repo.ListAll(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListAll repeat: %v", err)
	}
	for i := range first {
		if first[i].Product.ID != second[i].Product.ID {
			t.Fatalf("the order is not stable: read one %s, read two %s",
				first[i].Product.ID, second[i].Product.ID)
		}
	}
}

// visibleListQuery builds the public list query with the given visible categories.
func visibleListQuery(ids ...uuid.UUID) domainrepo.VisibleListQuery {
	return domainrepo.VisibleListQuery{VisibleCategoryIDs: ids, Page: 1, PageSize: 100}
}

// FR-001, FR-002, FR-004, SC-001: the catalogue shows exactly one entry per
// product, priced at the lowest on-sale variant (an out-of-stock variant's cheaper
// price never headlines it), imaged by the default variant falling back to the
// product's primary gallery picture, and visible only when a variant is on sale.
func TestCatalogueIsOneEntryPerProductWithLowestOnSalePrice(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-catalogue")
	visible := []uuid.UUID{category}

	// Product A: three variants — an out-of-stock cheap one, an on-sale dearer
	// one and a coming-soon one. The card price must follow the on-sale variant
	// and the card image the default (position 0) variant.
	productA, err := f.rawInsertProduct(ctx, validRaw(category, "catalogue-a"))
	if err != nil {
		t.Fatalf("insert product A: %v", err)
	}
	cheap := int64(500)
	if _, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productA, Name: "Cheap", SKU: "catalogue-a-cheap",
		PriceAmount: &cheap, Position: 1, SellState: "OUT_OF_STOCK",
	}); err != nil {
		t.Fatalf("insert the out-of-stock variant: %v", err)
	}
	public, url := "demo/a", "https://example.test/a-default.jpg"
	width, height := 10, 10
	dear := int64(2000)
	if _, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productA, Name: "Default", SKU: "catalogue-a-default",
		PriceAmount: &dear, ImagePublic: &public, ImageURL: &url, ImageWidth: &width, ImageHeight: &height,
		Position: 0, SellState: "ACTIVE",
	}); err != nil {
		t.Fatalf("insert the default variant: %v", err)
	}
	if _, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productA, Name: "Soon", SKU: "catalogue-a-soon",
		PriceAmount: &cheap, Position: 2, SellState: "COMING_SOON",
	}); err != nil {
		t.Fatalf("insert the coming-soon variant: %v", err)
	}

	// Product B: one on-sale variant with no own price (falls back to the product
	// default 1000) and no variant image — the card image is the gallery picture.
	productB, err := f.rawInsertProduct(ctx, validRaw(category, "catalogue-b"))
	if err != nil {
		t.Fatalf("insert product B: %v", err)
	}
	if _, err := f.rawInsertVariant(ctx, validRawVariant(productB, "catalogue-b-sku")); err != nil {
		t.Fatalf("insert product B's variant: %v", err)
	}
	if _, err := f.rawInsertPictureURL(ctx, productB, "https://example.test/b-gallery.jpg", true); err != nil {
		t.Fatalf("insert product B's gallery picture: %v", err)
	}

	// Product C: no on-sale variant, so it is hidden from the catalogue.
	productC, err := f.rawInsertProduct(ctx, validRaw(category, "catalogue-c"))
	if err != nil {
		t.Fatalf("insert product C: %v", err)
	}
	if _, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productC, Name: "Gone", SKU: "catalogue-c-sku", Position: 0, SellState: "DISCONTINUED",
	}); err != nil {
		t.Fatalf("insert product C's variant: %v", err)
	}

	entries, total, err := f.repo.ListCatalogue(ctx, visibleListQuery(visible...))
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("expected the two visible products, got %d of a total %d", len(entries), total)
	}
	bySlug := map[string]domainrepo.CatalogueEntry{}
	for _, entry := range entries {
		bySlug[entry.Slug] = entry
	}
	if _, ok := bySlug["catalogue-c"]; ok {
		t.Fatalf("a product with no on-sale variant leaked into the catalogue")
	}
	a, ok := bySlug["catalogue-a"]
	if !ok {
		t.Fatalf("product A is missing from the catalogue")
	}
	if a.Price.Amount != 2000 || a.Price.Currency != "VND" {
		t.Fatalf("product A carried price %d %s, want 2000 VND (the on-sale variant)", a.Price.Amount, a.Price.Currency)
	}
	if a.ImageURL != "https://example.test/a-default.jpg" {
		t.Fatalf("product A carried image %q, want the default variant's", a.ImageURL)
	}
	b, ok := bySlug["catalogue-b"]
	if !ok {
		t.Fatalf("product B is missing from the catalogue")
	}
	if b.Price.Amount != 1000 {
		t.Fatalf("product B must fall back to the product default price, got %d", b.Price.Amount)
	}
	if b.ImageURL != "https://example.test/b-gallery.jpg" {
		t.Fatalf("product B must fall back to the gallery picture, got %q", b.ImageURL)
	}

	// An empty visible set answers an empty page, never an error.
	empty, total, err := f.repo.ListCatalogue(ctx, visibleListQuery())
	if err != nil {
		t.Fatalf("ListCatalogue with no visible category: %v", err)
	}
	if total != 0 || len(empty) != 0 {
		t.Fatalf("an empty catalogue of visible categories must answer an empty page, got %d of %d", len(empty), total)
	}
}

// FR-003, FR-005, FR-008: the public detail read returns the product's shared
// content, its gallery, its visible variants (on sale or out of stock, ordered)
// and the summary; a hidden, retired, unknown or empty-visible-set product answers
// the same not-found.
func TestProductDetailCarriesVisibleVariantsOnly(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-detail")
	visible := []uuid.UUID{category}

	productID, err := f.rawInsertProduct(ctx, validRaw(category, "detail-product"))
	if err != nil {
		t.Fatalf("insert the product: %v", err)
	}
	public, url := "demo/d", "https://example.test/d-default.jpg"
	width, height := 10, 10
	dear := int64(3000)
	cheap := int64(1000)

	onSale, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productID, Name: "On sale", SKU: "detail-active",
		PriceAmount: &dear, ImagePublic: &public, ImageURL: &url, ImageWidth: &width, ImageHeight: &height,
		Position: 0, SellState: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("insert the on-sale variant: %v", err)
	}
	outOfStock, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productID, Name: "Sold out", SKU: "detail-oos",
		PriceAmount: &cheap, Position: 1, SellState: "OUT_OF_STOCK",
	})
	if err != nil {
		t.Fatalf("insert the out-of-stock variant: %v", err)
	}
	comingSoon, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productID, Name: "Soon", SKU: "detail-soon",
		Position: 2, SellState: "COMING_SOON",
	})
	if err != nil {
		t.Fatalf("insert the coming-soon variant: %v", err)
	}
	retired, err := f.rawInsertVariant(ctx, rawVariant{
		ProductID: productID, Name: "Retired", SKU: "detail-retired",
		Position: 3, SellState: "DISCONTINUED",
	})
	if err != nil {
		t.Fatalf("insert the retired variant: %v", err)
	}
	if _, err := f.rawInsertPictureURL(ctx, productID, "https://example.test/d-gallery.jpg", true); err != nil {
		t.Fatalf("insert the gallery picture: %v", err)
	}

	view, err := f.repo.FindVisibleBySlug(ctx, "detail-product", visible)
	if err != nil {
		t.Fatalf("FindVisibleBySlug: %v", err)
	}
	if len(view.Variants) != 2 {
		t.Fatalf("the public detail must carry the two visible variants, got %d", len(view.Variants))
	}
	if view.Variants[0].ID != onSale || view.Variants[1].ID != outOfStock {
		t.Fatalf("the visible variants are not in display order: %s, %s", view.Variants[0].ID, view.Variants[1].ID)
	}
	for _, variant := range view.Variants {
		if variant.ID == comingSoon || variant.ID == retired {
			t.Fatalf("a hidden variant was returned to a customer")
		}
	}
	if view.Summary.Price.Amount != 3000 {
		t.Fatalf("the summary price must be the lowest on-sale price, got %d", view.Summary.Price.Amount)
	}
	if view.Summary.ImageURL != "https://example.test/d-default.jpg" {
		t.Fatalf("the summary image must be the default variant's, got %q", view.Summary.ImageURL)
	}
	if len(view.Pictures) != 1 {
		t.Fatalf("the public detail must carry the gallery, got %d pictures", len(view.Pictures))
	}

	// The administrator read carries every variant, including the hidden two.
	admin, err := f.repo.FindByID(ctx, productID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(admin.Variants) != 4 {
		t.Fatalf("the administrator detail must carry every variant, got %d", len(admin.Variants))
	}

	// A product in a hidden category, one with no on-sale variant and an empty
	// visible set all answer not-found.
	hiddenCategory := f.requireCategory(t, "cat-detail-hidden")
	hiddenProduct, err := f.rawInsertProduct(ctx, validRaw(hiddenCategory, "detail-hidden-category"))
	if err != nil {
		t.Fatalf("insert the hidden-category product: %v", err)
	}
	if _, err := f.rawInsertVariant(ctx, validRawVariant(hiddenProduct, "detail-hidden-sku")); err != nil {
		t.Fatalf("insert the hidden-category variant: %v", err)
	}
	if _, err := f.repo.FindVisibleBySlug(ctx, "detail-hidden-category", visible); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product in a hidden category must not be found, got %v", err)
	}
	if _, err := f.repo.FindVisibleBySlug(ctx, "detail-product", nil); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("an empty visible set must answer not-found, got %v", err)
	}
}

// FR-011, research D17: a duplicate folded slug or code is reported as the module
// sentinel, and a category no row carries is reported as an invalid field naming
// categoryId — all classified from the constraint the database reported.
func TestAConstraintViolationIsReportedAsTheMatchingSentinel(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-classify")

	first := buildProduct(t, categoryID, "classify-slug")
	if err := f.repo.Create(ctx, first); err != nil {
		t.Fatalf("Create the first product: %v", err)
	}

	// A different product with the same slug but a different code.
	collision := newProduct(t, model.ProductDraft{
		Name:       "Collision",
		Code:       "code-collision",
		Slug:       "classify-slug",
		Price:      first.Price,
		CategoryID: categoryID,
		Variants:   []model.VariantDraft{{Name: "V", SKU: "sku-collision", Position: 0}},
	})
	if err := f.repo.Create(ctx, collision); !errors.Is(err, domainerr.ErrProductSlugTaken) {
		t.Fatalf("expected ErrProductSlugTaken, got %v", err)
	}

	// A different product with a different slug but the same code.
	codeClash := newProduct(t, model.ProductDraft{
		Name:       "Code clash",
		Code:       "CODE-CLASSIFY-SLUG",
		Slug:       "classify-code",
		Price:      first.Price,
		CategoryID: categoryID,
		Variants:   []model.VariantDraft{{Name: "V", SKU: "sku-code-clash", Position: 0}},
	})
	if err := f.repo.Create(ctx, codeClash); !errors.Is(err, domainerr.ErrProductCodeTaken) {
		t.Fatalf("expected ErrProductCodeTaken, got %v", err)
	}

	// A category identifier no row carries.
	unknown := newProduct(t, model.ProductDraft{
		Name:       "Unknown category",
		Code:       "code-unknown-category",
		Slug:       "classify-unknown-category",
		Price:      first.Price,
		CategoryID: uuid.New(),
		Variants:   []model.VariantDraft{{Name: "V", SKU: "sku-unknown-category", Position: 0}},
	})
	err := f.repo.Create(ctx, unknown)
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the invalid field to be %q, got %v", model.FieldCategoryID, err)
	}

	if got := f.count(t, `SELECT count(*) FROM products`); got != 1 {
		t.Fatalf("expected exactly one product to have survived, got %d", got)
	}
}
