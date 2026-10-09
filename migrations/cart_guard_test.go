package migrations

import (
	"strings"
	"testing"
)

// cartMigration introduces the two cart tables of module 06.
const cartMigration = "00008_cart.sql"

// readCartMigration returns the Up and Down halves of the migration, split on the
// goose marker. It fails the test when the file or either section is missing, so
// the assertions below can assume both are present.
func readCartMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(cartMigration)
	if err != nil {
		t.Fatalf("read %s: %v", cartMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", cartMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", cartMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestCartMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the cart (FR-001, FR-002, FR-006, FR-009): every constraint and
// index must be created in the Up section and dropped in the Down section, so a
// rollback cannot leave one behind and the migration stays reversible.
func TestCartMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readCartMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "one cart per account unique index",
			upNeedle: "CREATE UNIQUE INDEX carts_user_key",
			down:     "DROP INDEX carts_user_key",
		},
		{
			name:     "cascading cart owner foreign key",
			upNeedle: "ADD CONSTRAINT carts_user_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT carts_user_fk",
		},
		{
			name:     "positive quantity check",
			upNeedle: "ADD CONSTRAINT cart_items_quantity_ck CHECK",
			down:     "DROP CONSTRAINT cart_items_quantity_ck",
		},
		{
			name:     "positive unit price check",
			upNeedle: "ADD CONSTRAINT cart_items_unit_price_ck CHECK",
			down:     "DROP CONSTRAINT cart_items_unit_price_ck",
		},
		{
			name:     "currency shape check",
			upNeedle: "ADD CONSTRAINT cart_items_currency_ck CHECK",
			down:     "DROP CONSTRAINT cart_items_currency_ck",
		},
		{
			name:     "cascading cart foreign key",
			upNeedle: "ADD CONSTRAINT cart_items_cart_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT cart_items_cart_fk",
		},
		{
			name:     "one line per product unique index",
			upNeedle: "CREATE UNIQUE INDEX cart_items_cart_product_key",
			down:     "DROP INDEX cart_items_cart_product_key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", cartMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", cartMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"carts", "cart_items"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", cartMigration, table)
		}
	}
}

// TestCartMigrationUniquesTheAccountAndTheProductLine pins the two storage rules
// the whole of US1 and US3 rest on (FR-001, FR-002): one cart per account and one
// line per product, both refused by the database rather than only by an
// application check.
func TestCartMigrationUniquesTheAccountAndTheProductLine(t *testing.T) {
	upSection, _ := readCartMigration(t)

	const userKey = "CREATE UNIQUE INDEX carts_user_key"
	if !strings.Contains(upSection, userKey) {
		t.Fatalf("%s: Up section does not create the one-cart index %q", cartMigration, userKey)
	}
	if !strings.Contains(upSection, "ON carts (user_id)") {
		t.Errorf("%s: the one-cart index must be on carts (user_id)", cartMigration)
	}

	const lineKey = "CREATE UNIQUE INDEX cart_items_cart_product_key"
	if !strings.Contains(upSection, lineKey) {
		t.Fatalf("%s: Up section does not create the one-line index %q", cartMigration, lineKey)
	}
	if !strings.Contains(upSection, "ON cart_items (cart_id, product_id)") {
		t.Errorf("%s: the one-line index must be on cart_items (cart_id, product_id)", cartMigration)
	}
}

// TestCartMigrationKeepsTheProductReferenceLoose checks research D3: a cart line
// references its product with NO foreign key, so removing a product leaves the
// customer's line in place and the cart can report it as no longer available
// (FR-012). The assertion is on constraint declarations, not on the comment prose,
// so a later edit that adds the key is caught even if it keeps the comment.
func TestCartMigrationKeepsTheProductReferenceLoose(t *testing.T) {
	upSection, _ := readCartMigration(t)

	for _, forbidden := range []string{
		"FOREIGN KEY (product_id)",
		"REFERENCES products",
	} {
		if strings.Contains(upSection, forbidden) {
			t.Errorf("%s: cart_items.product_id must carry no foreign key, found %q", cartMigration, forbidden)
		}
	}

	// The cart reference is the one that must be present, and it cascades.
	const cartFK = "cart_items_cart_fk FOREIGN KEY"
	index := strings.Index(upSection, cartFK)
	if index < 0 {
		t.Fatalf("%s: Up section does not declare %q", cartMigration, cartFK)
	}
	window := upSection[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "REFERENCES carts(id) ON DELETE CASCADE") {
		t.Errorf("%s: %q must reference carts(id) with ON DELETE CASCADE", cartMigration, cartFK)
	}
}

// TestCartMigrationCascadesTheOwnerCart checks FR-001: an account and its cart
// both take their lines with them when removed.
func TestCartMigrationCascadesTheOwnerCart(t *testing.T) {
	upSection, _ := readCartMigration(t)

	index := strings.Index(upSection, "carts_user_fk FOREIGN KEY")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare carts_user_fk", cartMigration)
	}
	window := upSection[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "REFERENCES users(id) ON DELETE CASCADE") {
		t.Errorf("%s: carts_user_fk must reference users(id) with ON DELETE CASCADE", cartMigration)
	}
}

// TestCartMigrationRefusesBadQuantitiesAndMoney pins the three check constraints
// FR-006 and FR-009 rest on: a stored zero or negative quantity, a non-positive
// captured price and a currency that is not three uppercase letters are refused
// by the database, not only by the application.
func TestCartMigrationRefusesBadQuantitiesAndMoney(t *testing.T) {
	upSection, _ := readCartMigration(t)

	for _, check := range []string{
		"CHECK (quantity >= 1)",
		"CHECK (unit_price_amount > 0)",
		`CHECK (currency ~ '^[A-Z]{3}$')`,
	} {
		if !strings.Contains(upSection, check) {
			t.Errorf("%s: Up section does not carry the check %q", cartMigration, check)
		}
	}
}

// TestCartMigrationDeclaresTheColumns asserts the documented columns are present
// so a later edit cannot silently drop one of them.
func TestCartMigrationDeclaresTheColumns(t *testing.T) {
	upSection, _ := readCartMigration(t)

	for _, column := range []string{
		"user_id ", "created_at ", "updated_at ",
		"cart_id ", "product_id ", "quantity ", "unit_price_amount ", "currency ",
	} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not declare column %q", cartMigration, strings.TrimSpace(column))
		}
	}
}
