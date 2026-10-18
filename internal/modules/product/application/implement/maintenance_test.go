package implement

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// This file exercises the administrator maintenance use cases over the in-memory
// repository and a recording auditor. Every write must record its audit action and
// the actor used in the audit row must be the one the caller put in the context,
// never a value drawn from the request.

func adminFixture(t *testing.T) (*memoryProducts, *recordingAuditor, appinterface.Actor, uuid.UUID) {
	t.Helper()
	repo := newMemoryProducts()
	recorder := &recordingAuditor{}
	category := uuid.New()
	repo.state.categories = map[uuid.UUID]bool{category: true}
	return repo, recorder, adminActor(), category
}

// FR-010, FR-012: create stores a product together with its variant and records
// PRODUCT_CREATED.
func TestCreateProductStoresItsVariantAndAudits(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)

	out, err := maintenanceService(repo, recorder).CreateProduct(adminContext(actor), dto.CreateProductInput{
		Name:       "  Acrylic stand  ",
		Code:       "acrylic-stand",
		Price:      price(120000),
		CategoryID: category,
		Position:   ptr(10),
		Variants:   []dto.CreateVariantInput{{Name: "Mặc định", SKU: "SKU-001", Position: ptr(0)}},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.Name != "Acrylic stand" || out.Code != "acrylic-stand" || out.Price.Amount != 120000 {
		t.Fatalf("the created product does not carry the stored values: %+v", out)
	}
	if out.VariantCount != 1 || len(out.Variants) != 1 || out.Variants[0].SKU != "SKU-001" {
		t.Fatalf("the created product must carry its variant: %+v", out.Variants)
	}
	if out.Variants[0].SellState != constant.SellStateComingSoon {
		t.Fatalf("a new variant must start in COMING_SOON, got %q", out.Variants[0].SellState)
	}
	if got := len(repo.variantsFor(out.ID)); got != 1 {
		t.Fatalf("expected one stored variant, got %d", got)
	}
	requireAudited(t, recorder, constant.AuditProductCreated, actor, "product", out.ID)
}

// FR-012, research D14: a product with no variant is refused naming the field and
// nothing is written.
func TestCreateProductWithoutAVariantIsRefused(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)

	_, err := maintenanceService(repo, recorder).CreateProduct(adminContext(actor), dto.CreateProductInput{
		Name: "No variant", Code: "no-variant", Price: price(100), CategoryID: category,
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldVariants {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldVariants, err)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused create must record nothing, got %v", recorder.actions())
	}
}

// FR-011: a duplicate product code is refused; a duplicate SKU is refused.
func TestDuplicateCodeAndSKUAreRefused(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	svc := maintenanceService(repo, recorder)
	ctx := adminContext(actor)

	if _, err := svc.CreateProduct(ctx, dto.CreateProductInput{
		Name: "First", Code: "p-1", Price: price(100), CategoryID: category,
		Variants: []dto.CreateVariantInput{{Name: "A", SKU: "SKU-1"}},
	}); err != nil {
		t.Fatalf("CreateProduct first: %v", err)
	}
	if _, err := svc.CreateProduct(ctx, dto.CreateProductInput{
		Name: "Second", Code: "p-1", Price: price(100), CategoryID: category,
		Variants: []dto.CreateVariantInput{{Name: "B", SKU: "SKU-2"}},
	}); !errors.Is(err, domainerr.ErrProductCodeTaken) {
		t.Fatalf("expected ErrProductCodeTaken, got %v", err)
	}
	if _, err := svc.CreateProduct(ctx, dto.CreateProductInput{
		Name: "Third", Code: "p-3", Price: price(100), CategoryID: category,
		Variants: []dto.CreateVariantInput{{Name: "C", SKU: "SKU-1"}},
	}); !errors.Is(err, domainerr.ErrVariantSKUTaken) {
		t.Fatalf("expected ErrVariantSKUTaken, got %v", err)
	}
}

// FR-010: an edit changes the fields it carries and leaves the rest.
func TestUpdateProductChangesCarriedFields(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	seeded := repo.seedWithVariants(t, productDraft("Cũ", "cu", 100, category, 3), nil, time.Now().UTC().Add(-time.Hour))

	name := "Mới"
	newPrice := price(555)
	out, err := maintenanceService(repo, recorder).UpdateProduct(adminContext(actor), dto.UpdateProductInput{
		ID: seeded.ID, Name: &name, Price: &newPrice,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if out.Name != name || out.Price.Amount != 555 {
		t.Fatalf("the edit did not change the fields it carried: %+v", out)
	}
	if out.Code != "cu" || out.Position != 3 {
		t.Fatalf("the edit changed a field it did not carry: %+v", out)
	}
	requireAudited(t, recorder, constant.AuditProductUpdated, actor, "product", seeded.ID)
}

// FR-010: removing a product cascades to its variants and records PRODUCT_DELETED.
func TestDeleteProductRemovesItsVariants(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	seeded := repo.seedWithVariants(t, productDraft("Alpha", "alpha", 100, category, 1), nil, time.Now().UTC().Add(-time.Hour))

	if err := maintenanceService(repo, recorder).DeleteProduct(adminContext(actor), dto.AdminProductRefInput{ID: seeded.ID}); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
	if _, err := repo.FindByID(context.Background(), seeded.ID); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("the removed product must be gone, got %v", err)
	}
	requireAudited(t, recorder, constant.AuditProductDeleted, actor, "product", seeded.ID)
}

// FR-014: adding a variant records PRODUCT_VARIANT_ADDED.
func TestAddVariantAudits(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	seeded := repo.seedWithVariants(t, productDraft("Alpha", "alpha", 100, category, 1), nil, time.Now().UTC().Add(-time.Hour))

	out, err := maintenanceService(repo, recorder).AddVariant(adminContext(actor), dto.AddVariantInput{
		ProductID: seeded.ID, Name: "Khổ lớn", SKU: "SKU-L", Position: ptr(1),
	})
	if err != nil {
		t.Fatalf("AddVariant: %v", err)
	}
	if len(out.Variants) != 2 {
		t.Fatalf("expected two variants, got %d", len(out.Variants))
	}
	requireAudited(t, recorder, constant.AuditProductVariantAdded, actor, "product_variant", out.Variants[1].ID)
}

// FR-014: editing a variant changes the carried fields and records
// PRODUCT_VARIANT_UPDATED; clearing the price falls back to the product's default.
func TestUpdateVariantChangesFieldsAndRecords(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Alpha", Code: "alpha", Price: price(100000), CategoryID: category,
		Variants: []model.VariantDraft{{Name: "Old", SKU: "SKU-1", PriceAmount: ptr[int64](120000), Position: 0}},
	}, nil, time.Now().UTC().Add(-time.Hour))
	variant := repo.variantsFor(product.ID)[0]

	name := "New"
	out, err := maintenanceService(repo, recorder).UpdateVariant(adminContext(actor), dto.UpdateVariantInput{
		ProductID: product.ID, VariantID: variant.ID, Name: &name, PriceAmountSet: true,
	})
	if err != nil {
		t.Fatalf("UpdateVariant: %v", err)
	}
	if out.Variants[0].Name != "New" {
		t.Fatalf("the edit was not applied: %+v", out.Variants[0])
	}
	if out.Variants[0].PriceAmount != nil || out.Variants[0].EffectivePrice.Amount != 100000 {
		t.Fatalf("clearing the price must fall back to the product's default, got %+v", out.Variants[0])
	}
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
}

