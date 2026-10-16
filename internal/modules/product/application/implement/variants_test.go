package implement

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the variant hide guard (specs/016-hide-guard US2, T013).
// Every move of a variant AWAY FROM on-sale (ACTIVE to any other sell state),
// whether operator-initiated (ChangeVariantState) or system-initiated (the
// inventory reconcile signal through MarkVariantOutOfStock), is checked against
// PENDING and PAYMENT_PENDING orders inside the use case's transaction; a move
// TO on-sale and a non-on-sale to non-on-sale move are never checked. A
// successful hide removes the variant from every cart in the same transaction
// and carries the removed line count in its audit event metadata; a blocked
// system signal is a recorded no-op that never fails the caller.

// --- Hide-guard fakes ---

// guardTxKey marks a context that is inside the fake transaction, so the
// contract fakes can record whether each call ran there.
type guardTxKey struct{}

func inGuardTx(ctx context.Context) bool {
	inTx, _ := ctx.Value(guardTxKey{}).(bool)
	return inTx
}

// fakeGuardUnitOfWork runs the function directly while marking the context, so
// the contract fakes and the repository can record whether they ran inside it.
type fakeGuardUnitOfWork struct {
	mu    sync.Mutex
	calls int
}

func (u *fakeGuardUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.mu.Lock()
	u.calls++
	u.mu.Unlock()
	return fn(context.WithValue(ctx, guardTxKey{}, true))
}

func (u *fakeGuardUnitOfWork) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

// guardOrder is one order the fake order guard knows: its status decides
// whether it blocks a hide (only PENDING and PAYMENT_PENDING block, FR-001).
type guardOrder struct {
	id       uuid.UUID
	status   string
	variants []uuid.UUID
}

// fakeVariantOrderGuard answers the pending-order lookup by filtering its
// orders to the two pending states, the same rule the storage layer enforces.
// It records every call, so a test proves the check is one bulk read.
type fakeVariantOrderGuard struct {
	mu     sync.Mutex
	calls  int
	inTx   []bool
	orders []guardOrder
}

func (f *fakeVariantOrderGuard) PendingOrdersForVariants(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.inTx = append(f.inTx, inGuardTx(ctx))
	wanted := make(map[uuid.UUID]bool, len(variantIDs))
	for _, id := range variantIDs {
		wanted[id] = true
	}
	var out []uuid.UUID
	for _, order := range f.orders {
		if order.status != "PENDING" && order.status != "PAYMENT_PENDING" {
			continue
		}
		for _, variant := range order.variants {
			if wanted[variant] {
				out = append(out, order.id)
				break
			}
		}
	}
	if out == nil {
		out = []uuid.UUID{}
	}
	return out, nil
}

// fakeVariantCartCleanup answers the bulk cart-line removal with a fixed
// removed count. It records every call and its input, so a test proves the
// cleanup runs once, with exactly the hidden variant, and never on a refused
// hide or a blocked system signal.
type fakeVariantCartCleanup struct {
	mu     sync.Mutex
	calls  int
	inTx   []bool
	inputs [][]uuid.UUID
	remove int64
}

func (f *fakeVariantCartCleanup) RemoveVariantsFromAllCarts(ctx context.Context, variantIDs []uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.inTx = append(f.inTx, inGuardTx(ctx))
	f.inputs = append(f.inputs, append([]uuid.UUID(nil), variantIDs...))
	return f.remove, nil
}

// guardedVariantService builds the service with the hide-guard dependencies
// wired to the fakes above.
func guardedVariantService(products *memoryProducts, authority *recordingAuditor,
	orders *fakeVariantOrderGuard, carts *fakeVariantCartCleanup, tx *fakeGuardUnitOfWork) *Service {
	return New(Service{
		Products:     products,
		Variants:     products.variants(),
		Availability: newFakeAvailability(),
		Audit:        authority,
		Media:        newFakeMedia(),
		Tx:           tx,
		Orders:       orders,
		Carts:        carts,
		Mapper:       mapper.New(),
	})
}

// seedActiveVariant stores a product with one variant moved to ACTIVE (on
// sale), the state every guarded move starts from.
func seedActiveVariant(t *testing.T, repo *memoryProducts, category uuid.UUID) (*model.Product, model.Variant) {
	t.Helper()
	product := repo.seedWithVariants(t, productDraft("Alpha", "alpha-"+uuid.NewString()[:8], 100, category, 1), nil,
		time.Now().UTC().Add(-time.Hour))
	variant := repo.variantsFor(product.ID)[0]
	repo.launch(t, variant.ID)
	return product, repo.variantBySKU(t, product.ID, variant.SKU)
}

