package migrations

import (
	"regexp"
	"strings"
	"testing"
)

// productMigration introduces the three product tables of module 04 and the
// restricting foreign key it adds to module 03's categories table.
const productMigration = "00006_product.sql"

// readProductMigration returns the Up and Down halves of the migration, split on
// the goose marker. It fails the test when the file or either section is missing,
// so the assertions below can assume both are present.
func readProductMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(productMigration)
	if err != nil {
		t.Fatalf("read %s: %v", productMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", productMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", productMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestProductMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the catalogue (FR-016, FR-022, FR-027, FR-028, FR-029, FR-030,
// FR-031, FR-038, FR-039): every constraint and index must be created in the Up
// section and dropped in the Down section, so a rollback cannot leave one behind
// and the migration stays reversible.
func TestProductMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readProductMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "unique slug folding key",
			upNeedle: "CREATE UNIQUE INDEX products_normalized_slug_key",
			down:     "DROP INDEX products_normalized_slug_key",
		},
		{
			name:     "slug shape check",
			upNeedle: "ADD CONSTRAINT products_normalized_slug_shape_ck CHECK",
			down:     "DROP CONSTRAINT products_normalized_slug_shape_ck",
		},
		{
			name:     "slug format check",
			upNeedle: "ADD CONSTRAINT products_slug_format_ck CHECK",
			down:     "DROP CONSTRAINT products_slug_format_ck",
		},
		{
			name:     "name length check",
			upNeedle: "ADD CONSTRAINT products_name_length_ck CHECK",
			down:     "DROP CONSTRAINT products_name_length_ck",
		},
		{
			name:     "slug length check",
			upNeedle: "ADD CONSTRAINT products_slug_length_ck CHECK",
			down:     "DROP CONSTRAINT products_slug_length_ck",
		},
		{
			name:     "description length check",
			upNeedle: "ADD CONSTRAINT products_description_length_ck CHECK",
			down:     "DROP CONSTRAINT products_description_length_ck",
		},
		{
			name:     "price amount check",
			upNeedle: "ADD CONSTRAINT products_price_amount_ck CHECK",
			down:     "DROP CONSTRAINT products_price_amount_ck",
		},
		{
			name:     "currency check",
			upNeedle: "ADD CONSTRAINT products_currency_ck CHECK",
			down:     "DROP CONSTRAINT products_currency_ck",
		},
		{
			name:     "sell state check",
			upNeedle: "ADD CONSTRAINT products_sell_state_ck CHECK",
			down:     "DROP CONSTRAINT products_sell_state_ck",
		},
		{
			name:     "pre-order state check",
			upNeedle: "ADD CONSTRAINT products_preorder_ck CHECK",
			down:     "DROP CONSTRAINT products_preorder_ck",
		},
		{
			name:     "pre-order date check",
			upNeedle: "ADD CONSTRAINT products_preorder_date_ck CHECK",
			down:     "DROP CONSTRAINT products_preorder_date_ck",
		},
		{
			name:     "restricting category foreign key",
			upNeedle: "ADD CONSTRAINT products_category_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT products_category_fk",
		},
		{
			name:     "ordering index",
			upNeedle: "CREATE INDEX products_ordering_idx",
			down:     "DROP INDEX products_ordering_idx",
		},
		{
			name:     "category index",
			upNeedle: "CREATE INDEX products_category_idx",
			down:     "DROP INDEX products_category_idx",
		},
		{
			name:     "cascading picture foreign key",
			upNeedle: "ADD CONSTRAINT product_images_product_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_images_product_fk",
		},
		{
			name:     "picture width check",
			upNeedle: "ADD CONSTRAINT product_images_width_ck CHECK",
			down:     "DROP CONSTRAINT product_images_width_ck",
		},
		{
			name:     "picture height check",
			upNeedle: "ADD CONSTRAINT product_images_height_ck CHECK",
			down:     "DROP CONSTRAINT product_images_height_ck",
		},
		{
			name:     "picture HTTPS link check",
			upNeedle: "ADD CONSTRAINT product_images_secure_url_ck CHECK",
			down:     "DROP CONSTRAINT product_images_secure_url_ck",
		},
		{
			name:     "one primary picture index",
			upNeedle: "CREATE UNIQUE INDEX product_images_one_primary_key",
			down:     "DROP INDEX product_images_one_primary_key",
		},
		{
			name:     "picture ordering index",
			upNeedle: "CREATE INDEX product_images_ordering_idx",
			down:     "DROP INDEX product_images_ordering_idx",
		},
		{
			name:     "set membership primary key",
			upNeedle: "ADD CONSTRAINT product_set_items_pkey PRIMARY KEY",
			down:     "DROP CONSTRAINT product_set_items_pkey",
		},
		{
			name:     "set self-reference check",
			upNeedle: "ADD CONSTRAINT product_set_items_not_self_ck CHECK",
			down:     "DROP CONSTRAINT product_set_items_not_self_ck",
		},
		{
			name:     "cascading set foreign key",
			upNeedle: "ADD CONSTRAINT product_set_items_set_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_set_items_set_fk",
		},
		{
			name:     "cascading member foreign key",
			upNeedle: "ADD CONSTRAINT product_set_items_member_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_set_items_member_fk",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", productMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", productMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"products", "product_images", "product_set_items"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", productMigration, table)
		}
	}
}

