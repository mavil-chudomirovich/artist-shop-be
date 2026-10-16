package implement

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file exercises the administrator maintenance use cases over the same
// in-memory repository the catalogue tests use. Every write must audit the action
// data-model.md's transition table specifies (FR-013, SC-005), and the actor used
// in the audit row must be the one the caller put in the context, never a value
// drawn from the request.

// auditSuccessOutcome is the outcome the shared writer spells for a successful
// write. It is asserted here as a literal so the use-case test does not depend on
// the audit package's constants to detect a wrong outcome.
const auditSuccessOutcome = "SUCCESS"

// recordedEvent is one call the maintenance use cases made to the auditor.
type recordedEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

// recordingAuditor captures the events the use cases emit so a test can assert
// the action, the outcome, the actor and the target of each write.
type recordingAuditor struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (a *recordingAuditor) Record(_ context.Context, action, outcome string, actorID *uuid.UUID,
	actorRole, targetType, targetID string, metadata map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, recordedEvent{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
		metadata:   metadata,
	})
}

var _ appinterface.Auditor = (*recordingAuditor)(nil)

func (a *recordingAuditor) snapshot() []recordedEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]recordedEvent, len(a.events))
	copy(out, a.events)
	return out
}

func (a *recordingAuditor) actions() []string {
	events := a.snapshot()
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.action)
	}
	return out
}

// maintenanceService builds the service the administrator tests drive.
func maintenanceService(repo *memoryCategories, authority appinterface.Auditor) *Service {
	return New(Service{Categories: repo, Audit: authority, Mapper: mapper.New()})
}

// requireAudited asserts one event of the given action was recorded, carrying the
// acting administrator as the actor and the category as the target.
func requireAudited(t *testing.T, recorder *recordingAuditor, action string, actor appinterface.Actor, targetID uuid.UUID) {
	t.Helper()
	events := recorder.snapshot()
	for _, event := range events {
		if event.action != action {
			continue
		}
		if event.outcome != auditSuccessOutcome {
			t.Fatalf("%s: expected outcome %s, got %q", action, auditSuccessOutcome, event.outcome)
		}
		if event.actorID == nil || *event.actorID != actor.ID {
			t.Fatalf("%s: expected actor %s, got %v", action, actor.ID, event.actorID)
		}
		if event.actorRole != string(actor.Role) {
			t.Fatalf("%s: expected actor role %q, got %q", action, actor.Role, event.actorRole)
		}
		if event.targetType != "category" {
			t.Fatalf("%s: expected the category as the target type, got %q", action, event.targetType)
		}
		if event.targetID != targetID.String() {
			t.Fatalf("%s: expected target %s, got %q", action, targetID, event.targetID)
		}
		return
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
}

// requireAuditCount asserts exactly want events carry the given action.
func requireAuditCount(t *testing.T, recorder *recordingAuditor, action string, want int) {
	t.Helper()
	got := 0
	for _, event := range recorder.snapshot() {
		if event.action == action {
			got++
		}
	}
	if got != want {
		t.Fatalf("expected %d %s events, got %d (all: %v)", want, action, got, recorder.actions())
	}
}

func adminActor() appinterface.Actor {
	return appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin}
}

