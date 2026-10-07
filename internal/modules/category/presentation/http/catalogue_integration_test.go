//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// The category module is now mounted in cmd/api (T030). This test builds the
// public group against a real PostgreSQL container: real migrations, the real
// repository adapter and the real public use cases, with rows seeded through the
// repository. The real *implement.Service serves every route — the administrator
// stub that stood in while its methods did not exist is gone.

const publicCategoriesPath = "/api/v1/categories"

func newCatalogueFixture(t *testing.T) (*pgxpool.Pool, *categorypostgres.CategoryRepository, http.Handler) {
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

	repo := categorypostgres.NewCategoryRepository(pool)
	service := implement.New(implement.Service{Categories: repo, Mapper: mapper.New()})
	handler := New(service, testLogger)

	root := chi.NewRouter()
	root.Use(withRequestID)
	root.Mount(publicCategoriesPath, handler.Router(middleware.AuthHooks{}))
	return pool, repo, root
}

// withRequestID fills the correlation id from the request header, so the two
// not-found answers below carry distinct request identifiers and the comparison
// that ignores requestId is not vacuously true.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := reqctx.WithCorrelation(r.Context(), r.Header.Get("X-Request-Id"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// seed inserts a category through the repository, so the rows the public read
// sees are the ones storage really holds.
func seed(t *testing.T, repo *categorypostgres.CategoryRepository, draft model.CategoryDraft, createdAt time.Time) *model.Category {
	t.Helper()
	category, err := model.NewCategory(draft, createdAt)
	if err != nil {
		t.Fatalf("NewCategory(%q): %v", draft.Name, err)
	}
	if err := repo.Create(context.Background(), category); err != nil {
		t.Fatalf("Create(%q): %v", draft.Name, err)
	}
	return category
}

func getWithRequestID(t *testing.T, handler http.Handler, path, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type catalogueListBody struct {
	Data []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	} `json:"data"`
	Meta struct {
		RequestID string `json:"requestId"`
		Page      int    `json:"page"`
		PageSize  int    `json:"pageSize"`
		Total     int64  `json:"total"`
	} `json:"meta"`
}

