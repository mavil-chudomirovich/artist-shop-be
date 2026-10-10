package migrations

import (
	"strings"
	"testing"
)

// orderMigration introduces the two order tables of module 07.
const orderMigration = "00009_order.sql"

// orderConfirmationMigration reshapes orders for feature 010's six-state machine
// (status set, renamed nullable deadline, version, confirmation instant) and adds
// order_edit_history.
const orderConfirmationMigration = "00011_order_confirmation.sql"

// readOrderMigration returns the Up and Down halves of the migration, split on the
// goose marker. It fails the test when the file or either section is missing, so
// the assertions below can assume both are present.
func readOrderMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()
	return readMigration(t, orderMigration)
}

// readMigration returns the Up and Down halves of a migration, split on the goose
// marker. It fails the test when the file or either section is missing.
func readMigration(t *testing.T, name string) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", name)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", name)
	}
	return content[:downIndex], content[downIndex:]
}

// TestOrderMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the order (FR-001, FR-002, FR-008, FR-009, FR-012, FR-018, FR-021):
// every constraint and index must be created in the Up section and dropped in the
// Down section, so a rollback cannot leave one behind and the migration stays
// reversible.
func TestOrderMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readOrderMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "status check",
			upNeedle: "ADD CONSTRAINT orders_status_ck CHECK",
			down:     "DROP CONSTRAINT orders_status_ck",
		},
		{
			name:     "currency check",
			upNeedle: "ADD CONSTRAINT orders_currency_ck CHECK",
			down:     "DROP CONSTRAINT orders_currency_ck",
		},
		{
			name:     "positive total check",
			upNeedle: "ADD CONSTRAINT orders_total_ck CHECK",
			down:     "DROP CONSTRAINT orders_total_ck",
		},
		{
			name:     "owner list index",
			upNeedle: "CREATE INDEX orders_owner_idx",
			down:     "DROP INDEX orders_owner_idx",
		},
		{
			name:     "admin list index",
			upNeedle: "CREATE INDEX orders_admin_idx",
			down:     "DROP INDEX orders_admin_idx",
		},
		{
			name:     "expiry sweep index",
			upNeedle: "CREATE INDEX orders_expiry_idx",
			down:     "DROP INDEX orders_expiry_idx",
		},
		{
			name:     "cascading order foreign key",
			upNeedle: "ADD CONSTRAINT order_items_order_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT order_items_order_fk",
		},
		{
			name:     "one line per product unique index",
			upNeedle: "CREATE UNIQUE INDEX order_items_order_product_key",
			down:     "DROP INDEX order_items_order_product_key",
		},
		{
			name:     "positive quantity check",
			upNeedle: "ADD CONSTRAINT order_items_quantity_ck CHECK",
			down:     "DROP CONSTRAINT order_items_quantity_ck",
		},
		{
			name:     "positive unit price check",
			upNeedle: "ADD CONSTRAINT order_items_unit_price_ck CHECK",
			down:     "DROP CONSTRAINT order_items_unit_price_ck",
		},
		{
			name:     "line currency check",
			upNeedle: "ADD CONSTRAINT order_items_currency_ck CHECK",
			down:     "DROP CONSTRAINT order_items_currency_ck",
		},
		{
			name:     "line ordering index",
			upNeedle: "CREATE INDEX order_items_ordering_idx",
			down:     "DROP INDEX order_items_ordering_idx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", orderMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", orderMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"orders", "order_items"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", orderMigration, table)
		}
	}
}

// TestOrderMigrationListsTheFiveStates checks the stored state cannot be a value
// the domain does not know (FR-009, research D9). The order module names the
// awaiting-payment state PENDING_PAYMENT.
func TestOrderMigrationListsTheFiveStates(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	for _, status := range []string{
		"'PENDING_PAYMENT'", "'PAID'", "'SHIPPED'", "'COMPLETED'", "'CANCELLED'",
	} {
		if !strings.Contains(upSection, status) {
			t.Errorf("%s: Up section does not list the state %s", orderMigration, status)
		}
	}
}