// requireNoGuardCalls asserts the guard never ran: no pending-order check and
// no cart cleanup.
func requireNoGuardCalls(t *testing.T, orders *fakeVariantOrderGuard, carts *fakeVariantCartCleanup) {
	t.Helper()
	if orders.calls != 0 || carts.calls != 0 {
		t.Fatalf("the guard must not run, got order=%d cart=%d calls", orders.calls, carts.calls)
	}
}

// requireGuardCheckedOnce asserts the pending-order check ran exactly once,
// inside the transaction.
func requireGuardCheckedOnce(t *testing.T, orders *fakeVariantOrderGuard) {
	t.Helper()
	if orders.calls != 1 {
		t.Fatalf("the pending-order check must run exactly once, got %d calls", orders.calls)
	}
	for _, ran := range orders.inTx {
		if !ran {
			t.Fatal("the pending-order check must run inside the transaction")
		}
	}
}

// hideEventWithVariant returns the hide audit event for the variant, failing
// when none was recorded.
func hideEventWithVariant(t *testing.T, recorder *recordingAuditor, variantID uuid.UUID) recordedEvent {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action == constant.AuditProductVariantUpdated && event.targetID == variantID.String() {
			return event
		}
	}
	t.Fatalf("the hide must emit %s for the variant, got %v", constant.AuditProductVariantUpdated, recorder.actions())
	return recordedEvent{}
}

// FR-005, SC-001 (specs/016-hide-guard US2): moving an on-sale variant to
// OUT_OF_STOCK while a PENDING order holds it is refused with the blocking
// order named, and the variant is byte-for-byte unchanged while carts are
// untouched.
func TestGuardedChangeVariantStateRefusesActiveToOutOfStockWhilePending(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, variant := seedActiveVariant(t, repo, category)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{variant.ID}},
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)
	ctx := adminContext(actor)

	before, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the seeded variant: %v", err)
	}

	out, err := svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{
		ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateOutOfStock,
	})
	if !errors.Is(err, domainerr.ErrVariantHasPendingOrders) {
		t.Fatalf("expected ErrVariantHasPendingOrders, got %v", err)
	}
	if !reflect.DeepEqual(out, dto.AdminProductDetailOutput{}) {
		t.Fatalf("a refused hide must not answer a product, got %+v", out)
	}
	var pending *domainerr.VariantPendingOrdersError
	if !errors.As(err, &pending) {
		t.Fatal("the refusal must carry the blocking order identifiers")
	}
	if len(pending.OrderIDs) != 1 || pending.OrderIDs[0] != blocking {
		t.Fatalf("the refusal must name exactly the blocking order, got %v", pending.OrderIDs)
	}

	after, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the variant after the refused hide: %v", err)
	}
	if !reflect.DeepEqual(*before, *after) {
		t.Fatalf("a refused hide must leave the variant byte-for-byte unchanged: %+v -> %+v", before, after)
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

// FR-005 (specs/016-hide-guard US2): ACTIVE to DISCONTINUED is a move away
// from on-sale too, so a PAYMENT_PENDING order blocks it the same way.
func TestGuardedChangeVariantStateRefusesActiveToDiscontinuedWhilePaymentPending(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, variant := seedActiveVariant(t, repo, category)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PAYMENT_PENDING", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	svc := guardedVariantService(repo, recorder, orders, carts, &fakeGuardUnitOfWork{})
	ctx := adminContext(actor)

	before, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the seeded variant: %v", err)
	}

	_, err = svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{
		ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateDiscontinued,
	})
	if !errors.Is(err, domainerr.ErrVariantHasPendingOrders) {
		t.Fatalf("expected ErrVariantHasPendingOrders, got %v", err)
	}
	var pending *domainerr.VariantPendingOrdersError
	if !errors.As(err, &pending) || len(pending.OrderIDs) != 1 || pending.OrderIDs[0] != blocking {
		t.Fatalf("the refusal must name exactly the blocking order, got %v", err)
	}

	after, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the variant after the refused hide: %v", err)
	}
	if !reflect.DeepEqual(*before, *after) {
		t.Fatalf("a refused hide must leave the variant byte-for-byte unchanged: %+v -> %+v", before, after)
	}
	if carts.calls != 0 {
		t.Fatalf("a refused hide must never touch carts, got %d cleanup calls", carts.calls)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused hide must record nothing, got %v", recorder.actions())
	}
	requireGuardCheckedOnce(t, orders)
}

