//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// This file proves US3's product half at the storage layer: a variant's price,
// sell state and image are its own and nothing is inherited from a sibling, and a
// variant with no own price falls back to the product's default (FR-014, FR-015,
// FR-017, SC-004). It reuses the fixture and the model builders of
// product_integration_test.go and variant_integration_test.go, because a second
// migration runner and a second product builder would be a second answer to the
// same question.
//
// It reads through the same projections the consumers use: the variant-facts read
// (the cart's and order's contract) for the effective price and the on-sale flag,
// and the ordered list read for the sell state and the image.

// factsByID reads a set of variants' facts and indexes them by identifier, so a
// test can assert one variant without depending on the read order.
func factsByID(t *testing.T, f *productFixture, ids []uuid.UUID) map[uuid.UUID]domainrepo.VariantFacts {
	t.Helper()
	facts, err := f.variants.FindFactsByIDs(context.Background(), ids)
	if err != nil {
		t.Fatalf("FindFactsByIDs: %v", err)
	}
	out := make(map[uuid.UUID]domainrepo.VariantFacts, len(facts))
	for _, fact := range facts {
		out[fact.VariantID] = fact
	}
	return out
}

// variantWithImage builds a domain-valid variant carrying a single image, so a
// picture-independence test can start from a variant that has one.
func variantWithImage(t *testing.T, productID uuid.UUID, name, sku string, image *model.VariantImage, position int) *model.Variant {
	t.Helper()
	variant, err := model.NewVariant(productID, model.VariantDraft{
		Name: name, SKU: sku, Image: image, Position: position,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariant(%q): %v", sku, err)
	}
	return variant
}

// FR-017, SC-004: a variant's effective price is its own when set and the product's
// default when not, and changing one variant's price or clearing it changes only
// that variant's price.
func TestVariantPriceIsIndependentAndFallsBackToTheProductDefault(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-sku-price")
	product := buildProduct(t, categoryID, "sku-price")
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	// The product's default price is the fallback a variant with no own price takes.
	fallback := buildVariant(t, product.ID, "Mặc định", "sku-price-default", nil, 0)
	own := int64(2500)
	premium := buildVariant(t, product.ID, "Cao cấp", "sku-price-premium", &own, 1)
	for _, variant := range []*model.Variant{fallback, premium} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	facts := factsByID(t, f, []uuid.UUID{fallback.ID, premium.ID})
	if facts[fallback.ID].Price.Amount != product.Price.Amount {
		t.Fatalf("a variant with no own price must fall back to the product default %d, got %d",
			product.Price.Amount, facts[fallback.ID].Price.Amount)
	}
	if facts[premium.ID].Price.Amount != 2500 {
		t.Fatalf("a variant with its own price must use it, got %d", facts[premium.ID].Price.Amount)
	}

	// Changing the premium variant's price changes only it.
	loaded, err := f.variants.FindByID(ctx, premium.ID)
	if err != nil {
		t.Fatalf("FindByID(premium): %v", err)
	}
	newPrice := int64(3000)
	if err := loaded.Apply(model.VariantEdit{PriceAmount: &newPrice, PriceAmountSet: true}, time.Now().UTC()); err != nil {
		t.Fatalf("Apply(premium price): %v", err)
	}
	if err := f.variants.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(premium): %v", err)
	}
	facts = factsByID(t, f, []uuid.UUID{fallback.ID, premium.ID})
	if facts[premium.ID].Price.Amount != 3000 {
		t.Fatalf("the premium variant's new price was not written, got %d", facts[premium.ID].Price.Amount)
	}
	if facts[fallback.ID].Price.Amount != product.Price.Amount {
		t.Fatalf("changing one variant's price changed its sibling, got %d", facts[fallback.ID].Price.Amount)
	}

	// Clearing the premium variant's own price falls back to the product default.
	loaded, err = f.variants.FindByID(ctx, premium.ID)
	if err != nil {
		t.Fatalf("FindByID(premium) after edit: %v", err)
	}
	if err := loaded.Apply(model.VariantEdit{PriceAmountSet: true}, time.Now().UTC()); err != nil {
		t.Fatalf("Apply(clear premium price): %v", err)
	}
	if err := f.variants.Update(ctx, loaded); err != nil {
		t.Fatalf("Update(premium clear): %v", err)
	}
	facts = factsByID(t, f, []uuid.UUID{premium.ID})
	if facts[premium.ID].Price.Amount != product.Price.Amount {
		t.Fatalf("clearing a variant's own price must fall back to the default %d, got %d",
			product.Price.Amount, facts[premium.ID].Price.Amount)
	}
}

