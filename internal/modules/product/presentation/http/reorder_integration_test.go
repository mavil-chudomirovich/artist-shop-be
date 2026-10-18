//go:build integration

package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	productconstant "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
)

// This file drives the two product reorder endpoints end-to-end against real
// PostgreSQL (specs/018-catalog-reorder US1, T009): the real migrations, the real
// product and variant adapters, the real audit writer and real access tokens. It
// proves what a fake cannot — that the single bulk UNNEST write (research D1)
// persists the complete submitted order, that the storefront and the
// administrator reads both reflect it, that the first variant after a reorder is
// the default variant (FR-014), and that every refusal leaves the stored order
// unchanged. Docker is required to run it; the file still compiles without a
// container so `go vet -tags integration` covers it.

// reorderUUIDList renders a UUID slice as the JSON array a reorder body carries.
func reorderUUIDList(ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%q", id.String()))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// reorderListBody is the member a list read answers with: the ordered records'
// identifiers and codes.
type reorderListBody struct {
	Data []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	} `json:"data"`
}

// adminProductCodes reads the administrator product list and returns the codes in
// the stored order.
func adminProductCodes(t *testing.T, f *variantMaintenanceFixture) []string {
	t.Helper()
	rec := f.call(http.MethodGet, productAdminPath, "", f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body reorderListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the admin list %q: %v", rec.Body.String(), err)
	}
	out := make([]string, 0, len(body.Data))
	for _, entry := range body.Data {
		out = append(out, entry.Code)
	}
	return out
}

// publicProductCodes reads the customer-facing catalogue and returns the codes in
// the stored order.
func publicProductCodes(t *testing.T, f *variantMaintenanceFixture) []string {
	t.Helper()
	rec := f.call(http.MethodGet, "/api/v1/products", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body reorderListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the public list %q: %v", rec.Body.String(), err)
	}
	out := make([]string, 0, len(body.Data))
	for _, entry := range body.Data {
		out = append(out, entry.Code)
	}
	return out
}