// FR-005, FR-007 (specs/016-hide-guard US2): orders in PAID, SHIPPED,
// COMPLETED or CANCELLED never block; the hide succeeds, every cart line
// holding the variant is removed in the same transaction, and the hide emits
// its audit action carrying the removed line count.
func TestGuardedChangeVariantStateSucceedsWithOnlySettledOrdersAndCleansCarts(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, variant := seedActiveVariant(t, repo, category)

	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{variant.ID}},
		{id: uuid.New(), status: "SHIPPED", variants: []uuid.UUID{variant.ID}},
		{id: uuid.New(), status: "COMPLETED", variants: []uuid.UUID{variant.ID}},
		{id: uuid.New(), status: "CANCELLED", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{remove: 2}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)
	ctx := adminContext(actor)

	out, err := svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{
		ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateOutOfStock,
	})
	if err != nil {
		t.Fatalf("a hide covered only by settled orders must succeed, got %v", err)
	}
	if out.Variants[0].SellState != constant.SellStateOutOfStock {
		t.Fatalf("expected OUT_OF_STOCK, got %q", out.Variants[0].SellState)
	}

	stored, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the hidden variant: %v", err)
	}
	if stored.SellState != constant.SellStateOutOfStock {
		t.Fatalf("the stored row must be out of stock, got %q", stored.SellState)
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
	if len(carts.inputs) != 1 || len(carts.inputs[0]) != 1 || carts.inputs[0][0] != variant.ID {
		t.Fatalf("the cleanup must run once with exactly the hidden variant, got %v", carts.inputs)
	}
	if tx.callCount() != 1 {
		t.Fatalf("the hide and the cleanup must share one transaction, got %d", tx.callCount())
	}

	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
	event := hideEventWithVariant(t, recorder, variant.ID)
	if got, ok := event.metadata["removedCartLines"]; !ok || got != int64(2) {
		t.Fatalf("the hide event must carry the removed cart-line count, got %v", event.metadata)
	}
}

// Spec edge case, FR-005 (specs/016-hide-guard US2): moving a variant back
// on-sale (OUT_OF_STOCK to ACTIVE) is never blocked and never runs the guard,
// even while a PENDING order covers the variant.
func TestMoveToActiveIsNeverGuarded(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product, variant := seedActiveVariant(t, repo, category)
	repo.sellOut(t, variant.ID)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	svc := guardedVariantService(repo, recorder, orders, carts, &fakeGuardUnitOfWork{})
	ctx := adminContext(actor)

	out, err := svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{
		ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateActive,
	})
	if err != nil {
		t.Fatalf("a move back on-sale must never be refused, got %v", err)
	}
	if out.Variants[0].SellState != constant.SellStateActive {
		t.Fatalf("expected ACTIVE, got %q", out.Variants[0].SellState)
	}
	requireNoGuardCalls(t, orders, carts)
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
}

// FR-005 (specs/016-hide-guard US2): COMING_SOON to DISCONTINUED is a
// non-on-sale to non-on-sale move, so it passes unguarded even while a
// PENDING order covers the variant.
func TestNonOnSaleToNonOnSalePassesUnguarded(t *testing.T) {
	repo, recorder, actor, category := adminFixture(t)
	product := repo.seedWithVariants(t, productDraft("Beta", "beta-"+uuid.NewString()[:8], 100, category, 1), nil,
		time.Now().UTC().Add(-time.Hour))
	variant := repo.variantsFor(product.ID)[0]

	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PENDING", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	svc := guardedVariantService(repo, recorder, orders, carts, &fakeGuardUnitOfWork{})
	ctx := adminContext(actor)

	out, err := svc.ChangeVariantState(ctx, dto.ChangeVariantStateInput{
		ProductID: product.ID, VariantID: variant.ID, To: constant.SellStateDiscontinued,
	})
	if err != nil {
		t.Fatalf("a non-on-sale move must pass unguarded, got %v", err)
	}
	if out.Variants[0].SellState != constant.SellStateDiscontinued {
		t.Fatalf("expected DISCONTINUED, got %q", out.Variants[0].SellState)
	}
	requireNoGuardCalls(t, orders, carts)
	requireAudited(t, recorder, constant.AuditProductVariantUpdated, actor, "product_variant", variant.ID)
}

