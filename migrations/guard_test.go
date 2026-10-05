package migrations

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// destructive matches statements that can destroy data when found in the Up
// section of a migration.
var destructive = regexp.MustCompile(`(?i)(DROP TABLE|DROP COLUMN|TRUNCATE|DELETE FROM|ALTER TABLE\s+\S+\s+DROP)`)

// TestMigrationsGuard enforces the destructive-migration policy (FR-005):
// every migration has an Up and Down section, and destructive Up statements
// require an explicit data-migration note.
func TestMigrationsGuard(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		found++

		raw, err := fs.ReadFile(FS, entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		content := string(raw)

		if !strings.Contains(content, "-- +goose Up") {
			t.Errorf("%s: missing '-- +goose Up' section", entry.Name())
		}
		downIndex := strings.Index(content, "-- +goose Down")
		if downIndex < 0 {
			t.Errorf("%s: missing '-- +goose Down' section (rollback path required)", entry.Name())
			continue
		}

		upSection := content[:downIndex]
		if destructive.MatchString(upSection) && !strings.Contains(upSection, "-- data-migration:") {
			t.Errorf("%s: destructive statement in Up requires a '-- data-migration:' note", entry.Name())
		}
	}

	if found == 0 {
		t.Fatal("no migrations found")
	}
}
