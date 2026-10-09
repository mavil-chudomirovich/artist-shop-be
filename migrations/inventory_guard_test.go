package migrations

import (
	"strings"
	"testing"
)

// inventoryMigration introduces the three inventory tables of module 05.
const inventoryMigration = "00007_inventory.sql"

// readInventoryMigration returns the Up and Down halves of the migration, split on
// the goose marker. It fails the test when the file or either section is missing,
// so the assertions below can assume both are present.
func readInventoryMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(inventoryMigration)
	if err != nil {
		t.Fatalf("read %s: %v", inventoryMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", inventoryMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", inventoryMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestInventoryMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the module (FR-009, FR-010, FR-018, FR-019, FR-020, FR-022): every
// constraint and index must be created in the Up section and dropped in the Down
// section, so a rollback cannot leave one behind and the migration stays
// reversible.
func TestInventoryMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readInventoryMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "non-negative level check",
			upNeedle: "ADD CONSTRAINT stock_levels_quantity_ck CHECK",
			down:     "DROP CONSTRAINT stock_levels_quantity_ck",
		},
		{
			name:     "cascading level foreign key",
			upNeedle: "ADD CONSTRAINT stock_levels_product_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT stock_levels_product_fk",
		},
		{
			name:     "movement kind check",
			upNeedle: "ADD CONSTRAINT inventory_transactions_kind_ck CHECK",
			down:     "DROP CONSTRAINT inventory_transactions_kind_ck",
		},
		{
			name:     "non-zero delta check",
			upNeedle: "ADD CONSTRAINT inventory_transactions_delta_ck CHECK",
			down:     "DROP CONSTRAINT inventory_transactions_delta_ck",
		},
		{
			name:     "non-negative resulting quantity check",
			upNeedle: "ADD CONSTRAINT inventory_transactions_resulting_ck CHECK",
			down:     "DROP CONSTRAINT inventory_transactions_resulting_ck",
		},
		{
			name:     "cascading movement foreign key",
			upNeedle: "ADD CONSTRAINT inventory_transactions_product_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT inventory_transactions_product_fk",
		},
		{
			name:     "partially unique source reference",
			upNeedle: "CREATE UNIQUE INDEX inventory_transactions_source_key",
			down:     "DROP INDEX inventory_transactions_source_key",
		},
		{
			name:     "history index",
			upNeedle: "CREATE INDEX inventory_transactions_history_idx",
			down:     "DROP INDEX inventory_transactions_history_idx",
		},
		{
			name:     "positive hold quantity check",
			upNeedle: "ADD CONSTRAINT stock_holds_quantity_ck CHECK",
			down:     "DROP CONSTRAINT stock_holds_quantity_ck",
		},
		{
			name:     "hold status check",
			upNeedle: "ADD CONSTRAINT stock_holds_status_ck CHECK",
			down:     "DROP CONSTRAINT stock_holds_status_ck",
		},
		{
			name:     "resolved consistency check",
			upNeedle: "ADD CONSTRAINT stock_holds_resolved_ck CHECK",
			down:     "DROP CONSTRAINT stock_holds_resolved_ck",
		},
		{
			name:     "cascading hold foreign key",
			upNeedle: "ADD CONSTRAINT stock_holds_product_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT stock_holds_product_fk",
		},
		{
			name:     "partially unique active hold",
			upNeedle: "CREATE UNIQUE INDEX stock_holds_active_key",
			down:     "DROP INDEX stock_holds_active_key",
		},
		{
			name:     "active hold product index",
			upNeedle: "CREATE INDEX stock_holds_active_idx",
			down:     "DROP INDEX stock_holds_active_idx",
		},
		{
			name:     "hold sweep index",
			upNeedle: "CREATE INDEX stock_holds_sweep_idx",
			down:     "DROP INDEX stock_holds_sweep_idx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", inventoryMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", inventoryMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"stock_levels", "inventory_transactions", "stock_holds"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", inventoryMigration, table)
		}
	}
}

// TestInventoryMigrationRefusesNegativeQuantities pins the two storage rules the
// whole of US2 rests on (FR-009, FR-010): neither the physical count nor the
// recorded result can describe a negative shelf, even for a writer that bypassed
// the application.
func TestInventoryMigrationRefusesNegativeQuantities(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	if !strings.Contains(upSection, "ADD CONSTRAINT stock_levels_quantity_ck CHECK (quantity >= 0)") {
		t.Errorf("%s: Up section does not refuse a negative stock_levels.quantity", inventoryMigration)
	}
	if !strings.Contains(upSection, "ADD CONSTRAINT inventory_transactions_resulting_ck CHECK (resulting_quantity >= 0)") {
		t.Errorf("%s: Up section does not refuse a negative resulting_quantity", inventoryMigration)
	}
}

