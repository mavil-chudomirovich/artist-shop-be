package httpapi

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/dto"
)

// This file exercises catalogue-wide uniqueness through the administrator
// endpoints a human uses, not through a stub that fabricates the error: it wires
// the real use cases and the real error mapping over a repository that enforces
// the same two unique indexes the migration creates. That is what lets it assert
// both halves of FR-020 — the refusal names the field, and the category that was
// already there is not disturbed by the attempt.

// collisionRepo is an in-memory CategoryRepository that enforces the two
// catalogue-wide unique indexes of migrations/00005_category.sql over the folded
// keys FoldKey produces.
//
// It exists so the collision tests can drive the real HTTP, use-case and mapping
// layers without a database; the storage guarantee itself is proven against real
// PostgreSQL by T035 and by the adapter's own integration test. The unique check
// is on the folded key, exactly as the index is on normalized_name and
// normalized_slug, so a case- or surrounding-whitespace-only difference is a
// collision here for the same reason it is one in storage.
type collisionRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]model.Category
}

func newCollisionRepo() *collisionRepo {
	return &collisionRepo{rows: make(map[uuid.UUID]model.Category)}
}

var _ domainrepo.CategoryRepository = (*collisionRepo)(nil)

// checkUnique reports the matching sentinel when another row already holds the
// folded name or slug. exclude is the row being written, if any: an UPDATE never
// compares a row with itself, which is what makes a same-name self-edit succeed
// (FR-022).
func (r *collisionRepo) checkUnique(category *model.Category, exclude uuid.UUID) error {
	nameKey := model.FoldKey(category.Name)
	slugKey := model.FoldKey(category.Slug)
	for id, row := range r.rows {
		if id != exclude && row.NormalizedName == nameKey {
			return domainerr.ErrCategoryNameTaken
		}
	}
	for id, row := range r.rows {
		if id != exclude && row.NormalizedSlug == slugKey {
			return domainerr.ErrCategorySlugTaken
		}
	}
	return nil
}

func (r *collisionRepo) Create(_ context.Context, category *model.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkUnique(category, uuid.Nil); err != nil {
		return err
	}
	r.rows[category.ID] = *category
	return nil
}

func (r *collisionRepo) Update(_ context.Context, category *model.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[category.ID]; !ok {
		return domainerr.ErrCategoryNotFound
	}
	if err := r.checkUnique(category, category.ID); err != nil {
		return err
	}
	r.rows[category.ID] = *category
	return nil
}

func (r *collisionRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[id]; !ok {
		return domainerr.ErrCategoryNotFound
	}
	delete(r.rows, id)
	return nil
}

func (r *collisionRepo) FindByID(_ context.Context, id uuid.UUID) (*model.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, domainerr.ErrCategoryNotFound
	}
	copied := row
	return &copied, nil
}

func (r *collisionRepo) FindVisibleBySlug(_ context.Context, slug string) (*model.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.Slug == slug && row.IsVisible {
			copied := row
			return &copied, nil
		}
	}
	return nil, domainerr.ErrCategoryNotFound
}

func (r *collisionRepo) ListVisible(_ context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return r.list(true, page, pageSize)
}

func (r *collisionRepo) ListAll(_ context.Context, page, pageSize int) ([]model.Category, int64, error) {
	return r.list(false, page, pageSize)
}

// VisibleIDs mirrors the adapter's cross-module read (research D1).
func (r *collisionRepo) VisibleIDs(_ context.Context) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, 0, len(r.rows))
	for id, row := range r.rows {
		if row.IsVisible {
			out = append(out, id)
		}
	}
	return out, nil
}

// IDBySlug mirrors the adapter's cross-module slug lookup (research D1).
func (r *collisionRepo) IDBySlug(_ context.Context, slug string) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, row := range r.rows {
		if row.Slug == slug {
			return id, true, nil
		}
	}
	return uuid.Nil, false, nil
}