// TestProductMigrationRestrictingCategoryForeignKey pins the constraint the whole
// of US4 rests on (FR-034, FR-035, research D15): a category with products cannot
// be removed because the database refuses the delete, not because one code path
// checks for products.
func TestProductMigrationRestrictingCategoryForeignKey(t *testing.T) {
	upSection, _ := readProductMigration(t)

	if !strings.Contains(upSection, "products_category_fk") {
		t.Fatalf("%s: Up section does not declare products_category_fk", productMigration)
	}
	if !strings.Contains(upSection, "REFERENCES categories(id) ON DELETE RESTRICT") {
		t.Errorf("%s: products_category_fk must reference categories(id) with ON DELETE RESTRICT", productMigration)
	}
	if strings.Contains(upSection, "REFERENCES categories(id) ON DELETE CASCADE") {
		t.Errorf("%s: the category reference must never cascade: removing a category must not delete its products", productMigration)
	}
}

// TestProductMigrationCascadesPicturesAndSetMembership checks the two cascade
// directions of research D13: a removed product takes its pictures and its
// membership rows with it, and nothing else (US5 scenario 3).
func TestProductMigrationCascadesPicturesAndSetMembership(t *testing.T) {
	upSection, _ := readProductMigration(t)

	for _, fk := range []string{
		"product_images_product_fk FOREIGN KEY",
		"product_set_items_set_fk FOREIGN KEY",
		"product_set_items_member_fk FOREIGN KEY",
	} {
		index := strings.Index(upSection, fk)
		if index < 0 {
			t.Errorf("%s: Up section does not declare %q", productMigration, fk)
			continue
		}
		// The references clause follows on the next line in this migration.
		window := upSection[index:]
		if end := strings.Index(window, ";"); end >= 0 {
			window = window[:end+1]
		}
		if !strings.Contains(window, "ON DELETE CASCADE") {
			t.Errorf("%s: %q must cascade on delete", productMigration, fk)
		}
	}
}

// TestProductMigrationKeepsOnePrimaryPicture checks the partial unique index of
// FR-016 and research D7: "at most one main picture" is a uniqueness property the
// database enforces, not an application check.
func TestProductMigrationKeepsOnePrimaryPicture(t *testing.T) {
	upSection, _ := readProductMigration(t)

	const index = "CREATE UNIQUE INDEX product_images_one_primary_key"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the one-primary index %q", productMigration, index)
	}
	if !strings.Contains(upSection, "ON product_images (product_id) WHERE is_primary") {
		t.Errorf("%s: the one-primary index must be partial: ON product_images (product_id) WHERE is_primary", productMigration)
	}
}

// TestProductMigrationSlugFormatMatchesTheContract checks that the database check
// uses the exact URL-safe pattern the domain rule and
// contracts/openapi.yaml declare; the three must agree or a slug one accepts would
// be refused by another.
func TestProductMigrationSlugFormatMatchesTheContract(t *testing.T) {
	upSection, _ := readProductMigration(t)

	const pattern = `^[a-z0-9]+(-[a-z0-9]+)*$`
	if !strings.Contains(upSection, pattern) {
		t.Errorf("%s: Up section does not carry the contract slug pattern %q", productMigration, pattern)
	}
	if !strings.Contains(upSection, "slug ~ '"+pattern+"'") {
		t.Errorf("%s: Up section does not apply the slug pattern as a regex check on slug", productMigration)
	}
}