func decodeCatalogueList(t *testing.T, rec *httptest.ResponseRecorder) catalogueListBody {
	t.Helper()
	var body catalogueListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-002, FR-003, FR-005, SC-001, SC-002, SC-004 against real PostgreSQL: a
// withheld category is absent from the list, meta.total counts only what is
// visible, the order is stable across two requests, and the withheld-slug answer
// is identical to the unknown-slug answer apart from the request identifier.
func TestPublicCatalogueAgainstPostgres(t *testing.T) {
	_, repo, handler := newCatalogueFixture(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	// Visible: a position gap, then a tie at position 20 broken by created_at.
	seed(t, repo, model.CategoryDraft{Name: "Tranh sơn dầu", Slug: "tranh-son-dau", Description: "Tranh", Position: 10}, base)
	seed(t, repo, model.CategoryDraft{Name: "Tượng", Slug: "tuong", Description: "Tượng", Position: 20}, base.Add(time.Minute))
	seed(t, repo, model.CategoryDraft{Name: "Gốm", Slug: "gom", Description: "Gốm", Position: 20}, base.Add(2*time.Minute))

	// Withheld: at position 0 it would sort first, so its absence from the list
	// and from the count is what proves the display filter, not an ordering
	// accident.
	withheld := seed(t, repo, model.CategoryDraft{Name: "Nháp", Slug: "nhap", Description: "Chưa công khai", Position: 0}, base.Add(3*time.Minute))
	withheld.Hide(time.Now().UTC())
	if err := repo.Update(context.Background(), withheld); err != nil {
		t.Fatalf("hide the withheld category: %v", err)
	}

	// Removed: it must leave no trace in the customer-facing catalogue.
	removed := seed(t, repo, model.CategoryDraft{Name: "Cũ", Slug: "cu", Description: "Đã xóa", Position: -5}, base.Add(4*time.Minute))
	if err := repo.Delete(context.Background(), removed.ID); err != nil {
		t.Fatalf("remove the retired category: %v", err)
	}

	wantOrder := []string{"tranh-son-dau", "tuong", "gom"}

	first := getWithRequestID(t, handler, publicCategoriesPath, "list-1")
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", first.Code, first.Body.String())
	}
	body := decodeCatalogueList(t, first)
	if body.Meta.Total != 3 {
		t.Fatalf("meta.total must count only the visible categories, got %d", body.Meta.Total)
	}
	if len(body.Data) != 3 {
		t.Fatalf("expected 3 visible categories, got %d", len(body.Data))
	}
	for i, slug := range wantOrder {
		if body.Data[i].Slug != slug {
			t.Fatalf("expected the configured order %v, got entry %d as %q", wantOrder, i, body.Data[i].Slug)
		}
	}
	for _, entry := range body.Data {
		if entry.Slug == withheld.Slug || entry.Slug == removed.Slug {
			t.Fatalf("a withheld or removed category leaked into the list: %+v", body.Data)
		}
	}

	// Two identical requests return the same order.
	second := getWithRequestID(t, handler, publicCategoriesPath, "list-2")
	if second.Code != http.StatusOK {
		t.Fatalf("the repeat list: expected 200, got %d (%s)", second.Code, second.Body.String())
	}
	secondBody := decodeCatalogueList(t, second)
	if len(secondBody.Data) != len(body.Data) {
		t.Fatalf("the list size changed between two requests: %d then %d", len(body.Data), len(secondBody.Data))
	}
	for i := range body.Data {
		if body.Data[i].Slug != secondBody.Data[i].Slug {
			t.Fatalf("the order is not stable: %q then %q", body.Data[i].Slug, secondBody.Data[i].Slug)
		}
	}

	// The withheld slug and an unused slug answer the same not-found.
	withheldRec := getWithRequestID(t, handler, publicCategoriesPath+"/"+withheld.Slug, "probe-withheld")
	unknownRec := getWithRequestID(t, handler, publicCategoriesPath+"/never-existed", "probe-unknown")

	if withheldRec.Code != http.StatusNotFound || unknownRec.Code != http.StatusNotFound {
		t.Fatalf("expected both 404, got %d and %d", withheldRec.Code, unknownRec.Code)
	}

	withheldBody := decodeEnvelope(t, withheldRec)
	unknownBody := decodeEnvelope(t, unknownRec)

	withheldErr, ok := withheldBody["error"].(map[string]any)
	if !ok {
		t.Fatalf("the withheld answer is not an error envelope: %s", withheldRec.Body.String())
	}
	unknownErr, ok := unknownBody["error"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown answer is not an error envelope: %s", unknownRec.Body.String())
	}
	if withheldErr["code"] != "CATEGORY_NOT_FOUND" || unknownErr["code"] != "CATEGORY_NOT_FOUND" {
		t.Fatalf("expected CATEGORY_NOT_FOUND for both, got %v and %v", withheldErr["code"], unknownErr["code"])
	}

	withheldRequestID, _ := withheldErr["requestId"].(string)
	unknownRequestID, _ := unknownErr["requestId"].(string)
	if withheldRequestID == unknownRequestID {
		t.Fatalf("expected distinct request identifiers so the comparison below is meaningful, got %q", withheldRequestID)
	}

	// Strip the request identifier from both and compare the rest byte for byte
	// at the value level: the two answers must be indistinguishable.
	delete(withheldErr, "requestId")
	delete(unknownErr, "requestId")
	if !reflect.DeepEqual(withheldBody, unknownBody) {
		t.Fatalf("the withheld and unknown answers differ beyond the request identifier:\n withheld %s\n unknown  %s",
			withheldRec.Body.String(), unknownRec.Body.String())
	}
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}