func (r *collisionRepo) list(onlyVisible bool, page, pageSize int) ([]model.Category, int64, error) {
	r.mu.Lock()
	all := make([]model.Category, 0, len(r.rows))
	for _, row := range r.rows {
		if onlyVisible && !row.IsVisible {
			continue
		}
		all = append(all, row)
	}
	r.mu.Unlock()

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

// count reports how many rows the catalogue holds, so a test can assert a
// refused write stored nothing.
func (r *collisionRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

// collisionRouter builds the administrator surface over the real use cases and
// the collision-enforcing repository, returning the repository so a test can
// count the stored rows.
func collisionRouter() (http.Handler, *collisionRepo) {
	repo := newCollisionRepo()
	service := implement.New(implement.Service{Categories: repo, Mapper: mapper.New()})
	return newAdminRouter(service, maintenanceHooks(nil)), repo
}

// createExisting seeds the one category every collision is aimed at, through the
// create endpoint, so the row under test is the shape a client would produce.
func createExisting(t *testing.T, router http.Handler) httpdto.AdminCategoryResponse {
	t.Helper()
	rec := performJSON(router, http.MethodPost, adminCategoriesPath,
		`{"name":"Tranh sơn dầu","slug":"tranh-son-dau","description":"gốc","position":1}`,
		"admin-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAdminCategory(t, rec)
}

// FR-016, FR-017, FR-020, SC-003: a duplicate name, a name differing only in
// letter case, a name differing only in surrounding whitespace and a duplicate
// slug are each refused with 409, the module code that names the colliding field,
// and the original category left untouched with no second row stored.
func TestACollisionIsRefusedWithTheFieldNamedAndTheOriginalUntouched(t *testing.T) {
	router, repo := collisionRouter()
	existing := createExisting(t, router)

	cases := []struct {
		name  string
		body  string
		code  string
		field string
	}{
		{
			name:  "duplicate name",
			body:  `{"name":"Tranh sơn dầu","slug":"khac-mot"}`,
			code:  "CATEGORY_NAME_TAKEN",
			field: "name",
		},
		{
			name:  "name differing only in letter case",
			body:  `{"name":"TRANH SƠN DẦU","slug":"khac-hai"}`,
			code:  "CATEGORY_NAME_TAKEN",
			field: "name",
		},
		{
			name:  "name differing only in surrounding whitespace",
			body:  `{"name":"  Tranh sơn dầu  ","slug":"khac-ba"}`,
			code:  "CATEGORY_NAME_TAKEN",
			field: "name",
		},
		{
			name:  "duplicate slug",
			body:  `{"name":"Tên khác","slug":"tranh-son-dau"}`,
			code:  "CATEGORY_SLUG_TAKEN",
			field: "slug",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performJSON(router, http.MethodPost, adminCategoriesPath, tc.body, "admin-token")
			if rec.Code != http.StatusConflict {
				t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeAdminError(t, rec)
			if body.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != tc.field {
				t.Fatalf("expected one detail naming %q, got %+v", tc.field, body.Error.Details)
			}

			// The existing category is exactly as it was: a refused create writes
			// nothing and disturbs no one.
			read := performJSON(router, http.MethodGet, adminCategoriesPath+"/"+existing.ID.String(), "", "admin-token")
			if read.Code != http.StatusOK {
				t.Fatalf("read the existing category: expected 200, got %d (%s)", read.Code, read.Body.String())
			}
			unchanged := decodeAdminCategory(t, read)
			if unchanged != existing {
				t.Fatalf("a refused collision changed the existing category:\n before %+v\n after  %+v", existing, unchanged)
			}
			if got := repo.count(); got != 1 {
				t.Fatalf("a refused collision must store nothing, the catalogue holds %d rows", got)
			}
		})
	}
}

// FR-022, SC-006: re-sending a category's own name and slug succeeds — alone,
// and together with other changes. This is the case a naive unique check gets
// wrong: the row is excluded from its own uniqueness comparison, because an
// UPDATE never compares a row with itself.
func TestReSendingACategorysOwnNameAndSlugSucceeds(t *testing.T) {
	router, _ := collisionRouter()
	existing := createExisting(t, router)
	path := adminCategoriesPath + "/" + existing.ID.String()

	t.Run("name and slug alone", func(t *testing.T) {
		rec := performJSON(router, http.MethodPatch, path,
			`{"name":"Tranh sơn dầu","slug":"tranh-son-dau"}`, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("a same-name, same-slug edit must succeed, got %d (%s)", rec.Code, rec.Body.String())
		}
		got := decodeAdminCategory(t, rec)
		if got.Name != existing.Name || got.Slug != existing.Slug {
			t.Fatalf("the edit changed the values it re-sent: %+v", got)
		}
		if got.ID != existing.ID {
			t.Fatalf("the identifier moved on a self-edit: %s", got.ID)
		}
	})

	t.Run("together with other changes", func(t *testing.T) {
		rec := performJSON(router, http.MethodPatch, path,
			`{"name":"Tranh sơn dầu","slug":"tranh-son-dau","description":"mới","position":9}`, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("a self-edit alongside other changes must succeed, got %d (%s)", rec.Code, rec.Body.String())
		}
		got := decodeAdminCategory(t, rec)
		if got.Name != existing.Name || got.Slug != existing.Slug {
			t.Fatalf("a self-edit changed the name or slug it re-sent: %+v", got)
		}
		if got.Description != "mới" || got.Position != 9 {
			t.Fatalf("a self-edit did not apply the other changes: %+v", got)
		}
	})
}
