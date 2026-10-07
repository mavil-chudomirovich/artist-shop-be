package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// stubCategoryService records what the handlers passed down and answers with
// controlled data, so an HTTP test is about the transport and not the use case.
// The administrator methods exist only to satisfy the interface: US1 routes
// never reach them, and a test asserts that.
type stubCategoryService struct {
	mu     sync.Mutex
	called []string

	listIn    appdto.ListPublicInput
	list      appdto.PublicCategoryPage
	listErr   error
	detailIn  appdto.PublicCategoryRefInput
	detail    appdto.PublicCategoryOutput
	detailErr error
}

func (s *stubCategoryService) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = append(s.called, call)
}

func (s *stubCategoryService) wasCalled(call string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range s.called {
		if name == call {
			return true
		}
	}
	return false
}

func (s *stubCategoryService) ListPublic(_ context.Context, in appdto.ListPublicInput) (appdto.PublicCategoryPage, error) {
	s.record("ListPublic")
	s.listIn = in
	if s.listErr != nil {
		return appdto.PublicCategoryPage{}, s.listErr
	}
	return s.list, nil
}

func (s *stubCategoryService) GetPublicBySlug(_ context.Context, in appdto.PublicCategoryRefInput) (appdto.PublicCategoryOutput, error) {
	s.record("GetPublicBySlug")
	s.detailIn = in
	if s.detailErr != nil {
		return appdto.PublicCategoryOutput{}, s.detailErr
	}
	return s.detail, nil
}

func (s *stubCategoryService) ListAdmin(context.Context, appdto.ListAdminInput) (appdto.AdminCategoryPage, error) {
	s.record("ListAdmin")
	return appdto.AdminCategoryPage{}, nil
}

func (s *stubCategoryService) GetAdmin(context.Context, appdto.AdminCategoryRefInput) (appdto.AdminCategoryOutput, error) {
	s.record("GetAdmin")
	return appdto.AdminCategoryOutput{}, nil
}

func (s *stubCategoryService) CreateCategory(context.Context, appdto.CreateCategoryInput) (appdto.AdminCategoryOutput, error) {
	s.record("CreateCategory")
	return appdto.AdminCategoryOutput{}, nil
}

func (s *stubCategoryService) UpdateCategory(context.Context, appdto.UpdateCategoryInput) (appdto.AdminCategoryOutput, error) {
	s.record("UpdateCategory")
	return appdto.AdminCategoryOutput{}, nil
}

func (s *stubCategoryService) DeleteCategory(context.Context, appdto.AdminCategoryRefInput) error {
	s.record("DeleteCategory")
	return nil
}

var _ appinterface.CategoryService = (*stubCategoryService)(nil)

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// newCategoryRouter mounts the module's public group under the API prefix, the
// way the composition root mounts it.
func newCategoryRouter(svc appinterface.CategoryService) http.Handler {
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount("/api/v1/categories", handler.Router(middleware.AuthHooks{}))
	return root
}

func perform(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type categoryListBody struct {
	Data []httpdto.PublicCategoryResponse `json:"data"`
	Meta struct {
		RequestID string `json:"requestId"`
		Timestamp string `json:"timestamp"`
		Page      int    `json:"page"`
		PageSize  int    `json:"pageSize"`
		Total     int64  `json:"total"`
	} `json:"meta"`
}

func oneVisibleCategory() *stubCategoryService {
	id := uuid.New()
	return &stubCategoryService{
		list: appdto.PublicCategoryPage{
			Page: 1, PageSize: 20, Total: 1,
			Categories: []appdto.PublicCategoryOutput{{
				ID:          id,
				Name:        "Tranh sơn dầu",
				Slug:        "tranh-son-dau",
				Description: "Mô tả",
			}},
		},
		detail: appdto.PublicCategoryOutput{
			ID:          id,
			Name:        "Tranh sơn dầu",
			Slug:        "tranh-son-dau",
			Description: "Mô tả",
		},
	}
}

// FR-001, FR-014: the public list is reachable with no token at all.
func TestPublicCatalogueIsReachableWithoutAToken(t *testing.T) {
	svc := oneVisibleCategory()

	rec := perform(newCategoryRouter(svc), http.MethodGet, "/api/v1/categories", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a request with no token, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !svc.wasCalled("ListPublic") {
		t.Fatal("the public list route must reach the list use case")
	}
	// No authentication middleware means no identity was required; the response
	// still carries the standard envelope.
	body := decodeCategoryList(t, rec)
	if body.Meta.Page != 1 || body.Meta.PageSize != 20 || body.Meta.Total != 1 {
		t.Fatalf("expected the paged metadata block, got %+v", body.Meta)
	}
}

// FR-004, FR-007: each public entry carries exactly id, name, slug and
// description — no display state, no position, no folding key.
func TestAPublicCategoryEntryCarriesExactlyTheFourPublicMembers(t *testing.T) {
	svc := oneVisibleCategory()

	rec := perform(newCategoryRouter(svc), http.MethodGet, "/api/v1/categories", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Count the members of the entry from the raw JSON, so an added or renamed
	// member fails here rather than passing a struct-shaped assertion.
	var raw struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the raw list body %q: %v", rec.Body.String(), err)
	}
	if len(raw.Data) != 1 {
		t.Fatalf("expected one entry, got %d", len(raw.Data))
	}
	want := map[string]bool{"id": true, "name": true, "slug": true, "description": true}
	if len(raw.Data[0]) != len(want) {
		t.Fatalf("expected exactly %d members, got %d: %v", len(want), len(raw.Data[0]), keys(raw.Data[0]))
	}
	for member := range raw.Data[0] {
		if !want[member] {
			t.Fatalf("the public entry carries an unexpected member %q: %v", member, keys(raw.Data[0]))
		}
	}
	for member := range want {
		if _, ok := raw.Data[0][member]; !ok {
			t.Fatalf("the public entry is missing %q: %v", member, keys(raw.Data[0]))
		}
	}
	// The forbidden members must not appear anywhere in the encoded body, which
	// also covers the folding keys that never have a response shape at all.
	for _, forbidden := range []string{"isVisible", "position", "normalizedName", "normalizedSlug"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("the public body must not carry %q: %s", forbidden, rec.Body.String())
		}
	}

	body := decodeCategoryList(t, rec)
	entry := body.Data[0]
	if entry.Name != "Tranh sơn dầu" || entry.Slug != "tranh-son-dau" || entry.Description != "Mô tả" {
		t.Fatalf("the entry does not carry the stored values: %+v", entry)
	}
	if entry.ID == uuid.Nil {
		t.Fatalf("the entry must carry its identifier: %+v", entry)
	}
}

func decodeCategoryList(t *testing.T, rec *httptest.ResponseRecorder) categoryListBody {
	t.Helper()
	var body categoryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", rec.Body.String(), err)
	}
	return body
}

func keys(raw map[string]json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for member := range raw {
		out = append(out, member)
	}
	return out
}
