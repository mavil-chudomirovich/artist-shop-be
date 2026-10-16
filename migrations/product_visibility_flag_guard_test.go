package migrations

import (
	"strings"
	"testing"
)

// productVisibilityFlagMigration adds the product's own visibility flag: a
// boolean column defaulting to visible, beside the existing category and
// on-sale-variant terms of the catalogue predicate (specs/016-hide-guard,
// research D6).
const productVisibilityFlagMigration = "00016_product_visibility_flag.sql"

// readProductVisibilityFlagMigration returns the Up and Down halves of the
// migration, split on the goose marker. It fails the test when the file or
// either section is missing, so the assertions below can assume both are
// present.
func readProductVisibilityFlagMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(productVisibilityFlagMigration)
	if err != nil {
		t.Fatalf("read %s: %v", productVisibilityFlagMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", productVisibilityFlagMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", productVisibilityFlagMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestProductVisibilityFlagMigrationAddsAVisibleByDefaultFlag guards the Up
// direction: the flag column is added as NOT NULL with a true default, so every
// existing product stays visible and no backfill is needed.
func TestProductVisibilityFlagMigrationAddsAVisibleByDefaultFlag(t *testing.T) {
	upSection, _ := readProductVisibilityFlagMigration(t)

	const added = "ALTER TABLE products ADD COLUMN is_visible boolean NOT NULL DEFAULT true"
	if !strings.Contains(upSection, added) {
		t.Errorf("%s: Up section does not add %q", productVisibilityFlagMigration, added)
	}
}

// TestProductVisibilityFlagMigrationDownDropsTheColumn pins the rollback: the
// flag column is dropped, restoring the catalogue predicate to the category and
// on-sale-variant terms it already had.
func TestProductVisibilityFlagMigrationDownDropsTheColumn(t *testing.T) {
	_, downSection := readProductVisibilityFlagMigration(t)

	const dropped = "ALTER TABLE products DROP COLUMN is_visible"
	if !strings.Contains(downSection, dropped) {
		t.Errorf("%s: Down section does not drop %q", productVisibilityFlagMigration, dropped)
	}
}