// FR-008, FR-013: create returns the category on display and audits the change.
func TestCreateCategoryIsOnDisplayAndAudited(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)

	out, err := maintenanceService(repo, recorder).CreateCategory(ctx, dto.CreateCategoryInput{
		Name:        "  Tranh sơn dầu  ",
		Slug:        "tranh-son-dau",
		Description: "Mô tả",
		Position:    3,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if !out.IsVisible {
		t.Fatal("a newly created category must be on display (FR-008)")
	}
	if out.ID == uuid.Nil {
		t.Fatal("a created category must carry its identifier")
	}
	if out.Name != "Tranh sơn dầu" || out.Slug != "tranh-son-dau" ||
		out.Description != "Mô tả" || out.Position != 3 {
		t.Fatalf("the created category does not carry the stored values: %+v", out)
	}

	stored, err := repo.FindByID(context.Background(), out.ID)
	if err != nil {
		t.Fatalf("the created category must be persisted: %v", err)
	}
	if !stored.IsVisible || stored.Name != "Tranh sơn dầu" {
		t.Fatalf("the stored row does not match the answer: %+v", stored)
	}
	requireAudited(t, recorder, constant.AuditCategoryCreated, actor, out.ID)
}

// FR-010, FR-013: an edit changes name, slug, description and position, and
// audits the change.
func TestUpdateCategoryEditsEveryContentFieldAndAudits(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	seeded := repo.add(t, model.CategoryDraft{Name: "Cũ", Slug: "cu", Description: "Cũ", Position: 1},
		time.Now().UTC().Add(-time.Hour))

	name, slug, description, position := "Mới", "moi", "Mới mô tả", 9
	out, err := maintenanceService(repo, recorder).UpdateCategory(ctx, dto.UpdateCategoryInput{
		ID:          seeded.ID,
		Name:        &name,
		Slug:        &slug,
		Description: &description,
		Position:    &position,
	})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if out.Name != name || out.Slug != slug || out.Description != description || out.Position != position {
		t.Fatalf("the edit did not change every content field: %+v", out)
	}

	stored, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the edited category: %v", err)
	}
	if stored.Name != name || stored.Slug != slug || stored.Description != description || stored.Position != position {
		t.Fatalf("the stored row does not carry the edit: %+v", stored)
	}
	requireAudited(t, recorder, constant.AuditCategoryUpdated, actor, seeded.ID)
}

// FR-011, FR-013: hide and show move the display state without destroying
// anything, and each transition audits its own action.
func TestUpdateCategoryHidesAndShowsWithoutDestroying(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	svc := maintenanceService(repo, recorder)
	seeded := repo.add(t, model.CategoryDraft{Name: "Tranh", Slug: "tranh", Description: "Mô tả", Position: 2},
		time.Now().UTC().Add(-time.Hour))

	hidden := false
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &hidden})
	if err != nil {
		t.Fatalf("hide: %v", err)
	}
	if out.IsVisible {
		t.Fatal("hide must take the category off display")
	}
	// Nothing is destroyed: every content field survives the transition.
	if out.Name != "Tranh" || out.Slug != "tranh" || out.Description != "Mô tả" || out.Position != 2 {
		t.Fatalf("hide destroyed a member: %+v", out)
	}
	// The customer-facing catalogue no longer lists it (FR-002).
	page, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic after hide: %v", err)
	}
	if len(page.Categories) != 0 {
		t.Fatalf("a hidden category must be absent from the public list, got %+v", page.Categories)
	}
	requireAudited(t, recorder, constant.AuditCategoryHidden, actor, seeded.ID)

	shown := true
	out, err = svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &shown})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !out.IsVisible {
		t.Fatal("show must put the category back on display")
	}
	page, err = svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic after show: %v", err)
	}
	if len(page.Categories) != 1 || page.Categories[0].Slug != "tranh" {
		t.Fatalf("the shown category must reappear in its position, got %+v", page.Categories)
	}
	requireAudited(t, recorder, constant.AuditCategoryShown, actor, seeded.ID)
}

// data-model.md: hiding an already hidden category is a no-op, not an error, and
// records nothing.
func TestHidingAnAlreadyHiddenCategoryIsANoOpAndRecordsNothing(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	svc := maintenanceService(repo, recorder)
	seeded := repo.add(t, model.CategoryDraft{Name: "Tranh", Slug: "tranh"}, time.Now().UTC().Add(-time.Hour))
	repo.hide(seeded.ID)

	hidden := false
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &hidden})
	if err != nil {
		t.Fatalf("a repeated hide must not be an error, got %v", err)
	}
	if out.IsVisible {
		t.Fatal("the category must stay hidden")
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op hide must record nothing, got %v", recorder.actions())
	}
}