// FR-012: removing the product's last variant is refused; removing one of several
// succeeds.
func TestRemoveVariantRefusesTheLastOne(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	single := repo.seedWithVariants(t, productDraft("One", "one", 100, category, 1), nil, time.Now().UTC().Add(-time.Hour))
	singleVariant := repo.variantsFor(single.ID)[0]

	svc := maintenanceService(repo, recorder)
	if err := svc.RemoveVariant(adminContext(actor), dto.VariantRefInput{ProductID: single.ID, VariantID: singleVariant.ID}); !errors.Is(err, domainerr.ErrVariantLastRemoval) {
		t.Fatalf("expected ErrVariantLastRemoval, got %v", err)
	}

	two := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Two", Code: "two", Price: price(100), CategoryID: category,
		Variants: []model.VariantDraft{{Name: "A", SKU: "A", Position: 0}, {Name: "B", SKU: "B", Position: 1}},
	}, nil, time.Now().UTC().Add(-time.Minute))
	variants := repo.variantsFor(two.ID)
	if err := svc.RemoveVariant(adminContext(actor), dto.VariantRefInput{ProductID: two.ID, VariantID: variants[1].ID}); err != nil {
		t.Fatalf("RemoveVariant: %v", err)
	}
	if got := len(repo.variantsFor(two.ID)); got != 1 {
		t.Fatalf("expected one variant left, got %d", got)
	}
	requireAudited(t, recorder, constant.AuditProductVariantRemoved, actor, "product_variant", variants[1].ID)
}

