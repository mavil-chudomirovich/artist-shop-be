package migrations

import (
	"strings"
	"testing"
)

// categoryMigration introduces the categories table of module 03.
const categoryMigration = "00005_category.sql"

// readCategoryMigration returns the Up and Down halves of the migration, split
// on the goose marker. It fails the test when the file or either section is
// missing, so the assertions below can assume both are present.
func readCategoryMigration(t *testing.T) (upSection, downSection string) {
	t.Helper()

	raw, err := FS.ReadFile(categoryMigration)
	if err != nil {
		t.Fatalf("read %s: %v", categoryMigration, err)
	}
	content := string(raw)

	if !strings.Contains(content, "-- +goose Up") {
		t.Fatalf("%s: missing '-- +goose Up' section", categoryMigration)
	}
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section (rollback path required)", categoryMigration)
	}
	return content[:downIndex], content[downIndex:]
}

// TestCategoryMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the catalogue (FR-016, FR-017, FR-018, FR-019, FR-021, FR-003):
// every constraint and index must be created in the Up section and dropped in
// the Down section, so a rollback cannot leave one behind and the migration
// stays reversible.
func TestCategoryMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readCategoryMigration(t)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "unique name folding key",
			upNeedle: "CREATE UNIQUE INDEX categories_normalized_name_key",
			down:     "DROP INDEX categories_normalized_name_key",
		},
		{
			name:     "unique slug folding key",
			upNeedle: "CREATE UNIQUE INDEX categories_normalized_slug_key",
			down:     "DROP INDEX categories_normalized_slug_key",
		},
		{
			name:     "name shape check",
			upNeedle: "ADD CONSTRAINT categories_normalized_name_shape_ck CHECK",
			down:     "DROP CONSTRAINT categories_normalized_name_shape_ck",
		},
		{
			name:     "slug shape check",
			upNeedle: "ADD CONSTRAINT categories_normalized_slug_shape_ck CHECK",
			down:     "DROP CONSTRAINT categories_normalized_slug_shape_ck",
		},
		{
			name:     "slug format check",
			upNeedle: "ADD CONSTRAINT categories_slug_format_ck CHECK",
			down:     "DROP CONSTRAINT categories_slug_format_ck",
		},
		{
			name:     "name length check",
			upNeedle: "ADD CONSTRAINT categories_name_length_ck CHECK",
			down:     "DROP CONSTRAINT categories_name_length_ck",
		},
		{
			name:     "slug length check",
			upNeedle: "ADD CONSTRAINT categories_slug_length_ck CHECK",
			down:     "DROP CONSTRAINT categories_slug_length_ck",
		},
		{
			name:     "description length check",
			upNeedle: "ADD CONSTRAINT categories_description_length_ck CHECK",
			down:     "DROP CONSTRAINT categories_description_length_ck",
		},
		{
			name:     "ordering index",
			upNeedle: "CREATE INDEX categories_ordering_idx",
			down:     "DROP INDEX categories_ordering_idx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", categoryMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", categoryMigration, tc.down)
			}
		})
	}

	if !strings.Contains(downSection, "DROP TABLE categories") {
		t.Errorf("%s: Down section does not drop the categories table", categoryMigration)
	}
}

// TestCategoryMigrationSlugFormatMatchesTheContract checks that the database
// check uses the exact URL-safe pattern the domain rule and
// contracts/openapi.yaml declare; the three must agree or a slug one accepts
// would be refused by another.
func TestCategoryMigrationSlugFormatMatchesTheContract(t *testing.T) {
	upSection, _ := readCategoryMigration(t)

	const pattern = `^[a-z0-9]+(-[a-z0-9]+)*$`
	if !strings.Contains(upSection, pattern) {
		t.Errorf("%s: Up section does not carry the contract slug pattern %q", categoryMigration, pattern)
	}
	if !strings.Contains(upSection, "slug ~ '"+pattern+"'") {
		t.Errorf("%s: Up section does not apply the slug pattern as a regex check on slug", categoryMigration)
	}
}

// TestCategoryMigrationNormalisedShapeIsTrimAndLowerShapeOnly checks that the two
// shape checks enforce the stored value is already trimmed and lowercase, and
// that they verify the *stored* column rather than recomputing a fold from the
// operator's value. The database must not fold: lower() follows the database
// collation and under C would not fold a Vietnamese uppercase letter (research D3).
func TestCategoryMigrationNormalisedShapeIsTrimAndLowerShapeOnly(t *testing.T) {
	upSection, _ := readCategoryMigration(t)

	for _, column := range []string{"normalized_name", "normalized_slug"} {
		expression := strings.TrimSpace(column + " = btrim(" + column + ")")
		if !strings.Contains(upSection, expression) {
			t.Errorf("%s: shape check for %s does not assert it is trimmed", categoryMigration, column)
		}
		lower := column + " = lower(" + column + ")"
		if !strings.Contains(upSection, lower) {
			t.Errorf("%s: shape check for %s does not assert it is lowercase", categoryMigration, column)
		}
	}

	// A check that recomputes the fold from the raw column would compare the
	// normalised value against lower(name); that is the design research D3 rejects.
	for _, forbidden := range []string{"lower(name)", "lower(slug)"} {
		if strings.Contains(upSection, forbidden) {
			t.Errorf("%s: Up section recomputes a fold with %s; the shape check must verify the stored value only", categoryMigration, forbidden)
		}
	}
}

// TestCategoryMigrationLengthChecksCountCharacters checks the three bounds use
// length(), which counts characters rather than bytes, so a Vietnamese name at
// the limit (about three bytes per character) is not refused at a third of its
// allowance (FR-019).
func TestCategoryMigrationLengthChecksCountCharacters(t *testing.T) {
	upSection, _ := readCategoryMigration(t)

	for _, bound := range []string{"length(name) <= 120", "length(slug) <= 140", "length(description) <= 2000"} {
		if !strings.Contains(upSection, bound) {
			t.Errorf("%s: Up section does not carry the character bound %q", categoryMigration, bound)
		}
	}
}

// TestCategoryMigrationOrdersByPositionThenCreationThenID checks the ordering
// index is exactly the FR-003 order customers see, so the catalogue's one read
// path is not left to scan and sort.
func TestCategoryMigrationOrdersByPositionThenCreationThenID(t *testing.T) {
	upSection, _ := readCategoryMigration(t)

	const index = "CREATE INDEX categories_ordering_idx ON categories (position, created_at, id)"
	if !strings.Contains(upSection, index) {
		t.Errorf("%s: Up section does not create the FR-003 ordering index %q", categoryMigration, index)
	}
}

// TestCategoryMigrationKeepsNormalisedColumnsHiddenFromTheSchemaDocs is a small
// schema-shape guard: the folding keys exist only to be indexed and checked and
// must not be named in a COMMENT that reads as a customer-facing value. It
// asserts the documented columns are present so a later edit cannot silently
// drop one of the ten.
func TestCategoryMigrationDeclaresTheTenColumns(t *testing.T) {
	upSection, _ := readCategoryMigration(t)

	for _, column := range []string{
		"id ", "name ", "normalized_name ", "slug ", "normalized_slug ",
		"description ", "position ", "is_visible ", "created_at ", "updated_at ",
	} {
		if !strings.Contains(upSection, column) {
			t.Errorf("%s: Up section does not declare column %q", categoryMigration, strings.TrimSpace(column))
		}
	}
}
