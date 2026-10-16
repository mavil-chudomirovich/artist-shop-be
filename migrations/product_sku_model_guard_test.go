package migrations

import (
	"strings"
	"testing"
)

// productSKUModelMigration restructures the catalogue into a parent product with
// child variants (SKUs), repoints inventory/cart/order to the variant and removes
// feature 014's grouping structures (FR-011, FR-012, FR-014–FR-022, FR-025,
// FR-026; research D11).
const productSKUModelMigration = "00014_product_sku_model.sql"

// readProductSKUModelMigration returns the Up and Down halves of the migration,
// split on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readProductSKUModelMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(productSKUModelMigration)
	if err != nil {
		t.Fatalf("read %s: %v", productSKUModelMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", productSKUModelMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", productSKUModelMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestProductSKUModelMigrationCreatesAndDropsEveryConstraint guards the
// storage-level invariants of the restructure: every table, column, constraint and
// index created in the Up section must be dropped in the Down section, so a
// rollback cannot leave one behind and the migration stays reversible.
func TestProductSKUModelMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readProductSKUModelMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "variant table",
			upNeedle: "CREATE TABLE product_variants",
			down:     "DROP TABLE product_variants",
		},
		{
			name:     "cascading product foreign key",
			upNeedle: "ADD CONSTRAINT product_variants_product_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT product_variants_product_fk",
		},
		{
			name:     "variant price check",
			upNeedle: "ADD CONSTRAINT product_variants_price_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_price_ck",
		},
		{
			name:     "variant sell state check",
			upNeedle: "ADD CONSTRAINT product_variants_sell_state_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_sell_state_ck",
		},
		{
			name:     "variant name length check",
			upNeedle: "ADD CONSTRAINT product_variants_name_length_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_name_length_ck",
		},
		{
			name:     "variant image url check",
			upNeedle: "ADD CONSTRAINT product_variants_image_url_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_image_url_ck",
		},
		{
			name:     "variant image width check",
			upNeedle: "ADD CONSTRAINT product_variants_image_width_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_image_width_ck",
		},
		{
			name:     "variant image height check",
			upNeedle: "ADD CONSTRAINT product_variants_image_height_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_image_height_ck",
		},
		{
			name:     "variant image all-or-none check",
			upNeedle: "ADD CONSTRAINT product_variants_image_ck CHECK",
			down:     "DROP CONSTRAINT product_variants_image_ck",
		},
		{
			name:     "unique variant sku index",
			upNeedle: "CREATE UNIQUE INDEX product_variants_sku_key",
			down:     "DROP INDEX product_variants_sku_key",
		},
		{
			name:     "variant ordering index",
			upNeedle: "CREATE INDEX product_variants_product_idx",
			down:     "DROP INDEX product_variants_product_idx",
		},
		{
			name:     "product code column",
			upNeedle: "ALTER TABLE products ADD COLUMN code text",
			down:     "ALTER TABLE products DROP COLUMN code",
		},
		{
			name:     "product code length check",
			upNeedle: "ADD CONSTRAINT products_code_length_ck CHECK",
			down:     "DROP CONSTRAINT products_code_length_ck",
		},
		{
			name:     "product code shape check",
			upNeedle: "ADD CONSTRAINT products_code_shape_ck CHECK",
			down:     "DROP CONSTRAINT products_code_shape_ck",
		},
		{
			name:     "unique product code index",
			upNeedle: "CREATE UNIQUE INDEX products_code_key",
			down:     "DROP INDEX products_code_key",
		},
		{
			name:     "stock level variant column",
			upNeedle: "ALTER TABLE stock_levels ADD COLUMN variant_id uuid",
			down:     "ALTER TABLE stock_levels DROP COLUMN variant_id",
		},
		{
			name:     "stock level variant foreign key",
			upNeedle: "ADD CONSTRAINT stock_levels_variant_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT stock_levels_variant_fk",
		},
		{
			name:     "stock level primary key moves to variant",
			upNeedle: "ADD CONSTRAINT stock_levels_pkey PRIMARY KEY (variant_id)",
			down:     "ADD CONSTRAINT stock_levels_pkey PRIMARY KEY (product_id)",
		},
		{
			name:     "inventory transaction variant column",
			upNeedle: "ALTER TABLE inventory_transactions ADD COLUMN variant_id uuid",
			down:     "ALTER TABLE inventory_transactions DROP COLUMN variant_id",
		},
		{
			name:     "inventory transaction variant foreign key",
			upNeedle: "ADD CONSTRAINT inventory_transactions_variant_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT inventory_transactions_variant_fk",
		},
		{
			name:     "stock hold variant column",
			upNeedle: "ALTER TABLE stock_holds ADD COLUMN variant_id uuid",
			down:     "ALTER TABLE stock_holds DROP COLUMN variant_id",
		},
		{
			name:     "stock hold variant foreign key",
			upNeedle: "ADD CONSTRAINT stock_holds_variant_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT stock_holds_variant_fk",
		},
		{
			name:     "cart item variant column",
			upNeedle: "ALTER TABLE cart_items ADD COLUMN variant_id uuid",
			down:     "ALTER TABLE cart_items DROP COLUMN variant_id",
		},
		{
			name:     "unique cart variant index",
			upNeedle: "CREATE UNIQUE INDEX cart_items_cart_variant_key",
			down:     "DROP INDEX cart_items_cart_variant_key",
		},
		{
			name:     "order item variant column",
			upNeedle: "ADD COLUMN variant_id uuid",
			down:     "ALTER TABLE order_items DROP COLUMN variant_id",
		},
		{
			name:     "order item product code snapshot column",
			upNeedle: "ADD COLUMN product_code text",
			down:     "ALTER TABLE order_items DROP COLUMN product_code",
		},
		{
			name:     "order item variant name snapshot column",
			upNeedle: "ADD COLUMN variant_name text",
			down:     "ALTER TABLE order_items DROP COLUMN variant_name",
		},
		{
			name:     "order item sku snapshot column",
			upNeedle: "ADD COLUMN sku text",
			down:     "ALTER TABLE order_items DROP COLUMN sku",
		},
		{
			name:     "unique order code and sku index",
			upNeedle: "CREATE UNIQUE INDEX order_items_order_code_sku_key",
			down:     "DROP INDEX order_items_order_code_sku_key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", productSKUModelMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", productSKUModelMigration, tc.down)
			}
		})
	}
}

