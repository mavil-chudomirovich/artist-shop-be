package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// var _ documents the transport's dependency explicitly: the real use-case
// service must satisfy the single declared use-case surface the routes consume.
var _ appinterface.ProductService = (*productimplement.Service)(nil)

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// stubRepository is an in-memory ProductRepository. It embeds the interface so
// only the two public reads are implemented; it applies the same visibility
// predicate the adapter applies, so the HTTP tests exercise the real use case
// and the handler rather than a permissive fake.
type stubRepository struct {
	domainrepo.ProductRepository

	mu       sync.Mutex
	products map[uuid.UUID]model.Product
	pictures map[uuid.UUID][]model.Picture

	lastQuery  domainrepo.VisibleListQuery
	listCalls  int
	detailSlug string
}

func newStubRepository() *stubRepository {
	return &stubRepository{
		products: make(map[uuid.UUID]model.Product),
		pictures: make(map[uuid.UUID][]model.Picture),
	}
}

func (r *stubRepository) seed(t *testing.T, draft model.ProductDraft, createdAt time.Time) *model.Product {
	t.Helper()
	product, err := model.NewProduct(draft, createdAt)
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Name, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.products[product.ID] = *product
	return product
}

func (r *stubRepository) move(t *testing.T, id uuid.UUID, transition func(*model.Product, time.Time) error) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	product, ok := r.products[id]
	if !ok {
		t.Fatalf("product %s is not seeded", id)
	}
	if err := transition(&product, time.Now().UTC()); err != nil {
		t.Fatalf("transition %s: %v", id, err)
	}
	r.products[id] = product
}

func (r *stubRepository) addPicture(picture model.Picture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pictures[picture.ProductID] = append(r.pictures[picture.ProductID], picture)
}

func (r *stubRepository) lastQueryValue() domainrepo.VisibleListQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastQuery
}