// FR-010, FR-020: an invalid edit is refused and leaves the stored row untouched.
func TestAnInvalidEditChangesNothing(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	svc := maintenanceService(repo, recorder)
	seeded := repo.add(t, model.CategoryDraft{Name: "Tranh", Slug: "tranh", Position: 2},
		time.Now().UTC().Add(-time.Hour))
	before, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the seeded category: %v", err)
	}

	badSlug := "Tranh Sơn Dầu"
	out, err := svc.UpdateCategory(context.Background(), dto.UpdateCategoryInput{ID: seeded.ID, Slug: &badSlug})
	if !errors.Is(err, domainerr.ErrCategoryInvalid) {
		t.Fatalf("expected ErrCategoryInvalid, got %v", err)
	}
	if out != (dto.AdminCategoryOutput{}) {
		t.Fatalf("a refused edit must not answer a category, got %+v", out)
	}
	after, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the category after the refused edit: %v", err)
	}
	if after.Slug != before.Slug || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("a refused edit changed the row: %+v -> %+v", before, after)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused edit must record nothing, got %v", recorder.actions())
	}
}

// FR-012, FR-013, FR-024: remove deletes the row, leaves no trace in the public
// catalogue, and leaves no gap in the ordering of what remains.
func TestDeleteCategoryLeavesNoTraceAndNoGapInTheOrder(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	svc := maintenanceService(repo, recorder)
	base := time.Now().UTC().Add(-time.Hour)
	first := repo.add(t, model.CategoryDraft{Name: "Alpha", Slug: "alpha", Position: 1}, base)
	middle := repo.add(t, model.CategoryDraft{Name: "Beta", Slug: "beta", Position: 2}, base.Add(time.Minute))
	last := repo.add(t, model.CategoryDraft{Name: "Gamma", Slug: "gamma", Position: 3}, base.Add(2*time.Minute))

	if err := svc.DeleteCategory(ctx, dto.AdminCategoryRefInput{ID: middle.ID}); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	if _, err := repo.FindByID(context.Background(), middle.ID); !errors.Is(err, domainerr.ErrCategoryNotFound) {
		t.Fatalf("the removed row must be gone, got %v", err)
	}
	requireAudited(t, recorder, constant.AuditCategoryDeleted, actor, middle.ID)

	page, err := svc.ListPublic(context.Background(), dto.ListPublicInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListPublic after remove: %v", err)
	}
	got := slugs(page.Categories)
	if !equalStrings(got, []string{first.Slug, last.Slug}) {
		t.Fatalf("the removal left a gap or a trace: expected [alpha gamma], got %v", got)
	}
	if page.Total != 2 {
		t.Fatalf("expected the remaining two in the count, got %d", page.Total)
	}
	// The public detail of the removed slug is the ordinary not-found.
	if _, err := svc.GetPublicBySlug(context.Background(), dto.PublicCategoryRefInput{Slug: middle.Slug}); !errors.Is(err, domainerr.ErrCategoryNotFound) {
		t.Fatalf("a removed slug must answer the same not-found, got %v", err)
	}
}

