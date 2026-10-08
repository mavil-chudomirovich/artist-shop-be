package implement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// This file exercises the public browse use cases over an in-memory repository
// and a fake visibility port. The repository applies the whole visibility
// predicate the adapter applies — (ACTIVE or pre-order) and category in the
// visible set — so the tests prove what the use case does with the answers:
// it fetches the visible set once, passes it to the repository and never
// filters a page in Go (research D1).

// memoryProducts is an in-memory ProductRepository. It embeds the interface so
// only the two reads the public use cases reach are implemented; every other
// method is deliberately absent, because a call to one would be a defect the
// tests must not paper over.
type memoryProducts struct {
	domainrepo.ProductRepository

	mu       sync.Mutex
	products map[uuid.UUID]model.Product
	pictures map[uuid.UUID][]model.Picture

	// lastQuery records the last public list query, so a test can assert the
	// visible set reached the repository rather than being applied here.
	lastQuery domainrepo.VisibleListQuery
	// listCalls counts how many times the list read was reached.
	listCalls int
	// detailSlug records the slug the detail read was addressed by.
	detailSlug string
}

func newMemoryProducts() *memoryProducts {
	return &memoryProducts{
		products: make(map[uuid.UUID]model.Product),
		pictures: make(map[uuid.UUID][]model.Picture),
	}
}

// seed stores one product built through the domain constructor. The creation
// time is the first tie-break key, exactly as the adapter orders by it.
func (m *memoryProducts) seed(t *testing.T, draft model.ProductDraft, createdAt time.Time) *model.Product {
	t.Helper()
	product, err := model.NewProduct(draft, createdAt)
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.products[product.ID] = *product
	return product
}

// move runs one sell-state transition over a seeded product.
func (m *memoryProducts) move(t *testing.T, id uuid.UUID, transition func(*model.Product, time.Time) error) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	product, ok := m.products[id]
	if !ok {
		t.Fatalf("product %s is not seeded", id)
	}
	if err := transition(&product, time.Now().UTC()); err != nil {
		t.Fatalf("transition %s: %v", id, err)
	}
	m.products[id] = product
}

// ListVisible mirrors the adapter's public list, predicate included, so the
// use case cannot pass by relying on the fake being permissive (FR-002, FR-006).
func (m *memoryProducts) ListVisible(_ context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.ProductListItem, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastQuery = query
	m.listCalls++

	visible := make(map[uuid.UUID]bool, len(query.VisibleCategoryIDs))
	for _, id := range query.VisibleCategoryIDs {
		visible[id] = true
	}

	all := make([]domainrepo.ProductListItem, 0, len(m.products))
	for _, product := range m.products {
		if !visible[product.CategoryID] {
			continue
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			continue
		}
		if query.CategoryID != nil && product.CategoryID != *query.CategoryID {
			continue
		}
		item := domainrepo.ProductListItem{
			Product:      product,
			PictureCount: len(m.pictures[product.ID]),
		}
		if primary, ok := model.PrimaryPicture(m.pictures[product.ID]); ok {
			item.ImageURL = primary.URL
		}
		all = append(all, item)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Product.Position != all[j].Product.Position {
			return all[i].Product.Position < all[j].Product.Position
		}
		if !all[i].Product.CreatedAt.Equal(all[j].Product.CreatedAt) {
			return all[i].Product.CreatedAt.Before(all[j].Product.CreatedAt)
		}
		return all[i].Product.ID.String() < all[j].Product.ID.String()
	})

	total := int64(len(all))
	start := (query.Page - 1) * query.PageSize
	if query.Page < 1 || start >= len(all) {
		return []domainrepo.ProductListItem{}, total, nil
	}
	end := start + query.PageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

// FindVisibleBySlug mirrors the adapter's public detail read, predicate
// included, so a hidden product is indistinguishable from an unknown one
// (FR-003).
func (m *memoryProducts) FindVisibleBySlug(_ context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*domainrepo.ProductView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detailSlug = slug

	visible := make(map[uuid.UUID]bool, len(visibleCategoryIDs))
	for _, id := range visibleCategoryIDs {
		visible[id] = true
	}
	for _, product := range m.products {
		if product.Slug != slug {
			continue
		}
		if !visible[product.CategoryID] {
			return nil, domainerr.ErrProductNotFound
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			return nil, domainerr.ErrProductNotFound
		}
		return &domainrepo.ProductView{Product: product, Pictures: m.pictures[product.ID]}, nil
	}
	return nil, domainerr.ErrProductNotFound
}

// fakeVisibility is the cross-module category query the use case consumes. It
// records how often the visible set was asked for, so a test can prove it is
// fetched once per request rather than per product (research D1).
type fakeVisibility struct {
	visible      []uuid.UUID
	slugs        map[string]uuid.UUID
	visibleCalls int
	slugCalls    int
}

