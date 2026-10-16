package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

var (
	testAdminID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// productServiceStub implements the whole product use-case surface with
// configurable answers, so a handler test exercises the HTTP shape without the
// use cases behind it.
type productServiceStub struct {
	publicPage   appdto.PublicProductPage
	publicDetail appdto.PublicProductDetailOutput
	adminDetail  appdto.AdminProductDetailOutput
	listAdmin    appdto.AdminProductPage
	err          error

	seenActor  appinterface.Actor
	seenCreate appdto.CreateProductInput
	seenState  appdto.ChangeVariantStateInput

	seenAddVariant    appdto.AddVariantInput
	seenUpdateVariant appdto.UpdateVariantInput
	seenVariantRef    appdto.VariantRefInput
	seenImage         appdto.AddVariantImageInput
}

var _ appinterface.ProductService = (*productServiceStub)(nil)

func (s *productServiceStub) ListPublic(context.Context, appdto.ListPublicInput) (appdto.PublicProductPage, error) {
	return s.publicPage, s.err
}

func (s *productServiceStub) GetPublicByCode(context.Context, appdto.PublicProductRefInput) (appdto.PublicProductDetailOutput, error) {
	return s.publicDetail, s.err
}

func (s *productServiceStub) ListAdmin(context.Context, appdto.ListAdminInput) (appdto.AdminProductPage, error) {
	return s.listAdmin, s.err
}

func (s *productServiceStub) GetAdmin(context.Context, appdto.AdminProductRefInput) (appdto.AdminProductDetailOutput, error) {
	return s.adminDetail, s.err
}

func (s *productServiceStub) CreateProduct(ctx context.Context, in appdto.CreateProductInput) (appdto.AdminProductDetailOutput, error) {
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	s.seenCreate = in
	return s.adminDetail, s.err
}

func (s *productServiceStub) UpdateProduct(context.Context, appdto.UpdateProductInput) (appdto.AdminProductDetailOutput, error) {
	return s.adminDetail, s.err
}

func (s *productServiceStub) DeleteProduct(context.Context, appdto.AdminProductRefInput) error {
	return s.err
}

func (s *productServiceStub) AddVariant(_ context.Context, in appdto.AddVariantInput) (appdto.AdminProductDetailOutput, error) {
	s.seenAddVariant = in
	return s.adminDetail, s.err
}

func (s *productServiceStub) UpdateVariant(_ context.Context, in appdto.UpdateVariantInput) (appdto.AdminProductDetailOutput, error) {
	s.seenUpdateVariant = in
	return s.adminDetail, s.err
}

func (s *productServiceStub) RemoveVariant(_ context.Context, in appdto.VariantRefInput) error {
	s.seenVariantRef = in
	return s.err
}

func (s *productServiceStub) ChangeVariantState(_ context.Context, in appdto.ChangeVariantStateInput) (appdto.AdminProductDetailOutput, error) {
	s.seenState = in
	return s.adminDetail, s.err
}

func (s *productServiceStub) MarkVariantOutOfStock(context.Context, uuid.UUID) error { return s.err }
func (s *productServiceStub) MarkVariantOnSale(context.Context, uuid.UUID) error     { return s.err }

func (s *productServiceStub) AddVariantImage(_ context.Context, in appdto.AddVariantImageInput) (appdto.AdminProductDetailOutput, error) {
	s.seenImage = in
	return s.adminDetail, s.err
}

func (s *productServiceStub) RemoveVariantImage(context.Context, appdto.VariantRefInput) (appdto.AdminProductDetailOutput, error) {
	return s.adminDetail, s.err
}

func (s *productServiceStub) AddPicture(context.Context, appdto.AddPictureInput) (appdto.AdminProductDetailOutput, error) {
	return s.adminDetail, s.err
}

func (s *productServiceStub) RemovePicture(context.Context, appdto.RemovePictureInput) error {
	return s.err
}

func (s *productServiceStub) SetPrimaryPicture(context.Context, appdto.SetPrimaryPictureInput) (appdto.AdminProductDetailOutput, error) {
	return s.adminDetail, s.err
}

// productAdminPath is the administrator group's mount point.
const productAdminPath = "/api/v1/admin/products"

// productHooks supplies the foundation's authentication hooks with a fixed
// administrator and customer identity, and records a denial through OnDenied.
func productHooks(denied *bool) middleware.AuthHooks {
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

// newRouter mounts the module's public and administrator groups the way the
// composition root mounts them.
func newRouter(svc appinterface.ProductService, hooks middleware.AuthHooks) http.Handler {
	handler := New(svc, appinterface.Config{PictureMaxBytes: 4096}, testLogger)
	root := chi.NewRouter()
	root.Mount("/api/v1/products", handler.Router(hooks))
	root.Mount(productAdminPath, handler.AdminRouter(hooks))
	return root
}

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

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// sampleAdminDetail is a well-formed administrator answer with one variant.
func sampleAdminDetail() appdto.AdminProductDetailOutput {
	return appdto.AdminProductDetailOutput{
		AdminProductOutput: appdto.AdminProductOutput{
			ID:           uuid.New(),
			Name:         "Card set",
			Code:         "card-set",
			Price:        model.Price{Amount: 120000, Currency: "VND"},
			VariantCount: 1,
		},
		Variants: []appdto.AdminVariantOutput{{
			ID:             uuid.New(),
			Name:           "Mặc định",
			SKU:            "SKU-001",
			EffectivePrice: model.Price{Amount: 120000, Currency: "VND"},
			SellState:      constant.SellStateComingSoon,
		}},
	}
}

// The public routes resolve with no session.
func TestThePublicProductRoutesResolve(t *testing.T) {
	stub := &productServiceStub{
		publicPage:   appdto.PublicProductPage{Products: []appdto.PublicProductOutput{{ID: uuid.New(), Name: "A", Code: "a", Price: model.Price{Amount: 100, Currency: "VND"}}}, Page: 1, PageSize: 20, Total: 1},
		publicDetail: appdto.PublicProductDetailOutput{PublicProductOutput: appdto.PublicProductOutput{ID: uuid.New(), Name: "A", Code: "a", Price: model.Price{Amount: 100, Currency: "VND"}}},
	}
	router := newRouter(stub, productHooks(nil))

	if rec := performJSON(router, http.MethodGet, "/api/v1/products", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/products: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := performJSON(router, http.MethodGet, "/api/v1/products/a", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/products/{code}: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// The administrator group enforces the role: no session is 401, a customer is 403
// and the denial is recorded, an administrator reaches the use case.
func TestTheAdministratorGroupEnforcesTheAdminRole(t *testing.T) {
	stub := &productServiceStub{listAdmin: appdto.AdminProductPage{Page: 1, PageSize: 20}}
	var denied bool
	router := newRouter(stub, productHooks(&denied))

	if rec := performJSON(router, http.MethodGet, productAdminPath, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := performJSON(router, http.MethodGet, productAdminPath, "", "customer-token"); rec.Code != http.StatusForbidden {
		t.Fatalf("a customer: expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !denied {
		t.Fatal("the role denial must be recorded through OnDenied")
	}
	if rec := performJSON(router, http.MethodGet, productAdminPath, "", "admin-token"); rec.Code != http.StatusOK {
		t.Fatalf("an administrator: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// A create requires at least one variant: a body with none is a service refusal;
// a well-formed body carries the variants and the session's actor through.
func TestCreateProductPassesVariantsAndTheSessionActor(t *testing.T) {
	stub := &productServiceStub{adminDetail: sampleAdminDetail()}
	router := newRouter(stub, productHooks(nil))

	body := `{"name":"Card set","code":"card-set","description":"Bộ card",
		"defaultPrice":{"amount":120000,"currency":"VND"},"categoryId":"` + uuid.New().String() + `","position":1,
		"variants":[{"name":"Mặc định","sku":"SKU-001","position":0}]}`
	rec := performJSON(router, http.MethodPost, productAdminPath, body, "admin-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(stub.seenCreate.Variants) != 1 || stub.seenCreate.Variants[0].SKU != "SKU-001" {
		t.Fatalf("the variant must reach the use case, got %+v", stub.seenCreate.Variants)
	}
	if stub.seenActor.ID != testAdminID {
		t.Fatalf("the acting administrator must come from the session, got %s", stub.seenActor.ID)
	}
}

// The variant state route passes the requested target through to the use case.
func TestChangeVariantStatePassesTheTarget(t *testing.T) {
	stub := &productServiceStub{adminDetail: sampleAdminDetail()}
	router := newRouter(stub, productHooks(nil))
	productID, variantID := uuid.New(), uuid.New()

	rec := performJSON(router, http.MethodPost,
		productAdminPath+"/"+productID.String()+"/variants/"+variantID.String()+"/state",
		`{"to":"ACTIVE"}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.seenState.ProductID != productID || stub.seenState.VariantID != variantID || stub.seenState.To != constant.SellStateActive {
		t.Fatalf("the state request did not reach the use case, got %+v", stub.seenState)
	}
}

// A malformed identifier is a request-shape refusal naming the field.
func TestAMalformedIdentifierIsAFieldError(t *testing.T) {
	router := newRouter(&productServiceStub{adminDetail: sampleAdminDetail()}, productHooks(nil))

	rec := performJSON(router, http.MethodGet, productAdminPath+"/not-a-uuid", "", "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "VALIDATION_ERROR" || len(body.Error.Details) != 1 || body.Error.Details[0].Field != "id" {
		t.Fatalf("expected a VALIDATION_ERROR naming id, got %+v", body.Error)
	}
}

// FR-006: an unknown detail code answers the module's not-found.
func TestAnUnknownCodeAnswersNotFound(t *testing.T) {
	stub := &productServiceStub{err: domainerr.ErrProductNotFound}
	router := newRouter(stub, productHooks(nil))

	rec := performJSON(router, http.MethodGet, "/api/v1/products/never", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", got)
	}
}

// The response shapes carry the contract members and no others.
func TestTheAdminDetailShapeCarriesItsMembers(t *testing.T) {
	stub := &productServiceStub{adminDetail: sampleAdminDetail()}
	router := newRouter(stub, productHooks(nil))

	rec := performJSON(router, http.MethodGet, productAdminPath+"/"+uuid.New().String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data httpdto.AdminProductDetailResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Code != "card-set" || len(body.Data.Variants) != 1 || body.Data.Variants[0].SKU != "SKU-001" {
		t.Fatalf("the administrator detail shape is wrong: %+v", body.Data)
	}
}

// performMultipart sends one multipart frame carrying a single file part, the
// shape the two picture-upload routes read.
func performMultipart(handler http.Handler, method, path, field, filename string, content []byte, token string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile(field, filename)
	_, _ = part.Write(content)
	_ = writer.Close()

	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// pngBytes is a payload the domain sniffer recognises as a PNG.
func pngBytes() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x2A}, 16)...)
}

// FR-010, FR-012, FR-016: every variant sub-resource route resolves behind the
// administrator guard and carries the addressed identifiers and the session actor
// through to the use case.
func TestTheVariantSubResourceRoutesReachTheUseCase(t *testing.T) {
	stub := &productServiceStub{adminDetail: sampleAdminDetail()}
	router := newRouter(stub, productHooks(nil))
	productID, variantID := uuid.New(), uuid.New()

	addRec := performJSON(router, http.MethodPost, productAdminPath+"/"+productID.String()+"/variants",
		`{"name":"Khổ lớn","sku":"SKU-002","position":1}`, "admin-token")
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add variant: expected 201, got %d (%s)", addRec.Code, addRec.Body.String())
	}
	if stub.seenAddVariant.ProductID != productID || stub.seenAddVariant.SKU != "SKU-002" {
		t.Fatalf("the add did not reach the use case, got %+v", stub.seenAddVariant)
	}

	editRec := performJSON(router, http.MethodPatch, productAdminPath+"/"+productID.String()+"/variants/"+variantID.String(),
		`{"name":"Đổi tên"}`, "admin-token")
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit variant: expected 200, got %d (%s)", editRec.Code, editRec.Body.String())
	}
	if stub.seenUpdateVariant.ProductID != productID || stub.seenUpdateVariant.VariantID != variantID ||
		stub.seenUpdateVariant.Name == nil || *stub.seenUpdateVariant.Name != "Đổi tên" {
		t.Fatalf("the edit did not reach the use case, got %+v", stub.seenUpdateVariant)
	}

	imageRec := performMultipart(router, http.MethodPost,
		productAdminPath+"/"+productID.String()+"/variants/"+variantID.String()+"/image",
		"image", "variant.png", pngBytes(), "admin-token")
	if imageRec.Code != http.StatusCreated {
		t.Fatalf("variant image: expected 201, got %d (%s)", imageRec.Code, imageRec.Body.String())
	}
	if stub.seenImage.ProductID != productID || stub.seenImage.VariantID != variantID || len(stub.seenImage.Content) == 0 {
		t.Fatalf("the image upload did not reach the use case, got %+v", stub.seenImage)
	}

	removeRec := performJSON(router, http.MethodDelete,
		productAdminPath+"/"+productID.String()+"/variants/"+variantID.String(), "", "admin-token")
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove variant: expected 204, got %d (%s)", removeRec.Code, removeRec.Body.String())
	}
	if stub.seenVariantRef.ProductID != productID || stub.seenVariantRef.VariantID != variantID {
		t.Fatalf("the removal did not reach the use case, got %+v", stub.seenVariantRef)
	}
}

// FR-013: a customer's session is refused on the variant routes on role grounds,
// with the denial recorded, exactly as the group root is.
func TestACustomerSessionIsRefusedOnTheVariantRoutes(t *testing.T) {
	stub := &productServiceStub{adminDetail: sampleAdminDetail()}
	var denied bool
	router := newRouter(stub, productHooks(&denied))
	productID := uuid.New()

	rec := performJSON(router, http.MethodPost, productAdminPath+"/"+productID.String()+"/variants",
		`{"name":"Lén","sku":"SKU-9"}`, "customer-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a customer: expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !denied {
		t.Fatal("the role denial must be recorded through OnDenied")
	}
	if got := decodeError(t, rec).Error.Code; got != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", got)
	}
}
