package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The two public paths resolve: the list at the group root and the detail under
// it (research D7).
func TestPublicCategoryRoutesResolve(t *testing.T) {
	svc := oneVisibleCategory()
	router := newCategoryRouter(svc)

	if rec := perform(router, http.MethodGet, "/api/v1/categories", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/categories: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := perform(router, http.MethodGet, "/api/v1/categories/tranh-son-dau", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/categories/{slug}: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !svc.wasCalled("ListPublic") {
		t.Fatal("the list path did not reach the list use case")
	}
	if !svc.wasCalled("GetPublicBySlug") {
		t.Fatal("the detail path did not reach the detail use case")
	}
}

// The public detail route carries a slug, not an identifier: the path parameter
// is handed to the detail use case as the slug string, and there is no public
// route that addresses a category by its identifier. A UUID-shaped segment is
// therefore treated as a slug, and the administrator read is never reached.
func TestThePublicDetailRouteTakesASlugNotAnIdentifier(t *testing.T) {
	svc := oneVisibleCategory()
	router := newCategoryRouter(svc)

	perform(router, http.MethodGet, "/api/v1/categories/tranh-son-dau", "")
	if svc.detailIn.Slug != "tranh-son-dau" {
		t.Fatalf("expected the slug tranh-son-dau, got %q", svc.detailIn.Slug)
	}

	id := uuid.New()
	perform(router, http.MethodGet, "/api/v1/categories/"+id.String(), "")
	if svc.detailIn.Slug != id.String() {
		t.Fatalf("a UUID-shaped segment must reach the detail use case as a slug, got %q", svc.detailIn.Slug)
	}
	if svc.wasCalled("GetAdmin") {
		t.Fatal("the public surface must never address a category by identifier")
	}
}
