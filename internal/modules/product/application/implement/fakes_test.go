package implement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// This file holds the shared in-memory VariantGroupRepository fake the group and
// catalogue use-case tests build on. It mirrors the adapter's observable
// behaviour where a use case depends on it — a slug collision, a missing
// category, a group that still has options, the option visibility predicate —
// because a fake that is more permissive than the adapter would let a use case
// pass a test the real storage would refuse (research D3, D7).
//
// It embeds the port so a method no test needs is inherited rather than
// stubbed; the compile-time assertion below is what proves the fake still
// satisfies the whole interface.

// memoryVariantGroups is an in-memory VariantGroupRepository. It is safe for the
// sequential use the use-case tests make of it; access is guarded so a future
// parallel test does not race.
type memoryVariantGroups struct {
	domainrepo.VariantGroupRepository

	mu     sync.Mutex
	groups map[uuid.UUID]model.VariantGroup
	images map[uuid.UUID][]model.VariantGroupImage
	// options is what the option reads answer from. The fake does not own
	// products, so a test seeds the options it expects with seedOptions; that
	// keeps the fake independent of the product repository's state.
	options map[uuid.UUID][]domainrepo.GroupOptionItem
	// categories, when non-nil, is the set of category identifiers this fake
	// knows. A create or update naming one outside it is refused the way the
	// adapter classifies the foreign-key violation (FR-023). A nil map accepts
	// any category, so a test that does not care about the category need not seed
	// one.
	categories map[uuid.UUID]bool
}

var _ domainrepo.VariantGroupRepository = (*memoryVariantGroups)(nil)

// newMemoryVariantGroups returns an empty fake.
func newMemoryVariantGroups() *memoryVariantGroups {
	return &memoryVariantGroups{
		groups:  make(map[uuid.UUID]model.VariantGroup),
		images:  make(map[uuid.UUID][]model.VariantGroupImage),
		options: make(map[uuid.UUID][]domainrepo.GroupOptionItem),
	}
}

// seedGroup stores one group as the tests built it, without going through the
// create path, so a test can arrange a state a real create could reach.
func (m *memoryVariantGroups) seedGroup(group model.VariantGroup) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[group.ID] = group
}

// seedImage stores one picture under a group without going through the ceiling
// check, so a test can reach a full gallery.
func (m *memoryVariantGroups) seedImage(image model.VariantGroupImage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images[image.GroupID] = append(m.images[image.GroupID], image)
}

// seedOptions stores the options a group read should answer with, in the order
// given.
func (m *memoryVariantGroups) seedOptions(groupID uuid.UUID, options ...domainrepo.GroupOptionItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.options[groupID] = append([]domainrepo.GroupOptionItem(nil), options...)
}

// Create mirrors the adapter: a slug collision and a missing category are the
// two refusals a use case depends on (FR-019, FR-023).
func (m *memoryVariantGroups) Create(_ context.Context, group *model.VariantGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.groups {
		if id != group.ID && existing.Slug == group.Slug {
			return domainerr.ErrVariantGroupSlugTaken
		}
	}
	if m.categories != nil && !m.categories[group.CategoryID] {
		return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
	}
	m.groups[group.ID] = *group
	return nil
}

// Update mirrors the adapter: writing the slug a group already holds succeeds,
// a collision with another group is refused, and an unknown identifier is
// not-found.
func (m *memoryVariantGroups) Update(_ context.Context, group *model.VariantGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[group.ID]; !ok {
		return domainerr.ErrVariantGroupNotFound
	}
	for id, existing := range m.groups {
		if id != group.ID && existing.Slug == group.Slug {
			return domainerr.ErrVariantGroupSlugTaken
		}
	}
	if m.categories != nil && !m.categories[group.CategoryID] {
		return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
	}
	m.groups[group.ID] = *group
	return nil
}

// Delete mirrors the restricting foreign key: a group with options is refused
// (FR-025), a group without them is removed with its pictures, and an unknown
// identifier is not-found.
func (m *memoryVariantGroups) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[id]; !ok {
		return domainerr.ErrVariantGroupNotFound
	}
	if len(m.options[id]) > 0 {
		return domainerr.ErrVariantGroupHasOptions
	}
	delete(m.groups, id)
	delete(m.images, id)
	return nil
}

// FindByID returns the group with its pictures and every option, including the
// ones a customer cannot see (FR-013).
func (m *memoryVariantGroups) FindByID(_ context.Context, id uuid.UUID) (*domainrepo.VariantGroupView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	group, ok := m.groups[id]
	if !ok {
		return nil, domainerr.ErrVariantGroupNotFound
	}
	return &domainrepo.VariantGroupView{
		Group:   group,
		Images:  cloneGroupImages(m.images[id]),
		Options: cloneGroupOptions(m.options[id]),
	}, nil
}