// FR-015: a legal transition moves the variant; an illegal one is refused naming
// the current state; an unknown target names the field.
func TestChangeVariantState(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product := repo.seedWithVariants(t, productDraft("Alpha", "alpha", 100, category, 1), nil, time.Now().UTC().Add(-time.Hour))
	variant := repo.variantsFor(product.ID)[0]
	svc := maintenanceService(repo, recorder)
	ctx := adminContext(actor)

	out, err := svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateActive})
	if err != nil {
		t.Fatalf("ChangeVariantState: %v", err)
	}
	if out.Variants[0].SellState != constant.SellStateActive {
		t.Fatalf("expected ACTIVE, got %q", out.Variants[0].SellState)
	}
	// A state move is a mutation, so it carries its own audit event naming the
	// variant as the target (FR-014).
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)

	_, err = svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateComingSoon})
	if !errors.Is(err, domainerr.ErrVariantStateTransitionInvalid) {
		t.Fatalf("expected ErrVariantStateTransitionInvalid, got %v", err)
	}
	var transition *domainerr.VariantStateTransitionError
	if !errors.As(err, &transition) || transition.Current != constant.SellStateActive {
		t.Fatalf("the refusal must name the current state, got %v", err)
	}

	_, err = svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{ProductID: product.ID, VariantID: variant.ID, To: constant.SellState("ON_SALE")})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid for an unknown target, got %v", err)
	}
}

// FR-015: the availability signals move only the edge they own and are tolerant
// no-ops otherwise, with no human actor recorded.
func TestVariantAvailabilitySignals(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Alpha", Code: "alpha", Price: price(100), CategoryID: category,
		Variants: []model.VariantDraft{{Name: "Active", SKU: "A", Position: 0}, {Name: "Announced", SKU: "B", Position: 1}},
	}, nil, time.Now().UTC().Add(-time.Hour))
	variants := repo.variantsFor(product.ID)
	active, announced := variants[0], variants[1]
	repo.launch(t, active.ID)

	svc := maintenanceService(repo, recorder)
	ctx := adminContext(actor)

	if err := svc.MarkVariantOutOfStock(ctx, active.ID); err != nil {
		t.Fatalf("MarkVariantOutOfStock: %v", err)
	}
	if got := repo.variantBySKU(t, product.ID, "A").SellState; got != constant.SellStateOutOfStock {
		t.Fatalf("stored state = %q, want OUT_OF_STOCK", got)
	}
	// An announced variant is untouched by the signal.
	if err := svc.MarkVariantOutOfStock(ctx, announced.ID); err != nil {
		t.Fatalf("MarkVariantOutOfStock on an announced variant: %v", err)
	}
	if got := repo.variantBySKU(t, product.ID, "B").SellState; got != constant.SellStateComingSoon {
		t.Fatalf("an announced variant must not move, got %q", got)
	}
	// An out-of-stock variant returns to sale.
	if err := svc.MarkVariantOnSale(ctx, active.ID); err != nil {
		t.Fatalf("MarkVariantOnSale: %v", err)
	}
	if got := repo.variantBySKU(t, product.ID, "A").SellState; got != constant.SellStateActive {
		t.Fatalf("stored state = %q, want ACTIVE", got)
	}
	// An unknown variant is the module's not-found.
	if err := svc.MarkVariantOutOfStock(ctx, uuid.New()); !errors.Is(err, domainerr.ErrVariantNotFound) {
		t.Fatalf("expected ErrVariantNotFound, got %v", err)
	}
}

// --- Product visibility-flag hide guard (specs/016-hide-guard US3, T018) ---
//
// Turning a product's own flag off checks ALL of the product's variants against
// PENDING and PAYMENT_PENDING orders inside the use case's transaction; a
// refusal names the blocking orders and leaves everything untouched, while a
// successful hide removes every variant from every cart in the same transaction
// and carries the removed line count in its audit event metadata. Turning the
// flag back on, or an edit that does not touch the flag, never runs the guard.

// seedTwoVariantProduct stores a visible product with two variants, the unit
// the flag-guard tests below share.
func seedTwoVariantProduct(t *testing.T, repo *memoryProducts, category uuid.UUID) (*model.Product, model.Variant, model.Variant) {
	t.Helper()
	product := repo.seedWithVariants(t, model.ProductDraft{
		Name: "Card set", Code: "card-set", Price: price(150000), CategoryID: category, Position: 1,
		Variants: []model.VariantDraft{
			{Name: "Small", SKU: "CARD-2-S", Position: 0},
			{Name: "Large", SKU: "CARD-2-L", Position: 1},
		},
	}, nil, time.Now().UTC().Add(-time.Hour))
	variants := repo.variantsFor(product.ID)
	if len(variants) != 2 {
		t.Fatalf("expected two seeded variants, got %d", len(variants))
	}
	return product, variants[0], variants[1]
}

// hideProductEvent returns the product hide audit event carrying the
// cart-cleanup count, failing when none was recorded. A combined patch also
// records a plain content-edit event under the same action, so the lookup
// selects the one carrying the removed line count.
func hideProductEvent(t *testing.T, recorder *recordingAuditor, productID uuid.UUID) recordedEvent {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action != constant.AuditProductUpdated || event.targetID != productID.String() {
			continue
		}
		if _, ok := event.metadata["removedCartLines"]; ok {
			return event
		}
	}
	t.Fatalf("the hide must emit %s carrying the removed cart-line count, got %v",
		constant.AuditProductUpdated, recorder.actions())
	return recordedEvent{}
}

