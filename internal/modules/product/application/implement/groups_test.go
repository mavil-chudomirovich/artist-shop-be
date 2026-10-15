package implement

import (
	"context"
	"errors"
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

// This file exercises the variant-group maintenance use cases (US2) over the
// shared in-memory group repository, the in-memory product repository and a
// recording auditor. Every mutation must record its audit action naming the group
// and the acting administrator (FR-014, SC-006), a slug collision and a missing
// category must be refused naming the field (FR-019, FR-023), and the group
// picture gallery must follow the same ceiling and one-primary rules a product
// gallery does (FR-018).

// groupFixture builds the service with the two in-memory repositories, a
// recording auditor, the fake media store and the fake unit of work.
type groupFixture struct {
	service  *Service
	groups   *memoryVariantGroups
	products *fauxProducts
	audit    *recordingAuditor
	media    *fakeMedia
	tx       *fauxUnitOfWork
	actor    appinterface.Actor
	category uuid.UUID
}

func newGroupFixture(t *testing.T) *groupFixture {
	t.Helper()
	groups := newMemoryVariantGroups()
	products := newFauxProducts()
	audit := &recordingAuditor{}
	actor := adminActor()
	category := uuid.New()
	groups.categories = map[uuid.UUID]bool{category: true}
	service := New(Service{
		Products: products,
		Groups:   groups,
		Audit:    audit,
		Media:    newFakeMedia(),
		Tx:       &fauxUnitOfWork{},
		Mapper:   mapper.New(),
	})
	return &groupFixture{
		service:  service,
		groups:   groups,
		products: products,
		audit:    audit,
		media:    service.Media.(*fakeMedia),
		tx:       service.Tx.(*fauxUnitOfWork),
		actor:    actor,
		category: category,
	}
}

// ctx is the request context carrying the session's administrator, never an
// actor drawn from the input (FR-015).
func (f *groupFixture) ctx() context.Context {
	return appinterface.WithActor(context.Background(), f.actor)
}

// createGroup stores one group through the use case and returns its answer.
func (f *groupFixture) createGroup(t *testing.T, name, slug, optionName string) dto.AdminVariantGroupDetailOutput {
	t.Helper()
	out, err := f.service.CreateVariantGroup(f.ctx(), dto.CreateVariantGroupInput{
		Name:       name,
		Slug:       slug,
		CategoryID: f.category,
		Position:   1,
		OptionName: optionName,
	})
	if err != nil {
		t.Fatalf("CreateVariantGroup(%q): %v", slug, err)
	}
	return out
}

// requireGroupAudited asserts one event of the given action was recorded, naming
// the acting administrator and the group as the target (FR-014, SC-006).
func requireGroupAudited(t *testing.T, recorder *recordingAuditor, action string, actor appinterface.Actor, groupID uuid.UUID) {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action != action {
			continue
		}
		if event.outcome != "SUCCESS" {
			t.Fatalf("%s: expected outcome SUCCESS, got %q", action, event.outcome)
		}
		if event.actorID == nil || *event.actorID != actor.ID {
			t.Fatalf("%s: expected actor %s, got %v", action, actor.ID, event.actorID)
		}
		if event.actorRole != string(actor.Role) {
			t.Fatalf("%s: expected actor role %q, got %q", action, actor.Role, event.actorRole)
		}
		if event.targetType != "variant_group" {
			t.Fatalf("%s: expected the group as the target type, got %q", action, event.targetType)
		}
		if event.targetID != groupID.String() {
			t.Fatalf("%s: expected target %s, got %q", action, groupID, event.targetID)
		}
		return
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
}

// FR-009, FR-014: creating a group persists it, gives it a stable identifier and
// records PRODUCT_VARIANT_GROUP_CREATED naming the administrator.
func TestCreateVariantGroupPersistsItAndRecordsTheAuditAction(t *testing.T) {
	f := newGroupFixture(t)

	out := f.createGroup(t, "  Card Genshin  ", "card-genshin", "Card")

	if out.ID == uuid.Nil {
		t.Fatal("a created group must carry its identifier")
	}
	if out.Name != "Card Genshin" || out.Slug != "card-genshin" || out.OptionName != "Card" {
		t.Fatalf("the created group must carry the stored values: %+v", out)
	}
	if _, err := f.groups.FindByID(context.Background(), out.ID); err != nil {
		t.Fatalf("the created group must be persisted: %v", err)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupCreated, f.actor, out.ID)
}

// FR-019: a second group with the same slug is refused with the collision sentinel.
func TestCreatingAGroupWithATakenSlugIsRefused(t *testing.T) {
	f := newGroupFixture(t)
	f.createGroup(t, "Card Genshin", "card-genshin", "Card")

	_, err := f.service.CreateVariantGroup(f.ctx(), dto.CreateVariantGroupInput{
		Name: "Another", Slug: "card-genshin", CategoryID: f.category, OptionName: "Card",
	})
	if !errors.Is(err, domainerr.ErrVariantGroupSlugTaken) {
		t.Fatalf("expected ErrVariantGroupSlugTaken, got %v", err)
	}
}

// FR-023: creating a group with a category that does not exist is refused naming
// the field, and nothing is written.
func TestCreatingAGroupWithAnUnknownCategoryIsRefusedNamingTheField(t *testing.T) {
	f := newGroupFixture(t)
	groupsBefore := len(f.audit.snapshot())

	_, err := f.service.CreateVariantGroup(f.ctx(), dto.CreateVariantGroupInput{
		Name: "Orphan", Slug: "orphan", CategoryID: uuid.New(), OptionName: "Card",
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldCategoryID, err)
	}
	if len(f.audit.snapshot()) != groupsBefore {
		t.Fatalf("a refused create must record nothing, got %v", f.audit.actions())
	}
}

// FR-011: an edit changes the fields it carries, keeps the ones it does not, and
// records PRODUCT_VARIANT_GROUP_UPDATED.
func TestUpdateVariantGroupChangesCarriedFieldsAndAudits(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")

	name := "Card Genshin v2"
	optionName := "Kiểu"
	out, err := f.service.UpdateVariantGroup(f.ctx(), dto.UpdateVariantGroupInput{
		ID: created.ID, Name: &name, OptionName: &optionName,
	})
	if err != nil {
		t.Fatalf("UpdateVariantGroup: %v", err)
	}
	if out.Name != name || out.OptionName != optionName {
		t.Fatalf("the edit did not change the fields it carried: %+v", out)
	}
	if out.Slug != "card-genshin" || out.Position != 1 {
		t.Fatalf("the edit changed a field it did not carry: %+v", out)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupUpdated, f.actor, created.ID)
}

// FR-023: editing a group to a category that does not exist is refused naming the
// field and leaves the row untouched.
func TestEditingAGroupWithAnUnknownCategoryIsRefusedNamingTheField(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")

	unknown := uuid.New()
	_, err := f.service.UpdateVariantGroup(f.ctx(), dto.UpdateVariantGroupInput{ID: created.ID, CategoryID: &unknown})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldCategoryID, err)
	}
	stored, err := f.groups.FindByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("read the group: %v", err)
	}
	if stored.Group.CategoryID != f.category {
		t.Fatalf("a refused edit changed the row: %+v", stored.Group)
	}
}

