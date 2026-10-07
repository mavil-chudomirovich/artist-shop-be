package implement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
)

// This file exercises the public browse use cases over an in-memory repository.
// The repository sorts exactly the way the adapter orders its reads — position,
// then created_at, then id (FR-003) — so the tests prove what the use case does
// with that order: it preserves it, never re-derives it.

// memoryCategories is an in-memory CategoryRepository. Values are copied in and
// out, so a test can only observe what the use case really asked for.
type memoryCategories struct {
	mu   sync.Mutex
	rows map[uuid.UUID]model.Category
}

func newMemoryCategories() *memoryCategories {
	return &memoryCategories{rows: make(map[uuid.UUID]model.Category)}
}

// add seeds a category through the domain constructor with the given creation
// time, which is the first tie-break key.
func (m *memoryCategories) add(t *testing.T, draft model.CategoryDraft, createdAt time.Time) *model.Category {
	t.Helper()
	category, err := model.NewCategory(draft, createdAt)
	if err != nil {
		t.Fatalf("NewCategory(%q): %v", draft.Name, err)
	}
	m.Create(context.Background(), category)
	return category
}

// hide takes a seeded category off display.
func (m *memoryCategories) hide(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	category := m.rows[id]
	category.Hide(time.Now().UTC())
	m.rows[id] = category
}

func (m *memoryCategories) remove(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
}

func (m *memoryCategories) Create(_ context.Context, category *model.Category) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[category.ID] = *category
	return nil
}

func (m *memoryCategories) Update(_ context.Context, category *model.Category) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[category.ID]; !ok {
		return domainerr.ErrCategoryNotFound
	}
	m.rows[category.ID] = *category
	return nil
}

func (m *memoryCategories) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return domainerr.ErrCategoryNotFound
	}
	delete(m.rows, id)
	return nil
}

func (m *memoryCategories) FindByID(_ context.Context, id uuid.UUID) (*model.Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, domainerr.ErrCategoryNotFound
	}
	stored := row
	return &stored, nil
}

func (m *memoryCategories) FindVisibleBySlug(_ context.Context, slug string) (*model.Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.Slug == slug && row.IsVisible {
			stored := row
			return &stored, nil
		}
	}
	return nil, domainerr.ErrCategoryNotFound
}

func (m *memoryCategories) ListVisible(_ context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return m.list(true, page, pageSize)
}

func (m *memoryCategories) ListAll(_ context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return m.list(false, page, pageSize)
}

// list applies the same order and window the adapter applies, so a test cannot
// pass because the fake happened to keep insertion order.
func (m *memoryCategories) list(onlyVisible bool, page, pageSize int) ([]model.Category, int64, error) {
	m.mu.Lock()
	all := make([]model.Category, 0, len(m.rows))
	for _, row := range m.rows {
		if onlyVisible && !row.IsVisible {
			continue
		}
		all = append(all, row)
	}
	m.mu.Unlock()

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
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []model.Category{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

func publicService(repo *memoryCategories) *Service {
	return New(Service{Categories: repo, Mapper: mapper.New()})
}

// FR-002: only the categories the operator has left on display are returned.
func TestListPublicReturnsOnlyCategoriesOnDisplay(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	repo := newMemoryCategories()
	repo.add(t, model.CategoryDraft{Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Position: 1}, base)
	repo.add(t, model.CategoryDraft{Name: "Tượng", Slug: "tuong", Position: 2}, base.Add(time.Minute))
	hidden := repo.add(t, model.CategoryDraft{Name: "Bí mật", Slug: "bi-mat", Position: 0}, base.Add(2*time.Minute))
	repo.hide(hidden.ID)

	page, err := publicService(repo).ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected a total of 2 visible categories, got %d", page.Total)
	}
	if len(page.Categories) != 2 {
		t.Fatalf("expected 2 visible categories, got %d", len(page.Categories))
	}
	for _, category := range page.Categories {
		if category.Slug == hidden.Slug {
			t.Fatalf("a withheld category leaked into the public list: %+v", page.Categories)
		}
	}
}

// FR-003: the list is in the configured order and two identical calls agree.
func TestListPublicOrderIsTheConfiguredOneAndStableAcrossTwoCalls(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	repo := newMemoryCategories()
	// Seeded out of order on purpose, with a tie on position 5 broken by time.
	repo.add(t, model.CategoryDraft{Name: "Gamma", Slug: "gamma", Position: 5}, base.Add(2*time.Minute))
	repo.add(t, model.CategoryDraft{Name: "Alpha", Slug: "alpha", Position: 5}, base)
	repo.add(t, model.CategoryDraft{Name: "Delta", Slug: "delta", Position: 1}, base.Add(time.Minute))

	svc := publicService(repo)
	first, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	want := []string{"delta", "alpha", "gamma"}
	if got := slugs(first.Categories); !equalStrings(got, want) {
		t.Fatalf("expected the configured order %v, got %v", want, got)
	}

	second, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic repeat: %v", err)
	}
	if got := slugs(second.Categories); !equalStrings(got, slugs(first.Categories)) {
		t.Fatalf("the order changed between two identical calls: %v then %v",
			slugs(first.Categories), got)
	}
}

