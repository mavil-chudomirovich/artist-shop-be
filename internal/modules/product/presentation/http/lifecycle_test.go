package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
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
)

// This file drives the sell-state route through the real use case over an
// in-memory repository, so the handler, the request shape, the domain transition
// and the error mapping are exercised together: a legal move is 200, an illegal
// one is 409 naming the current state, an unknown target is 400 naming `to`, a
// customer is 403, an unknown product is 404, and the edit endpoint does not carry
// the state at all (FR-023, FR-024, research D11, quickstart scenario 7).

// lifecycleRepo is an in-memory ProductRepository supporting the reads and the
// single write the lifecycle use case reaches. It embeds the interface so only
// those methods exist; a call to another one panics, which a test must not paper
// over.
type lifecycleRepo struct {
	domainrepo.ProductRepository

	mu       sync.Mutex
	products map[uuid.UUID]model.Product
}

func newLifecycleRepo() *lifecycleRepo {
	return &lifecycleRepo{products: make(map[uuid.UUID]model.Product)}
}

// seed stores a product already in the requested state, reached through the
// domain's own transitions.
func (r *lifecycleRepo) seed(t *testing.T, state constant.SellState, preorder bool) *model.Product {
	t.Helper()
	slug := "p-" + strings.ReplaceAll(strings.ToLower(string(state)), "_", "-")
	if preorder {
		slug += "-pre"
	}
	product, err := model.NewProduct(model.ProductDraft{
		Name: "Product " + slug, Slug: slug, Price: model.Price{Amount: 100, Currency: "VND"},
		CategoryID: uuid.New(), Position: 1, IsPreorder: preorder,
	}, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", slug, err)
	}
	switch state {
	case constant.SellStateComingSoon:
	case constant.SellStateActive:
		if err := product.Launch(time.Now().UTC()); err != nil {
			t.Fatalf("set up ACTIVE: %v", err)
		}
	case constant.SellStateOutOfStock:
		if err := product.Launch(time.Now().UTC()); err != nil {
			t.Fatalf("set up OUT_OF_STOCK: %v", err)
		}
		if err := product.SellOut(time.Now().UTC()); err != nil {
			t.Fatalf("set up OUT_OF_STOCK: %v", err)
		}
	case constant.SellStateDiscontinued:
		if err := product.Retire(time.Now().UTC()); err != nil {
			t.Fatalf("set up DISCONTINUED: %v", err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.products[product.ID] = *product
	return product
}

func (r *lifecycleRepo) FindByID(_ context.Context, id uuid.UUID) (*domainrepo.ProductView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	product, ok := r.products[id]
	if !ok {
		return nil, domainerr.ErrProductNotFound
	}
	return &domainrepo.ProductView{Product: product}, nil
}

func (r *lifecycleRepo) Update(_ context.Context, product *model.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.products[product.ID]; !ok {
		return domainerr.ErrProductNotFound
	}
	r.products[product.ID] = *product
	return nil
}

func (r *lifecycleRepo) stored(t *testing.T, id uuid.UUID) model.Product {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	product, ok := r.products[id]
	if !ok {
		t.Fatalf("product %s is not seeded", id)
	}
	return product
}

// newLifecycleFixture mounts the administrator group over the real use case and
// the in-memory repository, exactly the way the composition root mounts it.
func newLifecycleFixture(t *testing.T) (http.Handler, *lifecycleRepo) {
	t.Helper()
	repo := newLifecycleRepo()
	service := productimplement.New(productimplement.Service{Products: repo, Mapper: mapper.New()})
	handler := New(service, appinterface.Config{}, testLogger)
	root := chi.NewRouter()
	root.Mount(adminProductsPath, handler.AdminRouter(maintenanceHooks(nil)))
	return root, repo
}

func changeStateRequest(router http.Handler, id uuid.UUID, to, token string) *httptest.ResponseRecorder {
	return performJSON(router, http.MethodPost,
		adminProductsPath+"/"+id.String()+"/state", `{"to":"`+to+`"}`, token)
}

// quickstart 7a: a legal move answers 200 with the product in its new state, and
// the stored row holds that state too.
func TestStateRouteAcceptsALegalMoveAndAnswersTheProduct(t *testing.T) {
	router, repo := newLifecycleFixture(t)
	product := repo.seed(t, constant.SellStateComingSoon, false)

	rec := changeStateRequest(router, product.ID, "ACTIVE", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeAdminProduct(t, rec); got.SellState != string(constant.SellStateActive) {
		t.Fatalf("expected sellState ACTIVE, got %q", got.SellState)
	}
	if got := repo.stored(t, product.ID).SellState; got != constant.SellStateActive {
		t.Fatalf("the stored row must be ACTIVE, got %q", got)
	}
}

// quickstart 7d: an illegal move is 409 PRODUCT_STATE_TRANSITION_INVALID, the
// message names the current state, and the row is left untouched.
func TestStateRouteRefusesAnIllegalMoveWith409NamingTheState(t *testing.T) {
	router, repo := newLifecycleFixture(t)
	product := repo.seed(t, constant.SellStateOutOfStock, false)

	rec := changeStateRequest(router, product.ID, "COMING_SOON", "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeAdminError(t, rec)
	if body.Error.Code != "PRODUCT_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected PRODUCT_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, string(constant.SellStateOutOfStock)) {
		t.Fatalf("the message must name the current state, got %q", body.Error.Message)
	}
	if got := repo.stored(t, product.ID).SellState; got != constant.SellStateOutOfStock {
		t.Fatalf("a refused move must not change the row, got %q", got)
	}
}

// quickstart 7h: a target that is not one of the four states is 400
// VALIDATION_ERROR naming `to`, and nothing is written.
func TestStateRouteAnswers400ForAnUnknownTargetNamingTo(t *testing.T) {
	for _, target := range []string{"ON_SALE", "", "active"} {
		t.Run(target, func(t *testing.T) {
			router, repo := newLifecycleFixture(t)
			product := repo.seed(t, constant.SellStateComingSoon, false)

			rec := changeStateRequest(router, product.ID, target, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %q, got %d (%s)", target, rec.Code, rec.Body.String())
			}
			body := decodeAdminError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "to" {
				t.Fatalf("expected the detail to name to, got %+v", body.Error.Details)
			}
			if got := repo.stored(t, product.ID).SellState; got != constant.SellStateComingSoon {
				t.Fatalf("a rejected target must not change the row, got %q", got)
			}
		})
	}
}

// FR-014, quickstart 14c: the customer token is refused on the state route, and
// the acting account is never drawn from the body.
func TestStateRouteRefusesACustomerToken(t *testing.T) {
	router, repo := newLifecycleFixture(t)
	product := repo.seed(t, constant.SellStateComingSoon, false)

	rec := changeStateRequest(router, product.ID, "ACTIVE", "customer-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := repo.stored(t, product.ID).SellState; got != constant.SellStateComingSoon {
		t.Fatalf("a refused request must not change the row, got %q", got)
	}
}

// An unknown product is 404 PRODUCT_NOT_FOUND, not a state refusal.
func TestStateRouteAnswers404ForAnUnknownProduct(t *testing.T) {
	router, _ := newLifecycleFixture(t)

	rec := changeStateRequest(router, uuid.New(), "ACTIVE", "admin-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeAdminError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}
}

// research D11: sending sellState to the edit endpoint is refused rather than
// accepted — a state change is a transition and has its own endpoint. The edit
// body does not carry the member, so the shared decoder refuses it.
func TestTheEditEndpointRefusesASellState(t *testing.T) {
	router, repo := newLifecycleFixture(t)
	product := repo.seed(t, constant.SellStateComingSoon, false)

	rec := performJSON(router, http.MethodPatch,
		adminProductsPath+"/"+product.ID.String(), `{"sellState":"ACTIVE"}`, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected the edit to refuse sellState with 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeAdminError(t, rec); body.Error.Code != "MALFORMED_REQUEST" {
		t.Fatalf("expected MALFORMED_REQUEST for an unknown member, got %s", body.Error.Code)
	}
	if got := repo.stored(t, product.ID).SellState; got != constant.SellStateComingSoon {
		t.Fatalf("a refused edit must not change the state, got %q", got)
	}
}