// TestOrderMigrationRefusesBadAmountsAndQuantities pins the check constraints
// FR-002 and FR-008 rest on: a non-positive stored total, a non-positive snapshot
// price, a line of zero or fewer and a currency that is not three uppercase
// letters are refused by the database, not only by the application.
func TestOrderMigrationRefusesBadAmountsAndQuantities(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	for _, check := range []string{
		"CHECK (total_amount > 0)",
		"CHECK (unit_price_amount > 0)",
		"CHECK (quantity >= 1)",
		`CHECK (currency ~ '^[A-Z]{3}$')`,
	} {
		if !strings.Contains(upSection, check) {
			t.Errorf("%s: Up section does not carry the check %q", orderMigration, check)
		}
	}
}

// TestOrderMigrationUniquesTheLinePerProduct pins FR-002: a cart holds one line
// per product, so an order does too, and the database refuses the duplicate
// rather than only the application.
func TestOrderMigrationUniquesTheLinePerProduct(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	const lineKey = "CREATE UNIQUE INDEX order_items_order_product_key"
	if !strings.Contains(upSection, lineKey) {
		t.Fatalf("%s: Up section does not create the one-line index %q", orderMigration, lineKey)
	}
	if !strings.Contains(upSection, "ON order_items (order_id, product_id)") {
		t.Errorf("%s: the one-line index must be on order_items (order_id, product_id)", orderMigration)
	}
}

// TestOrderMigrationCascadesFromOrders checks FR-001: a deleted order takes its
// lines with it, through the cascading foreign key.
func TestOrderMigrationCascadesFromOrders(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	index := strings.Index(upSection, "order_items_order_fk FOREIGN KEY")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare order_items_order_fk", orderMigration)
	}
	window := upSection[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "REFERENCES orders(id) ON DELETE CASCADE") {
		t.Errorf("%s: order_items_order_fk must reference orders(id) with ON DELETE CASCADE", orderMigration)
	}
}

// TestOrderMigrationKeepsTheOwnerAndProductReferencesLoose checks research D3 and
// D12: a deleted product leaves its order lines, and a deleted account leaves its
// orders. Neither `orders.user_id` nor `order_items.product_id` carries a foreign
// key. The assertion is on constraint declarations, not on the comment prose, so a
// later edit that adds a key is caught even if it keeps the comment.
func TestOrderMigrationKeepsTheOwnerAndProductReferencesLoose(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	for _, forbidden := range []string{
		"FOREIGN KEY (user_id)",
		"REFERENCES users",
		"FOREIGN KEY (product_id)",
		"REFERENCES products",
	} {
		if strings.Contains(upSection, forbidden) {
			t.Errorf("%s: the owner and product references must be loose, found %q", orderMigration, forbidden)
		}
	}
}

// TestOrderMigrationDeclaresTheColumns asserts the documented columns are present
// so a later edit cannot silently drop one of them.
func TestOrderMigrationDeclaresTheColumns(t *testing.T) {
	upSection, _ := readOrderMigration(t)

	for _, column := range []string{
		"user_id ", "status ", "total_amount ", "currency ",
		"recipient_name ", "recipient_phone ",
		"province_code ", "province_name ", "ward_code ", "ward_name ", "street_address ",
		"expires_at ", "created_at ", "updated_at ",
		"order_id ", "product_id ", "name ", "slug ",
		"unit_price_amount ", "quantity ", "position ",
	} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not declare column %q", orderMigration, strings.TrimSpace(column))
		}
	}
}