// FR-012: removing an unknown category is a not-found, not a failure — which is
// also what a second removal of the same category answers.
func TestDeleteUnknownCategoryAnswersNotFound(t *testing.T) {
	svc := maintenanceService(newMemoryCategories(), &recordingAuditor{})

	if err := svc.DeleteCategory(context.Background(), dto.AdminCategoryRefInput{ID: uuid.New()}); !errors.Is(err, domainerr.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
}

// --- Hide-guard fakes (specs/016-hide-guard US1, T009) ---
//
// The guard crosses module boundaries through the three contracts, so the unit
// tests fake all three plus the UnitOfWork. Every fake records its call count
// and whether each call ran inside the transaction, which is what proves the
// check and the cleanup stay bounded (one bulk call however many variants, spec
// SC-004) and atomic (research D3).

// guardTxKey marks a context that is inside the fake transaction.
type guardTxKey struct{}

func inGuardTx(ctx context.Context) bool {
	inTx, _ := ctx.Value(guardTxKey{}).(bool)
	return inTx
}

// fakeUnitOfWork runs the function directly while marking the context, so the
// contract fakes and the repository can record whether they ran inside it.
type fakeUnitOfWork struct {
	mu    sync.Mutex
	calls int
}

func (u *fakeUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.mu.Lock()
	u.calls++
	u.mu.Unlock()
	return fn(context.WithValue(ctx, guardTxKey{}, true))
}

func (u *fakeUnitOfWork) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

var _ appinterface.UnitOfWork = (*fakeUnitOfWork)(nil)

// fakeCategoryVariants answers the category-to-variant resolution with a fixed
// mapping. It records every call, so a test proves the resolution is one bulk
// read however many variants the category holds.
type fakeCategoryVariants struct {
	mu         sync.Mutex
	calls      int
	inTx       []bool
	byCategory map[uuid.UUID][]uuid.UUID
}

func (f *fakeCategoryVariants) VariantIDsByCategories(ctx context.Context, categoryIDs []uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.inTx = append(f.inTx, inGuardTx(ctx))
	var out []uuid.UUID
	for _, id := range categoryIDs {
		out = append(out, f.byCategory[id]...)
	}
	if out == nil {
		out = []uuid.UUID{}
	}
	return out, nil
}

var _ appinterface.ProductCategoryVariants = (*fakeCategoryVariants)(nil)

// guardOrder is one order the fake order guard knows: its state decides whether
// it blocks a hide (FR-001: only PENDING and PAYMENT_PENDING block).
type guardOrder struct {
	id       uuid.UUID
	status   string
	variants []uuid.UUID
}

// fakeOrderGuard answers the pending-order lookup by filtering its orders to the
// two pending states, the same rule the storage layer enforces. It records every
// call, so a test proves the check is one bulk read.
type fakeOrderGuard struct {
	mu     sync.Mutex
	calls  int
	inTx   []bool
	orders []guardOrder
}

func (f *fakeOrderGuard) PendingOrdersForVariants(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
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

var _ appinterface.OrderHideGuard = (*fakeOrderGuard)(nil)

// fakeCartCleanup answers the bulk cart-line removal with a fixed removed count.
// It records every call and its input, so a test proves the cleanup runs once,
// with exactly the hidden variants, and never on a refused hide.
type fakeCartCleanup struct {
	mu     sync.Mutex
	calls  int
	inTx   []bool
	inputs [][]uuid.UUID
	remove int64
}

func (f *fakeCartCleanup) RemoveVariantsFromAllCarts(ctx context.Context, variantIDs []uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.inTx = append(f.inTx, inGuardTx(ctx))
	f.inputs = append(f.inputs, append([]uuid.UUID(nil), variantIDs...))
	return f.remove, nil
}

var _ appinterface.CartHideCleanup = (*fakeCartCleanup)(nil)

// guardedService builds the service with the hide-guard dependencies wired to
// the fakes above. The existing maintenanceService helper keeps building the
// dependency-free service the earlier tests drive.
func guardedService(repo *memoryCategories, authority appinterface.Auditor, variants *fakeCategoryVariants,
	orders *fakeOrderGuard, carts *fakeCartCleanup, tx *fakeUnitOfWork) *Service {
	return New(Service{
		Categories: repo,
		Audit:      authority,
		Mapper:     mapper.New(),
		Tx:         tx,
		Orders:     orders,
		Variants:   variants,
		Carts:      carts,
	})
}

// requireNoGuardCalls asserts the guard never ran: no resolution, no
// pending-order check and no cart cleanup.
func requireNoGuardCalls(t *testing.T, variants *fakeCategoryVariants, orders *fakeOrderGuard, carts *fakeCartCleanup) {
	t.Helper()
	if variants.calls != 0 || orders.calls != 0 || carts.calls != 0 {
		t.Fatalf("the guard must not run, got variant=%d order=%d cart=%d calls",
			variants.calls, orders.calls, carts.calls)
	}
}

// requireGuardRanOnce asserts each guard step ran exactly once, inside the
// transaction: one bounded resolution, one bounded check and one bounded cleanup
// however many variants the category holds (spec SC-004, research D3).
func requireGuardRanOnce(t *testing.T, variants *fakeCategoryVariants, orders *fakeOrderGuard, carts *fakeCartCleanup) {
	t.Helper()
	if variants.calls != 1 || orders.calls != 1 || carts.calls != 1 {
		t.Fatalf("each guard step must run exactly once, got variant=%d order=%d cart=%d calls",
			variants.calls, orders.calls, carts.calls)
	}
	for _, ran := range variants.inTx {
		if !ran {
			t.Fatal("the variant resolution must run inside the transaction")
		}
	}
	for _, ran := range orders.inTx {
		if !ran {
			t.Fatal("the pending-order check must run inside the transaction")
		}
	}
	for _, ran := range carts.inTx {
		if !ran {
			t.Fatal("the cart cleanup must run inside the transaction")
		}
	}
}

// hideCategoryID is the category under test with its two variants, the unit
// shared by the hide-guard tests below.
func hideGuardSetup(t *testing.T) (*memoryCategories, *model.Category, uuid.UUID, uuid.UUID) {
	t.Helper()
	repo := newMemoryCategories()
	seeded := repo.add(t, model.CategoryDraft{Name: "Tranh", Slug: "tranh", Description: "Mô tả", Position: 2},
		time.Now().UTC().Add(-time.Hour))
	return repo, seeded, uuid.New(), uuid.New()
}

// FR-004, SC-001 (specs/016-hide-guard US1): hiding a category covered by a
// PENDING order is refused with the blocking order named, and the category is
// byte-for-byte unchanged while carts are untouched.
func TestGuardedHideRefusedWhilePendingOrdersHoldItsVariants(t *testing.T) {
	repo, seeded, v1, v2 := hideGuardSetup(t)
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)

	blocking := uuid.New()
	settled := uuid.New()
	variants := &fakeCategoryVariants{byCategory: map[uuid.UUID][]uuid.UUID{seeded.ID: {v1, v2}}}
	orders := &fakeOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{v1}},
		{id: settled, status: "PAID", variants: []uuid.UUID{v2}},
	}}
	carts := &fakeCartCleanup{}
	tx := &fakeUnitOfWork{}
	svc := guardedService(repo, recorder, variants, orders, carts, tx)

	before, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the seeded category: %v", err)
	}

	hidden := false
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &hidden})
	if !errors.Is(err, domainerr.ErrCategoryHasPendingOrders) {
		t.Fatalf("expected ErrCategoryHasPendingOrders, got %v", err)
	}
	if out != (dto.AdminCategoryOutput{}) {
		t.Fatalf("a refused hide must not answer a category, got %+v", out)
	}
	var pending *domainerr.CategoryPendingOrdersError
	if !errors.As(err, &pending) {
		t.Fatal("the refusal must carry the blocking order identifiers")
	}
	if len(pending.OrderIDs) != 1 || pending.OrderIDs[0] != blocking {
		t.Fatalf("the refusal must name exactly the blocking order, got %v", pending.OrderIDs)
	}

	after, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the category after the refused hide: %v", err)
	}
	if !reflect.DeepEqual(*before, *after) {
		t.Fatalf("a refused hide must leave the category byte-for-byte unchanged: %+v -> %+v", before, after)
	}
	if carts.calls != 0 {
		t.Fatalf("a refused hide must never touch carts, got %d cleanup calls", carts.calls)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused hide must record nothing, got %v", recorder.actions())
	}
	if variants.calls != 1 || orders.calls != 1 {
		t.Fatalf("the guard must still check before refusing, got variant=%d order=%d calls", variants.calls, orders.calls)
	}
	if tx.callCount() != 1 {
		t.Fatalf("the refused hide must run inside one transaction, got %d", tx.callCount())
	}
}

