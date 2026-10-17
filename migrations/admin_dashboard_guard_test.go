package migrations

import (
	"strings"
	"testing"
)

// adminDashboardMigration adds the category snapshot to each order line, so the
// dashboard can report historical revenue by category without re-reading the
// product's current category (specs/017-admin-dashboard, data-model.md).
const adminDashboardMigration = "00017_order_item_category_snapshot.sql"

// readAdminDashboardMigration returns the Up and Down halves of the migration,
// split on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readAdminDashboardMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(adminDashboardMigration)
	if err != nil {
		t.Fatalf("read %s: %v", adminDashboardMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", adminDashboardMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", adminDashboardMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestAdminDashboardMigrationAddsTheCategorySnapshot guards the Up direction: the
// snapshot is two nullable columns, so every existing line migrates without a
// default and a legacy line stays null (FR-022).
func TestAdminDashboardMigrationAddsTheCategorySnapshot(t *testing.T) {
	upSection, _ := readAdminDashboardMigration(t)

	for _, added := range []string{"ADD COLUMN category_id uuid", "ADD COLUMN category_name text"} {
		if !strings.Contains(upSection, added) {
			t.Errorf("%s: Up section does not add %q", adminDashboardMigration, added)
		}
	}
}

// TestAdminDashboardMigrationEnforcesBothOrNone pins the storage constraint: a
// half snapshot (an id with no name, or a name with no id) is refused by the
// database, not only by application code.
func TestAdminDashboardMigrationEnforcesBothOrNone(t *testing.T) {
	upSection, _ := readAdminDashboardMigration(t)

	const check = "((category_id IS NULL) = (category_name IS NULL))"
	if !strings.Contains(upSection, check) {
		t.Errorf("%s: Up section does not enforce %q", adminDashboardMigration, check)
	}
}

// TestAdminDashboardMigrationBackfillsExistingLines guards the data migration:
// existing lines are backfilled from the product's current category where it
// still resolves, and the note the migrations policy requires for a backfill is
// present (migrations/README.md).
func TestAdminDashboardMigrationBackfillsExistingLines(t *testing.T) {
	upSection, _ := readAdminDashboardMigration(t)

	if !strings.Contains(upSection, "-- data-migration:") {
		t.Errorf("%s: Up section backfills data but carries no '-- data-migration:' note", adminDashboardMigration)
	}
	const backfill = "SET category_id = p.category_id"
	if !strings.Contains(upSection, backfill) {
		t.Errorf("%s: Up section does not backfill the snapshot (%q)", adminDashboardMigration, backfill)
	}
}

// TestAdminDashboardMigrationDownDropsCheckThenColumns pins the rollback order:
// the check constraint references the columns, so it must be dropped before them.
func TestAdminDashboardMigrationDownDropsCheckThenColumns(t *testing.T) {
	_, downSection := readAdminDashboardMigration(t)

	const dropCheck = "DROP CONSTRAINT order_items_category_ck"
	dropCheckAt := strings.Index(downSection, dropCheck)
	if dropCheckAt < 0 {
		t.Fatalf("%s: Down section does not drop %q", adminDashboardMigration, dropCheck)
	}
	for _, column := range []string{"DROP COLUMN category_name", "DROP COLUMN category_id"} {
		at := strings.Index(downSection, column)
		if at < 0 {
			t.Fatalf("%s: Down section does not drop %q", adminDashboardMigration, column)
		}
		if at < dropCheckAt {
			t.Errorf("%s: Down section drops %q before the check constraint", adminDashboardMigration, column)
		}
	}
}
