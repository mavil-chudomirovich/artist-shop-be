package migrations

import (
	"strings"
	"testing"
)

// orderItemImageMigration snapshots the display image of the variant a line sold,
// falling back to the product's primary gallery picture, so a past order never
// depends on the product or variant still existing (FR-002).
const orderItemImageMigration = "00022_order_item_image_url.sql"

// readOrderItemImageMigration returns the Up and Down halves of the migration,
// split on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readOrderItemImageMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(orderItemImageMigration)
	if err != nil {
		t.Fatalf("read %s: %v", orderItemImageMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", orderItemImageMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", orderItemImageMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestOrderItemImageMigrationAddsTheSnapshotColumn guards the Up direction: the
// column is added with an empty fallback, existing rows are backfilled from the
// current variant or product image, and the storage-level HTTPS rule is installed.
func TestOrderItemImageMigrationAddsTheSnapshotColumn(t *testing.T) {
	upSection, _ := readOrderItemImageMigration(t)

	for _, want := range []string{
		"ALTER TABLE order_items ADD COLUMN image_url text NOT NULL DEFAULT ''",
		"UPDATE order_items oi",
		"NULLIF(v.image_secure_url, '')",
		"FROM product_images i",
		"ADD CONSTRAINT order_items_image_url_ck CHECK (image_url = '' OR image_url LIKE 'https://%')",
	} {
		if !strings.Contains(upSection, want) {
			t.Errorf("%s: Up section does not contain %q", orderItemImageMigration, want)
		}
	}
}

// TestOrderItemImageMigrationDownDropsTheColumn guards the rollback: the check is
// dropped before the column it references.
func TestOrderItemImageMigrationDownDropsTheColumn(t *testing.T) {
	_, downSection := readOrderItemImageMigration(t)

	droppedConstraint := strings.Index(downSection, "DROP CONSTRAINT order_items_image_url_ck")
	droppedColumn := strings.Index(downSection, "DROP COLUMN image_url")
	if droppedConstraint < 0 {
		t.Fatalf("%s: Down section does not drop the image check", orderItemImageMigration)
	}
	if droppedColumn < 0 {
		t.Fatalf("%s: Down section does not drop image_url", orderItemImageMigration)
	}
	if droppedConstraint > droppedColumn {
		t.Errorf("%s: Down section must drop the check before the column it references", orderItemImageMigration)
	}
}
