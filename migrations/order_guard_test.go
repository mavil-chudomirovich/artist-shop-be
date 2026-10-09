package migrations

import (
	"strings"
	"testing"
)

// orderMigration introduces the two order tables of module 07.
const orderMigration = "00009_order.sql"

// readOrderMigration returns the Up and Down halves of the migration, split on the
// goose marker. It fails the test when the file or either section is missing, so
// the assertions below can assume both are present.
func readOrderMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(orderMigration)
	if err != nil {
		t.Fatalf("read %s: %v", orderMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", orderMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", orderMigration)
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
