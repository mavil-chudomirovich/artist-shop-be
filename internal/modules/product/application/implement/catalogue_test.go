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
	// catalogueGroups holds the variant-group rows the merged read should
	// include. The fake does not own groups, so a test seeds the group row the
	// real adapter's UNION ALL would derive from the group and its visible
	// options (FR-001, research D4).
	catalogueGroups []domainrepo.CatalogueItem
	// lastCatalogueQuery records the query the merged read received, so a test
	// can prove the visible set reached the merged read.
	lastCatalogueQuery domainrepo.VisibleListQuery
	// catalogueCalls counts how many times the merged read was reached.
	catalogueCalls int
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

// ListCatalogue mirrors the adapter's merged public list: the seeded group rows
// plus the standalone products (a product with variant_group_id null), under the
// same visibility predicate, in the FR-009 order. A product that belongs to a
// group is excluded, because the group represents it (FR-001, SC-002).
func (m *memoryProducts) ListCatalogue(_ context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.CatalogueItem, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCatalogueQuery = query
	m.catalogueCalls++

	visible := make(map[uuid.UUID]bool, len(query.VisibleCategoryIDs))
	for _, id := range query.VisibleCategoryIDs {
		visible[id] = true
	}

	all := make([]domainrepo.CatalogueItem, 0, len(m.catalogueGroups)+len(m.products))
	for _, item := range m.catalogueGroups {
		if !visible[item.CategoryID] {
			continue
		}
		if query.CategoryID != nil && item.CategoryID != *query.CategoryID {
			continue
		}
		// Mirror the adapter's EXISTS(visible option) half of the group
		// predicate: a group row is emitted only while at least one of its
		// options is itself visible (FR-006). A group whose every option is
		// hidden is absent from the list, the same way it answers not-found on
		// its own route, so the fake cannot be more permissive than the SQL.
		if !m.hasVisibleOption(item.ID, visible) {
			continue
		}
		all = append(all, item)
	}
	for _, product := range m.products {
		if product.VariantGroupID != nil {
			continue
		}
		if !visible[product.CategoryID] {
			continue
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			continue
		}
		if query.CategoryID != nil && product.CategoryID != *query.CategoryID {
			continue
		}
		item := domainrepo.CatalogueItem{
			Type:        domainrepo.CatalogueEntryProduct,
			ID:          product.ID,
			Name:        product.Name,
			Slug:        product.Slug,
			Description: product.Description,
			Price:       product.Price,
			IsPreorder:  product.IsPreorder,
			CategoryID:  product.CategoryID,
			Position:    product.Position,
			CreatedAt:   product.CreatedAt,
		}
		if primary, ok := model.PrimaryPicture(m.pictures[product.ID]); ok {
			item.ImageURL = primary.URL
		}
		all = append(all, item)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Position != all[j].Position {
			return all[i].Position < all[j].Position
		}
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].ID.String() < all[j].ID.String()
	})

	total := int64(len(all))
	start := (query.Page - 1) * query.PageSize
	if query.Page < 1 || start >= len(all) {
		return []domainrepo.CatalogueItem{}, total, nil
	}
	end := start + query.PageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

// hasVisibleOption mirrors the adapter's EXISTS(visible option) predicate: it
// reports whether the group named by groupID has at least one option a customer
// may see. The option's own category must be visible and it must be on sale or
// an announced pre-order, exactly the rule the merged read's standalone loop
// applies to a product (FR-006, research D4). It is what keeps the merged list
// from showing a group whose every option is hidden.
func (m *memoryProducts) hasVisibleOption(groupID uuid.UUID, visible map[uuid.UUID]bool) bool {
	for _, product := range m.products {
		if product.VariantGroupID == nil || *product.VariantGroupID != groupID {
			continue
		}
		if !visible[product.CategoryID] {
			continue
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			continue
		}
		return true
	}
	return false
}