// createReorderProduct creates a product with one variant through the
// administrator route and returns both identifiers.
func createReorderProduct(t *testing.T, f *variantMaintenanceFixture, name, code, sku string, categoryID uuid.UUID) (string, string) {
	t.Helper()
	rec := f.call(http.MethodPost, productAdminPath, variantProductBody(name, code, sku, categoryID), f.adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	created := decodeVariantAdminDetail(t, rec)
	return created.Data.ID, created.Data.Variants[0].ID
}

// activateReorderVariant moves a variant to ACTIVE, so its product appears on the
// customer-facing catalogue.
func activateReorderVariant(t *testing.T, f *variantMaintenanceFixture, productID, variantID string) {
	t.Helper()
	rec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants/"+variantID+"/state",
		`{"to":"ACTIVE"}`, f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// addReorderVariant adds one variant to a product and returns the new variant's
// identifier. The request omits the position, so US2's auto-front placement lands
// the variant first; the variant is therefore resolved by the SKU the request
// named rather than by its index in the answer.
func addReorderVariant(t *testing.T, f *variantMaintenanceFixture, productID, sku string) string {
	t.Helper()
	rec := f.call(http.MethodPost, productAdminPath+"/"+productID+"/variants",
		fmt.Sprintf(`{"name":%q,"sku":%q}`, "Biến thể "+sku, sku), f.adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add variant: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	detail := decodeVariantAdminDetail(t, rec)
	for _, variant := range detail.Data.Variants {
		if variant.SKU == sku {
			return variant.ID
		}
	}
	t.Fatalf("the added variant %q is absent from the answer", sku)
	return ""
}

// variantIDsOf reads the administrator detail and returns the variant ids in
// stored order.
func variantIDsOf(t *testing.T, f *variantMaintenanceFixture, productID string) []string {
	t.Helper()
	rec := f.call(http.MethodGet, productAdminPath+"/"+productID, "", f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("read product: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	detail := decodeVariantAdminDetail(t, rec)
	out := make([]string, 0, len(detail.Data.Variants))
	for _, variant := range detail.Data.Variants {
		out = append(out, variant.ID)
	}
	return out
}

// Spec 018 US1 (T009) against real PostgreSQL: the product reorder stores the
// complete submitted order with one bulk statement (research D1), the
// administrator and customer-facing lists both reflect it, and every refusal
// leaves the stored order unchanged.
func TestProductReorderAgainstPostgres(t *testing.T) {
	f := newVariantMaintenanceFixture(t)
	category := seedSKUCategory(t, f.categories, "reorder-products", true)

	alphaID, alphaVariant := createReorderProduct(t, f, "Alpha", "alpha-reorder", "AL-1", category.ID)
	betaID, betaVariant := createReorderProduct(t, f, "Beta", "beta-reorder", "BE-1", category.ID)
	gammaID, gammaVariant := createReorderProduct(t, f, "Gamma", "gamma-reorder", "GA-1", category.ID)
	activateReorderVariant(t, f, alphaID, alphaVariant)
	activateReorderVariant(t, f, betaID, betaVariant)
	activateReorderVariant(t, f, gammaID, gammaVariant)

	seeded := append([]string(nil), adminProductCodes(t, f)...)
	if got := publicProductCodes(t, f); !reflect.DeepEqual(got, seeded) {
		t.Fatalf("the seeded order must match the storefront, got %v want %v", got, seeded)
	}

	// Every refusal leaves the stored order untouched (FR-004, SC-001).
	refusals := []struct {
		name string
		body string
		want int
		code string
	}{
		{"duplicate", `{"productIds":` + reorderUUIDList([]uuid.UUID{mustReorderUUID(t, gammaID), mustReorderUUID(t, alphaID), mustReorderUUID(t, gammaID)}) + `}`,
			http.StatusBadRequest, productconstant.CodeProductReorderDuplicateID},
		{"incomplete", `{"productIds":` + reorderUUIDList([]uuid.UUID{mustReorderUUID(t, alphaID), mustReorderUUID(t, betaID)}) + `}`,
			http.StatusBadRequest, "VALIDATION_ERROR"},
		{"unknown", `{"productIds":` + reorderUUIDList([]uuid.UUID{mustReorderUUID(t, alphaID), mustReorderUUID(t, betaID), uuid.New()}) + `}`,
			http.StatusNotFound, productconstant.CodeProductNotFound},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			rec := f.call(http.MethodPut, productAdminPath+"/reorder", refusal.body, f.adminToken)
			if rec.Code != refusal.want {
				t.Fatalf("expected %d, got %d (%s)", refusal.want, rec.Code, rec.Body.String())
			}
			if got := decodeVariantErrorCode(t, rec); got != refusal.code {
				t.Fatalf("expected %s, got %s", refusal.code, got)
			}
			if got := adminProductCodes(t, f); !reflect.DeepEqual(got, seeded) {
				t.Fatalf("a refused reorder must leave the order unchanged, got %v", got)
			}
		})
	}

	// The successful reorder stores the submitted order in one bulk write: the
	// affected-row count equals the submitted size (research D1).
	order := []uuid.UUID{mustReorderUUID(t, gammaID), mustReorderUUID(t, betaID), mustReorderUUID(t, alphaID)}
	rec := f.call(http.MethodPut, productAdminPath+"/reorder",
		`{"productIds":`+reorderUUIDList(order)+`}`, f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			UpdatedCount int64 `json:"updatedCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the reorder answer %q: %v", rec.Body.String(), err)
	}
	if body.Data.UpdatedCount != 3 {
		t.Fatalf("expected the bulk write to touch 3 rows, got %d", body.Data.UpdatedCount)
	}

	want := []string{"gamma-reorder", "beta-reorder", "alpha-reorder"}
	if got := adminProductCodes(t, f); !reflect.DeepEqual(got, want) {
		t.Fatalf("the administrator list must reflect the reorder, got %v", got)
	}
	if got := publicProductCodes(t, f); !reflect.DeepEqual(got, want) {
		t.Fatalf("the storefront must reflect the reorder, got %v", got)
	}

	f.requireAdminAudit(t, productconstant.AuditProductReordered, "product", "", 1)
}

// Spec 018 US1 (T009) against real PostgreSQL: a product's variant reorder stores
// the complete submitted order in one bulk write, the first variant becomes the
// default variant (FR-014), a variant of another product is refused, and a reorder
// for an unknown product answers the not-found.
func TestVariantReorderAgainstPostgres(t *testing.T) {
	f := newVariantMaintenanceFixture(t)
	category := seedSKUCategory(t, f.categories, "reorder-variants", true)

	productID, firstVariant := createReorderProduct(t, f, "Bộ ba", "bo-ba-reorder", "VB-1", category.ID)
	secondVariant := addReorderVariant(t, f, productID, "VB-2")
	thirdVariant := addReorderVariant(t, f, productID, "VB-3")

	order := []string{thirdVariant, firstVariant, secondVariant}
	body := `{"variantIds":[` + strings.Join(uuidStringsAsJSON(order), ",") + `]}`
	rec := f.call(http.MethodPut, productAdminPath+"/"+productID+"/variants/reorder", body, f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var decoded struct {
		Data struct {
			UpdatedCount int64 `json:"updatedCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode the reorder answer %q: %v", rec.Body.String(), err)
	}
	if decoded.Data.UpdatedCount != 3 {
		t.Fatalf("expected the bulk write to touch 3 rows, got %d", decoded.Data.UpdatedCount)
	}

	stored := variantIDsOf(t, f, productID)
	if !reflect.DeepEqual(stored, order) {
		t.Fatalf("the stored variant order must equal the submitted one, got %v want %v", stored, order)
	}
	if stored[0] != thirdVariant {
		t.Fatalf("the first variant after reorder must be the default, got %s", stored[0])
	}
	f.requireAdminAudit(t, productconstant.AuditProductVariantsReordered, "product", productID, 1)

	// A variant of another product is refused with the dedicated code, and the
	// stored order is unchanged (FR-008).
	_, foreignVariant := createReorderProduct(t, f, "Khác", "khac-variant-reorder", "VB-9", category.ID)
	foreign := `{"variantIds":[` + strings.Join(uuidStringsAsJSON([]string{thirdVariant, firstVariant, foreignVariant}), ",") + `]}`
	refused := f.call(http.MethodPut, productAdminPath+"/"+productID+"/variants/reorder", foreign, f.adminToken)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("foreign variant: expected 400, got %d (%s)", refused.Code, refused.Body.String())
	}
	if got := decodeVariantErrorCode(t, refused); got != productconstant.CodeVariantBelongsToOtherProduct {
		t.Fatalf("expected %s, got %s", productconstant.CodeVariantBelongsToOtherProduct, got)
	}
	if got := variantIDsOf(t, f, productID); !reflect.DeepEqual(got, order) {
		t.Fatalf("a refused reorder must leave the order unchanged, got %v", got)
	}

	// A reorder for an unknown product answers the not-found (FR-007).
	unknown := f.call(http.MethodPut, productAdminPath+"/"+uuid.NewString()+"/variants/reorder",
		`{"variantIds":[`+strings.Join(uuidStringsAsJSON([]string{firstVariant}), ",")+`]}`, f.adminToken)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown parent: expected 404, got %d (%s)", unknown.Code, unknown.Body.String())
	}
	if got := decodeVariantErrorCode(t, unknown); got != productconstant.CodeProductNotFound {
		t.Fatalf("expected %s, got %s", productconstant.CodeProductNotFound, got)
	}
}

