//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the VariantRepository adapter against a real PostgreSQL
// container. It reuses the fixture and the raw helpers of
// product_integration_test.go, because a second migration runner and a second
// category insert would be a second answer to the same question.

// seedProduct writes one raw product and returns its identifier, so a variant
// test has a real parent to attach to.
func seedProduct(t *testing.T, f *productFixture) uuid.UUID {
	t.Helper()
	categoryID := f.requireCategory(t, "category-variant-repo")
	productID, err := f.rawInsertProduct(context.Background(), validRaw(categoryID, "variant-repo-product"))
	if err != nil {
		t.Fatalf("insert the product: %v", err)
	}
	return productID
}

// FR-003, FR-010, FR-012: a variant is created, read back with its own fields,
// listed in display order, counted, edited and removed through the adapter.
func TestVariantRepositoryPersistsAndReadsBack(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	productID := seedProduct(t, f)

	second := int64(2000)
	image := &model.VariantImage{PublicID: "demo/v", URL: "https://example.test/v.jpg", Width: 10, Height: 20}
	firstVariant, err := model.NewVariant(productID, model.VariantDraft{
		Name: "First", SKU: "repo-sku-first", PriceAmount: nil, Position: 0,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariant first: %v", err)
	}
	secondVariant, err := model.NewVariant(productID, model.VariantDraft{
		Name: "Second", SKU: "repo-sku-second", PriceAmount: &second, Image: image, Position: 1,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariant second: %v", err)
	}
	for _, variant := range []*model.Variant{firstVariant, secondVariant} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	// A new variant starts COMING_SOON and keeps a nil price as a nil amount.
	got, err := f.variants.FindByID(ctx, firstVariant.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.SellState != "COMING_SOON" {
		t.Fatalf("a new variant must start COMING_SOON, got %q", got.SellState)
	}
	if got.PriceAmount != nil {
		t.Fatalf("a variant with no own price must rehydrate as nil, got %d", *got.PriceAmount)
	}
	gotSecond, err := f.variants.FindByID(ctx, secondVariant.ID)
	if err != nil {
		t.Fatalf("FindByID second: %v", err)
	}
	if gotSecond.Image == nil || gotSecond.Image.URL != "https://example.test/v.jpg" ||
		gotSecond.Image.Width != 10 || gotSecond.Image.Height != 20 {
		t.Fatalf("the variant image did not round-trip: %+v", gotSecond.Image)
	}

	// ListByProduct is ordered by position, then created_at, then id.
	listed, err := f.variants.ListByProduct(ctx, productID)
	if err != nil {
		t.Fatalf("ListByProduct: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != firstVariant.ID || listed[1].ID != secondVariant.ID {
		t.Fatalf("the variants are not in display order: %+v", listed)
	}
	if count, err := f.variants.CountByProduct(ctx, productID); err != nil || count != 2 {
		t.Fatalf("CountByProduct = %d, %v; want 2", count, err)
	}

	// Edit through the entity: rename, reprice and clear the image.
	newName := "First renamed"
	newPrice := int64(1500)
	loaded := *got
	if err := loaded.Apply(model.VariantEdit{Name: &newName, PriceAmount: &newPrice, PriceAmountSet: true}, time.Now().UTC()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	loaded.Image = nil
	if err := f.variants.Update(ctx, &loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterEdit, err := f.variants.FindByID(ctx, firstVariant.ID)
	if err != nil {
		t.Fatalf("FindByID after edit: %v", err)
	}
	if afterEdit.Name != newName || afterEdit.PriceAmount == nil || *afterEdit.PriceAmount != newPrice {
		t.Fatalf("the edit was not written: %+v", afterEdit)
	}

	// Remove one variant; the other survives.
	if err := f.variants.Delete(ctx, secondVariant.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.variants.FindByID(ctx, secondVariant.ID); !errors.Is(err, domainerr.ErrVariantNotFound) {
		t.Fatalf("expected ErrVariantNotFound after delete, got %v", err)
	}
	if count, err := f.variants.CountByProduct(ctx, productID); err != nil || count != 1 {
		t.Fatalf("CountByProduct after delete = %d, %v; want 1", count, err)
	}
	if err := f.variants.Delete(ctx, uuid.New()); !errors.Is(err, domainerr.ErrVariantNotFound) {
		t.Fatalf("expected ErrVariantNotFound deleting an unknown variant, got %v", err)
	}
}

// FR-011: a SKU another variant holds is reported as VARIANT_SKU_TAKEN, compared
// ignoring case and surrounding whitespace; writing the SKU a variant already
// holds succeeds, because the unique index excludes the row being updated.
func TestVariantSKUCollisionIsReported(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	productID := seedProduct(t, f)

	firstVariant := buildVariant(t, productID, "First", "sku-collision-one", nil, 0)
	if err := f.variants.Create(ctx, firstVariant); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	secondVariant := buildVariant(t, productID, "Second", "sku-collision-two", nil, 1)
	if err := f.variants.Create(ctx, secondVariant); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	// A new variant with the same SKU (a different case and padding) is refused.
	clashing := buildVariant(t, productID, "Clash", "  SKU-COLLISION-ONE  ", nil, 2)
	if err := f.variants.Create(ctx, clashing); !errors.Is(err, domainerr.ErrVariantSKUTaken) {
		t.Fatalf("expected ErrVariantSKUTaken, got %v", err)
	}

	// An update moving the second variant onto the first's SKU is refused.
	moved := *secondVariant
	moved.SKU = "SKU-COLLISION-ONE"
	if err := f.variants.Update(ctx, &moved); !errors.Is(err, domainerr.ErrVariantSKUTaken) {
		t.Fatalf("expected ErrVariantSKUTaken on update, got %v", err)
	}
	// Writing the SKU the variant already holds succeeds.
	if err := f.variants.Update(ctx, secondVariant); err != nil {
		t.Fatalf("re-writing a variant's own SKU must succeed, got %v", err)
	}
}

// FR-012: a variant cannot be created for a product that does not exist; the
// foreign-key refusal is reported as the variant not-found sentinel.
func TestVariantCreateWithUnknownProductIsNotFound(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()

	orphan := buildVariant(t, uuid.New(), "Orphan", "sku-orphan-variant", nil, 0)
	if err := f.variants.Create(ctx, orphan); !errors.Is(err, domainerr.ErrVariantNotFound) {
		t.Fatalf("expected ErrVariantNotFound, got %v", err)
	}
}

// FR-007, FR-017: the variant-facts read answers the effective price (the
// variant's own amount, else the product's default) and the on-sale flag, and an
// identifier no variant carries is absent rather than an error.
func TestVariantFactsReadEffectivePriceAndOnSale(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-facts")
	product := buildProduct(t, categoryID, "facts-product")
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	own := int64(2500)
	onSale := buildVariant(t, product.ID, "On sale", "sku-facts-active", &own, 0)
	if err := onSale.Launch(time.Now().UTC()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// A variant reaches OUT_OF_STOCK through the legal edge COMING_SOON -> ACTIVE
	// -> OUT_OF_STOCK, never by a direct write (FR-015).
	fallback := buildVariant(t, product.ID, "Fallback", "sku-facts-fallback", nil, 1)
	if err := fallback.Launch(time.Now().UTC()); err != nil {
		t.Fatalf("Launch fallback: %v", err)
	}
	if err := fallback.SellOut(time.Now().UTC()); err != nil {
		t.Fatalf("SellOut fallback: %v", err)
	}
	for _, variant := range []*model.Variant{onSale, fallback} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	missing := uuid.New()
	facts, err := f.variants.FindFactsByIDs(ctx, []uuid.UUID{onSale.ID, fallback.ID, missing})
	if err != nil {
		t.Fatalf("FindFactsByIDs: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected the two existing variants, got %d", len(facts))
	}
	byID := map[uuid.UUID]bool{}
	for _, fact := range facts {
		byID[fact.VariantID] = true
		if fact.ProductID != product.ID {
			t.Fatalf("fact %s carried the wrong product %s", fact.VariantID, fact.ProductID)
		}
		if fact.ProductCode != product.Code || fact.ProductName != product.Name {
			t.Fatalf("fact %s carried product %q/%q", fact.VariantID, fact.ProductName, fact.ProductCode)
		}
		switch fact.VariantID {
		case onSale.ID:
			if !fact.OnSale {
				t.Fatalf("the on-sale variant must report OnSale")
			}
			if fact.Price.Amount != own || fact.Price.Currency != "VND" {
				t.Fatalf("the on-sale variant must carry its own price, got %d %s", fact.Price.Amount, fact.Price.Currency)
			}
		case fallback.ID:
			if fact.OnSale {
				t.Fatalf("an out-of-stock variant must not report OnSale")
			}
			if fact.Price.Amount != product.Price.Amount {
				t.Fatalf("a variant with no own price must fall back to the default %d, got %d",
					product.Price.Amount, fact.Price.Amount)
			}
		}
	}
	if byID[missing] {
		t.Fatalf("an identifier no variant carries must be absent from the facts")
	}

	// An empty set answers an empty slice without touching the database.
	empty, err := f.variants.FindFactsByIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("an empty id set must answer an empty slice, got %d, %v", len(empty), err)
	}
}

// specs/016-hide-guard FR-006 edge case: the facts of an ACTIVE variant whose
// product flag is off, or whose category is hidden, are absent from the read —
// exactly like a removed variant — while an ACTIVE variant of a visible product
// in a visible category is still reported on sale.
func TestVariantFactsHidesInvisibleProductsAndCategories(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	visibleCat := f.requireCategory(t, "category-facts-visible")
	hiddenCat := f.requireCategory(t, "category-facts-hidden")
	if _, err := f.pool.Exec(ctx, `UPDATE categories SET is_visible = false WHERE id = $1`, hiddenCat); err != nil {
		t.Fatalf("hide the category: %v", err)
	}

	visibleProduct := buildProduct(t, visibleCat, "facts-visible")
	if err := f.repo.Create(ctx, visibleProduct); err != nil {
		t.Fatalf("Create visible product: %v", err)
	}
	hiddenFlagProduct := buildProduct(t, visibleCat, "facts-flag-off")
	hiddenFlagProduct.Hide(now)
	if err := f.repo.Create(ctx, hiddenFlagProduct); err != nil {
		t.Fatalf("Create flag-off product: %v", err)
	}
	hiddenCatProduct := buildProduct(t, hiddenCat, "facts-hidden-cat")
	if err := f.repo.Create(ctx, hiddenCatProduct); err != nil {
		t.Fatalf("Create hidden-category product: %v", err)
	}

	visibleVariant := buildVariant(t, visibleProduct.ID, "Visible", "sku-facts-visible-active", nil, 0)
	if err := visibleVariant.Launch(now); err != nil {
		t.Fatalf("Launch visible: %v", err)
	}
	flagOffVariant := buildVariant(t, hiddenFlagProduct.ID, "Flag off", "sku-facts-flagoff-active", nil, 0)
	if err := flagOffVariant.Launch(now); err != nil {
		t.Fatalf("Launch flag-off: %v", err)
	}
	hiddenCatVariant := buildVariant(t, hiddenCatProduct.ID, "Hidden cat", "sku-facts-hiddencat-active", nil, 0)
	if err := hiddenCatVariant.Launch(now); err != nil {
		t.Fatalf("Launch hidden-category: %v", err)
	}
	for _, variant := range []*model.Variant{visibleVariant, flagOffVariant, hiddenCatVariant} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	facts, err := f.variants.FindFactsByIDs(ctx, []uuid.UUID{visibleVariant.ID, flagOffVariant.ID, hiddenCatVariant.ID, uuid.New()})
	if err != nil {
		t.Fatalf("FindFactsByIDs: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("only the visible ACTIVE variant must be reported, got %d facts", len(facts))
	}
	if facts[0].VariantID != visibleVariant.ID {
		t.Fatalf("the reported fact must be the visible variant, got %s", facts[0].VariantID)
	}
	if !facts[0].OnSale {
		t.Fatalf("the visible ACTIVE variant must report OnSale")
	}
}

// FR-002, FR-010: ListByProduct orders by position, then created_at, then id, and
// CountByProduct follows the same product filter.
func TestVariantListOrderingAndCount(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-variant-order")
	firstProduct := buildProduct(t, categoryID, "variant-order-one")
	if err := f.repo.Create(ctx, firstProduct); err != nil {
		t.Fatalf("Create first product: %v", err)
	}
	secondProduct := buildProduct(t, categoryID, "variant-order-two")
	if err := f.repo.Create(ctx, secondProduct); err != nil {
		t.Fatalf("Create second product: %v", err)
	}

	late := buildVariant(t, firstProduct.ID, "Late", "sku-order-late", nil, 5)
	early := buildVariant(t, firstProduct.ID, "Early", "sku-order-early", nil, 1)
	other := buildVariant(t, secondProduct.ID, "Other", "sku-order-other", nil, 0)
	for _, variant := range []*model.Variant{late, early, other} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	listed, err := f.variants.ListByProduct(ctx, firstProduct.ID)
	if err != nil {
		t.Fatalf("ListByProduct: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != early.ID || listed[1].ID != late.ID {
		t.Fatalf("the variants are not in ascending position order: %+v", listed)
	}
	if count, err := f.variants.CountByProduct(ctx, secondProduct.ID); err != nil || count != 1 {
		t.Fatalf("CountByProduct = %d, %v; want 1", count, err)
	}
	// A product with no variant answers an empty slice.
	emptyProduct := buildProduct(t, categoryID, "variant-order-empty")
	if err := f.repo.Create(ctx, emptyProduct); err != nil {
		t.Fatalf("Create empty product: %v", err)
	}
	none, err := f.variants.ListByProduct(ctx, emptyProduct.ID)
	if err != nil || len(none) != 0 {
		t.Fatalf("a product with no variant must answer an empty slice, got %d, %v", len(none), err)
	}
}