// Spec edge case, research D5 (specs/016-hide-guard US2): a system stock-out
// transition blocked by a pending order keeps the variant on sale and records
// the blocked event with a system actor, without failing the caller — the
// stock write the signal travels with must still commit.
func TestBlockedSystemStockOutIsARecordedNoOp(t *testing.T) {
	repo, recorder, _, category := adminFixture(t)
	_, variant := seedActiveVariant(t, repo, category)

	blocking := uuid.New()
	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{remove: 5}
	svc := guardedVariantService(repo, recorder, orders, carts, &fakeGuardUnitOfWork{})

	before, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the seeded variant: %v", err)
	}

	if err := svc.MarkVariantOutOfStock(context.Background(), variant.ID); err != nil {
		t.Fatalf("a blocked system signal must never fail the stock write, got %v", err)
	}

	after, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the variant after the blocked signal: %v", err)
	}
	if !reflect.DeepEqual(*before, *after) {
		t.Fatalf("a blocked signal must leave the variant on sale and untouched: %+v -> %+v", before, after)
	}
	if after.SellState != constant.SellStateActive {
		t.Fatalf("a blocked signal must keep the on-sale state, got %q", after.SellState)
	}
	if carts.calls != 0 {
		t.Fatalf("a blocked signal must never touch carts, got %d cleanup calls", carts.calls)
	}
	requireGuardCheckedOnce(t, orders)

	event := hideEventWithVariant(t, recorder, variant.ID)
	if event.actorID != nil || event.actorRole != "" {
		t.Fatalf("the blocked signal must be recorded with a system actor, got %v/%q", event.actorID, event.actorRole)
	}
	found := false
	for _, value := range event.metadata["blockedOrderIds"].([]string) {
		if value == blocking.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("the blocked event must name the blocking orders, got %v", event.metadata)
	}
}

// Research D5 (specs/016-hide-guard US2): an unblocked system stock-out moves
// the variant out of sale, removes it from every cart in the same transaction
// and records the change with a system actor.
func TestSystemStockOutSucceedsAndCleansCarts(t *testing.T) {
	repo, recorder, _, category := adminFixture(t)
	_, variant := seedActiveVariant(t, repo, category)

	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{remove: 1}
	tx := &fakeGuardUnitOfWork{}
	svc := guardedVariantService(repo, recorder, orders, carts, tx)

	if err := svc.MarkVariantOutOfStock(context.Background(), variant.ID); err != nil {
		t.Fatalf("MarkVariantOutOfStock: %v", err)
	}
	stored, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the variant after the signal: %v", err)
	}
	if stored.SellState != constant.SellStateOutOfStock {
		t.Fatalf("stored state = %q, want OUT_OF_STOCK", stored.SellState)
	}

	requireGuardCheckedOnce(t, orders)
	if carts.calls != 1 || len(carts.inputs) != 1 || len(carts.inputs[0]) != 1 || carts.inputs[0][0] != variant.ID {
		t.Fatalf("the cleanup must run once with exactly the hidden variant, got %v", carts.inputs)
	}
	if tx.callCount() != 1 {
		t.Fatalf("the signal and the cleanup must share one transaction, got %d", tx.callCount())
	}

	event := hideEventWithVariant(t, recorder, variant.ID)
	if event.actorID != nil || event.actorRole != "" {
		t.Fatalf("the system signal must be recorded with a system actor, got %v/%q", event.actorID, event.actorRole)
	}
	if got, ok := event.metadata["removedCartLines"]; !ok || got != int64(1) {
		t.Fatalf("the signal event must carry the removed cart-line count, got %v", event.metadata)
	}
}

// Research D5 (specs/016-hide-guard US2): the on-sale signal is a move back
// on-sale, so it never passes through the guard even while a PENDING order
// covers the variant.
func TestSystemRestockIsNeverGuarded(t *testing.T) {
	repo, recorder, _, category := adminFixture(t)
	_, variant := seedActiveVariant(t, repo, category)
	repo.sellOut(t, variant.ID)

	orders := &fakeVariantOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PENDING", variants: []uuid.UUID{variant.ID}},
	}}
	carts := &fakeVariantCartCleanup{}
	svc := guardedVariantService(repo, recorder, orders, carts, &fakeGuardUnitOfWork{})

	if err := svc.MarkVariantOnSale(context.Background(), variant.ID); err != nil {
		t.Fatalf("a restock must never be refused, got %v", err)
	}
	stored, err := repo.variants().FindByID(context.Background(), variant.ID)
	if err != nil {
		t.Fatalf("read the variant after the restock: %v", err)
	}
	if stored.SellState != constant.SellStateActive {
		t.Fatalf("stored state = %q, want ACTIVE", stored.SellState)
	}
	requireNoGuardCalls(t, orders, carts)
	event := hideEventWithVariant(t, recorder, variant.ID)
	if event.actorID != nil {
		t.Fatalf("the restock must be recorded with a system actor, got %v", event.actorID)
	}
}