// FR-012, FR-025: a group that still has options cannot be removed; the operator
// detaches them first.
func TestRemovingAGroupWithOptionsIsRefused(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	f.groups.seedOptions(created.ID, domainrepo.GroupOptionItem{
		ID: uuid.New(), Slug: "flins", Label: "Flins", Price: price(100),
		SellState: constant.SellStateActive, Position: 1,
	})

	err := f.service.DeleteVariantGroup(f.ctx(), dto.AdminVariantGroupRefInput{ID: created.ID})
	if !errors.Is(err, domainerr.ErrVariantGroupHasOptions) {
		t.Fatalf("expected ErrVariantGroupHasOptions, got %v", err)
	}
}

// FR-012, FR-014: removing an empty group succeeds and records
// PRODUCT_VARIANT_GROUP_DELETED; a second removal is the module not-found.
func TestRemovingAnEmptyGroupSucceedsAndAudits(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")

	if err := f.service.DeleteVariantGroup(f.ctx(), dto.AdminVariantGroupRefInput{ID: created.ID}); err != nil {
		t.Fatalf("DeleteVariantGroup: %v", err)
	}
	if _, err := f.groups.FindByID(context.Background(), created.ID); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("the removed group must be gone, got %v", err)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupDeleted, f.actor, created.ID)

	if err := f.service.DeleteVariantGroup(f.ctx(), dto.AdminVariantGroupRefInput{ID: created.ID}); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("a second removal must answer not-found, got %v", err)
	}
}