// FR-001, FR-004, FR-007 (specs/016-hide-guard US1): orders in PAID, SHIPPED,
// COMPLETED or CANCELLED never block; the hide succeeds, every cart line holding
// the category's variants is removed in the same transaction, and the hide emits
// its audit action carrying the removed line count.
func TestGuardedHideSucceedsWithOnlySettledOrdersAndCleansCarts(t *testing.T) {
	repo, seeded, v1, v2 := hideGuardSetup(t)
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)

	variants := &fakeCategoryVariants{byCategory: map[uuid.UUID][]uuid.UUID{seeded.ID: {v1, v2}}}
	orders := &fakeOrderGuard{orders: []guardOrder{
		{id: uuid.New(), status: "PAID", variants: []uuid.UUID{v1}},
		{id: uuid.New(), status: "SHIPPED", variants: []uuid.UUID{v2}},
		{id: uuid.New(), status: "COMPLETED", variants: []uuid.UUID{v1, v2}},
		{id: uuid.New(), status: "CANCELLED", variants: []uuid.UUID{v1}},
	}}
	carts := &fakeCartCleanup{remove: 3}
	tx := &fakeUnitOfWork{}
	svc := guardedService(repo, recorder, variants, orders, carts, tx)

	hidden := false
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &hidden})
	if err != nil {
		t.Fatalf("a hide covered only by settled orders must succeed, got %v", err)
	}
	if out.IsVisible {
		t.Fatal("the category must be hidden")
	}
	if out.Name != "Tranh" || out.Slug != "tranh" || out.Description != "Mô tả" || out.Position != 2 {
		t.Fatalf("hide must not destroy a member: %+v", out)
	}

	stored, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the hidden category: %v", err)
	}
	if stored.IsVisible {
		t.Fatal("the stored row must be hidden")
	}

	requireGuardRanOnce(t, variants, orders, carts)
	if len(carts.inputs) != 1 || len(carts.inputs[0]) != 2 {
		t.Fatalf("the cleanup must run once with exactly the hidden variants, got %v", carts.inputs)
	}
	for _, id := range carts.inputs[0] {
		if id != v1 && id != v2 {
			t.Fatalf("the cleanup must remove exactly the hidden variants, got %v", carts.inputs[0])
		}
	}

	requireAudited(t, recorder, constant.AuditCategoryHidden, actor, seeded.ID)
	requireAuditCount(t, recorder, constant.AuditCategoryHidden, 1)
	// The cart-cleanup count travels as metadata on the hide event (research D8).
	found := false
	for _, event := range recorder.snapshot() {
		if event.action != constant.AuditCategoryHidden {
			continue
		}
		found = true
		if got, ok := event.metadata["removedCartLines"]; !ok || got != int64(3) {
			t.Fatalf("the hide event must carry the removed cart-line count, got %v", event.metadata)
		}
	}
	if !found {
		t.Fatal("the hide must emit its audit action")
	}
}

