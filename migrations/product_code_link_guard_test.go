package migrations

import (
	"strings"
	"testing"
)

// productCodeLinkMigration collapses a product's code and slug into one value: the
// code becomes the single public link segment and the duplicate slug columns, their
// constraints and the order line's slug snapshot are removed (FR-011).
const productCodeLinkMigration = "00015_product_code_as_link_segment.sql"

// readProductCodeLinkMigration returns the Up and Down halves of the migration,
// split on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readProductCodeLinkMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(productCodeLinkMigration)
	if err != nil {
		t.Fatalf("read %s: %v", productCodeLinkMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", productCodeLinkMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", productCodeLinkMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestProductCodeLinkMigrationDropsTheSlugAndDropsEveryConstraint guards the Up
// direction: the duplicate link columns, their constraints and the order line's
// slug snapshot are dropped, and the code keeps a length check while gaining the
// URL-safe format check.
func TestProductCodeLinkMigrationDropsTheSlugAndDropsEveryConstraint(t *testing.T) {
	upSection, _ := readProductCodeLinkMigration(t)

	for _, dropped := range []string{
		"DROP CONSTRAINT IF EXISTS products_normalized_slug_shape_ck",
		"DROP CONSTRAINT IF EXISTS products_slug_format_ck",
		"DROP CONSTRAINT IF EXISTS products_slug_length_ck",
		"DROP INDEX IF EXISTS products_normalized_slug_key",
		"ALTER TABLE products DROP COLUMN IF EXISTS normalized_slug",
		"ALTER TABLE products DROP COLUMN IF EXISTS slug",
		"ALTER TABLE order_items DROP COLUMN IF EXISTS slug",
	} {
		if !strings.Contains(upSection, dropped) {
			t.Errorf("%s: Up section does not drop %q", productCodeLinkMigration, dropped)
		}
	}

	for _, added := range []string{
		"ADD CONSTRAINT products_code_format_ck CHECK",
		"ADD CONSTRAINT products_code_length_ck CHECK",
	} {
		if !strings.Contains(upSection, added) {
			t.Errorf("%s: Up section does not add %q", productCodeLinkMigration, added)
		}
	}
}

// TestProductCodeLinkMigrationCodeFormatMatchesTheContract checks the code shape
// check uses the exact URL-safe pattern the domain rule declares, so the two cannot
// drift and one would refuse a value the other accepts (FR-011).
func TestProductCodeLinkMigrationCodeFormatMatchesTheContract(t *testing.T) {
	upSection, _ := readProductCodeLinkMigration(t)

	const pattern = `^[a-z0-9]+(-[a-z0-9]+)*$`
	if !strings.Contains(upSection, "code ~ '"+pattern+"'") {
		t.Errorf("%s: Up section does not apply the code pattern as a regex check on code", productCodeLinkMigration)
	}
}

// TestProductCodeLinkMigrationCodeLengthBoundIs140 checks the code bound is the
// same 140-character ceiling the domain enforces.
func TestProductCodeLinkMigrationCodeLengthBoundIs140(t *testing.T) {
	upSection, _ := readProductCodeLinkMigration(t)

	if !strings.Contains(upSection, "length(code) <= 140") {
		t.Errorf("%s: Up section does not bound code to 140 characters", productCodeLinkMigration)
	}
}

// TestProductCodeLinkMigrationDownRestoresTheSlugColumns pins the rollback: the
// slug and normalized_slug columns are recreated, backfilled from code, made
// required, given their 00006 constraints and index back, and the order line's slug
// snapshot is recreated.
func TestProductCodeLinkMigrationDownRestoresTheSlugColumns(t *testing.T) {
	_, downSection := readProductCodeLinkMigration(t)

	for _, restored := range []string{
		"ALTER TABLE products ADD COLUMN slug text",
		"UPDATE products SET slug = code",
		"ALTER TABLE products ADD COLUMN normalized_slug text",
		"UPDATE products SET normalized_slug = lower(slug)",
		"ADD CONSTRAINT products_normalized_slug_shape_ck CHECK",
		"ADD CONSTRAINT products_slug_format_ck CHECK",
		"ADD CONSTRAINT products_slug_length_ck CHECK",
		"CREATE UNIQUE INDEX products_normalized_slug_key",
		"ALTER TABLE order_items ADD COLUMN slug text",
		"UPDATE order_items SET slug = product_code",
	} {
		if !strings.Contains(downSection, restored) {
			t.Errorf("%s: Down section does not restore %q", productCodeLinkMigration, restored)
		}
	}
}
