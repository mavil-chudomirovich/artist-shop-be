package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the public browse use cases over the in-memory repository
// and a fake visibility port. The repository applies the whole visibility
// predicate the adapter applies — the category is visible and at least one variant
// is on sale — so the tests prove what the use case does with the answers.

func price(amount int64) model.Price { return model.Price{Amount: amount, Currency: "VND"} }

// productDraft builds a product draft with one default variant.
func productDraft(name, code string, amount int64, category uuid.UUID, position int) model.ProductDraft {
	return model.ProductDraft{
		Name: name, Code: code, Price: price(amount),
		CategoryID: category, Position: position,
		Variants: []model.VariantDraft{{Name: "Mặc định", SKU: "SKU-" + code, Position: 0}},
	}
}

// FR-001, FR-002: a product appears only when its category is visible and at least
// one variant is on sale.
func TestListPublicReturnsOnlyProductsWithAnOnSaleVariant(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	visibleCat, hiddenCat := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	listed := repo.seedWithVariants(t, productDraft("On sale", "on-sale", 100, visibleCat, 1), nil, base)
	repo.launch(t, repo.variantsFor(listed.ID)[0].ID)
	repo.seedWithVariants(t, productDraft("No variant on sale", "announced", 100, visibleCat, 2), nil, base.Add(time.Minute))
	hidden := repo.seedWithVariants(t, productDraft("Hidden", "hidden", 100, hiddenCat, 3), nil, base.Add(2*time.Minute))
	repo.launch(t, repo.variantsFor(hidden.ID)[0].ID)

	visibility := &fakeVisibility{visible: []uuid.UUID{visibleCat}}
	page, err := publicService(repo, visibility).ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Total != 1 || len(page.Products) != 1 || page.Products[0].Code != "on-sale" {
		t.Fatalf("expected only the product with an on-sale variant, got %+v", page.Products)
	}
}

// FR-002: the card price is the lowest on-sale variant's effective price and the
// card image is the default variant's image.
func TestListPublicUsesLowestOnSalePrice(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Card set", Code: "card-set", Price: price(150000), CategoryID: category, Position: 1,
		Variants: []model.VariantDraft{
			{Name: "Small", SKU: "S", PriceAmount: ptr[int64](120000), Position: 0},
			{Name: "Large", SKU: "L", PriceAmount: ptr[int64](180000), Position: 1},
			{Name: "Default", SKU: "D", Position: 2},
		},
	}, nil, base)
	for _, variant := range repo.variantsFor(product.ID) {
		repo.launch(t, variant.ID)
	}

	page, err := publicService(repo, &fakeVisibility{visible: []uuid.UUID{category}}).ListPublic(
		context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if len(page.Products) != 1 || page.Products[0].Price.Amount != 120000 {
		t.Fatalf("expected the lowest on-sale price 120000, got %+v", page.Products)
	}
}

// research D1: the visible set reaches the repository, so the filter runs in the
// query, and it is asked for exactly once per request.
func TestListPublicPassesTheVisibleSetOnce(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	product := repo.seedWithVariants(t, productDraft("A", "a", 100, category, 1), nil, base)
	repo.launch(t, repo.variantsFor(product.ID)[0].ID)

	visibility := &fakeVisibility{visible: []uuid.UUID{category}}
	if _, err := publicService(repo, visibility).ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if visibility.visibleCalls != 1 {
		t.Fatalf("the visible set must be fetched exactly once, got %d", visibility.visibleCalls)
	}
}

// FR-003, FR-005: the detail carries the visible variants only (on sale or out of
// stock) with their effective price and stock status, and per-variant availability
// costs one bulk read.
func TestGetPublicByCodeReturnsVisibleVariantsWithStockStatus(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Card set", Code: "card-set", Description: "Bộ card", Price: price(100000),
		CategoryID: category, Position: 1,
		Variants: []model.VariantDraft{
			{Name: "Active", SKU: "A", PriceAmount: ptr[int64](120000), Position: 0},
			{Name: "Sold out", SKU: "S", Position: 1},
			{Name: "Coming", SKU: "C", Position: 2},
			{Name: "Retired", SKU: "R", Position: 3},
		},
	}, nil, base)
	variants := repo.variantsFor(product.ID)
	repo.launch(t, variants[0].ID)
	repo.launch(t, variants[1].ID)
	repo.sellOut(t, variants[1].ID)
	repo.retire(t, variants[3].ID)

	availability := newFakeAvailability()
	availability.available[variants[0].ID] = 5
	availability.available[variants[1].ID] = 0

	svc := serviceFor(repo, availability, &fakeVisibility{visible: []uuid.UUID{category}}, nil, nil, nil)
	out, err := svc.GetPublicByCode(context.Background(), dto.PublicProductRefInput{Code: "card-set"})
	if err != nil {
		t.Fatalf("GetPublicByCode: %v", err)
	}
	if len(out.Variants) != 2 {
		t.Fatalf("expected the on-sale and out-of-stock variants, got %+v", out.Variants)
	}
	if out.Variants[0].Name != "Active" || !out.Variants[0].InStock || out.Variants[0].Price.Amount != 120000 {
		t.Fatalf("first variant = %+v", out.Variants[0])
	}
	if out.Variants[1].Name != "Sold out" || out.Variants[1].InStock || out.Variants[1].SellState != constant.SellStateOutOfStock {
		t.Fatalf("second variant = %+v", out.Variants[1])
	}
	if availability.calls != 1 {
		t.Fatalf("the detail must cost one bulk availability read, got %d", availability.calls)
	}
}