// mustReorderUUID decodes the string identifiers the create helper returned, so a
// test can build a reorder body.
func mustReorderUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("parse the identifier %q: %v", raw, err)
	}
	return id
}

// uuidStringsAsJSON quotes each identifier string for a JSON array body.
func uuidStringsAsJSON(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, fmt.Sprintf("%q", id))
	}
	return out
}

// adminProductsListBody is the members of one administrator product list answer
// this test inspects.
type adminProductsListBody struct {
	Data []struct {
		Code string `json:"code"`
	} `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

// Spec 018 US3 (T017) against real PostgreSQL: `all=true` returns the complete
// ordered product list beyond one page.
func TestProductListAllSelectorAgainstPostgres(t *testing.T) {
	f := newVariantMaintenanceFixture(t)
	category := seedSKUCategory(t, f.categories, "list-all-products", true)

	const total = 25
	for i := 0; i < total; i++ {
		createReorderProduct(t, f, fmt.Sprintf("Sản phẩm %02d", i),
			fmt.Sprintf("batch-%02d", i), fmt.Sprintf("BA-%02d", i), category.ID)
	}

	rec := f.call(http.MethodGet, productAdminPath+"?all=true", "", f.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("all list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body adminProductsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the all list %q: %v", rec.Body.String(), err)
	}
	if len(body.Data) != total || body.Meta.Total != total {
		t.Fatalf("all=true must return the whole list, got %d of %d", len(body.Data), body.Meta.Total)
	}
}
