//go:build integration

package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the variant-group adapter and the merged catalogue read
// against a real PostgreSQL container. Its centre of gravity is what a fake
// cannot prove: that the migration applies, that the merged list is one ordered
// page with a correct cross-kind total, that a group's price is the lowest of its
// *visible* options, and that the section-014 invariants (label uniqueness, one
// group per product, set↔option exclusivity, the restricting group delete, the two
// slug namespaces) hold against a writer that bypassed the application.
//
// It reuses the fixture and the raw-insert helpers of
// product_integration_test.go, because a second migration runner and a second
// category insert would be a second answer to the same question. Only the
// helpers the variant tables need are added here.

// The constraint and index names migrations/00013_product_variants.sql must
// create (data-model.md).
const (
	idxVariantGroupSlugKey = "product_variant_groups_slug_key"
	idxVariantLabelKey     = "products_variant_label_key"
	fkProductVariantGroup  = "products_variant_group_fk"
	ckSetNotVariant        = "products_set_not_variant_ck"
	ckVariantLabel         = "products_variant_label_ck"
)

// rawInsertGroup writes one group row straight into the table, skipping the
// domain and the adapter.
func (f *productFixture) rawInsertGroup(ctx context.Context, categoryID uuid.UUID, slug string, position int) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO product_variant_groups
			(id, name, slug, description, category_id, position, option_name, created_at, updated_at)
		VALUES ($1, $2, $3, '', $4, $5, 'Card', now(), now())`
	_, err := f.pool.Exec(ctx, query, id, "Group "+slug, slug, categoryID, position)
	return id, err
}

// rawInsertGroupImage writes one group picture straight into the table.
func (f *productFixture) rawInsertGroupImage(ctx context.Context, groupID uuid.UUID, url string, primary bool) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO product_variant_group_images
			(id, group_id, public_id, secure_url, width, height, position, is_primary)
		VALUES ($1, $2, $3, $4, 10, 10, 0, $5)`
	_, err := f.pool.Exec(ctx, query, id, groupID, "demo/"+id.String(), url, primary)
	return id, err
}

// rawInsertVariantProduct writes a product with an explicit attachment, so a
// subtest can set the group and the label independently — including the invalid
// combinations the check constraints must refuse.
func (f *productFixture) rawInsertVariantProduct(ctx context.Context, p rawProduct, groupID *uuid.UUID, label *string) (uuid.UUID, error) {
	id := uuid.New()
	const query = `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder,
			 preorder_expected_at, created_at, updated_at, variant_group_id, variant_label)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now(), now(), $14, $15)`
	_, err := f.pool.Exec(ctx, query, id, p.Name, p.Slug, p.NormalizedSlug, p.Description,
		p.PriceAmount, p.Currency, p.CategoryID, p.Position, p.SellState,
		p.IsSet, p.IsPreorder, p.ExpectedAt, groupID, label)
	return id, err
}

// visibleOption builds a raw product row a customer may see, in the given
// category.
func visibleOption(categoryID uuid.UUID, slug string, amount int64) rawProduct {
	p := validRaw(categoryID, slug)
	p.SellState = "ACTIVE"
	p.PriceAmount = amount
	return p
}

// variantGroupRepositoryFor builds the adapter under test over the shared pool.
func (f *productFixture) variantGroupRepositoryFor() *VariantGroupRepository {
	return NewVariantGroupRepository(f.pool)
}

// FR-001, FR-006, SC-002: the merged list is one page whose total counts both
// kinds. A group appears once — never once per option — and a grouped product
// never appears on the standalone side.
func TestMergedCatalogueListsGroupsAndStandaloneInOnePage(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-merged")

	groupID, err := f.rawInsertGroup(ctx, category, "merged-group", 1)
	if err != nil {
		t.Fatalf("insert the group: %v", err)
	}
	for i, label := range []string{"Flins", "Dottore"} {
		option := visibleOption(category, "merged-option-"+label, int64(1000*(i+1)))
		option.Position = i
		if _, err := f.rawInsertVariantProduct(ctx, option, &groupID, &label); err != nil {
			t.Fatalf("insert the option %q: %v", label, err)
		}
	}
	standalone := visibleOption(category, "merged-standalone", 999)
	if _, err := f.rawInsertVariantProduct(ctx, standalone, nil, nil); err != nil {
		t.Fatalf("insert the standalone product: %v", err)
	}
	// A product that is not visible is excluded, so the total counts only what a
	// customer may see.
	hidden := validRaw(category, "merged-hidden")
	hidden.SellState = "COMING_SOON"
	if _, err := f.rawInsertVariantProduct(ctx, hidden, nil, nil); err != nil {
		t.Fatalf("insert the hidden product: %v", err)
	}

	entries, total, err := f.repo.ListCatalogue(ctx, visibleListQuery(category))
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("expected a page of 2 entries (one group, one product), got %d of a total %d", len(entries), total)
	}

	types := map[string]string{}
	for _, entry := range entries {
		types[entry.Slug] = string(entry.Type)
	}
	if types["merged-group"] != "variant-group" {
		t.Fatalf("the group must appear once as a variant-group, got %q", types["merged-group"])
	}
	if types["merged-standalone"] != "product" {
		t.Fatalf("the standalone product must appear as a product, got %q", types["merged-standalone"])
	}
	for slug := range types {
		if slug != "merged-group" && slug != "merged-standalone" {
			t.Fatalf("only the group and the standalone product may be entries, got %q", slug)
		}
	}
	if _, ok := types["merged-option-Flins"]; ok {
		t.Fatalf("an option leaked into the merged list as its own entry")
	}
}