// TestProductSKUModelMigrationReKeysInventoryCartAndOrder pins the four modules
// that move to the variant as their unit (FR-014, FR-018, FR-019, FR-022, research
// D5, D7, D8): each new variant_id foreign key/index is present and the old
// product-keyed unique index is dropped in the same section.
func TestProductSKUModelMigrationReKeysInventoryCartAndOrder(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	for _, needle := range []string{
		"REFERENCES product_variants(id) ON DELETE CASCADE",
		"CREATE UNIQUE INDEX stock_holds_active_key\n    ON stock_holds (order_id, variant_id) WHERE status = 'ACTIVE'",
		"CREATE UNIQUE INDEX cart_items_cart_variant_key ON cart_items (cart_id, variant_id)",
		"CREATE UNIQUE INDEX order_items_order_code_sku_key\n    ON order_items (order_id, product_code, sku)",
	} {
		if !strings.Contains(upSection, needle) {
			t.Errorf("%s: Up section does not carry the re-key %q", productSKUModelMigration, needle)
		}
	}

	for _, old := range []string{
		"DROP INDEX cart_items_cart_product_key",
		"DROP INDEX order_items_order_product_key",
	} {
		if !strings.Contains(upSection, old) {
			t.Errorf("%s: Up section must drop the old product-keyed index %q", productSKUModelMigration, old)
		}
	}
}

// TestProductSKUModelMigrationSkuIndexIsCaseAndSpaceInsensitive checks the SKU is
// globally unique only after trimming surrounding whitespace and ignoring letter
// case (FR-011), the same rule the product slug uses.
func TestProductSKUModelMigrationSkuIndexIsCaseAndSpaceInsensitive(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	const index = "CREATE UNIQUE INDEX product_variants_sku_key ON product_variants (lower(btrim(sku)))"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the folded SKU index %q", productSKUModelMigration, index)
	}
}