// FindVisibleGroupBySlug mirrors the adapter's visibility predicate: the group's
// category must be visible and at least one option must exist (FR-006, FR-008).
func (m *memoryVariantGroups) FindVisibleGroupBySlug(_ context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*domainrepo.VariantGroupView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	visible := make(map[uuid.UUID]bool, len(visibleCategoryIDs))
	for _, id := range visibleCategoryIDs {
		visible[id] = true
	}
	for id, group := range m.groups {
		if group.Slug != slug {
			continue
		}
		if !visible[group.CategoryID] {
			return nil, domainerr.ErrVariantGroupNotFound
		}
		if len(m.options[id]) == 0 {
			return nil, domainerr.ErrVariantGroupNotFound
		}
		return &domainrepo.VariantGroupView{
			Group:   group,
			Images:  cloneGroupImages(m.images[id]),
			Options: cloneGroupOptions(m.options[id]),
		}, nil
	}
	return nil, domainerr.ErrVariantGroupNotFound
}

// ListAll returns a page of every group in the FR-009 order, plus the total.
func (m *memoryVariantGroups) ListAll(_ context.Context, page, pageSize int) ([]domainrepo.VariantGroupListItem, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]domainrepo.VariantGroupListItem, 0, len(m.groups))
	for id, group := range m.groups {
		item := domainrepo.VariantGroupListItem{Group: group, ImageCount: len(m.images[id])}
		for _, image := range m.images[id] {
			if image.IsPrimary {
				item.ImageURL = image.URL
			}
		}
		all = append(all, item)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Group.Position != all[j].Group.Position {
			return all[i].Group.Position < all[j].Group.Position
		}
		if !all[i].Group.CreatedAt.Equal(all[j].Group.CreatedAt) {
			return all[i].Group.CreatedAt.Before(all[j].Group.CreatedAt)
		}
		return all[i].Group.ID.String() < all[j].Group.ID.String()
	})

	total := int64(len(all))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []domainrepo.VariantGroupListItem{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

// ListImages returns the group's pictures in the order they were seeded.
func (m *memoryVariantGroups) ListImages(_ context.Context, groupID uuid.UUID) ([]model.VariantGroupImage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneGroupImages(m.images[groupID]), nil
}

// FindImage returns one picture scoped to its group, or not-found.
func (m *memoryVariantGroups) FindImage(_ context.Context, groupID, imageID uuid.UUID) (*model.VariantGroupImage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, image := range m.images[groupID] {
		if image.ID == imageID {
			found := image
			return &found, nil
		}
	}
	return nil, domainerr.ErrVariantGroupNotFound
}

// AddImage appends a picture; a group that does not exist is not-found.
func (m *memoryVariantGroups) AddImage(_ context.Context, image *model.VariantGroupImage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[image.GroupID]; !ok {
		return domainerr.ErrVariantGroupNotFound
	}
	m.images[image.GroupID] = append(m.images[image.GroupID], *image)
	return nil
}

// DeleteImage removes one picture scoped to its group, or answers not-found.
func (m *memoryVariantGroups) DeleteImage(_ context.Context, groupID, imageID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	images := m.images[groupID]
	out := make([]model.VariantGroupImage, 0, len(images))
	removed := false
	for _, image := range images {
		if image.ID == imageID {
			removed = true
			continue
		}
		out = append(out, image)
	}
	if !removed {
		return domainerr.ErrVariantGroupNotFound
	}
	m.images[groupID] = out
	return nil
}

// SetPrimaryImage promotes one picture and clears the flag on the rest, so at
// most one main picture survives (FR-018).
func (m *memoryVariantGroups) SetPrimaryImage(_ context.Context, groupID, imageID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	images := m.images[groupID]
	found := false
	for _, image := range images {
		if image.ID == imageID {
			found = true
		}
	}
	if !found {
		return domainerr.ErrVariantGroupNotFound
	}
	for i := range images {
		images[i].IsPrimary = images[i].ID == imageID
	}
	return nil
}

// CountImages reports how many pictures the group has.
func (m *memoryVariantGroups) CountImages(_ context.Context, groupID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.images[groupID]), nil
}

// ListOptions returns the group's options in the seeded order. The public
// variant applies the option's own state predicate, mirroring the adapter's
// visible read (FR-002, FR-027).
func (m *memoryVariantGroups) ListOptions(_ context.Context, groupID uuid.UUID, includeHidden bool) ([]domainrepo.GroupOptionItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	options := m.options[groupID]
	if includeHidden {
		return cloneGroupOptions(options), nil
	}
	out := make([]domainrepo.GroupOptionItem, 0, len(options))
	for _, option := range options {
		if !optionVisible(option) {
			continue
		}
		out = append(out, option)
	}
	return out, nil
}

// optionVisible mirrors the adapter's product visibility predicate: on sale, or
// an announced non-retired pre-order (FR-002, FR-027).
func optionVisible(option domainrepo.GroupOptionItem) bool {
	if option.SellState == constant.SellStateDiscontinued {
		return false
	}
	return option.SellState == constant.SellStateActive || option.IsPreorder
}