// Spec edge case (specs/016-hide-guard US1): showing a category again, or an
// edit that does not touch visibility, is never blocked and never runs the
// guard — even while a PENDING order covers the category.
func TestShowAndContentEditsSkipTheHideGuard(t *testing.T) {
	repo, seeded, v1, _ := hideGuardSetup(t)
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)

	blocking := uuid.New()
	variants := &fakeCategoryVariants{byCategory: map[uuid.UUID][]uuid.UUID{seeded.ID: {v1}}}
	orders := &fakeOrderGuard{orders: []guardOrder{
		{id: blocking, status: "PENDING", variants: []uuid.UUID{v1}},
	}}
	carts := &fakeCartCleanup{}
	tx := &fakeUnitOfWork{}
	svc := guardedService(repo, recorder, variants, orders, carts, tx)

	// A content-only edit succeeds without the guard.
	name := "Tranh mới"
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, Name: &name})
	if err != nil {
		t.Fatalf("a content edit must never be guarded, got %v", err)
	}
	if out.Name != name || !out.IsVisible {
		t.Fatalf("the edit must apply without touching visibility: %+v", out)
	}
	requireNoGuardCalls(t, variants, orders, carts)
	requireAudited(t, recorder, constant.AuditCategoryUpdated, actor, seeded.ID)

	// Showing a hidden category back succeeds without the guard.
	repo.hide(seeded.ID)
	shown := true
	out, err = svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, IsVisible: &shown})
	if err != nil {
		t.Fatalf("showing again must never be guarded, got %v", err)
	}
	if !out.IsVisible {
		t.Fatal("the category must be visible again")
	}
	requireNoGuardCalls(t, variants, orders, carts)
	requireAudited(t, recorder, constant.AuditCategoryShown, actor, seeded.ID)
}