// FR-015, SC-004: a variant's sell state is its own; launching or selling out one
// variant leaves its sibling exactly where it was.
func TestVariantSellStateIsIndependent(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-sku-state")
	product := buildProduct(t, categoryID, "sku-state")
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	first := buildVariant(t, product.ID, "Bán", "sku-state-active", nil, 0)
	second := buildVariant(t, product.ID, "Sắp ra mắt", "sku-state-soon", nil, 1)
	for _, variant := range []*model.Variant{first, second} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	// Both start COMING_SOON; launching the first moves only it.
	if err := first.Launch(time.Now().UTC()); err != nil {
		t.Fatalf("Launch(first): %v", err)
	}
	if err := f.variants.Update(ctx, first); err != nil {
		t.Fatalf("Update(first): %v", err)
	}
	gotFirst, err := f.variants.FindByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("FindByID(first): %v", err)
	}
	gotSecond, err := f.variants.FindByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("FindByID(second): %v", err)
	}
	if gotFirst.SellState != "ACTIVE" {
		t.Fatalf("the launched variant must be ACTIVE, got %s", gotFirst.SellState)
	}
	if gotSecond.SellState != "COMING_SOON" {
		t.Fatalf("launching one variant moved its sibling, got %s", gotSecond.SellState)
	}

	// Selling the first out moves only it; the sibling stays announced.
	if err := gotFirst.SellOut(time.Now().UTC()); err != nil {
		t.Fatalf("SellOut(first): %v", err)
	}
	if err := f.variants.Update(ctx, gotFirst); err != nil {
		t.Fatalf("Update(first) after sell out: %v", err)
	}
	if state := sellStateOf(t, f, first.ID); state != "OUT_OF_STOCK" {
		t.Fatalf("the sold-out variant must be OUT_OF_STOCK, got %s", state)
	}
	if state := sellStateOf(t, f, second.ID); state != "COMING_SOON" {
		t.Fatalf("selling one variant out moved its sibling, got %s", state)
	}
}

// FR-016, SC-004: a variant's image is its own; giving one variant an image or
// changing it leaves its sibling's image untouched.
func TestVariantPictureIsIndependent(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	categoryID := f.requireCategory(t, "category-sku-image")
	product := buildProduct(t, categoryID, "sku-image")
	if err := f.repo.Create(ctx, product); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	withImage := variantWithImage(t, product.ID, "Có ảnh", "sku-image-one",
		&model.VariantImage{PublicID: "demo/one", URL: "https://example.test/one.jpg", Width: 10, Height: 20}, 0)
	without := buildVariant(t, product.ID, "Không ảnh", "sku-image-two", nil, 1)
	for _, variant := range []*model.Variant{withImage, without} {
		if err := f.variants.Create(ctx, variant); err != nil {
			t.Fatalf("Create(%q): %v", variant.SKU, err)
		}
	}

	gotWith, err := f.variants.FindByID(ctx, withImage.ID)
	if err != nil {
		t.Fatalf("FindByID(with image): %v", err)
	}
	if gotWith.Image == nil || gotWith.Image.URL != "https://example.test/one.jpg" {
		t.Fatalf("the variant image did not round-trip: %+v", gotWith.Image)
	}
	gotWithout, err := f.variants.FindByID(ctx, without.ID)
	if err != nil {
		t.Fatalf("FindByID(without image): %v", err)
	}
	if gotWithout.Image != nil {
		t.Fatalf("a sibling without an image must stay without one, got %+v", gotWithout.Image)
	}

	// Giving the second variant an image leaves the first's untouched.
	gotWithout.Image = &model.VariantImage{PublicID: "demo/two", URL: "https://example.test/two.jpg", Width: 5, Height: 5}
	if err := f.variants.Update(ctx, gotWithout); err != nil {
		t.Fatalf("Update(second image): %v", err)
	}
	gotWith, err = f.variants.FindByID(ctx, withImage.ID)
	if err != nil {
		t.Fatalf("FindByID(with image) after sibling edit: %v", err)
	}
	if gotWith.Image == nil || gotWith.Image.URL != "https://example.test/one.jpg" {
		t.Fatalf("giving one variant an image changed its sibling's, got %+v", gotWith.Image)
	}
	changed, err := f.variants.FindByID(ctx, without.ID)
	if err != nil {
		t.Fatalf("FindByID(second) after edit: %v", err)
	}
	if changed.Image == nil || changed.Image.URL != "https://example.test/two.jpg" {
		t.Fatalf("the second variant's new image was not written: %+v", changed.Image)
	}
}

// sellStateOf reads one variant's sell state through the repository, so an
// assertion is on what was stored rather than on a freshly built value.
func sellStateOf(t *testing.T, f *productFixture, variantID uuid.UUID) string {
	t.Helper()
	variant, err := f.variants.FindByID(context.Background(), variantID)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", variantID, err)
	}
	return string(variant.SellState)
}