// TestInventoryMigrationPartiallyUniquesTheSourceReference checks the mechanism
// exactly-once application rests on (FR-020, FR-022): a unique index over the
// source reference, partial so the many manual movements that carry no reference
// are unconstrained.
func TestInventoryMigrationPartiallyUniquesTheSourceReference(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	const index = "CREATE UNIQUE INDEX inventory_transactions_source_key"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the source reference index %q", inventoryMigration, index)
	}
	if !strings.Contains(upSection, "ON inventory_transactions (source_reference) WHERE source_reference IS NOT NULL") {
		t.Errorf("%s: the source reference index must be partial: ON inventory_transactions (source_reference) WHERE source_reference IS NOT NULL", inventoryMigration)
	}
}

// TestInventoryMigrationPartiallyUniquesTheActiveHold checks the mechanism a
// double hold is refused by (FR-019): a unique index over order and product,
// partial so the resolved holds of a repeated checkout do not collide.
func TestInventoryMigrationPartiallyUniquesTheActiveHold(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	const index = "CREATE UNIQUE INDEX stock_holds_active_key"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the active hold index %q", inventoryMigration, index)
	}
	if !strings.Contains(upSection, "ON stock_holds (order_id, product_id) WHERE status = 'ACTIVE'") {
		t.Errorf("%s: the active hold index must be partial: ON stock_holds (order_id, product_id) WHERE status = 'ACTIVE'", inventoryMigration)
	}
}

// TestInventoryMigrationCascadesFromProducts checks research D11: a removed
// product takes its level, its ledger and its holds with it, and nothing else.
func TestInventoryMigrationCascadesFromProducts(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	for _, fk := range []string{
		"stock_levels_product_fk FOREIGN KEY",
		"inventory_transactions_product_fk FOREIGN KEY",
		"stock_holds_product_fk FOREIGN KEY",
	} {
		index := strings.Index(upSection, fk)
		if index < 0 {
			t.Errorf("%s: Up section does not declare %q", inventoryMigration, fk)
			continue
		}
		window := upSection[index:]
		if end := strings.Index(window, ";"); end >= 0 {
			window = window[:end+1]
		}
		if !strings.Contains(window, "ON DELETE CASCADE") {
			t.Errorf("%s: %q must cascade on delete", inventoryMigration, fk)
		}
		if !strings.Contains(window, "REFERENCES products(id)") {
			t.Errorf("%s: %q must reference products(id)", inventoryMigration, fk)
		}
	}
}

// TestInventoryMigrationListsTheFourMovementKinds checks the stored kind cannot be
// a value the domain does not know (FR-004, research D9).
func TestInventoryMigrationListsTheFourMovementKinds(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	for _, kind := range []string{"'RESTOCK'", "'DAMAGE'", "'ADJUSTMENT'", "'SALE'"} {
		if !strings.Contains(upSection, kind) {
			t.Errorf("%s: Up section does not list the movement kind %s", inventoryMigration, kind)
		}
	}
}

// TestInventoryMigrationListsTheThreeHoldStatuses checks the stored status cannot
// be a value the domain does not know (the clarification).
func TestInventoryMigrationListsTheThreeHoldStatuses(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	for _, status := range []string{"'ACTIVE'", "'CONSUMED'", "'RELEASED'"} {
		if !strings.Contains(upSection, status) {
			t.Errorf("%s: Up section does not list the hold status %s", inventoryMigration, status)
		}
	}
}

// TestInventoryMigrationDeclaresTheColumns asserts the documented columns are
// present so a later edit cannot silently drop one of them.
func TestInventoryMigrationDeclaresTheColumns(t *testing.T) {
	upSection, _ := readInventoryMigration(t)

	for _, column := range []string{
		"product_id ", "quantity ", "updated_at ",
		"kind ", "delta ", "resulting_quantity ", "source_reference ",
		"actor_id ", "note ", "created_at ",
		"order_id ", "status ", "expires_at ", "resolved_at ",
	} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not declare column %q", inventoryMigration, strings.TrimSpace(column))
		}
	}
}