// TestProductSKUModelMigrationCodeIndexIsCaseAndSpaceInsensitive checks the
// product code is unique across products, compared after trimming surrounding
// whitespace and ignoring letter case (FR-011).
func TestProductSKUModelMigrationCodeIndexIsCaseAndSpaceInsensitive(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	const index = "CREATE UNIQUE INDEX products_code_key ON products (lower(btrim(code)))"
	if !strings.Contains(upSection, index) {
		t.Fatalf("%s: Up section does not create the folded code index %q", productSKUModelMigration, index)
	}
	if !strings.Contains(upSection, "UPDATE products SET code = slug;") {
		t.Errorf("%s: Up section must backfill products.code from the slug (FR-026)", productSKUModelMigration)
	}
}

// TestProductSKUModelMigrationListsTheFourVariantSellStates checks the stored
// variant state cannot be a value the domain does not know (FR-015).
func TestProductSKUModelMigrationListsTheFourVariantSellStates(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	const check = "product_variants_sell_state_ck"
	if !strings.Contains(upSection, check) {
		t.Fatalf("%s: Up section does not declare %s", productSKUModelMigration, check)
	}
	for _, state := range []string{"'COMING_SOON'", "'ACTIVE'", "'OUT_OF_STOCK'", "'DISCONTINUED'"} {
		if !strings.Contains(upSection, state) {
			t.Errorf("%s: Up section does not list the sell state %s", productSKUModelMigration, state)
		}
	}
}

// TestProductSKUModelMigrationBackfillsOneDefaultVariantPerProduct checks the
// migration inserts exactly one default variant per product from the product's
// code, price, sell state and primary picture (FR-026, research D11).
func TestProductSKUModelMigrationBackfillsOneDefaultVariantPerProduct(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	if !strings.Contains(upSection, "INSERT INTO product_variants") {
		t.Fatalf("%s: Up section does not backfill product_variants", productSKUModelMigration)
	}
	if !strings.Contains(upSection, "FROM products p") {
		t.Errorf("%s: the default-variant backfill must read every product", productSKUModelMigration)
	}
	if !strings.Contains(upSection, "LEFT JOIN LATERAL") {
		t.Errorf("%s: the default-variant backfill must take the product's primary picture", productSKUModelMigration)
	}
}

// TestProductSKUModelMigrationDropsFeature014AndTheProductFlags checks the
// supersede direction of research D2 and D12: the grouping structures and the
// product-level flags are removed, tolerating a database that never ran 00013.
func TestProductSKUModelMigrationDropsFeature014AndTheProductFlags(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	for _, dropped := range []string{
		"DROP TABLE IF EXISTS product_set_items",
		"DROP TABLE IF EXISTS product_variant_group_images",
		"DROP TABLE IF EXISTS product_variant_groups",
		"DROP COLUMN IF EXISTS variant_group_id",
		"DROP COLUMN IF EXISTS variant_label",
		"DROP COLUMN IF EXISTS sell_state",
		"DROP COLUMN IF EXISTS is_preorder",
		"DROP COLUMN IF EXISTS preorder_expected_at",
		"DROP COLUMN IF EXISTS is_set",
	} {
		if !strings.Contains(upSection, dropped) {
			t.Errorf("%s: Up section does not drop %q", productSKUModelMigration, dropped)
		}
	}
}

// TestProductSKUModelMigrationDropsGroupForeignKeyBeforeItsTable checks the drop
// order research D11 warns about: the products foreign key into the group must go
// before the group table, and the group images before the group itself, or the
// migration fails on a database that ran 00013.
func TestProductSKUModelMigrationDropsGroupForeignKeyBeforeItsTable(t *testing.T) {
	upSection, _ := readProductSKUModelMigration(t)

	fkIndex := strings.Index(upSection, "DROP CONSTRAINT IF EXISTS products_variant_group_fk")
	groupsIndex := strings.Index(upSection, "DROP TABLE IF EXISTS product_variant_groups")
	if fkIndex < 0 || groupsIndex < 0 {
		t.Fatalf("%s: Up section does not drop the group foreign key and the group table", productSKUModelMigration)
	}
	if fkIndex > groupsIndex {
		t.Errorf("%s: the products->group foreign key must be dropped before the group table", productSKUModelMigration)
	}

	imagesIndex := strings.Index(upSection, "DROP TABLE IF EXISTS product_variant_group_images")
	if imagesIndex < 0 {
		t.Fatalf("%s: Up section does not drop product_variant_group_images", productSKUModelMigration)
	}
	if imagesIndex > groupsIndex {
		t.Errorf("%s: the group images must be dropped before the group table", productSKUModelMigration)
	}
}