// FR-005, research D7: a group's card price is the lowest price among its
// *visible* options, and its card image is its own main picture.
func TestGroupCardPriceIsTheLowestVisibleOption(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-price")

	groupID, err := f.rawInsertGroup(ctx, category, "price-group", 1)
	if err != nil {
		t.Fatalf("insert the group: %v", err)
	}
	for i, amount := range []int64{3000, 1000, 2000} {
		label := "option-" + strconv.Itoa(i)
		option := visibleOption(category, "price-"+label, amount)
		option.Position = i
		if _, err := f.rawInsertVariantProduct(ctx, option, &groupID, &label); err != nil {
			t.Fatalf("insert the option %q: %v", label, err)
		}
	}
	// A retired option is not visible and its lower price must not count.
	retiredLabel := "retired"
	retired := validRaw(category, "price-retired")
	retired.SellState = "DISCONTINUED"
	retired.PriceAmount = 500
	if _, err := f.rawInsertVariantProduct(ctx, retired, &groupID, &retiredLabel); err != nil {
		t.Fatalf("insert the retired option: %v", err)
	}
	if _, err := f.rawInsertGroupImage(ctx, groupID, "https://example.test/price-group.jpg", true); err != nil {
		t.Fatalf("insert the group image: %v", err)
	}

	entries, _, err := f.repo.ListCatalogue(ctx, visibleListQuery(category))
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the one group, got %d entries", len(entries))
	}
	group := entries[0]
	if group.Price.Amount != 1000 {
		t.Fatalf("the group price must be the lowest visible option's, got %d", group.Price.Amount)
	}
	if group.Price.Currency != "VND" {
		t.Fatalf("the group price must carry the option's currency, got %q", group.Price.Currency)
	}
	if group.ImageURL != "https://example.test/price-group.jpg" {
		t.Fatalf("the group card image must be the group's own main picture, got %q", group.ImageURL)
	}
}

// FR-019, FR-021, FR-024, SC-004: the section-014 invariants are enforced by the
// storage layer, proven against a writer that bypassed the domain and the
// adapter. Each subtest names the one constraint it expects.
func TestVariantStorageInvariants(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-invariants")

	t.Run("a label is unique within its group", func(t *testing.T) {
		groupID, err := f.rawInsertGroup(ctx, category, "label-group", 0)
		if err != nil {
			t.Fatalf("insert the group: %v", err)
		}
		first := "Flins"
		if _, err := f.rawInsertVariantProduct(ctx, visibleOption(category, "label-first", 1000), &groupID, &first); err != nil {
			t.Fatalf("the first label must be accepted, got %v", err)
		}
		second := "Flins"
		_, err = f.rawInsertVariantProduct(ctx, visibleOption(category, "label-second", 1000), &groupID, &second)
		requireConstraint(t, err, sqlStateUnique, idxVariantLabelKey)
	})

	t.Run("a set cannot be an option", func(t *testing.T) {
		groupID, err := f.rawInsertGroup(ctx, category, "set-group", 0)
		if err != nil {
			t.Fatalf("insert the group: %v", err)
		}
		set := visibleOption(category, "set-option", 1000)
		set.IsSet = true
		label := "Set"
		_, err = f.rawInsertVariantProduct(ctx, set, &groupID, &label)
		requireConstraint(t, err, sqlStateCheck, ckSetNotVariant)
	})

	t.Run("a label belongs to a grouped product and to nothing else", func(t *testing.T) {
		// A label with no group.
		lone := "Lone"
		_, err := f.rawInsertVariantProduct(ctx, visibleOption(category, "lone-label", 1000), nil, &lone)
		requireConstraint(t, err, sqlStateCheck, ckVariantLabel)

		// A group with no label.
		groupID, err := f.rawInsertGroup(ctx, category, "unlabelled-group", 0)
		if err != nil {
			t.Fatalf("insert the group: %v", err)
		}
		_, err = f.rawInsertVariantProduct(ctx, visibleOption(category, "unlabelled-option", 1000), &groupID, nil)
		requireConstraint(t, err, sqlStateCheck, ckVariantLabel)
	})

	t.Run("a product belongs to at most one group", func(t *testing.T) {
		groupA, err := f.rawInsertGroup(ctx, category, "one-group-a", 0)
		if err != nil {
			t.Fatalf("insert group A: %v", err)
		}
		groupB, err := f.rawInsertGroup(ctx, category, "one-group-b", 0)
		if err != nil {
			t.Fatalf("insert group B: %v", err)
		}
		label := "Only"
		optionID, err := f.rawInsertVariantProduct(ctx, visibleOption(category, "one-group-option", 1000), &groupA, &label)
		if err != nil {
			t.Fatalf("insert the option: %v", err)
		}
		// The row carries a single variant_group_id, so moving it changes the
		// group rather than adding a second membership.
		if _, err := f.pool.Exec(ctx, `UPDATE products SET variant_group_id = $2 WHERE id = $1`, optionID, groupB); err != nil {
			t.Fatalf("move the option: %v", err)
		}
		if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1 AND variant_group_id = $2`, optionID, groupB); got != 1 {
			t.Fatalf("the option must belong to group B after the move, count is %d", got)
		}
		if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1 AND variant_group_id = $2`, optionID, groupA); got != 0 {
			t.Fatalf("the option must no longer belong to group A, count is %d", got)
		}
	})
}

