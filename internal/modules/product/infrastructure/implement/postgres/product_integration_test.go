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

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the product adapter against a real PostgreSQL container.
//
// Its centre of gravity is what a fake repository cannot prove: that the
// migration applies to a fresh database, and that the storage layer refuses a
// duplicate slug, a malformed value, a non-positive price, a bad currency, a
// second main picture and the removal of a category that still has products —
// even when the application is bypassed. Most of the constraint tests write the
// tables directly, with no domain validation and no use case, because a guard
// against a *future* writer can only be proven by pretending to be that writer.
//
// Every rejection asserts both the SQLSTATE and the constraint name, so a test
// fails if the constraint it names is removed from the migration. That mutation
// check is the evidence the T026 report records for the restricting foreign key
// and the one-primary index.

// SQLSTATE codes the constraint tests distinguish.
const (
	sqlStateUnique     = "23505"
	sqlStateForeignKey = "23503"
	sqlStateCheck      = "23514"
)

// The constraint and index names migrations/00006_product.sql must create
// (data-model.md).
const (
	idxNormalizedSlug     = "products_normalized_slug_key"
	ckNormalizedSlugShape = "products_normalized_slug_shape_ck"
	ckSlugFormat          = "products_slug_format_ck"
	ckNameLength          = "products_name_length_ck"
	ckSlugLength          = "products_slug_length_ck"
	ckDescriptionLength   = "products_description_length_ck"
	ckPriceAmount         = "products_price_amount_ck"
	ckCurrency            = "products_currency_ck"
	ckSellState           = "products_sell_state_ck"
	ckPreorder            = "products_preorder_ck"
	ckPreorderDate        = "products_preorder_date_ck"
	fkCategory            = "products_category_fk"
	idxOrdering           = "products_ordering_idx"
	idxCategory           = "products_category_idx"

	fkImageProduct  = "product_images_product_fk"
	ckImageWidth    = "product_images_width_ck"
	ckImageHeight   = "product_images_height_ck"
	ckImageHTTPS    = "product_images_secure_url_ck"
	idxOnePrimary   = "product_images_one_primary_key"
	idxImageOrder   = "product_images_ordering_idx"
	pkSetItems      = "product_set_items_pkey"
	ckSetItemsSelf  = "product_set_items_not_self_ck"
	fkSetItemsSet   = "product_set_items_set_fk"
	fkSetItemsMembr = "product_set_items_member_fk"
)

// productFixture is one migrated PostgreSQL container plus the adapter under
// test.
type productFixture struct {
	pool *pgxpool.Pool
	repo *ProductRepository
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
	return &productFixture{pool: pool, repo: NewProductRepository(pool)}
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

// rawProduct carries the columns a raw insert writes, deliberately skipping the
// domain and the adapter.
type rawProduct struct {
	CategoryID     uuid.UUID
	Name           string
	Slug           string
	NormalizedSlug string
	Description    string
	PriceAmount    int64
	Currency       string
	Position       int
	SellState      string
	IsSet          bool
	IsPreorder     bool
	ExpectedAt     *time.Time
}

// validRaw returns a row every constraint accepts, so a subtest changes only the
// value it is proving is refused.
func validRaw(categoryID uuid.UUID, slug string) rawProduct {
	return rawProduct{
		CategoryID:     categoryID,
		Name:           "Product " + slug,
		Slug:           slug,
		NormalizedSlug: slug,
		Description:    "a description",
		PriceAmount:    1000,
		Currency:       "VND",
		Position:       0,
		SellState:      "COMING_SOON",
	}
}

func (f *productFixture) rawInsertProduct(ctx context.Context, p rawProduct) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder,
			 preorder_expected_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now(), now())`
	_, err := f.pool.Exec(ctx, query, id, p.Name, p.Slug, p.NormalizedSlug, p.Description,
		p.PriceAmount, p.Currency, p.CategoryID, p.Position, p.SellState,
		p.IsSet, p.IsPreorder, p.ExpectedAt)
	return id, err
}

func (f *productFixture) rawInsertPicture(ctx context.Context, productID uuid.UUID, primary bool) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO product_images
			(id, product_id, public_id, secure_url, width, height, position, is_primary)
		VALUES ($1, $2, $3, 'https://example.test/image.jpg', 10, 10, 0, $4)`
	_, err := f.pool.Exec(ctx, query, id, productID, "demo/"+id.String(), primary)
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