func (f *fakeVisibility) VisibleCategoryIDs(context.Context) ([]uuid.UUID, error) {
	f.visibleCalls++
	return f.visible, nil
}

func (f *fakeVisibility) CategoryIDBySlug(_ context.Context, slug string) (uuid.UUID, bool, error) {
	f.slugCalls++
	id, ok := f.slugs[slug]
	return id, ok, nil
}

func publicService(repo *memoryProducts, visibility *fakeVisibility) *Service {
	return New(Service{Products: repo, Visibility: visibility, Mapper: mapper.New()})
}

func price(amount int64) model.Price { return model.Price{Amount: amount, Currency: "VND"} }

// FR-002: only products that are on sale or a pre-order, in a visible category,
// are returned.
func TestListPublicReturnsOnlyVisibleProducts(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	visibleCat, hiddenCat := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	onSale := repo.seed(t, model.ProductDraft{Name: "On sale", Slug: "on-sale", Price: price(100), CategoryID: visibleCat, Position: 1}, base)
	repo.move(t, onSale.ID, (*model.Product).Launch)
	repo.seed(t, model.ProductDraft{Name: "Announced", Slug: "announced", Price: price(200), CategoryID: visibleCat, Position: 2}, base.Add(time.Minute))
	repo.seed(t, model.ProductDraft{Name: "Pre-order", Slug: "pre-order", Price: price(300), CategoryID: visibleCat, Position: 3, IsPreorder: true}, base.Add(2*time.Minute))
	hidden := repo.seed(t, model.ProductDraft{Name: "Hidden", Slug: "hidden", Price: price(400), CategoryID: hiddenCat, Position: 0}, base.Add(3*time.Minute))
	repo.move(t, hidden.ID, (*model.Product).Launch)

	visibility := &fakeVisibility{visible: []uuid.UUID{visibleCat}}
	page, err := publicService(repo, visibility).ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected a total of 2 visible products, got %d", page.Total)
	}
	if len(page.Products) != 2 {
		t.Fatalf("expected 2 visible products, got %d", len(page.Products))
	}
	for _, entry := range page.Products {
		if entry.Slug == "announced" || entry.Slug == "hidden" {
			t.Fatalf("a product that is not visible leaked into the list: %+v", page.Products)
		}
	}
}

// research D1: the visible set reaches the repository, so the filter is applied
// in the query rather than in Go, and it is asked for exactly once per request.
func TestListPublicPassesTheVisibleSetToTheRepository(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	catA := uuid.New()

	repo := newMemoryProducts()
	product := repo.seed(t, model.ProductDraft{Name: "A", Slug: "a", Price: price(100), CategoryID: catA, Position: 1}, base)
	repo.move(t, product.ID, (*model.Product).Launch)

	visibility := &fakeVisibility{visible: []uuid.UUID{catA}}
	if _, err := publicService(repo, visibility).ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("ListPublic: %v", err)
	}

	if visibility.visibleCalls != 1 {
		t.Fatalf("the visible set must be fetched exactly once per request, got %d", visibility.visibleCalls)
	}
	if len(repo.lastQuery.VisibleCategoryIDs) != 1 || repo.lastQuery.VisibleCategoryIDs[0] != catA {
		t.Fatalf("the visible set did not reach the repository: %+v", repo.lastQuery.VisibleCategoryIDs)
	}
	if repo.lastQuery.CategoryID != nil {
		t.Fatalf("an unfiltered request must carry no category filter, got %s", *repo.lastQuery.CategoryID)
	}
}

// FR-003: a product in a hidden category is excluded from the list and answers
// not-found on its own route, the same answer an unknown slug gives.
func TestAProductInAHiddenCategoryIsExcludedAndAnswersNotFound(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	visibleCat, hiddenCat := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	visible := repo.seed(t, model.ProductDraft{Name: "Visible", Slug: "visible", Price: price(100), CategoryID: visibleCat, Position: 1}, base)
	repo.move(t, visible.ID, (*model.Product).Launch)
	hidden := repo.seed(t, model.ProductDraft{Name: "Hidden", Slug: "hidden", Price: price(200), CategoryID: hiddenCat, Position: 0}, base.Add(time.Minute))
	repo.move(t, hidden.ID, (*model.Product).Launch)

	svc := publicService(repo, &fakeVisibility{visible: []uuid.UUID{visibleCat}})
	ctx := context.Background()

	page, err := svc.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Total != 1 || len(page.Products) != 1 || page.Products[0].Slug != "visible" {
		t.Fatalf("a product in a hidden category leaked into the list: %+v", page.Products)
	}

	if _, err := svc.GetPublicBySlug(ctx, dto.PublicProductRefInput{Slug: "hidden"}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("a product in a hidden category must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicBySlug(ctx, dto.PublicProductRefInput{Slug: "never-existed"}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("an unknown slug must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicBySlug(ctx, dto.PublicProductRefInput{Slug: "visible"}); err != nil {
		t.Fatalf("a visible slug must resolve, got %v", err)
	}
}

// FR-007: an empty catalogue and a fully hidden one both answer an empty result
// rather than an error.
func TestListPublicAnswersEmptyForAnEmptyOrFullyHiddenCatalogue(t *testing.T) {
	ctx := context.Background()

	empty := publicService(newMemoryProducts(), &fakeVisibility{visible: nil})
	page, err := empty.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("an empty catalogue must not be an error, got %v", err)
	}
	if len(page.Products) != 0 || page.Total != 0 {
		t.Fatalf("expected an empty page, got %d products and a total of %d", len(page.Products), page.Total)
	}

	base := time.Now().UTC().Add(-time.Hour)
	repo := newMemoryProducts()
	product := repo.seed(t, model.ProductDraft{Name: "Hidden", Slug: "hidden", Price: price(100), CategoryID: uuid.New(), Position: 1}, base)
	repo.move(t, product.ID, (*model.Product).Launch)

	hidden := publicService(repo, &fakeVisibility{visible: nil})
	page, err = hidden.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("a fully hidden catalogue must not be an error, got %v", err)
	}
	if len(page.Products) != 0 || page.Total != 0 {
		t.Fatalf("expected an empty page, got %d products and a total of %d", len(page.Products), page.Total)
	}
}