// FR-006, SC-001 (specs/016-hide-guard US3): turning the flag off while a
// PENDING order holds one of the product's two variants is refused with the
// blocking order named, and the product is byte-for-byte unchanged — including
// a content change the same patch carries — while carts are untouched.
func TestGuardedProductFlagOffRefusedWhilePending(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, v1, v2 := seedTwoVariantProduct(t, repo, category)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{v1.ID}},
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{v2.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)
	ctx := adminContext(actor)

	before, err := repo.FindByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("read the seeded product: %v", err)
	}

	hidden := false
	name := "Tranh mới"
	out, err := svc.UpdateProduct(ctx, dto.UpdateProductInput{ID: product.ID, Name: &name, IsVisible: &hidden})
	if !errors.Is(err, domainerr.ErrProductHasPendingOrders) {
		t.Fatalf("expected ErrProductHasPendingOrders, got %v", err)
	}
	if !reflect.DeepEqual(out, dto.AdminProductDetailOutput{}) {
		t.Fatalf("a refused hide must not answer a product, got %+v", out)
	}
	var pending *domainerr.ProductPendingOrdersError
	if !errors.As(err, &pending) {
		t.Fatal("the refusal must carry the blocking order identifiers")
	}
	if len(pending.OrderIDs) != 1 || pending.OrderIDs[0] != blocking {
		t.Fatalf("the refusal must name exactly the blocking order, got %v", pending.OrderIDs)
	}

	after, err := repo.FindByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("read the product after the refused hide: %v", err)
	}
	if !reflect.DeepEqual(*before, *after) {
		t.Fatalf("a refused hide must leave the product byte-for-byte unchanged: %+v -> %+v", before, after)
	}
	if carts.calls != 0 {
		t.Fatalf("a refused hide must never touch carts, got %d cleanup calls", carts.calls)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused hide must record nothing, got %v", recorder.actions())
	}
	requireGuardCheckedOnce(t, orders)
	if tx.callCount() != 1 {
		t.Fatalf("the refused hide must run inside one transaction, got %d", tx.callCount())
	}
}

// FR-006, FR-007 (specs/016-hide-guard US3): orders in PAID, SHIPPING,
// COMPLETED or CANCELLED never block; the flag turns off, both variants leave
// every cart in the same transaction, and the hide emits its audit action
// carrying the removed line count.
func TestGuardedProductFlagOffSucceedsWithOnlySettledOrdersAndCleansCarts(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, v1, v2 := seedTwoVariantProduct(t, repo, category)

	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{v1.ID}},
		{id: uuid.New(), status: "SHIPPING", variants: []uuid.UUID{v2.ID}},
		{id: uuid.New(), status: "COMPLETED", variants: []uuid.UUID{v1.ID, v2.ID}},
		{id: uuid.New(), status: "CANCELLED", variants: []uuid.UUID{v1.ID}},
	}}
	carts := &fakeVariantCartCleanup{remove: 4}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)
	ctx := adminContext(actor)

	hidden := false
	out, err := svc.UpdateProduct(ctx, dto.UpdateProductInput{ID: product.ID, IsVisible: &hidden})
	if err != nil {
		t.Fatalf("a hide covered only by settled orders must succeed, got %v", err)
	}
	if out.IsVisible {
		t.Fatal("the product must be hidden")
	}
	if out.Name != "Card set" || out.Code != "card-set" {
		t.Fatalf("hide must not destroy a member: %+v", out)
	}

	stored, err := repo.FindByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("read the hidden product: %v", err)
	}
	if stored.Product.IsVisible {
		t.Fatal("the stored row must be hidden")
	}

	requireGuardCheckedOnce(t, orders)
	if carts.calls != 1 {
		t.Fatalf("the cart cleanup must run exactly once, got %d calls", carts.calls)
	}
	for _, ran := range carts.inTx {
		if !ran {
			t.Fatal("the cart cleanup must run inside the transaction")
		}
	}
	if len(carts.inputs) != 1 {
		t.Fatalf("the cleanup must run once, got %v", carts.inputs)
	}
	cleaned := make(map[uuid.UUID]bool, len(carts.inputs[0]))
	for _, id := range carts.inputs[0] {
		cleaned[id] = true
	}
	if len(cleaned) != 2 || !cleaned[v1.ID] || !cleaned[v2.ID] {
		t.Fatalf("the cleanup must remove exactly all of the product's variants, got %v", carts.inputs[0])
	}
	if tx.callCount() != 1 {
		t.Fatalf("the hide and the cleanup must share one transaction, got %d", tx.callCount())
	}

	requireAudited(t, recorder, constant.AuditProductUpdated, actor, "product", product.ID)
	event := hideProductEvent(t, recorder, product.ID)
	if got, ok := event.metadata["removedCartLines"]; !ok || got != int64(4) {
		t.Fatalf("the hide event must carry the removed cart-line count, got %v", event.metadata)
	}
}