// TestProductMigrationNormalisedSlugShapeIsTrimAndLowerShapeOnly checks the shape
// check enforces the stored value is already trimmed and lowercase, and that it
// verifies the *stored* column rather than recomputing a fold from the raw slug.
// The database must not fold: lower() follows the database collation and under C
// would not fold a Vietnamese uppercase letter (research D4).
func TestProductMigrationNormalisedSlugShapeIsTrimAndLowerShapeOnly(t *testing.T) {
	upSection, _ := readProductMigration(t)

	if !strings.Contains(upSection, "normalized_slug = btrim(normalized_slug)") {
		t.Errorf("%s: shape check for normalized_slug does not assert it is trimmed", productMigration)
	}
	if !strings.Contains(upSection, "normalized_slug = lower(normalized_slug)") {
		t.Errorf("%s: shape check for normalized_slug does not assert it is lowercase", productMigration)
	}
	// A check that recomputes the fold from the raw column would compare the
	// normalised value against lower(slug); that is the design research D4 rejects.
	if strings.Contains(upSection, "lower(slug)") {
		t.Errorf("%s: Up section recomputes a fold with lower(slug); the shape check must verify the stored value only", productMigration)
	}
}

// TestProductMigrationLengthChecksCountCharacters checks the three bounds use
// length(), which counts characters rather than bytes, so a Vietnamese
// description at the limit is not refused at a fraction of its allowance (FR-030).
func TestProductMigrationLengthChecksCountCharacters(t *testing.T) {
	upSection, _ := readProductMigration(t)

	for _, bound := range []string{
		"length(name) <= 120",
		"length(slug) <= 140",
		"length(description) <= 5000",
	} {
		if !strings.Contains(upSection, bound) {
			t.Errorf("%s: Up section does not carry the character bound %q", productMigration, bound)
		}
	}
}

// TestProductMigrationRefusesNonPositivePriceAndBadCurrency checks the two money
// invariants of FR-031 and FR-032 live in the schema.
func TestProductMigrationRefusesNonPositivePriceAndBadCurrency(t *testing.T) {
	upSection, _ := readProductMigration(t)

	if !strings.Contains(upSection, "price_amount > 0") {
		t.Errorf("%s: Up section does not refuse a non-positive price_amount", productMigration)
	}
	if !strings.Contains(upSection, "currency ~ '^[A-Z]{3}$'") {
		t.Errorf("%s: Up section does not require a three-uppercase-letter currency", productMigration)
	}
}

// TestProductMigrationListsTheFourSellStates checks the stored state cannot be a
// value the domain does not know (FR-022).
func TestProductMigrationListsTheFourSellStates(t *testing.T) {
	upSection, _ := readProductMigration(t)

	for _, state := range []string{"'COMING_SOON'", "'ACTIVE'", "'OUT_OF_STOCK'", "'DISCONTINUED'"} {
		if !strings.Contains(upSection, state) {
			t.Errorf("%s: Up section does not list the sell state %s", productMigration, state)
		}
	}
}

// TestProductMigrationOrdersByPositionThenCreationThenID checks the ordering index
// is exactly the FR-009 order customers see.
func TestProductMigrationOrdersByPositionThenCreationThenID(t *testing.T) {
	upSection, _ := readProductMigration(t)

	const index = "CREATE INDEX products_ordering_idx ON products (position, created_at, id)"
	if !strings.Contains(upSection, index) {
		t.Errorf("%s: Up section does not create the FR-009 ordering index %q", productMigration, index)
	}
}

// TestProductMigrationCarriesNoStockOrQuantityColumn is the negative half of
// FR-038: this feature tracks no quantity, so no stock or quantity column may be
// introduced, now or by a later convention. The regex matches a column
// declaration (a line whose first token is the name) and deliberately ignores
// prose in comments.
func TestProductMigrationCarriesNoStockOrQuantityColumn(t *testing.T) {
	upSection, _ := readProductMigration(t)

	column := regexp.MustCompile(`(?mi)^\s*(stock|quantity)[a-z_]*\s+[a-z]`)
	if match := column.FindString(upSection); match != "" {
		t.Errorf("%s: Up section declares a stock/quantity column (%q); FR-038 forbids tracking stock in this feature", productMigration, strings.TrimSpace(match))
	}
}

// TestProductMigrationDeclaresTheProductColumns asserts the documented columns are
// present so a later edit cannot silently drop one of the fifteen.
func TestProductMigrationDeclaresTheProductColumns(t *testing.T) {
	upSection, _ := readProductMigration(t)

	for _, column := range []string{
		"id ", "name ", "slug ", "normalized_slug ", "description ",
		"price_amount ", "currency ", "category_id ", "position ",
		"sell_state ", "is_set ", "is_preorder ", "preorder_expected_at ",
		"created_at ", "updated_at ",
	} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not declare column %q", productMigration, strings.TrimSpace(column))
		}
	}
}