// FR-009, SC-009: the order is the operator's, with position then creation time
// then identifier as the tie-break, and two identical calls return it unchanged.
func TestListPublicOrderIsTheConfiguredOneAndStableAcrossTwoCalls(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	category := uuid.New()

	repo := newMemoryProducts()
	// Seeded out of order on purpose, with a tie on position 5 broken by time.
	gamma := repo.seed(t, model.ProductDraft{Name: "Gamma", Slug: "gamma", Price: price(100), CategoryID: category, Position: 5}, base.Add(2*time.Minute))
	alpha := repo.seed(t, model.ProductDraft{Name: "Alpha", Slug: "alpha", Price: price(100), CategoryID: category, Position: 5}, base)
	delta := repo.seed(t, model.ProductDraft{Name: "Delta", Slug: "delta", Price: price(100), CategoryID: category, Position: 1}, base.Add(time.Minute))
	for _, product := range []*model.Product{gamma, alpha, delta} {
		repo.move(t, product.ID, (*model.Product).Launch)
	}

	svc := publicService(repo, &fakeVisibility{visible: []uuid.UUID{category}})
	first, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	want := []string{"delta", "alpha", "gamma"}
	if got := slugs(first.Products); !equalStrings(got, want) {
		t.Fatalf("expected the configured order %v, got %v", want, got)
	}

	second, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic repeat: %v", err)
	}
	if got := slugs(second.Products); !equalStrings(got, slugs(first.Products)) {
		t.Fatalf("the order changed between two identical calls: %v then %v", slugs(first.Products), got)
	}
}

// FR-006: the `category` filter resolves a slug through the contract; a known
// visible slug narrows the page, while an unknown slug answers an empty page
// without reaching the repository.
func TestListPublicResolvesTheCategorySlugFilter(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	catA, catB := uuid.New(), uuid.New()

	repo := newMemoryProducts()
	inA := repo.seed(t, model.ProductDraft{Name: "A", Slug: "a", Price: price(100), CategoryID: catA, Position: 1}, base)
	inB := repo.seed(t, model.ProductDraft{Name: "B", Slug: "b", Price: price(100), CategoryID: catB, Position: 2}, base.Add(time.Minute))
	for _, product := range []*model.Product{inA, inB} {
		repo.move(t, product.ID, (*model.Product).Launch)
	}

	visibility := &fakeVisibility{
		visible: []uuid.UUID{catA, catB},
		slugs:   map[string]uuid.UUID{"cat-a": catA, "cat-b": catB},
	}
	svc := publicService(repo, visibility)
	ctx := context.Background()

	page, err := svc.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20, CategorySlug: "cat-a"})
	if err != nil {
		t.Fatalf("filtered ListPublic: %v", err)
	}
	if page.Total != 1 || len(page.Products) != 1 || page.Products[0].Slug != "a" {
		t.Fatalf("expected only category A's product, got %+v", page.Products)
	}

	callsBefore := repo.listCalls
	unknown, err := svc.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20, CategorySlug: "never-existed"})
	if err != nil {
		t.Fatalf("an unknown filter slug must not be an error, got %v", err)
	}
	if len(unknown.Products) != 0 || unknown.Total != 0 {
		t.Fatalf("an unknown filter slug must answer an empty page, got %+v", unknown.Products)
	}
	if repo.listCalls != callsBefore {
		t.Fatalf("an unknown filter slug must answer the empty page without a storage read")
	}
}

func slugs(list []dto.PublicProductOutput) []string {
	out := make([]string, 0, len(list))
	for _, product := range list {
		out = append(out, product.Slug)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