// ListOptionsByGroupIDs mirrors the adapter's bulk option read for the detail
// selector: the visible options of the requested groups, keyed by group, under
// the option visibility predicate (FR-007, FR-027).
func (m *memoryProducts) ListOptionsByGroupIDs(_ context.Context, groupIDs []uuid.UUID, visibleCategoryIDs []uuid.UUID) (map[uuid.UUID][]domainrepo.GroupOptionItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[uuid.UUID][]domainrepo.GroupOptionItem)
	if len(groupIDs) == 0 || len(visibleCategoryIDs) == 0 {
		return out, nil
	}
	wanted := make(map[uuid.UUID]bool, len(groupIDs))
	for _, id := range groupIDs {
		wanted[id] = true
	}
	visible := make(map[uuid.UUID]bool, len(visibleCategoryIDs))
	for _, id := range visibleCategoryIDs {
		visible[id] = true
	}
	for _, product := range m.products {
		if product.VariantGroupID == nil || !wanted[*product.VariantGroupID] {
			continue
		}
		if !visible[product.CategoryID] {
			continue
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			continue
		}
		if product.VariantLabel == nil {
			continue
		}
		item := domainrepo.GroupOptionItem{
			ID:         product.ID,
			Name:       product.Name,
			Slug:       product.Slug,
			Label:      *product.VariantLabel,
			Price:      product.Price,
			Position:   product.Position,
			SellState:  product.SellState,
			IsPreorder: product.IsPreorder,
		}
		if primary, ok := model.PrimaryPicture(m.pictures[product.ID]); ok {
			item.ImageURL = primary.URL
		}
		groupID := *product.VariantGroupID
		out[groupID] = append(out[groupID], item)
	}
	for groupID := range out {
		options := out[groupID]
		sort.Slice(options, func(i, j int) bool {
			if options[i].Position != options[j].Position {
				return options[i].Position < options[j].Position
			}
			return options[i].ID.String() < options[j].ID.String()
		})
	}
	return out, nil
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

// catalogueService wires the merged read with the variant-group repository the
// public group detail and the product selector reach.
func catalogueService(repo *memoryProducts, groups *memoryVariantGroups, visibility *fakeVisibility) *Service {
	return New(Service{Products: repo, Groups: groups, Visibility: visibility, Mapper: mapper.New()})
}

func price(amount int64) model.Price { return model.Price{Amount: amount, Currency: "VND"} }