// FR-012, FR-025, research D10: a group with options cannot be removed. The
// database refuses the delete (the restricting foreign key), and the adapter
// classifies it as the group-with-options refusal.
func TestRemovingAGroupWithOptionsIsRestricted(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-restrict")
	repo := f.variantGroupRepositoryFor()

	groupID, err := f.rawInsertGroup(ctx, category, "restrict-group", 0)
	if err != nil {
		t.Fatalf("insert the group: %v", err)
	}
	label := "Flins"
	optionID, err := f.rawInsertVariantProduct(ctx, visibleOption(category, "restrict-option", 1000), &groupID, &label)
	if err != nil {
		t.Fatalf("insert the option: %v", err)
	}

	// The raw delete is refused by the restricting foreign key.
	_, err = f.pool.Exec(ctx, `DELETE FROM product_variant_groups WHERE id = $1`, groupID)
	requireConstraint(t, err, sqlStateForeignKey, fkProductVariantGroup)

	// The adapter maps the same refusal to the module sentinel.
	if err := repo.Delete(ctx, groupID); !errors.Is(err, domainerr.ErrVariantGroupHasOptions) {
		t.Fatalf("expected ErrVariantGroupHasOptions, got %v", err)
	}

	// The refused delete removed nothing.
	if got := f.count(t, `SELECT count(*) FROM product_variant_groups WHERE id = $1`, groupID); got != 1 {
		t.Fatalf("the group was removed despite the refusal, count is %d", got)
	}
	if got := f.count(t, `SELECT count(*) FROM products WHERE id = $1`, optionID); got != 1 {
		t.Fatalf("the option was removed despite the refusal, count is %d", got)
	}

	// Once the option is detached (removed), the group is removable and its own
	// pictures cascade away with it.
	if _, err := f.rawInsertGroupImage(ctx, groupID, "https://example.test/restrict.jpg", true); err != nil {
		t.Fatalf("insert the group image: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, optionID); err != nil {
		t.Fatalf("remove the option: %v", err)
	}
	if err := repo.Delete(ctx, groupID); err != nil {
		t.Fatalf("the group must be removable once it has no options, got %v", err)
	}
	if got := f.count(t, `SELECT count(*) FROM product_variant_group_images WHERE group_id = $1`, groupID); got != 0 {
		t.Fatalf("the group's pictures survived the cascade, count is %d", got)
	}
}

// FR-019, research D6: a group slug is unique among groups, and the group and
// product namespaces are independent, so the same value may exist once in each.
func TestGroupSlugIsUniqueAmongGroupsAndIndependentFromProductSlug(t *testing.T) {
	f := newProductFixture(t)
	ctx := context.Background()
	category := f.requireCategory(t, "cat-slugs")
	repo := f.variantGroupRepositoryFor()

	if _, err := f.rawInsertGroup(ctx, category, "shared-slug", 0); err != nil {
		t.Fatalf("the first group slug must be accepted, got %v", err)
	}
	// The product namespace accepts the same value: two separate namespaces.
	if _, err := f.rawInsertProduct(ctx, visibleOption(category, "shared-slug", 1000)); err != nil {
		t.Fatalf("a product slug equal to a group slug must be accepted, got %v", err)
	}

	// A second group with the same slug is refused by the unique index.
	_, err := f.rawInsertGroup(ctx, category, "shared-slug", 0)
	requireConstraint(t, err, sqlStateUnique, idxVariantGroupSlugKey)

	// The adapter reports the same collision as the module sentinel.
	group, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:       "Group shared-slug",
		Slug:       "shared-slug",
		CategoryID: category,
		OptionName: "Card",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	if err := repo.Create(ctx, group); !errors.Is(err, domainerr.ErrVariantGroupSlugTaken) {
		t.Fatalf("expected ErrVariantGroupSlugTaken, got %v", err)
	}

	// A category no row carries is reported as the invalid field naming
	// categoryId, classified from the foreign key.
	orphan, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:       "Orphan group",
		Slug:       "orphan-group",
		CategoryID: uuid.New(),
		OptionName: "Card",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVariantGroup: %v", err)
	}
	err = repo.Create(ctx, orphan)
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid for an unknown category, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the invalid field to be %q, got %v", model.FieldCategoryID, err)
	}
}