// Spec edge case (specs/016-hide-guard US3): turning the flag back on, or an
// edit that does not touch the flag, is never blocked and never runs the
// guard — even while a PENDING order covers the product.
func TestProductFlagOnAndContentEditsSkipTheGuard(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, v1, _ := seedTwoVariantProduct(t, repo, category)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{v1.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)
	ctx := adminContext(actor)

	// A content-only edit succeeds without the guard.
	name := "Tranh mới"
	out, err := svc.UpdateProduct(ctx, dto.UpdateProductInput{ID: product.ID, Name: &name})
	if err != nil {
		t.Fatalf("a content edit must never be guarded, got %v", err)
	}
	if out.Name != name || !out.IsVisible {
		t.Fatalf("the edit must apply without touching visibility: %+v", out)
	}
	requireNoGuardCalls(t, orders, carts)
	requireAudited(t, recorder, constant.AuditProductUpdated, actor, "product", product.ID)

	// Turning the flag back on succeeds without the guard. The product is hidden
	// first straight through the repository, because a guarded hide would be
	// refused while the pending order covers it.
	view, err := repo.FindByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("read the product: %v", err)
	}
	hidden := view.Product
	hidden.Hide(time.Now().UTC())
	if err := repo.Update(context.Background(), &hidden); err != nil {
		t.Fatalf("hide the product directly: %v", err)
	}

	shown := true
	out, err = svc.UpdateProduct(ctx, dto.UpdateProductInput{ID: product.ID, IsVisible: &shown})
	if err != nil {
		t.Fatalf("showing again must never be guarded, got %v", err)
	}
	if !out.IsVisible {
		t.Fatal("the product must be visible again")
	}
	requireNoGuardCalls(t, orders, carts)
	requireAudited(t, recorder, constant.AuditProductUpdated, actor, "product", product.ID)
}

// FR-016: adding a variant image stores the reference and auditing it; removing the
// image releases the asset.
func TestVariantImageAddAndRemove(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product := repo.seedWithVariants(t, productDraft("Alpha", "alpha", 100, category, 1), nil, time.Now().UTC().Add(-time.Hour))
	variant := repo.variantsFor(product.ID)[0]
	media := newFakeMedia()
	svc := serviceFor(repo, newFakeAvailability(), nil, recorder, media, &fauxUnitOfWork{})
	ctx := adminContext(actor)

	out, err := svc.AddVariantImage(ctx, dto.AddVariantImageInput{ProductID: product.ID, VariantID: variant.ID, Content: pngBytes()})
	if err != nil {
		t.Fatalf("AddVariantImage: %v", err)
	}
	if out.Variants[0].ImageURL == nil || *out.Variants[0].ImageURL != "https://cdn.example.test/products/one.jpg" {
		t.Fatalf("the variant must carry the stored reference, got %+v", out.Variants[0])
	}
	// Attaching and clearing an image are mutations of the variant, so each
	// carries its audit event naming the variant (FR-014, FR-016).
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
	if _, err := svc.RemoveVariantImage(ctx, dto.VariantRefInput{ProductID: product.ID, VariantID: variant.ID}); err != nil {
		t.Fatalf("RemoveVariantImage: %v", err)
	}
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
	if got := repo.variantBySKU(t, product.ID, variant.SKU); got.Image != nil {
		t.Fatalf("the image must be cleared, got %+v", got.Image)
	}
	if media.removed != 1 {
		t.Fatalf("expected the asset to be released once, got %d", media.removed)
	}
}

// --- Product reorder (specs/018-catalog-reorder US1, T008) ---
//
// A product reorder carries the complete, ordered product set in one action.
// Every refusal is checked before the bulk write, leaves the stored order
// byte-for-byte unchanged and records nothing; a successful reorder stores the
// submitted order and emits exactly one audit event whose target names the
// ordered scope and whose metadata carries the affected ids (research D2, D9).

// reorderEvent returns the one audit event of a reorder action, failing when none
// was recorded.
func reorderEvent(t *testing.T, recorder *recordingAuditor, action string) recordedEvent {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action == action {
			return event
		}
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
	return recordedEvent{}
}

// productPositions reads each product's stored position in the given order.
func productPositions(t *testing.T, repo *memoryProducts, ids []uuid.UUID) []int {
	t.Helper()
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		view, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatalf("read product %s: %v", id, err)
		}
		out = append(out, view.Product.Position)
	}
	return out
}