func (r *stubRepository) ListVisible(_ context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.ProductListItem, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastQuery = query
	r.listCalls++

	visible := make(map[uuid.UUID]bool, len(query.VisibleCategoryIDs))
	for _, id := range query.VisibleCategoryIDs {
		visible[id] = true
	}

	all := make([]domainrepo.ProductListItem, 0, len(r.products))
	for _, product := range r.products {
		if !visible[product.CategoryID] {
			continue
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			continue
		}
		if query.CategoryID != nil && product.CategoryID != *query.CategoryID {
			continue
		}
		item := domainrepo.ProductListItem{Product: product, PictureCount: len(r.pictures[product.ID])}
		if primary, ok := model.PrimaryPicture(r.pictures[product.ID]); ok {
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

func (r *stubRepository) FindVisibleBySlug(_ context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*domainrepo.ProductView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detailSlug = slug

	visible := make(map[uuid.UUID]bool, len(visibleCategoryIDs))
	for _, id := range visibleCategoryIDs {
		visible[id] = true
	}
	for _, product := range r.products {
		if product.Slug != slug {
			continue
		}
		if !visible[product.CategoryID] {
			return nil, domainerr.ErrProductNotFound
		}
		if product.SellState != constant.SellStateActive && !product.IsPreorder {
			return nil, domainerr.ErrProductNotFound
		}
		return &domainrepo.ProductView{Product: product, Pictures: model.OrderPictures(r.pictures[product.ID])}, nil
	}
	return nil, domainerr.ErrProductNotFound
}

// stubVisibility is the cross-module category query the use case consumes.
type stubVisibility struct {
	visible      []uuid.UUID
	slugs        map[string]uuid.UUID
	visibleCalls int
}

func (v *stubVisibility) VisibleCategoryIDs(context.Context) ([]uuid.UUID, error) {
	v.visibleCalls++
	return v.visible, nil
}

func (v *stubVisibility) CategoryIDBySlug(_ context.Context, slug string) (uuid.UUID, bool, error) {
	id, ok := v.slugs[slug]
	return id, ok, nil
}

// newPublicFixture builds the real public use cases over the in-memory reads and
// mounts the public group exactly the way the composition root mounts it.
func newPublicFixture(t *testing.T) (http.Handler, *stubRepository, *stubVisibility) {
	t.Helper()
	repo := newStubRepository()
	visibility := &stubVisibility{slugs: map[string]uuid.UUID{}}
	service := productimplement.New(productimplement.Service{
		Products:   repo,
		Visibility: visibility,
		Mapper:     mapper.New(),
	})
	handler := New(service, appinterface.Config{}, testLogger)

	root := chi.NewRouter()
	root.Mount("/api/v1/products", handler.Router(middleware.AuthHooks{}))
	return root, repo, visibility
}

func perform(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// seedVisibleProduct seeds one ACTIVE product in a visible category and returns
// the category identifier.
func seedVisibleProduct(t *testing.T, repo *stubRepository, visibility *stubVisibility, slug string, position int) (*model.Product, uuid.UUID) {
	t.Helper()
	category := uuid.New()
	product := repo.seed(t, model.ProductDraft{
		Name:        "Sản phẩm " + slug,
		Slug:        slug,
		Description: "Mô tả " + slug,
		Price:       model.Price{Amount: 999999999999, Currency: "VND"},
		CategoryID:  category,
		Position:    position,
	}, time.Now().UTC().Add(-time.Hour))
	repo.move(t, product.ID, (*model.Product).Launch)
	visibility.visible = append(visibility.visible, category)
	return product, category
}

type rawListBody struct {
	Data []map[string]json.RawMessage `json:"data"`
	Meta struct {
		RequestID string `json:"requestId"`
		Timestamp string `json:"timestamp"`
		Page      int    `json:"page"`
		PageSize  int    `json:"pageSize"`
		Total     int64  `json:"total"`
	} `json:"meta"`
}

func decodeRawList(t *testing.T, rec *httptest.ResponseRecorder) rawListBody {
	t.Helper()
	var body rawListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-001: the public list is reachable with no token at all.
func TestPublicProductListIsReachableWithoutAToken(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)

	rec := perform(handler, http.MethodGet, "/api/v1/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a request with no token, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeRawList(t, rec)
	if body.Meta.Page != 1 || body.Meta.PageSize != 20 || body.Meta.Total != 1 {
		t.Fatalf("expected the paged metadata block, got %+v", body.Meta)
	}
}

// FR-004, FR-008, SC-002, quickstart scenario 4: each public entry carries
// exactly id, name, slug, price, imageUrl and isPreorder — no sellState, no
// position, no categoryId, no isSet and no folding key — and the price
// round-trips as the exact integer sent (FR-030).
func TestAPublicProductEntryCarriesExactlyThePublicMembers(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	product, _ := seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)
	repo.addPicture(model.Picture{
		ID: uuid.New(), ProductID: product.ID, PublicID: "p1",
		URL: "https://cdn.example.test/stand.jpg", Width: 800, Height: 600,
		Position: 0, IsPrimary: true,
	})

	rec := perform(handler, http.MethodGet, "/api/v1/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	body := decodeRawList(t, rec)
	if len(body.Data) != 1 {
		t.Fatalf("expected one entry, got %d", len(body.Data))
	}
	want := map[string]bool{"id": true, "name": true, "slug": true, "price": true, "imageUrl": true, "isPreorder": true}
	entry := body.Data[0]
	if len(entry) != len(want) {
		t.Fatalf("expected exactly %d members, got %d: %v", len(want), len(entry), keys(entry))
	}
	for member := range entry {
		if !want[member] {
			t.Fatalf("the public entry carries an unexpected member %q: %v", member, keys(entry))
		}
	}
	for member := range want {
		if _, ok := entry[member]; !ok {
			t.Fatalf("the public entry is missing %q: %v", member, keys(entry))
		}
	}
	for _, forbidden := range []string{"sellState", "position", "categoryId", "isSet", "normalizedSlug", "normalized_slug"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("the public body must not carry %q: %s", forbidden, rec.Body.String())
		}
	}

	var price struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(entry["price"], &price); err != nil {
		t.Fatalf("decode the price %s: %v", entry["price"], err)
	}
	if price.Amount != 999999999999 || price.Currency != "VND" {
		t.Fatalf("the price must round-trip exactly, got %+v", price)
	}
	if string(entry["imageUrl"]) != `"https://cdn.example.test/stand.jpg"` {
		t.Fatalf("the entry must carry the main picture's link, got %s", entry["imageUrl"])
	}
	if string(entry["isPreorder"]) != "false" {
		t.Fatalf("an ordinary product must report isPreorder false, got %s", entry["isPreorder"])
	}
}

// FR-005: the public detail carries the description and every picture in order.
func TestPublicProductDetailCarriesDescriptionAndPicturesInOrder(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	product, _ := seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)
	repo.addPicture(model.Picture{
		ID: uuid.New(), ProductID: product.ID, PublicID: "second",
		URL: "https://cdn.example.test/second.jpg", Width: 800, Height: 600,
		Position: 1,
	})
	repo.addPicture(model.Picture{
		ID: uuid.New(), ProductID: product.ID, PublicID: "first",
		URL: "https://cdn.example.test/first.jpg", Width: 800, Height: 600,
		Position: 0, IsPrimary: true,
	})

	rec := perform(handler, http.MethodGet, "/api/v1/products/acrylic-stand")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the detail body %q: %v", rec.Body.String(), err)
	}
	if string(raw.Data["description"]) != `"Mô tả acrylic-stand"` {
		t.Fatalf("the detail must carry the description, got %s", raw.Data["description"])
	}
	var images []struct {
		URL       string `json:"url"`
		IsPrimary *bool  `json:"isPrimary"`
	}
	if err := json.Unmarshal(raw.Data["images"], &images); err != nil {
		t.Fatalf("decode the images %s: %v", raw.Data["images"], err)
	}
	if len(images) != 2 || images[0].URL != "https://cdn.example.test/first.jpg" || images[1].URL != "https://cdn.example.test/second.jpg" {
		t.Fatalf("the pictures must be in order, got %+v", images)
	}
	if images[0].IsPrimary != nil || images[1].IsPrimary != nil {
		t.Fatalf("the public picture shape must not expose the primary flag: %+v", images)
	}
}

// FR-006, quickstart 6d: filtering by a hidden category and by an unknown one
// both answer 200 with `data: []`, indistinguishable from each other, so the
// filter cannot be used to discover hidden categories.
func TestFilteringByAHiddenAndAnUnknownCategoryBothAnswerAnEmptyList(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	_, visibleCat := seedVisibleProduct(t, repo, visibility, "visible", 1)

	hiddenCat := uuid.New()
	hidden := repo.seed(t, model.ProductDraft{
		Name: "Hidden", Slug: "hidden", Price: model.Price{Amount: 100, Currency: "VND"},
		CategoryID: hiddenCat, Position: 0,
	}, time.Now().UTC().Add(-time.Hour))
	repo.move(t, hidden.ID, (*model.Product).Launch)
	// The hidden category is not in the visible set, but it still resolves by slug.
	visibility.slugs["visible-cat"] = visibleCat
	visibility.slugs["hidden-cat"] = hiddenCat

	visibleRec := perform(handler, http.MethodGet, "/api/v1/products?category=visible-cat")
	if visibleRec.Code != http.StatusOK {
		t.Fatalf("filtering by a visible category: expected 200, got %d (%s)", visibleRec.Code, visibleRec.Body.String())
	}
	if body := decodeRawList(t, visibleRec); len(body.Data) != 1 || body.Meta.Total != 1 {
		t.Fatalf("filtering by a visible category must return its product, got %d entries and a total of %d", len(body.Data), body.Meta.Total)
	}

	hiddenRec := perform(handler, http.MethodGet, "/api/v1/products?category=hidden-cat")
	unknownRec := perform(handler, http.MethodGet, "/api/v1/products?category=never-existed")
	if hiddenRec.Code != http.StatusOK || unknownRec.Code != http.StatusOK {
		t.Fatalf("both filters must answer 200, got %d and %d", hiddenRec.Code, unknownRec.Code)
	}
	if !strings.Contains(hiddenRec.Body.String(), `"data":[]`) || !strings.Contains(unknownRec.Body.String(), `"data":[]`) {
		t.Fatalf("both filters must answer data: [], got %s and %s", hiddenRec.Body.String(), unknownRec.Body.String())
	}
	hiddenBody := decodeRawList(t, hiddenRec)
	unknownBody := decodeRawList(t, unknownRec)
	if len(hiddenBody.Data) != 0 || hiddenBody.Meta.Total != 0 || len(unknownBody.Data) != 0 || unknownBody.Meta.Total != 0 {
		t.Fatalf("both filters must answer an empty page, got %+v and %+v", hiddenBody, unknownBody)
	}
}

// FR-002, FR-040, quickstart 5b: a pre-order is present in the public list with
// isPreorder true while an ordinary unlaunched product is absent. The two cases
// are asserted side by side because passing one and failing the other is exactly
// the contradiction the clarification resolved: the pre-order is the one
// unlaunched product a customer sees.
func TestAPreorderIsVisibleWhileAnOrdinaryUnlaunchedProductIsNot(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	category := uuid.New()
	visibility.visible = append(visibility.visible, category)

	repo.seed(t, model.ProductDraft{
		Name:       "Pre-order Aki",
		Slug:       "preorder-aki",
		Price:      model.Price{Amount: 120000, Currency: "VND"},
		CategoryID: category,
		Position:   1,
		IsPreorder: true,
	}, time.Now().UTC().Add(-2*time.Hour))
	repo.seed(t, model.ProductDraft{
		Name:       "Announced Aki",
		Slug:       "announced-aki",
		Price:      model.Price{Amount: 130000, Currency: "VND"},
		CategoryID: category,
		Position:   2,
	}, time.Now().UTC().Add(-time.Hour))

	rec := perform(handler, http.MethodGet, "/api/v1/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeRawList(t, rec)
	if len(body.Data) != 1 {
		t.Fatalf("expected exactly the pre-order in the list, got %d entries", len(body.Data))
	}
	if string(body.Data[0]["slug"]) != `"preorder-aki"` {
		t.Fatalf("expected the pre-order to be the visible entry, got %s", body.Data[0]["slug"])
	}
	if string(body.Data[0]["isPreorder"]) != "true" {
		t.Fatalf("the visible entry must report isPreorder true, got %s", body.Data[0]["isPreorder"])
	}
	if strings.Contains(rec.Body.String(), "announced-aki") {
		t.Fatalf("an ordinary unlaunched product must be absent: %s", rec.Body.String())
	}
}

func keys(raw map[string]json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for member := range raw {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}
