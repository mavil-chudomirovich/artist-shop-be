package implement

import (
	"context"
	"errors"
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