// strptr returns a pointer to its argument, so a one-line seed can attach a
// label or a group identifier without a named local.
func strptr(value string) *string { return &value }

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
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 visible products, got %d", len(page.Entries))
	}
	for _, entry := range page.Entries {
		if entry.Slug == "announced" || entry.Slug == "hidden" {
			t.Fatalf("a product that is not visible leaked into the list: %+v", page.Entries)
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
	if len(repo.lastCatalogueQuery.VisibleCategoryIDs) != 1 || repo.lastCatalogueQuery.VisibleCategoryIDs[0] != catA {
		t.Fatalf("the visible set did not reach the repository: %+v", repo.lastCatalogueQuery.VisibleCategoryIDs)
	}
	if repo.lastCatalogueQuery.CategoryID != nil {
		t.Fatalf("an unfiltered request must carry no category filter, got %s", *repo.lastCatalogueQuery.CategoryID)
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
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].Slug != "visible" {
		t.Fatalf("a product in a hidden category leaked into the list: %+v", page.Entries)
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
	if len(page.Entries) != 0 || page.Total != 0 {
		t.Fatalf("expected an empty page, got %d products and a total of %d", len(page.Entries), page.Total)
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
	if len(page.Entries) != 0 || page.Total != 0 {
		t.Fatalf("expected an empty page, got %d products and a total of %d", len(page.Entries), page.Total)
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
	if got := slugs(first.Entries); !equalStrings(got, want) {
		t.Fatalf("expected the configured order %v, got %v", want, got)
	}

	second, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic repeat: %v", err)
	}
	if got := slugs(second.Entries); !equalStrings(got, slugs(first.Entries)) {
		t.Fatalf("the order changed between two identical calls: %v then %v", slugs(first.Entries), got)
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
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].Slug != "a" {
		t.Fatalf("expected only category A's product, got %+v", page.Entries)
	}

	callsBefore := repo.catalogueCalls
	unknown, err := svc.ListPublic(ctx, dto.ListPublicInput{Page: 1, PageSize: 20, CategorySlug: "never-existed"})
	if err != nil {
		t.Fatalf("an unknown filter slug must not be an error, got %v", err)
	}
	if len(unknown.Entries) != 0 || unknown.Total != 0 {
		t.Fatalf("an unknown filter slug must answer an empty page, got %+v", unknown.Entries)
	}
	if repo.catalogueCalls != callsBefore {
		t.Fatalf("an unknown filter slug must answer the empty page without a storage read")
	}
}

func slugs(list []dto.PublicCatalogueEntry) []string {
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

// --- User Story 1: the merged list, the group detail and the selector ---

// FR-001, FR-027, SC-002: the merged public list carries a variant group once,
// discriminated by type "variant-group"; the group's options do not appear as
// separate entries, and a standalone product appears as type "product".
func TestListCatalogueShowsAGroupOnceAndAStandaloneProduct(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	groupID := uuid.New()
	flins := repo.seed(t, model.ProductDraft{
		Name: "Card Flins", Slug: "card-flins", Price: price(120000),
		CategoryID: category, Position: 2, VariantGroupID: &groupID, VariantLabel: strptr("Flins"),
	}, base.Add(time.Minute))
	dottore := repo.seed(t, model.ProductDraft{
		Name: "Card Dottore", Slug: "card-dottore", Price: price(150000),
		CategoryID: category, Position: 3, VariantGroupID: &groupID, VariantLabel: strptr("Dottore"),
	}, base.Add(2*time.Minute))
	standalone := repo.seed(t, model.ProductDraft{
		Name: "Standalone", Slug: "standalone", Price: price(90000),
		CategoryID: category, Position: 4,
	}, base.Add(3*time.Minute))
	for _, product := range []*model.Product{flins, dottore, standalone} {
		repo.move(t, product.ID, (*model.Product).Launch)
	}
	// The group row the real adapter derives from the group and its visible
	// options: the group's own primary picture and the lowest visible option
	// price (FR-005).
	repo.catalogueGroups = []domainrepo.CatalogueItem{{
		Type: domainrepo.CatalogueEntryVariantGroup, ID: groupID, Name: "Card Genshin",
		Slug: "card-genshin", Price: price(120000), CategoryID: category, Position: 1, CreatedAt: base,
	}}

	svc := catalogueService(repo, newMemoryVariantGroups(), &fakeVisibility{visible: []uuid.UUID{category}})
	page, err := svc.ListCatalogue(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if page.Total != 2 || len(page.Entries) != 2 {
		t.Fatalf("expected the group and the standalone product, got %d entries and a total of %d", len(page.Entries), page.Total)
	}
	types := make(map[string]string, len(page.Entries))
	for _, entry := range page.Entries {
		types[entry.Slug] = entry.Type
	}
	if types["card-genshin"] != dto.CatalogueEntryTypeVariantGroup {
		t.Fatalf("the group must appear once as %q, got %q", dto.CatalogueEntryTypeVariantGroup, types["card-genshin"])
	}
	if types["standalone"] != dto.CatalogueEntryTypeProduct {
		t.Fatalf("the standalone product must appear as %q, got %q", dto.CatalogueEntryTypeProduct, types["standalone"])
	}
	for _, leaked := range []string{"card-flins", "card-dottore"} {
		if _, ok := types[leaked]; ok {
			t.Fatalf("an option of the group must not appear as a separate entry, got %q", leaked)
		}
	}
	for _, entry := range page.Entries {
		if entry.Slug == "card-genshin" && entry.Price.Amount != 120000 {
			t.Fatalf("the group's price must be the lowest visible option's, got %d", entry.Price.Amount)
		}
	}
}

// FR-002, FR-005, FR-008: the public group detail carries the group's shared
// content and its own gallery, with its visible options in the configured order;
// a withdrawn option is absent.
func TestGetPublicGroupBySlugReturnsSharedContentAndOrderedVisibleOptions(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	groups := newMemoryVariantGroups()
	group := model.VariantGroup{
		ID: uuid.New(), Name: "Card Genshin", Slug: "card-genshin", Description: "Bộ card",
		CategoryID: category, Position: 1, OptionName: "Card", CreatedAt: base, UpdatedAt: base,
	}
	groups.seedGroup(group)
	groups.seedImage(model.VariantGroupImage{
		ID: uuid.New(), GroupID: group.ID, PublicID: "demo/g", URL: "https://cdn.example.test/g.jpg",
		Width: 10, Height: 10, IsPrimary: true,
	})
	groups.seedOptions(group.ID,
		domainrepo.GroupOptionItem{ID: uuid.New(), Slug: "flins", Label: "Flins", Price: price(120000), SellState: constant.SellStateActive, Position: 1},
		domainrepo.GroupOptionItem{ID: uuid.New(), Slug: "dottore", Label: "Dottore", Price: price(150000), SellState: constant.SellStateDiscontinued, Position: 2},
		domainrepo.GroupOptionItem{ID: uuid.New(), Slug: "furina", Label: "Furina", Price: price(130000), SellState: constant.SellStateActive, Position: 3},
	)

	svc := catalogueService(newMemoryProducts(), groups, &fakeVisibility{visible: []uuid.UUID{category}})
	out, err := svc.GetPublicGroupBySlug(context.Background(), dto.PublicVariantGroupRefInput{Slug: "card-genshin"})
	if err != nil {
		t.Fatalf("GetPublicGroupBySlug: %v", err)
	}
	if out.Name != "Card Genshin" || out.Description != "Bộ card" || out.OptionName != "Card" {
		t.Fatalf("the group detail must carry the shared content, got %+v", out)
	}
	if len(out.Images) != 1 {
		t.Fatalf("the group detail must carry its own gallery, got %d", len(out.Images))
	}
	var labels []string
	for _, option := range out.Options {
		labels = append(labels, option.Label)
	}
	if !equalStrings(labels, []string{"Flins", "Furina"}) {
		t.Fatalf("expected the visible options in position order [Flins Furina], got %v", labels)
	}
}

// FR-007: a public product detail of an option carries the variantGroup selector
// with the group's shared content and its visible siblings; a standalone product
// carries none.
func TestGetPublicBySlugOfAnOptionCarriesTheVariantGroupSelector(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	groups := newMemoryVariantGroups()
	group := model.VariantGroup{
		ID: uuid.New(), Name: "Card Genshin", Slug: "card-genshin", Description: "Bộ card",
		CategoryID: category, Position: 1, OptionName: "Card", CreatedAt: base, UpdatedAt: base,
	}
	groups.seedGroup(group)
	groups.seedImage(model.VariantGroupImage{
		ID: uuid.New(), GroupID: group.ID, PublicID: "demo/g", URL: "https://cdn.example.test/g.jpg",
		Width: 10, Height: 10, IsPrimary: true,
	})

	repo := newMemoryProducts()
	flins := repo.seed(t, model.ProductDraft{
		Name: "Card Flins", Slug: "card-flins", Price: price(120000),
		CategoryID: category, Position: 1, VariantGroupID: &group.ID, VariantLabel: strptr("Flins"),
	}, base.Add(time.Minute))
	furina := repo.seed(t, model.ProductDraft{
		Name: "Card Furina", Slug: "card-furina", Price: price(130000),
		CategoryID: category, Position: 2, VariantGroupID: &group.ID, VariantLabel: strptr("Furina"),
	}, base.Add(2*time.Minute))
	for _, product := range []*model.Product{flins, furina} {
		repo.move(t, product.ID, (*model.Product).Launch)
	}

	svc := catalogueService(repo, groups, &fakeVisibility{visible: []uuid.UUID{category}})
	out, err := svc.GetPublicBySlug(context.Background(), dto.PublicProductRefInput{Slug: "card-flins"})
	if err != nil {
		t.Fatalf("GetPublicBySlug: %v", err)
	}
	if out.VariantGroup == nil {
		t.Fatal("a product that is an option must carry the variantGroup selector")
	}
	if out.VariantGroup.ID != group.ID || out.VariantGroup.Name != "Card Genshin" || out.VariantGroup.OptionName != "Card" {
		t.Fatalf("the selector must carry the group's shared content, got %+v", out.VariantGroup)
	}
	var labels []string
	for _, option := range out.VariantGroup.Options {
		labels = append(labels, option.Label)
	}
	if !equalStrings(labels, []string{"Flins", "Furina"}) {
		t.Fatalf("the selector must carry the visible siblings in order, got %v", labels)
	}

	standalone := repo.seed(t, model.ProductDraft{
		Name: "Standalone", Slug: "standalone", Price: price(90000), CategoryID: category, Position: 3,
	}, base.Add(3*time.Minute))
	repo.move(t, standalone.ID, (*model.Product).Launch)
	plain, err := svc.GetPublicBySlug(context.Background(), dto.PublicProductRefInput{Slug: "standalone"})
	if err != nil {
		t.Fatalf("GetPublicBySlug standalone: %v", err)
	}
	if plain.VariantGroup != nil {
		t.Fatalf("a standalone product must not carry a selector, got %+v", plain.VariantGroup)
	}
}

// FR-006, FR-008: a group with no visible option answers the same not-found an
// unknown slug answers, so a hidden group is never confirmed.
func TestAGroupWithNoVisibleOptionAnswersNotFoundLikeAnUnknownSlug(t *testing.T) {
	category := uuid.New()
	groups := newMemoryVariantGroups()
	group := model.VariantGroup{
		ID: uuid.New(), Name: "Hidden group", Slug: "hidden-group",
		CategoryID: category, OptionName: "Card",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	groups.seedGroup(group)
	groups.seedOptions(group.ID, domainrepo.GroupOptionItem{
		ID: uuid.New(), Slug: "off", Label: "Off", Price: price(100),
		SellState: constant.SellStateDiscontinued, Position: 1,
	})

	svc := catalogueService(newMemoryProducts(), groups, &fakeVisibility{visible: []uuid.UUID{category}})
	ctx := context.Background()
	if _, err := svc.GetPublicGroupBySlug(ctx, dto.PublicVariantGroupRefInput{Slug: "hidden-group"}); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("a group with no visible option must answer not-found, got %v", err)
	}
	if _, err := svc.GetPublicGroupBySlug(ctx, dto.PublicVariantGroupRefInput{Slug: "never-existed"}); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("an unknown slug must answer not-found, got %v", err)
	}
}

// FR-006, FR-008, SC-002: a group whose every option is hidden is absent from the
// merged list and is not counted, so an empty group never lingers in the
// customer-facing catalogue. The not-found half of this story is already proven by
// TestAGroupWithNoVisibleOptionAnswersNotFoundLikeAnUnknownSlug; this is the list
// half, where the products fake mirrors the adapter's EXISTS-visible-option
// predicate and must therefore drop the group it was handed.
func TestAGroupWithNoVisibleOptionIsAbsentFromTheCatalogueList(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	category := uuid.New()

	repo := newMemoryProducts()
	groupID := uuid.New()
	// The group's only option is retired: hidden from customers on its own, so it
	// cannot keep the group on the list (FR-006).
	hidden := repo.seed(t, model.ProductDraft{
		Name: "Card Hidden", Slug: "card-hidden", Price: price(120000),
		CategoryID: category, Position: 1, VariantGroupID: &groupID, VariantLabel: strptr("Hidden"),
	}, base)
	repo.move(t, hidden.ID, (*model.Product).Retire)

	// The group row the adapter's UNION ALL would derive were the group visible;
	// seeded directly to prove the list drops it because it has no visible option.
	repo.catalogueGroups = []domainrepo.CatalogueItem{{
		Type: domainrepo.CatalogueEntryVariantGroup, ID: groupID, Name: "Card Genshin",
		Slug: "card-genshin", CategoryID: category, Position: 0, CreatedAt: base,
	}}

	svc := catalogueService(repo, newMemoryVariantGroups(), &fakeVisibility{visible: []uuid.UUID{category}})
	page, err := svc.ListCatalogue(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if len(page.Entries) != 0 || page.Total != 0 {
		t.Fatalf("a group with no visible option must be absent and uncounted, got %d entries and a total of %d",
			len(page.Entries), page.Total)
	}
}