// FR-008: a product with no on-sale variant, in a hidden category, removed or
// unknown all answer the same not-found.
func TestGetPublicByCodeAnswersNotFoundForAHiddenProduct(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	visibleCat, hiddenCat := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	visible := repo.seedWithVariants(t, productDraft("Visible", "visible", 100, visibleCat, 1), nil, base)
	repo.launch(t, repo.variantsFor(visible.ID)[0].ID)
	hidden := repo.seedWithVariants(t, productDraft("Hidden", "hidden", 100, hiddenCat, 2), nil, base.Add(time.Minute))
	repo.launch(t, repo.variantsFor(hidden.ID)[0].ID)
	repo.seedWithVariants(t, productDraft("No sale", "no-sale", 100, visibleCat, 3), nil, base.Add(2*time.Minute))

	svc := publicService(repo, &fakeVisibility{visible: []uuid.UUID{visibleCat}})
	ctx := context.Background()

	if _, err := svc.GetPublicByCode(ctx, dto.PublicProductRefInput{Code: "hidden"}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product in a hidden category must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicByCode(ctx, dto.PublicProductRefInput{Code: "no-sale"}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product with no on-sale variant must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicByCode(ctx, dto.PublicProductRefInput{Code: "never-existed"}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("an unknown code must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicByCode(ctx, dto.PublicProductRefInput{Code: "visible"}); err != nil {
		t.Fatalf("a visible code must resolve, got %v", err)
	}
}

// FR-006: the category filter resolves a slug through the contract; an unknown slug
// answers an empty page.
func TestListPublicResolvesTheCategorySlugFilter(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	catA, catB := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	inA := repo.seedWithVariants(t, productDraft("A", "a", 100, catA, 1), nil, base)
	repo.launch(t, repo.variantsFor(inA.ID)[0].ID)
	inB := repo.seedWithVariants(t, productDraft("B", "b", 100, catB, 2), nil, base.Add(time.Minute))
	repo.launch(t, repo.variantsFor(inB.ID)[0].ID)

	visibility := &fakeVisibility{
		visible: []uuid.UUID{catA, catB},
		slugs:   map[string]uuid.UUID{"cat-a": catA, "cat-b": catB},
	}
	page, err := publicService(repo, visibility).ListPublic(context.Background(),
		dto.ListPublicInput{Page: 1, PageSize: 20, CategorySlug: "cat-a"})
	if err != nil {
		t.Fatalf("filtered ListPublic: %v", err)
	}
	if page.Total != 1 || len(page.Products) != 1 || page.Products[0].Code != "a" {
		t.Fatalf("expected only category A's product, got %+v", page.Products)
	}

	unknown, err := publicService(repo, visibility).ListPublic(context.Background(),
		dto.ListPublicInput{Page: 1, PageSize: 20, CategorySlug: "never-existed"})
	if err != nil {
		t.Fatalf("an unknown filter slug must not be an error, got %v", err)
	}
	if len(unknown.Products) != 0 || unknown.Total != 0 {
		t.Fatalf("an unknown filter slug must answer an empty page, got %+v", unknown.Products)
	}
}

// FR-002: the catalogue card image is the default (first) variant's image, falling
// back to the product's primary gallery picture, and absent when neither exists.
// The cheapest out-of-stock variant never contributes an image or a price.
func TestListPublicUsesTheDefaultVariantImageWithFallback(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	variantImage := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Card set", Code: "card-set", Price: price(150000),
		CategoryID: category, Position: 1,
		Variants: []model.VariantDraft{
			{Name: "Small", SKU: "S", PriceAmount: ptr[int64](120000), Position: 0,
				Image: &model.VariantImage{PublicID: "v/s", URL: "https://cdn.test/small.jpg", Width: 10, Height: 10}},
			{Name: "Large", SKU: "L", PriceAmount: ptr[int64](180000), Position: 1,
				Image: &model.VariantImage{PublicID: "v/l", URL: "https://cdn.test/large.jpg", Width: 10, Height: 10}},
		},
	}, nil, base)
	// A product gallery picture the default variant's image must win over.
	repo.addSeedPicture(variantImage.ID, 0, true)
	for _, variant := range repo.variantsFor(variantImage.ID) {
		repo.launch(t, variant.ID)
	}

	// No variant image: the primary gallery picture is the card image.
	fallback := repo.seedWithVariants(t, productDraft("Fallback", "fallback", 100, category, 2), nil, base.Add(time.Minute))
	repo.addSeedPicture(fallback.ID, 0, true)
	repo.launch(t, repo.variantsFor(fallback.ID)[0].ID)

	// Neither: the card carries no image.
	bare := repo.seedWithVariants(t, productDraft("Bare", "bare", 100, category, 3), nil, base.Add(2*time.Minute))
	repo.launch(t, repo.variantsFor(bare.ID)[0].ID)

	page, err := publicService(repo, &fakeVisibility{visible: []uuid.UUID{category}}).ListPublic(
		context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	byCode := make(map[string]dto.PublicProductOutput, len(page.Products))
	for _, entry := range page.Products {
		byCode[entry.Code] = entry
	}

	if got := byCode["card-set"].ImageURL; got == nil || *got != "https://cdn.test/small.jpg" {
		t.Fatalf("the card image must be the default variant's image, got %+v", got)
	}
	if got := byCode["fallback"].ImageURL; got == nil || *got != "https://cdn.example.test/seed.jpg" {
		t.Fatalf("with no variant image the card must fall back to the primary picture, got %+v", got)
	}
	if got := byCode["bare"].ImageURL; got != nil {
		t.Fatalf("with neither image the card must carry no image, got %+v", got)
	}
}

// FR-002, FR-003: the product detail orders its visible variants so the first is
// the default variant, and its summary carries the default variant's image and the
// lowest on-sale price — never an out-of-stock variant's cheaper price.
func TestDetailOrdersVariantsDefaultFirstWithTheSummaryFigure(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Card set", Code: "card-set", Description: "Bộ card",
		Price: price(100000), CategoryID: category, Position: 1,
		Variants: []model.VariantDraft{
			{Name: "Default", SKU: "D", Position: 0,
				Image: &model.VariantImage{PublicID: "v/d", URL: "https://cdn.test/default.jpg", Width: 10, Height: 10}},
			{Name: "Cheaper", SKU: "C", PriceAmount: ptr[int64](60000), Position: 1,
				Image: &model.VariantImage{PublicID: "v/c", URL: "https://cdn.test/cheaper.jpg", Width: 10, Height: 10}},
			{Name: "Coming", SKU: "N", Position: 2},
		},
	}, nil, base)
	variants := repo.variantsFor(product.ID)
	repo.launch(t, variants[0].ID)
	repo.launch(t, variants[1].ID)
	// variants[2] stays COMING_SOON and must not be listed.

	availability := newFakeAvailability()
	availability.available[variants[0].ID] = 3
	availability.available[variants[1].ID] = 0

	svc := serviceFor(repo, availability, &fakeVisibility{visible: []uuid.UUID{category}}, nil, nil, nil)
	out, err := svc.GetPublicByCode(context.Background(), dto.PublicProductRefInput{Code: "card-set"})
	if err != nil {
		t.Fatalf("GetPublicByCode: %v", err)
	}
	if len(out.Variants) != 2 {
		t.Fatalf("only the visible variants must be listed, got %+v", out.Variants)
	}
	if out.Variants[0].Name != "Default" {
		t.Fatalf("the first (default) variant must be listed first, got %+v", out.Variants)
	}
	if out.Price.Amount != 60000 {
		t.Fatalf("the detail summary price must be the lowest on-sale price, got %d", out.Price.Amount)
	}
	if out.ImageURL == nil || *out.ImageURL != "https://cdn.test/default.jpg" {
		t.Fatalf("the detail summary image must be the default variant's image, got %+v", out.ImageURL)
	}
}

// ptr returns a pointer to its argument, so a one-line variant seed can carry an
// own price.
func ptr[T any](value T) *T { return &value }