// FR-018, FR-014: the first group picture becomes the main one, is stored through
// the media port and records PRODUCT_VARIANT_GROUP_IMAGE_ADDED.
func TestTheFirstGroupPictureBecomesTheMainOneAndIsAudited(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")

	out, err := f.service.AddGroupImage(f.ctx(), dto.AddGroupImageInput{GroupID: created.ID, Content: pngBytes()})
	if err != nil {
		t.Fatalf("AddGroupImage: %v", err)
	}
	if len(out.Images) != 1 || !out.Images[0].IsPrimary {
		t.Fatalf("the first group picture must be the main one: %+v", out.Images)
	}
	if f.media.uploads != 1 {
		t.Fatalf("expected one upload, got %d", f.media.uploads)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupImageAdded, f.actor, created.ID)
}

// FR-018: the group gallery obeys the same ten-picture ceiling a product gallery
// does, and the refused eleventh never reaches the media provider.
func TestTheEleventhGroupPictureIsRefusedBeforeTheProvider(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	for i := 0; i < model.MaxProductPictures; i++ {
		f.groups.seedImage(model.VariantGroupImage{
			ID: uuid.New(), GroupID: created.ID, PublicID: "demo/seed",
			URL: "https://cdn.example.test/seed.jpg", Width: 10, Height: 10,
			Position: i, IsPrimary: i == 0, CreatedAt: time.Now().UTC(),
		})
	}

	_, err := f.service.AddGroupImage(f.ctx(), dto.AddGroupImageInput{GroupID: created.ID, Content: pngBytes()})
	if !errors.Is(err, domainerr.ErrProductImageLimitReached) {
		t.Fatalf("expected ErrProductImageLimitReached, got %v", err)
	}
	if f.media.uploads != 0 {
		t.Fatalf("a refused eleventh picture must not reach the provider, got %d uploads", f.media.uploads)
	}
}

// FR-018, FR-014: removing the main group picture promotes the next by position,
// releases the asset and records PRODUCT_VARIANT_GROUP_IMAGE_REMOVED.
func TestRemovingTheMainGroupPicturePromotesTheNextAndAudits(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	first := model.VariantGroupImage{
		ID: uuid.New(), GroupID: created.ID, PublicID: "demo/one",
		URL: "https://cdn.example.test/one.jpg", Width: 10, Height: 10,
		Position: 0, IsPrimary: true, CreatedAt: time.Now().UTC(),
	}
	second := model.VariantGroupImage{
		ID: uuid.New(), GroupID: created.ID, PublicID: "demo/two",
		URL: "https://cdn.example.test/two.jpg", Width: 10, Height: 10,
		Position: 1, IsPrimary: false, CreatedAt: time.Now().UTC(),
	}
	f.groups.seedImage(first)
	f.groups.seedImage(second)

	if err := f.service.RemoveGroupImage(f.ctx(), dto.RemoveGroupImageInput{GroupID: created.ID, ImageID: first.ID}); err != nil {
		t.Fatalf("RemoveGroupImage: %v", err)
	}
	images, err := f.groups.ListImages(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) != 1 || !images[0].IsPrimary || images[0].ID != second.ID {
		t.Fatalf("removing the main picture must promote the next by position, got %+v", images)
	}
	if f.media.removed != 1 {
		t.Fatalf("expected the stored asset to be released once, got %d", f.media.removed)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupImageRemoved, f.actor, created.ID)
}

// FR-018, FR-014: making a picture the group's main one records
// PRODUCT_VARIANT_GROUP_IMAGE_PRIMARY_SET.
func TestSettingTheMainGroupPictureIsAudited(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	first := model.VariantGroupImage{
		ID: uuid.New(), GroupID: created.ID, PublicID: "demo/one",
		URL: "https://cdn.example.test/one.jpg", Width: 10, Height: 10,
		Position: 0, IsPrimary: true, CreatedAt: time.Now().UTC(),
	}
	second := model.VariantGroupImage{
		ID: uuid.New(), GroupID: created.ID, PublicID: "demo/two",
		URL: "https://cdn.example.test/two.jpg", Width: 10, Height: 10,
		Position: 1, IsPrimary: false, CreatedAt: time.Now().UTC(),
	}
	f.groups.seedImage(first)
	f.groups.seedImage(second)

	out, err := f.service.SetPrimaryGroupImage(f.ctx(), dto.SetPrimaryGroupImageInput{GroupID: created.ID, ImageID: second.ID})
	if err != nil {
		t.Fatalf("SetPrimaryGroupImage: %v", err)
	}
	if len(out.Images) != 2 || !out.Images[1].IsPrimary || out.Images[0].IsPrimary {
		t.Fatalf("the promoted picture must be the only main one: %+v", out.Images)
	}
	requireGroupAudited(t, f.audit, constant.AuditVariantGroupImagePrimarySet, f.actor, created.ID)
}

