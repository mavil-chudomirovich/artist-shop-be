package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file exercises the administrator HTTP surface: the role guard on every
// route, the status each operation answers with, the identifier parsing, and the
// fact that the acting administrator comes from the session rather than from
// anything the client sent (FR-014).

const adminCategoriesPath = "/api/v1/admin/categories"

// The tokens below are accepted by the test hooks and carry a fixed subject, so a
// test can assert the identity the handler handed to the use case.
var (
	testAdminID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// maintenanceStub answers the administrator use cases with controlled data, so an
// HTTP test is about the transport and not the use case. It embeds the public stub
// from the catalogue tests and overrides the administrator methods.
type maintenanceStub struct {
	*stubCategoryService

	listOut   appdto.AdminCategoryPage
	getOut    appdto.AdminCategoryOutput
	getErr    error
	createOut appdto.AdminCategoryOutput
	createErr error
	updateOut appdto.AdminCategoryOutput
	updateErr error
	deleteErr error

	// seenActor is the actor the use case received through the context, which is
	// how the test proves the identity travels beside the request.
	seenActor appinterface.Actor
}

func newMaintenanceStub() *maintenanceStub {
	return &maintenanceStub{stubCategoryService: &stubCategoryService{}}
}

func (s *maintenanceStub) ListAdmin(context.Context, appdto.ListAdminInput) (appdto.AdminCategoryPage, error) {
	return s.listOut, nil
}

func (s *maintenanceStub) GetAdmin(context.Context, appdto.AdminCategoryRefInput) (appdto.AdminCategoryOutput, error) {
	return s.getOut, s.getErr
}

func (s *maintenanceStub) CreateCategory(ctx context.Context, _ appdto.CreateCategoryInput) (appdto.AdminCategoryOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.createOut, s.createErr
}

func (s *maintenanceStub) UpdateCategory(ctx context.Context, _ appdto.UpdateCategoryInput) (appdto.AdminCategoryOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.updateOut, s.updateErr
}

func (s *maintenanceStub) DeleteCategory(ctx context.Context, _ appdto.AdminCategoryRefInput) error {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.deleteErr
}

var _ appinterface.CategoryService = (*maintenanceStub)(nil)

// sampleAdminCategory is a well-formed administrator answer.
func sampleAdminCategory() appdto.AdminCategoryOutput {
	return appdto.AdminCategoryOutput{
		ID:          uuid.New(),
		Name:        "Tranh sơn dầu",
		Slug:        "tranh-son-dau",
		Description: "Mô tả",
		Position:    4,
		IsVisible:   true,
	}
}

// maintenanceHooks supplies the foundation's authentication hooks with a fixed
// administrator and customer identity, and records a denial through OnDenied the
// way the composition root does.
func maintenanceHooks(denied *bool) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer admin-token":
				return &middleware.Identity{Subject: testAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			default:
				return nil, nil
			}
		},
		OnDenied: func(context.Context, middleware.Identity, *http.Request) {
			if denied != nil {
				*denied = true
			}
		},
	}
}

// newAdminRouter mounts the module's administrator group the way the composition
// root mounts it.
func newAdminRouter(svc appinterface.CategoryService, hooks middleware.AuthHooks) http.Handler {
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(adminCategoriesPath, handler.AdminRouter(hooks))
	return root
}

// performJSON is perform with an optional JSON body and token.
func performJSON(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type administeredCategoryBody struct {
	Data httpdto.AdminCategoryResponse `json:"data"`
}

func decodeAdminCategory(t *testing.T, rec *httptest.ResponseRecorder) httpdto.AdminCategoryResponse {
	t.Helper()
	var body administeredCategoryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the administrator category body %q: %v", rec.Body.String(), err)
	}
	return body.Data
}

type adminErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func decodeAdminError(t *testing.T, rec *httptest.ResponseRecorder) adminErrorBody {
	t.Helper()
	var body adminErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-014: every maintenance route answers 401 with no token and 403 with a
// customer token, and the denial is recorded through the foundation's OnDenied
// hook so the privilege denial is audited, exactly as module 01 audits its own.
func TestEveryMaintenanceRouteRefusesNoTokenAndCustomerToken(t *testing.T) {
	id := uuid.New()
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, adminCategoriesPath, ""},
		{http.MethodPost, adminCategoriesPath, `{"name":"Tranh","slug":"tranh"}`},
		{http.MethodGet, adminCategoriesPath + "/" + id.String(), ""},
		{http.MethodPatch, adminCategoriesPath + "/" + id.String(), `{"name":"Tranh"}`},
		{http.MethodDelete, adminCategoriesPath + "/" + id.String(), ""},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			denied := false
			router := newAdminRouter(newMaintenanceStub(), maintenanceHooks(&denied))

			noToken := performJSON(router, route.method, route.path, route.body, "")
			if noToken.Code != http.StatusUnauthorized {
				t.Fatalf("no token: expected 401, got %d (%s)", noToken.Code, noToken.Body.String())
			}
			if body := decodeAdminError(t, noToken); body.Error.Code != string("UNAUTHENTICATED") {
				t.Fatalf("no token: expected UNAUTHENTICATED, got %s", body.Error.Code)
			}

			customer := performJSON(router, route.method, route.path, route.body, "customer-token")
			if customer.Code != http.StatusForbidden {
				t.Fatalf("customer token: expected 403, got %d (%s)", customer.Code, customer.Body.String())
			}
			if body := decodeAdminError(t, customer); body.Error.Code != string("FORBIDDEN") {
				t.Fatalf("customer token: expected FORBIDDEN, got %s", body.Error.Code)
			}
			if !denied {
				t.Fatal("the role denial must be recorded through the foundation's OnDenied hook")
			}
		})
	}
}