// buildProduct constructs a domain-valid product for the adapter tests.
func buildProduct(t *testing.T, categoryID uuid.UUID, slug string, preorder bool) *model.Product {
	t.Helper()
	price, err := model.NewPrice(1000, "VND")
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	product, err := model.NewProduct(model.ProductDraft{
		Name:       "Product " + slug,
		Slug:       slug,
		Price:      price,
		CategoryID: categoryID,
		Position:   0,
		IsPreorder: preorder,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", slug, err)
	}
	return product
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

// FR-015, FR-016, FR-027 to FR-039: the migration is applied to a brand new
// container database — proof that it applies to a fresh database — and every
// constraint and index it must have created is checked by name.
func TestTheProductMigrationAppliesAndCreatesEveryConstraint(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()

	for _, table := range []string{"products", "product_images", "product_set_items"} {
		var exists bool
		if err := f.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("look up table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("the migration did not create the %s table", table)
		}
	}

	checks := []string{
		ckNormalizedSlugShape, ckSlugFormat, ckNameLength, ckSlugLength,
		ckDescriptionLength, ckPriceAmount, ckCurrency, ckSellState,
		ckPreorder, ckPreorderDate, fkCategory,
		ckImageWidth, ckImageHeight, ckImageHTTPS, fkImageProduct,
		pkSetItems, ckSetItemsSelf, fkSetItemsSet, fkSetItemsMembr,
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

	indexes := []string{idxNormalizedSlug, idxOrdering, idxCategory, idxOnePrimary, idxImageOrder}
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
	case strings.HasPrefix(name, "product_set_items_"):
		return "product_set_items"
	default:
		return "products"
	}
}

// FR-027 to FR-039: every storage-level rule holds against a writer that
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

	t.Run("unknown sell state", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) { p.SellState = "ARCHIVED" }, "sell-state-bad")
		requireConstraint(t, err, sqlStateCheck, ckSellState)
	})

	t.Run("pre-order label while on sale", func(t *testing.T) {
		err := insert(t, func(p *rawProduct) {
			p.SellState = "ACTIVE"
			p.IsPreorder = true
		}, "preorder-active")
		requireConstraint(t, err, sqlStateCheck, ckPreorder)
	})

	t.Run("expected date without the pre-order label", func(t *testing.T) {
		date := time.Now().UTC().Add(24 * time.Hour)
		err := insert(t, func(p *rawProduct) {
			p.IsPreorder = false
			p.ExpectedAt = &date
		}, "preorder-date")
		requireConstraint(t, err, sqlStateCheck, ckPreorderDate)
	})
}

// FR-034, FR-035, research D15: the restricting foreign key refuses the removal
// of a category that still has products, from the storage layer, so no code path
// can leave a product pointing at a category that is gone. The mutation check in
// the report drops products_category_fk and shows this test fail.
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

	// Once the product is gone, the same delete succeeds: the rule is the
	// reference, not the category.
	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE category_id = $1`, categoryID); err != nil {
		t.Fatalf("remove the product: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID); err != nil {
		t.Fatalf("the category must be removable once it has no products, got %v", err)
	}
}

// FR-016, research D7: "at most one main picture" is a uniqueness property the
// partial index enforces, not an application check. A second primary is refused,
// while any number of non-primary pictures is allowed — which is what makes the
// index partial. The mutation check in the report drops the index and shows this
// test fail.
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

	// A non-primary picture is not covered by the partial index and is accepted.
	nonPrimary, err := f.rawInsertPicture(ctx, productID, false)
	if err != nil {
		t.Fatalf("a non-primary picture must be accepted, got %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1`, productID); got != 2 {
		t.Fatalf("expected two pictures (one primary, one not), got %d", got)
	}

	// Promoting the non-primary picture must leave exactly one primary, the
	// promoted one: research D7's transition, through the adapter.
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

