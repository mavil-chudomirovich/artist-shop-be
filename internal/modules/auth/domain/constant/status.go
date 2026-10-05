// Package constant holds auth business constants.
package constant

// Status is an account's lifecycle state.
type Status string

// Supported statuses.
const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)
