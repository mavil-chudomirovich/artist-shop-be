//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	productmodel "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file drives the sell-state route against real PostgreSQL: the real
// migration, the real repository adapter, the real visibility predicate and the
// real audit writer. After every step it checks both what the public surface then
// shows and what the database actually stores, because a handler that writes the
// state and validates afterwards would pass one and fail the other (FR-002,
// FR-026, SC-003, quickstart scenario 7).

// storedSellState reads the sell_state column straight from the table, so the
// response cannot be trusted to describe the row.
func storedSellState(t *testing.T, f *productMaintenanceFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT sell_state FROM products WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatalf("read the stored sell_state: %v", err)
	}
	return state
}

// changeSellState calls the transition route, returning the status, the response
// state (empty unless 200) and the raw body.
func changeSellState(t *testing.T, f *productMaintenanceFixture, id uuid.UUID, to string) (int, string, string) {
	t.Helper()
	rec := f.call(http.MethodPost, adminProductsPath+"/"+id.String()+"/state",
		`{"to":"`+to+`"}`, f.adminToken)
	state := ""
	if rec.Code == http.StatusOK {
		state = decodeAdminProduct(t, rec).SellState
	}
	return rec.Code, state, rec.Body.String()
}

// TestProductSellStateLifecycleAgainstPostgres walks one product through its
// whole selling life and asserts, after every step, that the response, the
// database column and the public list agree: launching makes it appear, selling
// out removes it, restocking brings it back, retiring removes it for good, and a
// move out of the terminal state is refused without touching the row.
func TestProductSellStateLifecycleAgainstPostgres(t *testing.T) {
	f := newProductMaintenanceFixture(t)
	category := seedCatalogueCategory(t, f.categoryRepo, "tranh-lifecycle", true)

	// Create: a new product starts announced and hidden from customers.
	createRec := f.call(http.MethodPost, adminProductsPath,
		`{"name":"Acrylic stand","slug":"acrylic-stand","price":{"amount":120000,"currency":"VND"},"categoryId":"`+category.ID.String()+`","position":10}`,
		f.adminToken)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeAdminProduct(t, createRec)
	if created.SellState != string(constant.SellStateComingSoon) {
		t.Fatalf("a created product must be COMING_SOON, got %q", created.SellState)
	}
	if got := storedSellState(t, f, created.ID); got != string(constant.SellStateComingSoon) {
		t.Fatalf("stored state = %q, want COMING_SOON", got)
	}
	if containsSlug(f.publicSlugs(t), "acrylic-stand") {
		t.Fatal("a COMING_SOON product must not appear in the public list")
	}

	// Launch: the product appears to customers.
	code, state, body := changeSellState(t, f, created.ID, "ACTIVE")
	if code != http.StatusOK || state != string(constant.SellStateActive) {
		t.Fatalf("launch: expected 200 ACTIVE, got %d %q (%s)", code, state, body)
	}
	if got := storedSellState(t, f, created.ID); got != string(constant.SellStateActive) {
		t.Fatalf("stored state after launch = %q, want ACTIVE", got)
	}
	if !containsSlug(f.publicSlugs(t), "acrylic-stand") {
		t.Fatal("an on-sale product must appear in the public list")
	}

	// Sell out: it disappears from the list and from its own route.
	code, state, body = changeSellState(t, f, created.ID, "OUT_OF_STOCK")
	if code != http.StatusOK || state != string(constant.SellStateOutOfStock) {
		t.Fatalf("sell out: expected 200 OUT_OF_STOCK, got %d %q (%s)", code, state, body)
	}
	if got := storedSellState(t, f, created.ID); got != string(constant.SellStateOutOfStock) {
		t.Fatalf("stored state after selling out = %q, want OUT_OF_STOCK", got)
	}
	if containsSlug(f.publicSlugs(t), "acrylic-stand") {
		t.Fatal("an out-of-stock product must not appear in the public list")
	}
	if rec := getProductWithRequestID(t, f.handler, publicProductsPath+"/acrylic-stand", "sold-out"); rec.Code != http.StatusNotFound {
		t.Fatalf("an out-of-stock product must answer 404, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Restock: the restock edge brings it back.
	code, state, body = changeSellState(t, f, created.ID, "ACTIVE")
	if code != http.StatusOK || state != string(constant.SellStateActive) {
		t.Fatalf("restock: expected 200 ACTIVE, got %d %q (%s)", code, state, body)
	}
	if !containsSlug(f.publicSlugs(t), "acrylic-stand") {
		t.Fatal("a restocked product must reappear in the public list")
	}

	// Retire: it leaves the catalogue for good.
	code, state, body = changeSellState(t, f, created.ID, "DISCONTINUED")
	if code != http.StatusOK || state != string(constant.SellStateDiscontinued) {
		t.Fatalf("retire: expected 200 DISCONTINUED, got %d %q (%s)", code, state, body)
	}
	if got := storedSellState(t, f, created.ID); got != string(constant.SellStateDiscontinued) {
		t.Fatalf("stored state after retiring = %q, want DISCONTINUED", got)
	}
	if containsSlug(f.publicSlugs(t), "acrylic-stand") {
		t.Fatal("a retired product must not appear in the public list")
	}

	// Terminal: a move out of DISCONTINUED is refused and nothing is written.
	code, _, body = changeSellState(t, f, created.ID, "ACTIVE")
	if code != http.StatusConflict {
		t.Fatalf("a move out of DISCONTINUED must be refused with 409, got %d (%s)", code, body)
	}
	if got := storedSellState(t, f, created.ID); got != string(constant.SellStateDiscontinued) {
		t.Fatalf("a refused move changed the row: %q", got)
	}

	// Each of the four accepted moves left its audit entry.
	f.waitForAuditRows(t, constant.AuditProductStateChanged, 4)

	// FR-002, FR-026, FR-027: a retired product is hidden by its state even if it
	// still carries the pre-order label, because the state — not the label —
	// decides whether it is served. The row is written directly, bypassing the
	// domain transitions, so the read predicate is the only thing that can hide it.
	retiredPreorder := seedCatalogueProduct(t, f.products, productmodel.ProductDraft{
		Name: "Retired pre-order", Slug: "retired-preorder", Price: productPrice(100),
		CategoryID: category.ID, Position: 99, IsPreorder: true,
	}, (*productmodel.Product).Retire)
	if got := storedSellState(t, f, retiredPreorder.ID); got != string(constant.SellStateDiscontinued) {
		t.Fatalf("stored state = %q, want DISCONTINUED", got)
	}
	if containsSlug(f.publicSlugs(t), "retired-preorder") {
		t.Fatal("a retired pre-order must not be served: the terminal state hides it whatever wrote the row")
	}
	if rec := getProductWithRequestID(t, f.handler, publicProductsPath+"/retired-preorder", "retired-preorder"); rec.Code != http.StatusNotFound {
		t.Fatalf("a retired pre-order must answer 404, got %d (%s)", rec.Code, rec.Body.String())
	}
}