// TestOrderConfirmationMigrationBackfillsAndReshapes guards the storage-level
// invariants feature 010 rests on (FR-006, FR-008, FR-017, FR-018): the old
// PENDING_PAYMENT value is backfilled BEFORE the check is widened, the six-state
// check is installed, the deadline is renamed and made nullable, the version and
// confirmation columns are added, and the history table is created — each created
// in the Up section and reverted in the Down section, so the migration stays
// reversible.
func TestOrderConfirmationMigrationBackfillsAndReshapes(t *testing.T) {
	up, down := readMigration(t, orderConfirmationMigration)

	upNeeds := []string{
		"UPDATE orders SET status = 'PAYMENT_PENDING' WHERE status = 'PENDING_PAYMENT'",
		"DROP CONSTRAINT orders_status_ck",
		"ALTER TABLE orders ADD CONSTRAINT orders_status_ck CHECK",
		"'PENDING', 'PAYMENT_PENDING', 'PAID', 'SHIPPED', 'COMPLETED', 'CANCELLED'",
		"RENAME COLUMN expires_at TO payment_expires_at",
		"ALTER COLUMN payment_expires_at DROP NOT NULL",
		"ADD COLUMN order_version bigint NOT NULL DEFAULT 1",
		"orders_version_ck CHECK (order_version >= 1)",
		"ADD COLUMN confirmed_at timestamptz",
		"DROP INDEX orders_expiry_idx",
		"CREATE INDEX orders_expiry_idx ON orders (status, payment_expires_at)",
		"CREATE TABLE order_edit_history",
		"order_edit_history_order_fk FOREIGN KEY (order_id)",
		"REFERENCES orders(id) ON DELETE CASCADE",
		"order_edit_history_version_ck CHECK (version >= 1)",
		"CREATE INDEX order_edit_history_order_idx ON order_edit_history (order_id, version)",
	}
	for _, needle := range upNeeds {
		if !strings.Contains(up, needle) {
			t.Errorf("%s: Up section is missing %q", orderConfirmationMigration, needle)
		}
	}

	// The backfill must come before the widened check, or a database holding the
	// old value would fail the new check (research D1).
	if backfill, widen := strings.Index(up, "UPDATE orders SET status = 'PAYMENT_PENDING'"),
		strings.Index(up, "ADD CONSTRAINT orders_status_ck"); backfill < 0 || widen < 0 || backfill > widen {
		t.Errorf("%s: the PENDING_PAYMENT backfill must precede the widened status check", orderConfirmationMigration)
	}

	downNeeds := []string{
		"DROP TABLE order_edit_history",
		"DROP CONSTRAINT order_edit_history_order_fk",
		"DROP CONSTRAINT order_edit_history_version_ck",
		"DROP INDEX order_edit_history_order_idx",
		"DROP INDEX orders_expiry_idx",
		"RENAME COLUMN payment_expires_at TO expires_at",
		"CREATE INDEX orders_expiry_idx ON orders (status, expires_at)",
		"DROP COLUMN confirmed_at",
		"DROP CONSTRAINT orders_version_ck",
		"DROP COLUMN order_version",
		"DROP CONSTRAINT orders_status_ck",
	}
	for _, needle := range downNeeds {
		if !strings.Contains(down, needle) {
			t.Errorf("%s: Down section is missing %q", orderConfirmationMigration, needle)
		}
	}
}

// TestOrderConfirmationMigrationListsTheSixStates checks the widened check lists
// exactly the six states and no longer the renamed PENDING_PAYMENT (FR-001,
// FR-018, research D1).
func TestOrderConfirmationMigrationListsTheSixStates(t *testing.T) {
	up, _ := readMigration(t, orderConfirmationMigration)

	index := strings.Index(up, "orders_status_ck CHECK")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare the widened orders_status_ck", orderConfirmationMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end]
	}
	for _, status := range []string{
		"'PENDING'", "'PAYMENT_PENDING'", "'PAID'", "'SHIPPED'", "'COMPLETED'", "'CANCELLED'",
	} {
		if !strings.Contains(window, status) {
			t.Errorf("%s: the widened check does not list the state %s", orderConfirmationMigration, status)
		}
	}
	if strings.Contains(window, "'PENDING_PAYMENT'") {
		t.Errorf("%s: the widened check must not list the renamed PENDING_PAYMENT", orderConfirmationMigration)
	}
}

// TestOrderEditHistoryCascadesAndChecksVersion pins the history table's storage
// invariants (FR-017): a removed order takes its history with it, and the version
// is the order's positive counter.
func TestOrderEditHistoryCascadesAndChecksVersion(t *testing.T) {
	up, _ := readMigration(t, orderConfirmationMigration)

	index := strings.Index(up, "order_edit_history_order_fk FOREIGN KEY")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare order_edit_history_order_fk", orderConfirmationMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "REFERENCES orders(id) ON DELETE CASCADE") {
		t.Errorf("%s: order_edit_history_order_fk must reference orders(id) with ON DELETE CASCADE", orderConfirmationMigration)
	}
	if !strings.Contains(up, "order_edit_history_version_ck CHECK (version >= 1)") {
		t.Errorf("%s: Up section must constrain order_edit_history.version >= 1", orderConfirmationMigration)
	}
}
