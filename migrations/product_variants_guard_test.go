package migrations

import (
	"strings"
	"testing"
)

// productVariantsMigration adds the two variant tables and the two columns module
// 04 gains (FR-021, FR-022, FR-024, FR-025; research D3).
const productVariantsMigration = "00013_product_variants.sql"

// readProductVariantsMigration returns the Up and Down halves of the migration,
// split on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readProductVariantsMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(productVariantsMigration)
	if err != nil {
		t.Fatalf("read %s: %v", productVariantsMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", productVariantsMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", productVariantsMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestProductVariantsMigrationCreatesAndDropsEveryConstraint guards the
// storage-level invariants of the feature (FR-018 to FR-025): every constraint
// and index must be created in the Up section and dropped in the Down section, so
// a rollback cannot leave one behind and the migration stays reversible.
func TestProductVariantsMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readProductVariantsMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "unique group slug",
			upNeedle: "CREATE UNIQUE INDEX product_variant_groups_slug_key",
			down:     "DROP INDEX product_variant_groups_slug_key",
		},
		{
			name:     "group slug format check",
			upNeedle: "ADD CONSTRAINT product_variant_groups_slug_format_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_groups_slug_format_ck",
		},
		{
			name:     "group name length check",
			upNeedle: "ADD CONSTRAINT product_variant_groups_name_length_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_groups_name_length_ck",
		},
		{
			name:     "group slug length check",
			upNeedle: "ADD CONSTRAINT product_variant_groups_slug_length_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_groups_slug_length_ck",
		},
		{
			name:     "group description length check",
			upNeedle: "ADD CONSTRAINT product_variant_groups_description_length_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_groups_description_length_ck",
		},
		{
			name:     "restricting category foreign key",
			upNeedle: "ADD CONSTRAINT product_variant_groups_category_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_variant_groups_category_fk",
		},
		{
			name:     "group ordering index",
			upNeedle: "CREATE INDEX product_variant_groups_ordering_idx",
			down:     "DROP INDEX product_variant_groups_ordering_idx",
		},
		{
			name:     "group category index",
			upNeedle: "CREATE INDEX product_variant_groups_category_idx",
			down:     "DROP INDEX product_variant_groups_category_idx",
		},
		{
			name:     "cascading group image foreign key",
			upNeedle: "ADD CONSTRAINT product_variant_group_images_group_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_variant_group_images_group_fk",
		},
		{
			name:     "group image width check",
			upNeedle: "ADD CONSTRAINT product_variant_group_images_width_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_group_images_width_ck",
		},
		{
			name:     "group image height check",
			upNeedle: "ADD CONSTRAINT product_variant_group_images_height_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_group_images_height_ck",
		},
		{
			name:     "group image HTTPS link check",
			upNeedle: "ADD CONSTRAINT product_variant_group_images_secure_url_ck CHECK",
			down:     "DROP CONSTRAINT product_variant_group_images_secure_url_ck",
		},
		{
			name:     "one primary group image index",
			upNeedle: "CREATE UNIQUE INDEX product_variant_group_images_one_primary_key",
			down:     "DROP INDEX product_variant_group_images_one_primary_key",
		},
		{
			name:     "group image ordering index",
			upNeedle: "CREATE INDEX product_variant_group_images_ordering_idx",
			down:     "DROP INDEX product_variant_group_images_ordering_idx",
		},
		{
			name:     "restricting variant group foreign key",
			upNeedle: "ADD CONSTRAINT products_variant_group_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT products_variant_group_fk",
		},
		{
			name:     "label-belongs-to-a-group check",
			upNeedle: "ADD CONSTRAINT products_variant_label_ck CHECK",
			down:     "DROP CONSTRAINT products_variant_label_ck",
		},
		{
			name:     "set-not-an-option check",
			upNeedle: "ADD CONSTRAINT products_set_not_variant_ck CHECK",
			down:     "DROP CONSTRAINT products_set_not_variant_ck",
		},
		{
			name:     "partial option label unique index",
			upNeedle: "CREATE UNIQUE INDEX products_variant_label_key",
			down:     "DROP INDEX products_variant_label_key",
		},
		{
			name:     "variant group index on products",
			upNeedle: "CREATE INDEX products_variant_group_idx",
			down:     "DROP INDEX products_variant_group_idx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", productVariantsMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", productVariantsMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"product_variant_groups", "product_variant_group_images"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", productVariantsMigration, table)
		}
	}
}

// TestProductVariantsMigrationRestrictsGroupAndProductForeignKeys pins the two
// restricting foreign keys the feature rests on (FR-023, FR-025; research D10): a
// category with groups cannot be removed, and a group with options cannot be
// removed — because the database refuses the delete, not because one code path
// checks for references.
func TestProductVariantsMigrationRestrictsGroupAndProductForeignKeys(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	for _, fk := range []string{
		"product_variant_groups_category_fk",
		"products_variant_group_fk",
	} {
		if !strings.Contains(upSection, fk) {
			t.Fatalf("%s: Up section does not declare %s", productVariantsMigration, fk)
		}
	}
	if !strings.Contains(upSection, "REFERENCES categories(id) ON DELETE RESTRICT") {
		t.Errorf("%s: product_variant_groups_category_fk must reference categories(id) with ON DELETE RESTRICT", productVariantsMigration)
	}
	if !strings.Contains(upSection, "REFERENCES product_variant_groups(id) ON DELETE RESTRICT") {
		t.Errorf("%s: products_variant_group_fk must reference product_variant_groups(id) with ON DELETE RESTRICT", productVariantsMigration)
	}
	// A cascading products reference would silently delete options when a group
	// is removed, which FR-012 forbids. Scope the check to the products foreign
	// key: the group images foreign key is meant to cascade.
	const productsFK = "products_variant_group_fk FOREIGN KEY"
	index := strings.Index(upSection, productsFK)
	if index < 0 {
		t.Fatalf("%s: Up section does not declare %q", productVariantsMigration, productsFK)
	}
	window := upSection[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "ON DELETE RESTRICT") {
		t.Errorf("%s: %q must restrict on delete", productVariantsMigration, productsFK)
	}
	if strings.Contains(window, "ON DELETE CASCADE") {
		t.Errorf("%s: %q must never cascade: removing a group must not delete its options", productVariantsMigration, productsFK)
	}
}

