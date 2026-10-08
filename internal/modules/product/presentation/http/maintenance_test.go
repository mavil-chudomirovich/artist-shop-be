package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file exercises the administrator HTTP surface: the role guard on every
// route, the status each operation answers with, the identifier parsing, the
// slug-collision answer, and the upload validation driven through the multipart
// endpoint (FR-014, FR-019, FR-020, SC-007).

const adminProductsPath = "/api/v1/admin/products"

// The tokens below are accepted by the test hooks and carry a fixed subject, so a
// test can assert the identity the handler handed to the use case.
var (
	testAdminID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// stubProductService answers the administrator use cases with controlled data, so
// an HTTP test is about the transport and not the use case. It implements the
// whole ProductService surface the handler consumes.
type stubProductService struct {
	listAdminOut     appdto.AdminProductPage
	getOut           appdto.AdminProductDetailOutput
	getErr           error
	createOut        appdto.AdminProductDetailOutput
	createErr        error
	createIn         appdto.CreateProductInput
	updateOut        appdto.AdminProductDetailOutput
	updateErr        error
	updateIn         appdto.UpdateProductInput
	deleteErr        error
	addPictureOut    appdto.AdminProductDetailOutput
	addPictureErr    error
	removePictureErr error
	setPrimaryOut    appdto.AdminProductDetailOutput
	setPrimaryErr    error
	changeStateOut   appdto.AdminProductDetailOutput
	changeStateErr   error
	changeStateIn    appdto.ChangeSellStateInput

	// seenActor is the actor the use case received through the context, which is
	// how the test proves the identity travels beside the request.
	seenActor appinterface.Actor
}

func (s *stubProductService) ListPublic(context.Context, appdto.ListPublicInput) (appdto.PublicProductPage, error) {
	return appdto.PublicProductPage{}, nil
}

func (s *stubProductService) GetPublicBySlug(context.Context, appdto.PublicProductRefInput) (appdto.PublicProductDetailOutput, error) {
	return appdto.PublicProductDetailOutput{}, nil
}

func (s *stubProductService) ListAdmin(context.Context, appdto.ListAdminInput) (appdto.AdminProductPage, error) {
	return s.listAdminOut, nil
}

func (s *stubProductService) GetAdmin(context.Context, appdto.AdminProductRefInput) (appdto.AdminProductDetailOutput, error) {
	return s.getOut, s.getErr
}

func (s *stubProductService) CreateProduct(ctx context.Context, in appdto.CreateProductInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	s.createIn = in
	return s.createOut, s.createErr
}

func (s *stubProductService) UpdateProduct(ctx context.Context, in appdto.UpdateProductInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	s.updateIn = in
	return s.updateOut, s.updateErr
}

func (s *stubProductService) DeleteProduct(ctx context.Context, _ appdto.AdminProductRefInput) error {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.deleteErr
}

func (s *stubProductService) AddPicture(ctx context.Context, _ appdto.AddPictureInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.addPictureOut, s.addPictureErr
}

func (s *stubProductService) RemovePicture(ctx context.Context, _ appdto.RemovePictureInput) error {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.removePictureErr
}

func (s *stubProductService) SetPrimaryPicture(ctx context.Context, _ appdto.SetPrimaryPictureInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.setPrimaryOut, s.setPrimaryErr
}

func (s *stubProductService) ChangeSellState(ctx context.Context, in appdto.ChangeSellStateInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	s.changeStateIn = in
	return s.changeStateOut, s.changeStateErr
}

// The stub satisfies the one declared use-case surface the handler consumes, so
// the surface cannot drift away from what the routes reach.
var _ appinterface.ProductService = (*stubProductService)(nil)

// sampleAdminProduct is a well-formed administrator answer, carrying the members
// the public shape must not expose (FR-011).
func sampleAdminProduct() appdto.AdminProductDetailOutput {
	imageURL := "https://cdn.example.test/products/one.jpg"
	return appdto.AdminProductDetailOutput{
		AdminProductOutput: appdto.AdminProductOutput{
			ID:          uuid.New(),
			Name:        "Acrylic stand",
			Slug:        "acrylic-stand",
			Description: "Acrylic stand 15cm",
			Price:       model.Price{Amount: 120000, Currency: "VND"},
			CategoryID:  uuid.New(),
			Position:    10,
			SellState:   constant.SellStateComingSoon,
			IsSet:       false,
			IsPreorder:  false,
			ImageCount:  1,
			ImageURL:    &imageURL,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		},
		Images: []appdto.AdminImageOutput{{
			ID: uuid.New(), PublicID: "demo/one", URL: imageURL,
			Width: 1600, Height: 1200, Position: 0, IsPrimary: true,
		}},
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
func newAdminRouter(svc appinterface.ProductService, hooks middleware.AuthHooks) http.Handler {
	handler := New(svc, appinterface.Config{}, testLogger)
	root := chi.NewRouter()
	root.Mount(adminProductsPath, handler.AdminRouter(hooks))
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

type administeredProductBody struct {
	Data httpdto.AdminProductDetailResponse `json:"data"`
}

func decodeAdminProduct(t *testing.T, rec *httptest.ResponseRecorder) httpdto.AdminProductDetailResponse {
	t.Helper()
	var body administeredProductBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the administrator product body %q: %v", rec.Body.String(), err)
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

// adminRoute is one maintenance route the guard must protect.
type adminRoute struct {
	method string
	path   string
	body   string
}

// maintenanceRoutes enumerates every administrator route mounted in this phase.
func maintenanceRoutes(id, imageID uuid.UUID) []adminRoute {
	return []adminRoute{
		{http.MethodGet, adminProductsPath, ""},
		{http.MethodPost, adminProductsPath, `{"name":"A","slug":"a","price":{"amount":1,"currency":"VND"},"categoryId":"` + uuid.New().String() + `","position":1}`},
		{http.MethodGet, adminProductsPath + "/" + id.String(), ""},
		{http.MethodPatch, adminProductsPath + "/" + id.String(), `{"name":"B"}`},
		{http.MethodDelete, adminProductsPath + "/" + id.String(), ""},
		{http.MethodPost, adminProductsPath + "/" + id.String() + "/state", `{"to":"ACTIVE"}`},
		{http.MethodPost, adminProductsPath + "/" + id.String() + "/images", ""},
		{http.MethodDelete, adminProductsPath + "/" + id.String() + "/images/" + imageID.String(), ""},
		{http.MethodPost, adminProductsPath + "/" + id.String() + "/images/" + imageID.String() + "/primary", ""},
	}
}

// FR-014: every maintenance route answers 401 with no token and 403 with a
// customer token, and the denial is recorded through the foundation's OnDenied
// hook so the privilege denial is audited.
func TestEveryMaintenanceRouteRefusesNoTokenAndCustomerToken(t *testing.T) {
	id, imageID := uuid.New(), uuid.New()
	for _, route := range maintenanceRoutes(id, imageID) {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			denied := false
			router := newAdminRouter(&stubProductService{}, maintenanceHooks(&denied))

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
// the public shape must not (FR-010, FR-011).
func TestCreateProductAnswers201WithTheAdministratorShape(t *testing.T) {
	stub := &stubProductService{createOut: sampleAdminProduct()}
	router := newAdminRouter(stub, maintenanceHooks(nil))

	rec := performJSON(router, http.MethodPost, adminProductsPath,
		`{"name":"Acrylic stand","slug":"acrylic-stand","description":"Acrylic stand 15cm","price":{"amount":120000,"currency":"VND"},"categoryId":"`+uuid.New().String()+`","position":10}`,
		"admin-token")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	got := decodeAdminProduct(t, rec)
	if got.ID != stub.createOut.ID || got.SellState != string(constant.SellStateComingSoon) {
		t.Fatalf("unexpected administrator answer: %+v", got)
	}
	for _, member := range []string{`"sellState"`, `"position"`, `"categoryId"`, `"isSet"`, `"createdAt"`, `"updatedAt"`} {
		if !strings.Contains(rec.Body.String(), member) {
			t.Fatalf("the administrator answer must carry %s: %s", member, rec.Body.String())
		}
	}
}

// The edit route answers 200 and the remove route answers 204.
func TestEditAnswers200AndRemoveAnswers204(t *testing.T) {
	id := uuid.New()
	stub := &stubProductService{updateOut: sampleAdminProduct()}
	router := newAdminRouter(stub, maintenanceHooks(nil))

	edit := performJSON(router, http.MethodPatch, adminProductsPath+"/"+id.String(), `{"name":"Mới"}`, "admin-token")
	if edit.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", edit.Code, edit.Body.String())
	}

	remove := performJSON(router, http.MethodDelete, adminProductsPath+"/"+id.String(), "", "admin-token")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d (%s)", remove.Code, remove.Body.String())
	}
}

// An unknown identifier is 404 PRODUCT_NOT_FOUND on each route that addresses a
// product; a second removal of the same product is the same answer rather than a
// failure.
func TestUnknownAndAlreadyRemovedProductsAnswer404(t *testing.T) {
	id := uuid.New()
	stub := &stubProductService{
		getErr:    domainerr.ErrProductNotFound,
		updateErr: domainerr.ErrProductNotFound,
		deleteErr: domainerr.ErrProductNotFound,
	}
	router := newAdminRouter(stub, maintenanceHooks(nil))

	requests := []adminRoute{
		{http.MethodGet, adminProductsPath + "/" + id.String(), ""},
		{http.MethodPatch, adminProductsPath + "/" + id.String(), `{"name":"Mới"}`},
		{http.MethodDelete, adminProductsPath + "/" + id.String(), ""},
		{http.MethodDelete, adminProductsPath + "/" + id.String(), ""},
	}
	for _, request := range requests {
		t.Run(request.method, func(t *testing.T) {
			rec := performJSON(router, request.method, request.path, request.body, "admin-token")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeAdminError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
				t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
			}
		})
	}
}

// A malformed identifier is 400 VALIDATION_ERROR with the field named, on every
// route that takes one (contracts/error-codes.md).
func TestAMalformedIdentifierIs400NamingTheField(t *testing.T) {
	router := newAdminRouter(&stubProductService{}, maintenanceHooks(nil))
	id, imageID := uuid.New(), uuid.New()

	requests := []adminRoute{
		{http.MethodGet, adminProductsPath + "/not-a-uuid", ""},
		{http.MethodPatch, adminProductsPath + "/not-a-uuid", `{"name":"Mới"}`},
		{http.MethodDelete, adminProductsPath + "/not-a-uuid", ""},
		{http.MethodPost, adminProductsPath + "/not-a-uuid/images/" + imageID.String() + "/primary", ""},
	}
	for _, request := range requests {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			rec := performJSON(router, request.method, request.path, request.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeAdminError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "id" {
				t.Fatalf("expected the detail to name id, got %+v", body.Error.Details)
			}
		})
	}
	_ = id
}

// FR-028: a duplicate slug is 409 PRODUCT_SLUG_TAKEN with the slug named.
func TestADuplicateSlugIs409NamingTheSlug(t *testing.T) {
	stub := &stubProductService{createErr: domainerr.ErrProductSlugTaken}
	router := newAdminRouter(stub, maintenanceHooks(nil))

	rec := performJSON(router, http.MethodPost, adminProductsPath,
		`{"name":"A","slug":"acrylic-stand","price":{"amount":1,"currency":"VND"},"categoryId":"`+uuid.New().String()+`","position":1}`,
		"admin-token")

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeAdminError(t, rec)
	if body.Error.Code != "PRODUCT_SLUG_TAKEN" {
		t.Fatalf("expected PRODUCT_SLUG_TAKEN, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "slug" {
		t.Fatalf("expected the detail to name slug, got %+v", body.Error.Details)
	}
}

// The acting administrator comes from the session, never from the request: the
// use case receives the identity the token carried, and a body that tries to name
// an actor is refused as an unknown member.
func TestTheActorComesFromTheSessionNotTheRequestBody(t *testing.T) {
	stub := &stubProductService{createOut: sampleAdminProduct()}
	router := newAdminRouter(stub, maintenanceHooks(nil))

	rec := performJSON(router, http.MethodPost, adminProductsPath,
		`{"name":"A","slug":"a","price":{"amount":1,"currency":"VND"},"categoryId":"`+uuid.New().String()+`","position":1}`,
		"admin-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.seenActor.ID != testAdminID || stub.seenActor.Role != access.RoleAdmin {
		t.Fatalf("the use case must receive the session's administrator, got %+v", stub.seenActor)
	}

	stub.seenActor = appinterface.Actor{}
	rec = performJSON(router, http.MethodPost, adminProductsPath,
		`{"name":"A","slug":"a","actorId":"`+testCustomerID.String()+`"}`,
		"admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a body naming an actor must be refused, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.seenActor.ID != uuid.Nil {
		t.Fatalf("a refused body must not reach the use case, got %+v", stub.seenActor)
	}
}

// --- T039: upload validation driven through the multipart endpoint ---

// productMediaStub answers the shared media port without a provider. It counts
// uploads, which is how the tests prove a refused upload never reached the
// provider (FR-019, SC-007).
type productMediaStub struct {
	mu      sync.Mutex
	uploads int
	removed int
	ref     appinterface.MediaReference
	err     error
	// beforeUpload, when set, blocks each upload until the test releases it. It
	// is how the concurrency test starts two uploads at once (research D6).
	beforeUpload func()
}

func newProductMediaStub() *productMediaStub {
	return &productMediaStub{ref: appinterface.MediaReference{
		PublicID: "artist-shop/products/one",
		URL:      "https://cdn.example.test/products/one.jpg",
		Width:    1600,
		Height:   1200,
	}}
}

func (m *productMediaStub) Upload(context.Context, []byte, int) (appinterface.MediaReference, error) {
	if m.beforeUpload != nil {
		m.beforeUpload()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads++
	if m.err != nil {
		return appinterface.MediaReference{}, m.err
	}
	return m.ref, nil
}

func (m *productMediaStub) Remove(context.Context, appinterface.MediaReference) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed++
	return m.err
}

var _ appinterface.MediaStore = (*productMediaStub)(nil)

// httpUnitOfWork is an in-memory UnitOfWork for the upload fixture.
type httpUnitOfWork struct{}

func (httpUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// uploadRepo is an in-memory product repository that supports the picture upload
// path. It embeds the interface so only the operations the upload use case
// reaches are implemented.
type uploadRepo struct {
	domainrepo.ProductRepository

	mu       sync.Mutex
	products map[uuid.UUID]model.Product
	pictures map[uuid.UUID][]model.Picture
}

func newUploadRepo() *uploadRepo {
	return &uploadRepo{products: make(map[uuid.UUID]model.Product), pictures: make(map[uuid.UUID][]model.Picture)}
}

func (r *uploadRepo) seed(t *testing.T, slug string) *model.Product {
	t.Helper()
	product, err := model.NewProduct(model.ProductDraft{
		Name: "Product " + slug, Slug: slug, Price: model.Price{Amount: 100, Currency: "VND"},
		CategoryID: uuid.New(),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", slug, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.products[product.ID] = *product
	return product
}

func (r *uploadRepo) LockProduct(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.products[id]; !ok {
		return domainerr.ErrProductNotFound
	}
	return nil
}

func (r *uploadRepo) CountPictures(_ context.Context, productID uuid.UUID) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pictures[productID]), nil
}

func (r *uploadRepo) ListPictures(_ context.Context, productID uuid.UUID) ([]model.Picture, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pictures[productID]) == 0 {
		return nil, nil
	}
	return model.OrderPictures(r.pictures[productID]), nil
}

func (r *uploadRepo) AddPicture(_ context.Context, picture *model.Picture) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pictures[picture.ProductID] = append(r.pictures[picture.ProductID], *picture)
	return nil
}

func (r *uploadRepo) FindByID(_ context.Context, id uuid.UUID) (*domainrepo.ProductView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	product, ok := r.products[id]
	if !ok {
		return nil, domainerr.ErrProductNotFound
	}
	return &domainrepo.ProductView{Product: product, Pictures: model.OrderPictures(r.pictures[id])}, nil
}

// uploadFixture is the real picture use case over the in-memory repository and the
// media stub, mounted the way the composition root mounts it.
type uploadFixture struct {
	router http.Handler
	repo   *uploadRepo
	media  *productMediaStub
}

func newUploadFixture(t *testing.T, ceiling int64) *uploadFixture {
	t.Helper()
	repo := newUploadRepo()
	media := newProductMediaStub()
	cfg := appinterface.Config{PictureMaxBytes: ceiling}
	service := productimplement.New(productimplement.Service{
		Products: repo,
		Media:    media,
		Tx:       httpUnitOfWork{},
		Config:   cfg,
		Audit:    nil,
		Mapper:   mapper.New(),
	})
	handler := New(service, cfg, testLogger)
	root := chi.NewRouter()
	root.Mount(adminProductsPath, handler.AdminRouter(maintenanceHooks(nil)))
	return &uploadFixture{router: root, repo: repo, media: media}
}

// postImage posts one multipart request whose "image" part carries content under
// the given file name and declared content type. Neither is trusted by the
// service: the type is decided from the bytes.
func postImage(handler http.Handler, path, filename, declaredType string, content []byte, token string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, filename))
	if declaredType != "" {
		header.Set("Content-Type", declaredType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(content); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func pngBytes() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 16)...)
}

func jpegPayload() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 16)...)
}