// A patch that carries both a content change and a hide performs both inside
// the one transaction and records both actions.
func TestGuardedHideWithContentChangePersistsBothAndAuditsBoth(t *testing.T) {
	repo, seeded, v1, _ := hideGuardSetup(t)
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)

	variants := &fakeCategoryVariants{byCategory: map[uuid.UUID][]uuid.UUID{seeded.ID: {v1}}}
	orders := &fakeOrderGuard{}
	carts := &fakeCartCleanup{remove: 1}
	tx := &fakeUnitOfWork{}
	svc := guardedService(repo, recorder, variants, orders, carts, tx)

	name := "Tranh mới"
	hidden := false
	out, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: seeded.ID, Name: &name, IsVisible: &hidden})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if out.Name != name || out.IsVisible {
		t.Fatalf("both the content change and the hide must apply: %+v", out)
	}
	stored, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the hidden category: %v", err)
	}
	if stored.Name != name || stored.IsVisible {
		t.Fatalf("the stored row must carry both changes: %+v", stored)
	}
	requireGuardRanOnce(t, variants, orders, carts)
	requireAudited(t, recorder, constant.AuditCategoryUpdated, actor, seeded.ID)
	requireAudited(t, recorder, constant.AuditCategoryHidden, actor, seeded.ID)
}

// FR-013, SC-005: a whole lifecycle records exactly the transition table's five
// actions, in order, with no extra events.
func TestTheMaintenanceLifecycleRecordsEveryAuditActionInOrder(t *testing.T) {
	repo := newMemoryCategories()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	svc := maintenanceService(repo, recorder)

	created, err := svc.CreateCategory(ctx, dto.CreateCategoryInput{Name: "Tranh", Slug: "tranh"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	name := "Tranh mới"
	if _, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: created.ID, Name: &name}); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	hidden := false
	if _, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: created.ID, IsVisible: &hidden}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	shown := true
	if _, err := svc.UpdateCategory(ctx, dto.UpdateCategoryInput{ID: created.ID, IsVisible: &shown}); err != nil {
		t.Fatalf("show: %v", err)
	}
	if err := svc.DeleteCategory(ctx, dto.AdminCategoryRefInput{ID: created.ID}); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	want := []string{
		constant.AuditCategoryCreated,
		constant.AuditCategoryUpdated,
		constant.AuditCategoryHidden,
		constant.AuditCategoryShown,
		constant.AuditCategoryDeleted,
	}
	if got := recorder.actions(); !equalStrings(got, want) {
		t.Fatalf("expected the transition table's actions %v, got %v", want, got)
	}
	for _, action := range want {
		requireAuditCount(t, recorder, action, 1)
	}
}
