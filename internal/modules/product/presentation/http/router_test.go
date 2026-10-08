package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The two public paths resolve: the list at the group root and the detail under
// it (research D11).
func TestPublicProductRoutesResolve(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)

	if rec := perform(handler, http.MethodGet, "/api/v1/products"); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/products: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := perform(handler, http.MethodGet, "/api/v1/products/acrylic-stand"); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/products/{slug}: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// The public detail route carries a slug, not an identifier: the path parameter
// is handed to the detail use case as the slug string, and there is no public
// route that addresses a product by its identifier.
func TestThePublicDetailRouteTakesASlugNotAnIdentifier(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)

	perform(handler, http.MethodGet, "/api/v1/products/acrylic-stand")
	if repo.detailSlug != "acrylic-stand" {
		t.Fatalf("expected the slug acrylic-stand, got %q", repo.detailSlug)
	}

	id := uuid.New()
	perform(handler, http.MethodGet, "/api/v1/products/"+id.String())
	if repo.detailSlug != id.String() {
		t.Fatalf("a UUID-shaped segment must reach the detail use case as a slug, got %q", repo.detailSlug)
	}
}

// The list accepts the optional `category` filter: it is resolved to the
// category identifier and passed to the read, while an unfiltered request
// carries none.
func TestTheListAcceptsTheOptionalCategoryFilter(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	_, category := seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)
	visibility.slugs["acrylic"] = category

	perform(handler, http.MethodGet, "/api/v1/products?category=acrylic")
	if query := repo.lastQueryValue(); query.CategoryID == nil || *query.CategoryID != category {
		t.Fatalf("the category filter must reach the read as an identifier, got %+v", query.CategoryID)
	}

	perform(handler, http.MethodGet, "/api/v1/products")
	if query := repo.lastQueryValue(); query.CategoryID != nil {
		t.Fatalf("an unfiltered request must carry no category filter, got %s", *query.CategoryID)
	}
}

// The list accepts the pagination window and bounds pageSize before the read.
func TestTheListAcceptsThePaginationWindow(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	seedVisibleProduct(t, repo, visibility, "acrylic-stand", 1)

	perform(handler, http.MethodGet, "/api/v1/products?page=2&pageSize=5")
	query := repo.lastQueryValue()
	if query.Page != 2 || query.PageSize != 5 {
		t.Fatalf("expected the requested window page=2 pageSize=5, got page=%d pageSize=%d", query.Page, query.PageSize)
	}
}
