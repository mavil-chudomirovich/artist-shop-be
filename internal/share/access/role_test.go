package access

import "testing"

// TestRoleWireValues locks the string values persisted in PostgreSQL, embedded in
// JWT claims and stored in Redis. Changing them requires a migration.
func TestRoleWireValues(t *testing.T) {
	if RoleCustomer != "CUSTOMER" {
		t.Fatalf("unexpected customer role: %q", RoleCustomer)
	}
	if RoleAdmin != "ADMIN" {
		t.Fatalf("unexpected admin role: %q", RoleAdmin)
	}
	if RoleCustomer == RoleAdmin {
		t.Fatal("customer and admin roles must differ")
	}
}