// TestProductVariantsMigrationCascadesGroupImages checks the cascade direction of
// research D2: a removed group takes its own pictures with it, and nothing else.
func TestProductVariantsMigrationCascadesGroupImages(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	const fk = "product_variant_group_images_group_fk FOREIGN KEY"
	index := strings.Index(upSection, fk)
	if index < 0 {
		t.Fatalf("%s: Up section does not declare %q", productVariantsMigration, fk)
	}
	window := upSection[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "ON DELETE CASCADE") {
		t.Errorf("%s: %q must cascade on delete", productVariantsMigration, fk)
	}
}

// TestProductVariantsMigrationKeepsOnePrimaryGroupImage checks the partial unique
// index of FR-018 and research D2: "at most one main picture" is a uniqueness
// property the database enforces, not an application check.
func TestProductVariantsMigrationKeepsOnePrimaryGroupImage(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	const index = "CREATE UNIQUE INDEX product_variant_group_images_one_primary_key"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the one-primary index %q", productVariantsMigration, index)
	}
	if !strings.Contains(upSection, "ON product_variant_group_images (group_id) WHERE is_primary") {
		t.Errorf("%s: the one-primary index must be partial: ON product_variant_group_images (group_id) WHERE is_primary", productVariantsMigration)
	}
}

// TestProductVariantsMigrationRefusesSetAndOptionCombined checks the same-row
// check of FR-024 and research D9: a combo set and an option are mutually
// exclusive, and the database refuses the combination.
func TestProductVariantsMigrationRefusesSetAndOptionCombined(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	if !strings.Contains(upSection, "NOT (is_set AND variant_group_id IS NOT NULL)") {
		t.Errorf("%s: Up section does not refuse a product that is both a set and a variant option", productVariantsMigration)
	}
}

// TestProductVariantsMigrationKeepsLabelAndGroupTogether checks the same-row check
// of FR-021: a label belongs to a grouped product and to nothing else.
func TestProductVariantsMigrationKeepsLabelAndGroupTogether(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	if !strings.Contains(upSection, "(variant_group_id IS NULL) = (variant_label IS NULL)") {
		t.Errorf("%s: Up section does not bind the label to the group with (variant_group_id IS NULL) = (variant_label IS NULL)", productVariantsMigration)
	}
}

// TestProductVariantsMigrationSlugFormatMatchesTheContract checks that the group
// slug check uses the exact URL-safe pattern the domain rule and the product
// migration declare; the three must agree or a slug one accepts would be refused
// by another.
func TestProductVariantsMigrationSlugFormatMatchesTheContract(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	const pattern = `^[a-z0-9]+(-[a-z0-9]+)*$`
	if !strings.Contains(upSection, pattern) {
		t.Errorf("%s: Up section does not carry the contract slug pattern %q", productVariantsMigration, pattern)
	}
	if !strings.Contains(upSection, "slug ~ '"+pattern+"'") {
		t.Errorf("%s: Up section does not apply the slug pattern as a regex check on slug", productVariantsMigration)
	}
}

// TestProductVariantsMigrationLengthChecksCountCharacters checks the three group
// bounds use length(), which counts characters rather than bytes, so a Vietnamese
// description at the limit is not refused at a fraction of its allowance (FR-020).
func TestProductVariantsMigrationLengthChecksCountCharacters(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	for _, bound := range []string{
		"length(name) <= 120",
		"length(slug) <= 140",
		"length(description) <= 5000",
	} {
		if !strings.Contains(upSection, bound) {
			t.Errorf("%s: Up section does not carry the character bound %q", productVariantsMigration, bound)
		}
	}
}

// TestProductVariantsMigrationDeclaresTheTwoProductColumns asserts the two new
// products columns are added and dropped, so a later edit cannot silently drop one
// (data-model.md — Columns added to products).
func TestProductVariantsMigrationDeclaresTheTwoProductColumns(t *testing.T) {
	upSection, downSection := readProductVariantsMigration(t)

	for _, column := range []string{"variant_group_id uuid", "variant_label text"} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not add column %q", productVariantsMigration, column)
		}
	}
	for _, column := range []string{"DROP COLUMN variant_label", "DROP COLUMN variant_group_id"} {
		if !strings.Contains(downSection, column) {
			t.Errorf("%s: Down section does not drop column %q", productVariantsMigration, column)
		}
	}
}

// TestProductVariantsMigrationOrdersByPositionThenCreationThenID checks both new
// ordering indexes are exactly the FR-009 order customers see.
func TestProductVariantsMigrationOrdersByPositionThenCreationThenID(t *testing.T) {
	upSection, _ := readProductVariantsMigration(t)

	for _, index := range []string{
		"CREATE INDEX product_variant_groups_ordering_idx ON product_variant_groups (position, created_at, id)",
		"product_variant_group_images (group_id, position, id)",
	} {
		if !strings.Contains(upSection, index) {
			t.Errorf("%s: Up section does not create the FR-009 ordering index %q", productVariantsMigration, index)
		}
	}
}