// FR-006: an empty catalogue answers an empty result rather than an error.
func TestListPublicAnswersAnEmptyResultForAnEmptyCatalogue(t *testing.T) {
	page, err := publicService(newMemoryCategories()).ListPublic(
		context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("an empty catalogue must not be an error, got %v", err)
	}
	if len(page.Categories) != 0 || page.Total != 0 {
		t.Fatalf("expected an empty page, got %d categories and a total of %d",
			len(page.Categories), page.Total)
	}
}

// FR-005, SC-004: a withheld, removed or unknown slug answers the same
// not-found. The three errors are the identical sentinel, so nothing in the
// answer distinguishes them.
func TestGetPublicBySlugAnswersTheSameNotFoundForWithheldRemovedAndUnknown(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	repo := newMemoryCategories()
	live := repo.add(t, model.CategoryDraft{Name: "Tranh", Slug: "tranh", Position: 0}, base)
	withheld := repo.add(t, model.CategoryDraft{Name: "Ẩn", Slug: "an", Position: 1}, base.Add(time.Minute))
	repo.hide(withheld.ID)
	removed := repo.add(t, model.CategoryDraft{Name: "Xóa", Slug: "xoa", Position: 2}, base.Add(2*time.Minute))
	repo.remove(removed.ID)

	svc := publicService(repo)
	ctx := context.Background()

	for name, slug := range map[string]string{
		"withheld": withheld.Slug,
		"removed":  removed.Slug,
		"unknown":  "never-existed",
	} {
		_, err := svc.GetPublicBySlug(ctx, dto.PublicCategoryRefInput{Slug: slug})
		if !errors.Is(err, domainerr.ErrCategoryNotFound) {
			t.Fatalf("%s: expected ErrCategoryNotFound, got %v", name, err)
		}
		if err != domainerr.ErrCategoryNotFound {
			t.Fatalf("%s: the use case must not wrap the sentinel, got %v", name, err)
		}
	}

	// The visible category still resolves, so the not-found is about the three
	// cases and not about the query shape.
	got, err := svc.GetPublicBySlug(ctx, dto.PublicCategoryRefInput{Slug: live.Slug})
	if err != nil {
		t.Fatalf("a visible slug must resolve, got %v", err)
	}
	if got.Slug != live.Slug || got.ID != live.ID {
		t.Fatalf("expected the live category %s, got %+v", live.ID, got)
	}
}

func slugs(list []dto.PublicCategoryOutput) []string {
	out := make([]string, 0, len(list))
	for _, category := range list {
		out = append(out, category.Slug)
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