// cloneGroupImages copies a picture slice so a caller cannot mutate the fake's
// state through the returned slice.
func cloneGroupImages(images []model.VariantGroupImage) []model.VariantGroupImage {
	if len(images) == 0 {
		return nil
	}
	return append([]model.VariantGroupImage(nil), images...)
}

// cloneGroupOptions copies an option slice for the same reason.
func cloneGroupOptions(options []domainrepo.GroupOptionItem) []domainrepo.GroupOptionItem {
	if len(options) == 0 {
		return nil
	}
	return append([]domainrepo.GroupOptionItem(nil), options...)
}

// TestMemoryVariantGroupsFakeServesTheGroupUseCases proves the shared fake does
// what a use-case test will ask of it: seeding, the slug and category refusals,
// the restricting delete, the visibility predicate and the ordered option read.
// It is the guard that keeps the fake from drifting from the adapter's
// observable contract.
func TestMemoryVariantGroupsFakeServesTheGroupUseCases(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryVariantGroups()
	now := time.Now().UTC()

	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	group, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:       "Card Genshin",
		Slug:       "card-genshin",
		CategoryID: category,
		OptionName: "Card",
	}, now)
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	if err := repo.Create(ctx, group); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A second group with the same slug is refused the way the adapter reports it.
	duplicate, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:       "Another",
		Slug:       "card-genshin",
		CategoryID: category,
		OptionName: "Card",
	}, now)
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	if err := repo.Create(ctx, duplicate); !errors.Is(err, domainerr.ErrVariantGroupSlugTaken) {
		t.Fatalf("expected ErrVariantGroupSlugTaken, got %v", err)
	}

	// A category this fake does not know is refused naming the field.
	orphan, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:       "Orphan",
		Slug:       "orphan",
		CategoryID: uuid.New(),
		OptionName: "Card",
	}, now)
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	if err := repo.Create(ctx, orphan); !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}

	// A second, valid group, seeded without going through the create path, so the
	// list has two entries and the delete-with-options refusal is not confused
	// with an empty catalogue.
	otherCategory := uuid.New()
	repo.categories[otherCategory] = true
	repo.seedGroup(model.VariantGroup{
		ID: uuid.New(), Name: "Other", Slug: "other-group",
		CategoryID: otherCategory, OptionName: "Card", CreatedAt: now, UpdatedAt: now,
	})

	// Seed an image and two options: one visible, one retired.
	repo.seedImage(model.VariantGroupImage{
		ID: uuid.New(), GroupID: group.ID, PublicID: "demo/a", URL: "https://example.test/a.jpg",
		Width: 10, Height: 10, IsPrimary: true, CreatedAt: now,
	})
	repo.seedOptions(group.ID,
		domainrepo.GroupOptionItem{
			ID: uuid.New(), Slug: "flins", Label: "Flins",
			Price: model.Price{Amount: 1000, Currency: "VND"}, SellState: constant.SellStateActive,
		},
		domainrepo.GroupOptionItem{
			ID: uuid.New(), Slug: "dottore", Label: "Dottore",
			Price: model.Price{Amount: 2000, Currency: "VND"}, SellState: constant.SellStateDiscontinued,
		},
	)

	view, err := repo.FindByID(ctx, group.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(view.Images) != 1 || len(view.Options) != 2 {
		t.Fatalf("expected one image and both options including the hidden one, got %d and %d",
			len(view.Images), len(view.Options))
	}

	// The public read carries only the visible option.
	if _, err := repo.FindVisibleGroupBySlug(ctx, "orphan", []uuid.UUID{category}); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("expected not-found for an unknown slug, got %v", err)
	}
	visible, err := repo.FindVisibleGroupBySlug(ctx, "card-genshin", []uuid.UUID{category})
	if err != nil {
		t.Fatalf("FindVisibleGroupBySlug: %v", err)
	}
	public, err := repo.ListOptions(ctx, group.ID, false)
	if err != nil {
		t.Fatalf("ListOptions public: %v", err)
	}
	if len(public) != 1 || visible.Group.ID != group.ID {
		t.Fatalf("expected one visible option, got %d", len(public))
	}
	if _, err := repo.FindVisibleGroupBySlug(ctx, "card-genshin", nil); !errors.Is(err, domainerr.ErrVariantGroupNotFound) {
		t.Fatalf("an empty visible set must answer not-found, got %v", err)
	}

	// A group with options cannot be removed.
	if err := repo.Delete(ctx, group.ID); !errors.Is(err, domainerr.ErrVariantGroupHasOptions) {
		t.Fatalf("expected ErrVariantGroupHasOptions, got %v", err)
	}

	list, total, err := repo.ListAll(ctx, 1, 20)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("expected both groups, got %d of %d", len(list), total)
	}
}