// FR-013: the administrator group list and detail carry the members a customer
// must not receive, including an option that is not currently visible.
func TestTheAdministratorGroupReadSeesEveryOption(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	f.groups.seedOptions(created.ID, domainrepo.GroupOptionItem{
		ID: uuid.New(), Name: "Card Flins", Slug: "card-flins", Label: "Flins",
		Price: price(120000), SellState: constant.SellStateDiscontinued, Position: 1,
	})

	detail, err := f.service.GetAdminGroup(f.ctx(), dto.AdminVariantGroupRefInput{ID: created.ID})
	if err != nil {
		t.Fatalf("GetAdminGroup: %v", err)
	}
	if len(detail.Options) != 1 || detail.Options[0].Label != "Flins" || detail.Options[0].SellState != constant.SellStateDiscontinued {
		t.Fatalf("the administrator read must carry the withheld option: %+v", detail.Options)
	}

	page, err := f.service.ListAdminGroups(f.ctx(), dto.ListAdminGroupsInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAdminGroups: %v", err)
	}
	if page.Total != 1 || len(page.Groups) != 1 || page.Groups[0].ID != created.ID {
		t.Fatalf("the administrator list must carry the group: %+v", page.Groups)
	}
}

// FR-021: attaching a product that already belongs to a group is refused, because
// a product is an option of at most one group (the edge case of the spec).
func TestAttachingAProductThatIsAlreadyGroupedIsRefused(t *testing.T) {
	f := newGroupFixture(t)
	first := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	second := f.createGroup(t, "Card Honkai", "card-honkai", "Card")
	grouped := f.products.seed(t, model.ProductDraft{
		Name: "Card Flins", Slug: "card-flins", Price: price(120000), CategoryID: f.category,
		VariantGroupID: &first.ID, VariantLabel: strptr("Flins"),
	}, time.Now().UTC().Add(-time.Hour))

	_, err := f.service.UpdateProduct(f.ctx(), dto.UpdateProductInput{
		ID: grouped.ID, VariantGroupID: &second.ID, VariantGroupSet: true,
	})
	if !errors.Is(err, domainerr.ErrProductAlreadyGrouped) {
		t.Fatalf("expected ErrProductAlreadyGrouped, got %v", err)
	}
	stored, err := f.products.FindByID(context.Background(), grouped.ID)
	if err != nil {
		t.Fatalf("read the product: %v", err)
	}
	if stored.Product.VariantGroupID == nil || *stored.Product.VariantGroupID != first.ID {
		t.Fatalf("a refused re-attach must leave the product in its original group: %+v", stored.Product)
	}
}

// FR-021: an option label is unique within its group; a duplicate is refused with
// the collision sentinel, exactly as the storage index reports it.
func TestAttachingADuplicateLabelToAGroupIsRefused(t *testing.T) {
	f := newGroupFixture(t)
	created := f.createGroup(t, "Card Genshin", "card-genshin", "Card")
	f.products.seed(t, model.ProductDraft{
		Name: "Card Flins", Slug: "card-flins", Price: price(120000), CategoryID: f.category,
		VariantGroupID: &created.ID, VariantLabel: strptr("Flins"),
	}, time.Now().UTC().Add(-time.Hour))
	other := f.products.seed(t, model.ProductDraft{
		Name: "Card Dottore", Slug: "card-dottore", Price: price(120000), CategoryID: f.category,
	}, time.Now().UTC().Add(-time.Minute))

	_, err := f.service.UpdateProduct(f.ctx(), dto.UpdateProductInput{
		ID: other.ID, VariantGroupID: &created.ID, VariantGroupSet: true,
		VariantLabel: strptr("Flins"), VariantLabelSet: true,
	})
	if !errors.Is(err, domainerr.ErrVariantLabelTaken) {
		t.Fatalf("expected ErrVariantLabelTaken, got %v", err)
	}
}
