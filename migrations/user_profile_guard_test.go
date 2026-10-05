package migrations

import (
	"strings"
	"testing"
)

// userProfileMigration is the migration that introduces the profile columns on
// the users table and the addresses table.
const userProfileMigration = "00004_user.sql"

// TestUserProfileAvatarConstraintIsReversible guards the storage-level
// "avatar columns are either all null or all populated" invariant (FR-016): the
// constraint must be created in the Up section and dropped in the Down section,
// so the migration stays reversible and a rollback cannot leave it behind.
func TestUserProfileAvatarConstraintIsReversible(t *testing.T) {
	raw, err := FS.ReadFile(userProfileMigration)
	if err != nil {
		t.Fatalf("read %s: %v", userProfileMigration, err)
	}

	content := string(raw)
	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("%s: missing '-- +goose Down' section", userProfileMigration)
	}
	upSection, downSection := content[:downIndex], content[downIndex:]

	for name, section := range map[string]string{"Up": upSection, "Down": downSection} {
		if !strings.Contains(section, "users_avatar_columns_all_or_none_ck") {
			t.Errorf("%s: %s section does not mention users_avatar_columns_all_or_none_ck", userProfileMigration, name)
		}
	}

	if !strings.Contains(upSection, "ADD CONSTRAINT users_avatar_columns_all_or_none_ck CHECK") {
		t.Errorf("%s: Up section does not add users_avatar_columns_all_or_none_ck as a CHECK constraint", userProfileMigration)
	}
	if !strings.Contains(downSection, "DROP CONSTRAINT users_avatar_columns_all_or_none_ck") {
		t.Errorf("%s: Down section does not drop users_avatar_columns_all_or_none_ck", userProfileMigration)
	}
	if !strings.Contains(upSection, "COMMENT ON CONSTRAINT users_avatar_columns_all_or_none_ck") {
		t.Errorf("%s: Up section does not document users_avatar_columns_all_or_none_ck with COMMENT ON CONSTRAINT", userProfileMigration)
	}
}

// TestUserProfileAvatarConstraintCoversEveryAvatarColumn checks that the CHECK
// expression mentions all four avatar columns; a column left out of the
// expression would silently escape the invariant.
func TestUserProfileAvatarConstraintCoversEveryAvatarColumn(t *testing.T) {
	raw, err := FS.ReadFile(userProfileMigration)
	if err != nil {
		t.Fatalf("read %s: %v", userProfileMigration, err)
	}

	upSection := string(raw)
	if downIndex := strings.Index(upSection, "-- +goose Down"); downIndex >= 0 {
		upSection = upSection[:downIndex]
	}

	start := strings.Index(upSection, "ADD CONSTRAINT users_avatar_columns_all_or_none_ck CHECK")
	if start < 0 {
		t.Fatalf("%s: cannot find the avatar constraint to inspect", userProfileMigration)
	}
	rest := upSection[start:]
	end := strings.Index(rest, ");")
	if end < 0 {
		t.Fatalf("%s: avatar constraint expression is not terminated", userProfileMigration)
	}
	expression := rest[:end]

	for _, column := range []string{"avatar_public_id", "avatar_secure_url", "avatar_width", "avatar_height"} {
		if !strings.Contains(expression, column) {
			t.Errorf("%s: avatar constraint does not cover %s", userProfileMigration, column)
		}
	}
}