// seedThreeProducts stores three products in positions 1, 2, 3, the unit the
// product-reorder tests share.
func seedThreeProducts(t *testing.T, repo *memoryProducts, category uuid.UUID) (first, second, third *model.Product) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Hour)
	first = repo.seedWithVariants(t, productDraft("Alpha", "alpha", 100, category, 1), nil, base)
	second = repo.seedWithVariants(t, productDraft("Beta", "beta", 100, category, 2), nil, base.Add(time.Minute))
	third = repo.seedWithVariants(t, productDraft("Gamma", "gamma", 100, category, 3), nil, base.Add(2*time.Minute))
	return first, second, third
}

// FR-002, FR-004, FR-013: a reorder stores the submitted order, the administrator
// list reflects it, and exactly one audit event names the ordered scope with the
// affected ids in its metadata (FR-010, research D9).
func TestReorderProductsAppliesTheSubmittedOrderAndAudits(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, third := seedThreeProducts(t, repo, category)
	svc := maintenanceService(repo, recorder)
	ctx := adminContext(actor)

	order := []uuid.UUID{third.ID, first.ID, second.ID}
	out, err := svc.ReorderProducts(ctx, dto.ReorderProductsInput{ProductIDs: order})
	if err != nil {
		t.Fatalf("ReorderProducts: %v", err)
	}
	if out.UpdatedCount != 3 {
		t.Fatalf("expected 3 updated rows, got %d", out.UpdatedCount)
	}
	if got := productPositions(t, repo, order); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("the stored positions must equal the submitted order, got %v", got)
	}

	page, err := svc.ListAdmin(context.Background(), dto.ListAdminInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAdmin: %v", err)
	}
	codes := make([]string, 0, len(page.Products))
	for _, product := range page.Products {
		codes = append(codes, product.Code)
	}
	if !reflect.DeepEqual(codes, []string{"gamma", "alpha", "beta"}) {
		t.Fatalf("the administrator list must reflect the new order, got %v", codes)
	}

	event := reorderEvent(t, recorder, constant.AuditProductReordered)
	if event.outcome != "SUCCESS" {
		t.Fatalf("expected SUCCESS, got %q", event.outcome)
	}
	if event.actorID == nil || *event.actorID != actor.ID || event.actorRole != string(actor.Role) {
		t.Fatalf("the reorder must name the acting administrator, got %v/%q", event.actorID, event.actorRole)
	}
	if event.targetType != "product" || event.targetID != "" {
		t.Fatalf("a bulk reorder targets the scope with no single row, got %q/%q", event.targetType, event.targetID)
	}
	if got := event.metadata["count"]; got != 3 {
		t.Fatalf("the event metadata must carry the affected count, got %v", event.metadata)
	}
	ids, ok := event.metadata["ids"].([]string)
	if !ok || len(ids) != 3 || ids[0] != third.ID.String() {
		t.Fatalf("the event metadata must carry the affected ids in order, got %v", event.metadata)
	}
}

// FR-005: an empty array is a validation error naming the member.
func TestReorderProductsRefusesAnEmptyArray(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, _, _ := seedThreeProducts(t, repo, category)

	_, err := maintenanceService(repo, recorder).ReorderProducts(adminContext(actor), dto.ReorderProductsInput{})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "productIds" {
		t.Fatalf("expected the field productIds to be named, got %v", err)
	}
	if got := productPositions(t, repo, []uuid.UUID{first.ID}); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("a refused reorder must leave the stored order unchanged, got %v", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused reorder must record nothing, got %v", recorder.actions())
	}
}

// FR-006: a repeated identifier answers the dedicated duplicate code.
func TestReorderProductsRefusesADuplicateID(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, _ := seedThreeProducts(t, repo, category)

	_, err := maintenanceService(repo, recorder).ReorderProducts(adminContext(actor), dto.ReorderProductsInput{
		ProductIDs: []uuid.UUID{first.ID, second.ID, second.ID},
	})
	if !errors.Is(err, domainerr.ErrProductReorderDuplicateID) {
		t.Fatalf("expected ErrProductReorderDuplicateID, got %v", err)
	}
	if got := productPositions(t, repo, []uuid.UUID{first.ID, second.ID}); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("a refused reorder must leave the stored order unchanged, got %v", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused reorder must record nothing, got %v", recorder.actions())
	}
}

