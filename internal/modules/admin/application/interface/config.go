package appinterface

import "time"

// Config carries the settings the admin application cannot derive itself, resolved
// once in the composition root.
type Config struct {
	// Location is the shop timezone every calendar boundary and day label uses.
	// The composition loads it from SHOP_TIMEZONE and validates it at startup, so
	// a misconfiguration is refused before the endpoint serves (FR-007,
	// research D4).
	Location *time.Location
}