// research D13, US5 scenario 3: removing a product cascades its pictures and its
// membership rows, and removing a member removes it from the sets it was in —
// while the members of a removed set survive.
func TestCascadingDeletesRemovePicturesAndMembership(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()

	t.Run("removing a set removes its membership rows but not its members", func(t *testing.T) {
		categoryID := f.requireCategory(t, "category-cascade-set")
		setProduct := buildProduct(t, categoryID, "cascade-set", false)
		setProduct.IsSet = true
		member := buildProduct(t, categoryID, "cascade-member", false)
		for _, product := range []*model.Product{setProduct, member} {
			if err := f.repo.Create(ctx, product); err != nil {
				t.Fatalf("Create(%q): %v", product.Slug, err)
			}
		}
		if err := f.repo.ReplaceSetMembers(ctx, setProduct.ID, []uuid.UUID{member.ID}); err != nil {
			t.Fatalf("ReplaceSetMembers: %v", err)
		}
		picture := &model.Picture{
			ID: uuid.New(), ProductID: setProduct.ID, PublicID: "demo/cascade",
			URL: "https://example.test/cascade.jpg", Width: 10, Height: 10,
			Position: 0, IsPrimary: true, CreatedAt: time.Now().UTC(),
		}
		if err := f.repo.AddPicture(ctx, picture); err != nil {
			t.Fatalf("AddPicture: %v", err)
		}

		if err := f.repo.Delete(ctx, setProduct.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if got := f.count(t, `SELECT count(*) FROM product_images WHERE product_id = $1`, setProduct.ID); got != 0 {
			t.Errorf("the set's pictures survived the cascade, count is %d", got)
		}
		if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE set_product_id = $1`, setProduct.ID); got != 0 {
			t.Errorf("the set's membership rows survived the cascade, count is %d", got)
		}
		if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, member.ID); got != 1 {
			t.Errorf("removing a set must not remove its members, member count is %d", got)
		}
	})

	t.Run("removing a member removes it from the sets it was in", func(t *testing.T) {
		categoryID := f.requireCategory(t, "category-cascade-member")
		setProduct := buildProduct(t, categoryID, "cascade-set-two", false)
		setProduct.IsSet = true
		member := buildProduct(t, categoryID, "cascade-member-two", false)
		for _, product := range []*model.Product{setProduct, member} {
			if err := f.repo.Create(ctx, product); err != nil {
				t.Fatalf("Create(%q): %v", product.Slug, err)
			}
		}
		if err := f.repo.ReplaceSetMembers(ctx, setProduct.ID, []uuid.UUID{member.ID}); err != nil {
			t.Fatalf("ReplaceSetMembers: %v", err)
		}
		if err := f.repo.Delete(ctx, member.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if got := f.count(t, `SELECT count(*) FROM product_set_items WHERE member_product_id = $1`, member.ID); got != 0 {
			t.Errorf("the membership row of a removed member survived, count is %d", got)
		}
		if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, setProduct.ID); got != 1 {
			t.Errorf("removing a member must not remove the set, set count is %d", got)
		}
	})

	t.Run("adding a picture to an unknown product answers not-found", func(t *testing.T) {
		picture := &model.Picture{
			ID: uuid.New(), ProductID: uuid.New(), PublicID: "demo/orphan",
			URL: "https://example.test/orphan.jpg", Width: 10, Height: 10,
			CreatedAt: time.Now().UTC(),
		}
		if err := f.repo.AddPicture(ctx, picture); !errors.Is(err, domainerr.ErrProductNotFound) {
			t.Fatalf("expected ErrProductNotFound, got %v", err)
		}
	})
}

// FR-015, SC-008: the identifier never changes, and no update path can write it.
// A transition, an edit and a forged identifier are all exercised through the
// adapter, and the raw column is read back to prove the folding key is correct.
func TestTheIdentifierSurvivesEveryUpdateAndNoPathRewritesIt(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-stable-id")

	product := buildProduct(t, categoryID, "stable-id", true)
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create: %v", err)
	}
	original := product.ID

	// An edit that changes every editable member except the identifier.
	loaded, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if loaded.Product.ID != original {
		t.Fatalf("the read returned id %s, want %s", loaded.Product.ID, original)
	}
	newName := "Product renamed"
	newSlug := "stable-id-renamed"
	newDescription := "changed"
	newPosition := 7
	edited := loaded.Product
	if err := edited.Apply(model.ProductEdit{
		Name:        &newName,
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
		afterEdit.Product.Description != newDescription || afterEdit.Product.Position != newPosition {
		t.Fatalf("the edit was not written: %+v", afterEdit.Product)
	}

	// The folding key is recomputed by the adapter from the slug it writes, so
	// the unique index keeps a correct value to enforce.
	var storedKey string
	if err := f.pool.QueryRow(ctx, `SELECT normalized_slug FROM products WHERE id = $1`, original).Scan(&storedKey); err != nil {
		t.Fatalf("read the folding key: %v", err)
	}
	if storedKey != model.FoldKey(newSlug) {
		t.Fatalf("the update stored folding key %q, want %q", storedKey, model.FoldKey(newSlug))
	}

	// A sell-state transition persists through the same update path, and
	// launching clears the pre-order label (FR-039).
	launched, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID before the transition: %v", err)
	}
	if err := launched.Product.Launch(time.Now().UTC().Add(2 * time.Minute)); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := f.repo.Update(ctx, &launched.Product); err != nil {
		t.Fatalf("Update after the transition: %v", err)
	}
	var state string
	var preorder bool
	var expectedAt *time.Time
	if err := f.pool.QueryRow(ctx,
		`SELECT sell_state, is_preorder, preorder_expected_at FROM products WHERE id = $1`, original).
		Scan(&state, &preorder, &expectedAt); err != nil {
		t.Fatalf("read the state columns: %v", err)
	}
	if state != string(constant.SellStateActive) {
		t.Fatalf("the transition was not written: sell_state is %q", state)
	}
	if preorder || expectedAt != nil {
		t.Fatalf("launching must clear the pre-order label, got is_preorder=%v expected=%v", preorder, expectedAt)
	}

	// An update carrying a different identifier must not rewrite or create a
	// row: it matches nothing and is reported as not found.
	forged := launched.Product
	forged.ID = uuid.New()
	if err := f.repo.Update(ctx, &forged); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound for a forged identifier, got %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM products`); got != 1 {
		t.Fatalf("a forged-identifier update created or removed a row, count is %d", got)
	}
	survivor, err := f.repo.FindByID(ctx, original)
	if err != nil {
		t.Fatalf("FindByID after the forged update: %v", err)
	}
	if survivor.Product.ID != original {
		t.Fatalf("the surviving row's identifier moved: %s", survivor.Product.ID)
	}
}

// FR-009, SC-009: with several products sharing a position, two identical reads
// return the same order, and the immutable identifier is what breaks the
// remaining tie. The order is position, then created_at, then id.
func TestPageOrderingIsStableAcrossTwoReads(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-ordering")
	fixed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)

	for _, slug := range []string{"order-alpha", "order-beta", "order-gamma"} {
		if _, err := f.rawInsertProduct(ctx, validRaw(categoryID, slug)); err != nil {
			t.Fatalf("insert %q: %v", slug, err)
		}
		// validRaw writes position 0 for all three and now() for created_at;
		// force a shared, fixed creation time so the identifier is the only
		// tie-break left.
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

// FR-002, FR-006, research D1: both public reads carry the whole visibility
// predicate — (sell_state = 'ACTIVE' OR is_preorder) AND category_id = ANY($n) —
// in their own query. This proves the array binding and the predicate against
// real rows: a product is hidden by its own state, by a hidden category, or both,
// and an empty visible set answers empty without a query.
func TestVisibleReadsApplyTheWholeVisibilityPredicate(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	visibleCategory := f.requireCategory(t, "category-visible")
	hiddenCategory := f.requireCategory(t, "category-hidden")

	rows := map[string]rawProduct{
		"visible-active": {CategoryID: visibleCategory, Slug: "visible-active", SellState: "ACTIVE"},
		"visible-coming": {CategoryID: visibleCategory, Slug: "visible-coming", SellState: "COMING_SOON"},
		"visible-preorder": {
			CategoryID: visibleCategory, Slug: "visible-preorder",
			SellState: "COMING_SOON", IsPreorder: true,
		},
		"hidden-category": {CategoryID: hiddenCategory, Slug: "hidden-category", SellState: "ACTIVE"},
		"visible-retired": {CategoryID: visibleCategory, Slug: "visible-retired", SellState: "DISCONTINUED"},
	}
	for slug, base := range rows {
		p := base
		p.Name = "Product " + slug
		p.NormalizedSlug = slug
		p.PriceAmount = 1000
		p.Currency = "VND"
		if _, err := f.rawInsertProduct(ctx, p); err != nil {
			t.Fatalf("insert %q: %v", slug, err)
		}
	}

	visible, total, err := f.repo.ListVisible(ctx, visibleListQuery(visibleCategory))
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if total != 2 || len(visible) != 2 {
		t.Fatalf("expected only the on-sale and the pre-order product, got %d of %d", len(visible), total)
	}
	slugs := map[string]bool{}
	for _, item := range visible {
		slugs[item.Product.Slug] = true
	}
	if !slugs["visible-active"] || !slugs["visible-preorder"] {
		t.Fatalf("the visible page is wrong: %v", slugs)
	}

	// An empty visible set answers an empty page, never an error.
	empty, total, err := f.repo.ListVisible(ctx, visibleListQuery())
	if err != nil {
		t.Fatalf("ListVisible with no visible category: %v", err)
	}
	if total != 0 || len(empty) != 0 {
		t.Fatalf("an empty catalogue of visible categories must answer an empty page, got %d of %d", len(empty), total)
	}

	// The single read carries the same predicate: a product in a hidden
	// category is not found, and neither is one hidden by its own state.
	if _, err := f.repo.FindVisibleBySlug(ctx, "hidden-category", []uuid.UUID{visibleCategory}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product in a hidden category must not be found, got %v", err)
	}
	if _, err := f.repo.FindVisibleBySlug(ctx, "visible-coming", []uuid.UUID{visibleCategory}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product not on sale and not a pre-order must not be found, got %v", err)
	}
	if _, err := f.repo.FindVisibleBySlug(ctx, "visible-active", []uuid.UUID{visibleCategory}); err != nil {
		t.Fatalf("an on-sale product must be found, got %v", err)
	}
	if _, err := f.repo.FindVisibleBySlug(ctx, "visible-active", nil); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("an empty visible set must answer not-found, got %v", err)
	}
}

// visibleListQuery builds the public list query with the given visible categories.
func visibleListQuery(ids ...uuid.UUID) domainrepo.VisibleListQuery {
	return domainrepo.VisibleListQuery{VisibleCategoryIDs: ids, Page: 1, PageSize: 100}
}

// FR-027, FR-009, research D17: a duplicate folded slug is reported as the module
// sentinel PRODUCT_SLUG_TAKEN, and a category no row carries is reported as an
// invalid field naming categoryId — both classified from the constraint the
// database reported, never from its message.
func TestAConstraintViolationIsReportedAsTheMatchingSentinel(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-classify")

	first := buildProduct(t, categoryID, "classify-slug", false)
	if err := f.repo.Create(ctx, first); err != nil {
		t.Fatalf("Create the first product: %v", err)
	}

	// A different product with the same slug.
	collision := buildProduct(t, categoryID, "classify-slug", false)
	if err := f.repo.Create(ctx, collision); !errors.Is(err, domainerr.ErrProductSlugTaken) {
		t.Fatalf("expected ErrProductSlugTaken, got %v", err)
	}

	// A category identifier no row carries.
	unknown := buildProduct(t, uuid.New(), "classify-unknown-category", false)
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