// FR-005: an array that omits an existing sibling is a validation error.
func TestReorderProductsRefusesAnIncompleteArray(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, _ := seedThreeProducts(t, repo, category)

	_, err := maintenanceService(repo, recorder).ReorderProducts(adminContext(actor), dto.ReorderProductsInput{
		ProductIDs: []uuid.UUID{second.ID, first.ID},
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	if got := productPositions(t, repo, []uuid.UUID{first.ID, second.ID}); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("a refused reorder must leave the stored order unchanged, got %v", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused reorder must record nothing, got %v", recorder.actions())
	}
}

// FR-007: an identifier that names no product answers the module's not-found.
func TestReorderProductsRefusesAnUnknownID(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, _ := seedThreeProducts(t, repo, category)

	_, err := maintenanceService(repo, recorder).ReorderProducts(adminContext(actor), dto.ReorderProductsInput{
		ProductIDs: []uuid.UUID{first.ID, second.ID, uuid.New()},
	})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
	if got := productPositions(t, repo, []uuid.UUID{first.ID, second.ID}); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("a refused reorder must leave the stored order unchanged, got %v", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused reorder must record nothing, got %v", recorder.actions())
	}
}

// --- Reorder runs through the transaction port (specs/018-catalog-reorder T029) ---
//
// The sibling read, the completeness check and the bulk write share one
// transaction, so a defensive refusal after the write rolls it back and the
// stored order is left unchanged (FR-004, research D2).

// T029, FR-004: a successful product reorder runs through the UnitOfWork port, so
// the sibling read and the bulk write share one transaction.
func TestReorderProductsRunsInsideTheTransaction(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, third := seedThreeProducts(t, repo, category)
	tx := &fauxUnitOfWork{}
	svc := serviceFor(repo, newFakeAvailability(), nil, recorder, nil, tx)

	order := []uuid.UUID{third.ID, first.ID, second.ID}
	out, err := svc.ReorderProducts(adminContext(actor), dto.ReorderProductsInput{ProductIDs: order})
	if err != nil {
		t.Fatalf("ReorderProducts: %v", err)
	}
	if out.UpdatedCount != 3 {
		t.Fatalf("expected 3 updated rows, got %d", out.UpdatedCount)
	}
	if got := productPositions(t, repo, order); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("the stored positions must equal the submitted order, got %v", got)
	}
	if tx.calls != 1 {
		t.Fatalf("the reorder must run inside one transaction, got %d", tx.calls)
	}
}

// T029, FR-004, research D2: a defensive refusal after the bulk product write
// rolls it back, so the stored order is unchanged.
func TestReorderProductsRollsBackWhenTheDefensiveGuardRefuses(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	first, second, third := seedThreeProducts(t, repo, category)
	short := &shortWriteProducts{memoryProducts: repo}
	tx := &rollingBackUnitOfWork{state: repo.state}
	svc := New(Service{
		Products: short,
		Variants: repo.variants(),
		Audit:    recorder,
		Tx:       tx,
		Mapper:   mapper.New(),
	})

	before := productPositions(t, repo, []uuid.UUID{first.ID, second.ID, third.ID})

	_, err := svc.ReorderProducts(adminContext(actor), dto.ReorderProductsInput{
		ProductIDs: []uuid.UUID{third.ID, first.ID, second.ID},
	})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected the defensive refusal ErrProductNotFound, got %v", err)
	}
	if tx.callCount() != 1 {
		t.Fatalf("the reorder must run inside one transaction, got %d", tx.callCount())
	}
	if !short.wroteInTx {
		t.Fatal("the bulk write must run inside the transaction")
	}
	if after := productPositions(t, repo, []uuid.UUID{first.ID, second.ID, third.ID}); !reflect.DeepEqual(after, before) {
		t.Fatalf("a defensive refusal must roll the write back: got %v, want %v", after, before)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused reorder must record nothing, got %v", recorder.actions())
	}
}

// --- Auto-front placement on create (specs/018-catalog-reorder US2, T013) ---
//
// A product create that omits the position is placed at the front of the
// catalogue, smaller than every existing sibling; a variant whose position is
// omitted takes its index in the payload, so the first payload variant is the
// default; an explicit position is honoured; and an edit that omits the position
// keeps the record's current order (FR-011, FR-012, research D3, D6, D7).