// The create route answers 201 and the response carries the administrator members
// the public shape must not (FR-009, FR-029).
func TestCreateCategoryAnswers201WithTheAdministratorShape(t *testing.T) {
	stub := newMaintenanceStub()
	stub.createOut = sampleAdminCategory()

	rec := performJSON(newAdminRouter(stub, maintenanceHooks(nil)), http.MethodPost,
		adminCategoriesPath, `{"name":"Tranh sơn dầu","slug":"tranh-son-dau","description":"Mô tả","position":4}`,
		"admin-token")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	got := decodeAdminCategory(t, rec)
	if got.ID != stub.createOut.ID || !got.IsVisible || got.Position != 4 {
		t.Fatalf("unexpected administrator answer: %+v", got)
	}
	for _, member := range []string{`"isVisible"`, `"position"`, `"createdAt"`, `"updatedAt"`} {
		if !strings.Contains(rec.Body.String(), member) {
			t.Fatalf("the administrator answer must carry %s: %s", member, rec.Body.String())
		}
	}
}

// The edit route answers 200 and the remove route answers 204.
func TestEditAnswers200AndRemoveAnswers204(t *testing.T) {
	id := uuid.New()
	stub := newMaintenanceStub()
	stub.updateOut = sampleAdminCategory()
	router := newAdminRouter(stub, maintenanceHooks(nil))

	edit := performJSON(router, http.MethodPatch, adminCategoriesPath+"/"+id.String(), `{"name":"Mới"}`, "admin-token")
	if edit.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", edit.Code, edit.Body.String())
	}

	remove := performJSON(router, http.MethodDelete, adminCategoriesPath+"/"+id.String(), "", "admin-token")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d (%s)", remove.Code, remove.Body.String())
	}
}

// An unknown identifier is 404 CATEGORY_NOT_FOUND on each route that addresses a
// category; a second removal of the same category is the same answer rather than a
// failure.
func TestUnknownAndAlreadyRemovedCategoriesAnswer404(t *testing.T) {
	id := uuid.New()
	stub := newMaintenanceStub()
	stub.getErr = domainerr.ErrCategoryNotFound
	stub.updateErr = domainerr.ErrCategoryNotFound
	stub.deleteErr = domainerr.ErrCategoryNotFound
	router := newAdminRouter(stub, maintenanceHooks(nil))

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get unknown", http.MethodGet, adminCategoriesPath + "/" + id.String(), ""},
		{"edit unknown", http.MethodPatch, adminCategoriesPath + "/" + id.String(), `{"name":"Mới"}`},
		{"remove unknown", http.MethodDelete, adminCategoriesPath + "/" + id.String(), ""},
		{"remove again", http.MethodDelete, adminCategoriesPath + "/" + id.String(), ""},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			rec := performJSON(router, request.method, request.path, request.body, "admin-token")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeAdminError(t, rec); body.Error.Code != "CATEGORY_NOT_FOUND" {
				t.Fatalf("expected CATEGORY_NOT_FOUND, got %s", body.Error.Code)
			}
		})
	}
}

// A malformed identifier is 400 VALIDATION_ERROR with the field named, on every
// route that takes one (contracts/error-codes.md).
func TestAMalformedIdentifierIs400NamingTheField(t *testing.T) {
	router := newAdminRouter(newMaintenanceStub(), maintenanceHooks(nil))
	malformed := adminCategoriesPath + "/not-a-uuid"

	requests := []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"name":"Mới"}`},
		{http.MethodDelete, ""},
	}
	for _, request := range requests {
		t.Run(request.method, func(t *testing.T) {
			rec := performJSON(router, request.method, malformed, request.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeAdminError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "categoryId" {
				t.Fatalf("expected the detail to name categoryId, got %+v", body.Error.Details)
			}
		})
	}
}

// The acting administrator comes from the session, never from the request. The use
// case receives the identity the token carried, and a body that tries to name an
// actor is refused as an unknown member rather than honoured.
func TestTheActorComesFromTheSessionNotTheRequestBody(t *testing.T) {
	stub := newMaintenanceStub()
	stub.createOut = sampleAdminCategory()
	router := newAdminRouter(stub, maintenanceHooks(nil))

	rec := performJSON(router, http.MethodPost, adminCategoriesPath,
		`{"name":"Tranh","slug":"tranh"}`, "admin-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.seenActor.ID != testAdminID || stub.seenActor.Role != access.RoleAdmin {
		t.Fatalf("the use case must receive the session's administrator, got %+v", stub.seenActor)
	}

	// A client that tries to name the acting account is refused: the create schema
	// has no such member (additionalProperties: false).
	stub.seenActor = appinterface.Actor{}
	rec = performJSON(router, http.MethodPost, adminCategoriesPath,
		`{"name":"Tranh","slug":"tranh","actorId":"`+testCustomerID.String()+`"}`, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a body naming an actor must be refused, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.seenActor.ID != uuid.Nil {
		t.Fatalf("a refused body must not reach the use case, got %+v", stub.seenActor)
	}
}

// The administrator list route resolves under the /admin/categories group.
func TestTheAdministratorListRouteResolves(t *testing.T) {
	stub := newMaintenanceStub()
	stub.listOut = appdto.AdminCategoryPage{Page: 1, PageSize: 20, Total: 1,
		Categories: []appdto.AdminCategoryOutput{sampleAdminCategory()}}

	rec := performJSON(newAdminRouter(stub, maintenanceHooks(nil)), http.MethodGet, adminCategoriesPath, "", "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"isVisible"`) {
		t.Fatalf("the administrator list must carry the display state: %s", rec.Body.String())
	}
}