func webpPayload() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WEBP"), bytes.Repeat([]byte{0x00}, 16)...)
}

// FR-019: a JPEG, a PNG and a WebP are accepted and one row is stored.
func TestAcceptedImageFormatsAreStored(t *testing.T) {
	cases := map[string][]byte{
		"jpeg": jpegPayload(),
		"png":  pngBytes(),
		"webp": webpPayload(),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			f := newUploadFixture(t, 4096)
			product := f.repo.seed(t, "accepted-"+name)
			path := adminProductsPath + "/" + product.ID.String() + "/images"

			rec := postImage(f.router, path, "upload."+name, "", content, "admin-token")
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
			}
			if f.media.uploads != 1 {
				t.Fatalf("expected one upload, got %d", f.media.uploads)
			}
			if got := decodeAdminProduct(t, rec); len(got.Images) != 1 {
				t.Fatalf("expected the new picture in the answer, got %+v", got.Images)
			}
		})
	}
}

// FR-019, quickstart 10e: a PDF, a GIF and a text file are refused by their bytes
// — even under an image file name and a declared image content type — and nothing
// reaches the provider.
func TestUnsupportedUploadsAreRefusedByTheirBytes(t *testing.T) {
	cases := map[string][]byte{
		"pdf":  []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"),
		"gif":  []byte("GIF89a...."),
		"text": []byte("just some text, not an image at all"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			f := newUploadFixture(t, 4096)
			product := f.repo.seed(t, "refused-"+name)
			path := adminProductsPath + "/" + product.ID.String() + "/images"

			rec := postImage(f.router, path, "totally-an-image.png", "image/png", content, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeAdminError(t, rec)
			if body.Error.Code != "PRODUCT_IMAGE_TYPE_UNSUPPORTED" {
				t.Fatalf("expected PRODUCT_IMAGE_TYPE_UNSUPPORTED, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "image" {
				t.Fatalf("expected the detail to name image, got %+v", body.Error.Details)
			}
			if f.media.uploads != 0 {
				t.Fatalf("a refused upload must not reach the provider, got %d uploads", f.media.uploads)
			}
		})
	}
}

// FR-019, quickstart 10f: an upload over the ceiling is 413
// PRODUCT_IMAGE_TOO_LARGE and nothing reaches the provider.
func TestOversizedUploadIsRefusedWithTheSizeCode(t *testing.T) {
	f := newUploadFixture(t, 1024)
	product := f.repo.seed(t, "oversized")
	path := adminProductsPath + "/" + product.ID.String() + "/images"
	content := append(pngBytes(), bytes.Repeat([]byte{0x00}, 2048)...)

	rec := postImage(f.router, path, "big.png", "image/png", content, "admin-token")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeAdminError(t, rec)
	if body.Error.Code != "PRODUCT_IMAGE_TOO_LARGE" {
		t.Fatalf("expected PRODUCT_IMAGE_TOO_LARGE, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "image" {
		t.Fatalf("expected the detail to name image, got %+v", body.Error.Details)
	}
	if f.media.uploads != 0 {
		t.Fatalf("an oversized upload must not reach the provider, got %d uploads", f.media.uploads)
	}
}