// FR-011, research D3: a product create that omits the position places it at the
// front of the catalogue, smaller than every existing sibling.
func TestCreateProductWithoutPositionGoesToTheFront(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	sibling := repo.seedWithVariants(t, productDraft("Sibling", "sibling", 100, category, 5), nil, time.Now().UTC().Add(-time.Hour))

	out, err := maintenanceService(repo, recorder).CreateProduct(adminContext(actor), dto.CreateProductInput{
		Name:       "Mới",
		Code:       "moi",
		Price:      price(120000),
		CategoryID: category,
		Variants:   []dto.CreateVariantInput{{Name: "Mặc định", SKU: "SKU-001"}},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.Position != 4 {
		t.Fatalf("an omitted position must place the product at the front (4), got %d", out.Position)
	}
	if out.Position >= sibling.Position {
		t.Fatalf("the new product (%d) must be smaller than the sibling (%d)", out.Position, sibling.Position)
	}
	requireAudited(t, recorder, constant.AuditProductCreated, actor, "product", out.ID)
}

// FR-011, research D7: a batch create assigns the payload index to every variant
// whose position is omitted, so the first payload variant is the default.
func TestCreateProductAssignsPayloadOrderToOmittedVariants(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)

	out, err := maintenanceService(repo, recorder).CreateProduct(adminContext(actor), dto.CreateProductInput{
		Name:       "Bộ ba",
		Code:       "bo-ba",
		Price:      price(100),
		CategoryID: category,
		Variants: []dto.CreateVariantInput{
			{Name: "A", SKU: "SKU-A"},
			{Name: "B", SKU: "SKU-B"},
			{Name: "C", SKU: "SKU-C"},
		},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if len(out.Variants) != 3 {
		t.Fatalf("expected three variants, got %d", len(out.Variants))
	}
	wantSKUs := []string{"SKU-A", "SKU-B", "SKU-C"}
	for i, want := range wantSKUs {
		if out.Variants[i].SKU != want || out.Variants[i].Position != i {
			t.Fatalf("variant %d must carry position %d, got SKU %q position %d",
				i, i, out.Variants[i].SKU, out.Variants[i].Position)
		}
	}
	if out.Variants[0].SKU != "SKU-A" {
		t.Fatalf("the first payload variant must be the default, got %q", out.Variants[0].SKU)
	}
}

// FR-012: a supplied product position and a supplied variant position are
// honoured.
func TestCreateProductHonoursExplicitPositions(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)

	out, err := maintenanceService(repo, recorder).CreateProduct(adminContext(actor), dto.CreateProductInput{
		Name:       "Đặt chỗ",
		Code:       "dat-cho",
		Price:      price(100),
		CategoryID: category,
		Position:   ptr(7),
		Variants:   []dto.CreateVariantInput{{Name: "Mặc định", SKU: "SKU-007", Position: ptr(3)}},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.Position != 7 {
		t.Fatalf("an explicit product position must be honoured, got %d", out.Position)
	}
	if len(out.Variants) != 1 || out.Variants[0].Position != 3 {
		t.Fatalf("an explicit variant position must be honoured, got %+v", out.Variants)
	}
}

// FR-011: an edit that omits the position keeps the record's current order.
func TestUpdateProductWithoutPositionKeepsTheCurrentOrder(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	seeded := repo.seedWithVariants(t, productDraft("Cũ", "cu", 100, category, 3), nil, time.Now().UTC().Add(-time.Hour))

	name := "Mới"
	out, err := maintenanceService(repo, recorder).UpdateProduct(adminContext(actor), dto.UpdateProductInput{
		ID: seeded.ID, Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if out.Position != 3 {
		t.Fatalf("an edit that omits the position must keep the current order, got %d", out.Position)
	}
}

// --- Complete-list fetch via the `all` selector (specs/018-catalog-reorder US3, T017) ---
//
// `All=true` skips the page window and answers the complete ordered list, bounded
// by the safety maximum (FR-015, research D10). A catalogue beyond the maximum is
// refused with the `all` member named rather than returning an unbounded result.

// overCapProducts wraps the in-memory repository and reports a total beyond the
// safety maximum, so the refusal path is exercised without seeding 1000+ rows.
type overCapProducts struct {
	*memoryProducts
}

func (o *overCapProducts) ListAll(ctx context.Context, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	items, _, err := o.memoryProducts.ListAll(ctx, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return items, maxAdminListAll + 1, nil
}

// listAdminService wires the administrator list over any product repository, so
// the `all` refusal can be driven with one that reports a total beyond the safety
// maximum.
func listAdminService(products domainrepo.ProductRepository) *Service {
	return New(Service{Products: products, Mapper: mapper.New()})
}

// FR-015: `All=true` returns the whole ordered list with its total, not one page.
func TestListAdminAllReturnsTheWholeList(t *testing.T) {
	repo, _, _, category := adminFixture(t)
	seedThreeProducts(t, repo, category)

	page, err := listAdminService(repo).ListAdmin(context.Background(), dto.ListAdminInput{All: true})
	if err != nil {
		t.Fatalf("ListAdmin all: %v", err)
	}
	if page.Total != 3 || len(page.Products) != 3 {
		t.Fatalf("all=true must return the whole list, got %d of %d", len(page.Products), page.Total)
	}
	codes := make([]string, 0, len(page.Products))
	for _, product := range page.Products {
		codes = append(codes, product.Code)
	}
	if !reflect.DeepEqual(codes, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("all=true must preserve the stored order, got %v", codes)
	}
}

// FR-015, research D10: a catalogue beyond the safety maximum is refused, naming
// the `all` member and answering nothing.
func TestListAdminAllRefusesAListBeyondTheCap(t *testing.T) {
	repo, _, _, category := adminFixture(t)
	seedThreeProducts(t, repo, category)

	out, err := listAdminService(&overCapProducts{memoryProducts: repo}).ListAdmin(
		context.Background(), dto.ListAdminInput{All: true})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "all" {
		t.Fatalf("expected the field all to be named, got %v", err)
	}
	if len(out.Products) != 0 || out.Total != 0 {
		t.Fatalf("a refused all fetch must answer nothing, got %+v", out)
	}
}
