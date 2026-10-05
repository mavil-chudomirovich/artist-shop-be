// Package access defines roles and permissions shared across modules. Business
// modules depend on these value types rather than importing one another.
package access

// Role is an access level shared across modules.
type Role string

// Supported roles.
const (
	RoleCustomer Role = "CUSTOMER"
	RoleAdmin    Role = "ADMIN"
)
