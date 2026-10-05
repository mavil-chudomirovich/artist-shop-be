package model

import "strings"

// AvatarReference points at the customer's stored avatar in the external media
// service. Image bytes are never kept in the database: only this reference is
// (FR-016).
//
// A nil *AvatarReference means the customer has no photo, and the reference
// itself is the single signal for that — there is deliberately no separate
// "hasAvatar" flag that could disagree with the stored columns.
//
// The stored dimensions are the provider's own report after it applied the
// resize. The domain guard that keeps the width within the ceiling is added with
// the avatar use cases (T047).
type AvatarReference struct {
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width is the stored pixel width, at most the configured ceiling (FR-015).
	Width int
	// Height is the stored pixel height.
	Height int
}

// IsComplete reports whether the reference carries every value the four stored
// avatar columns are made of.
//
// The columns move together: they are all NULL when the customer has no photo
// and all populated otherwise, so a reference is either wholly present or
// wholly absent. A nil reference is never complete — the absence of a photo is
// expressed by the nil pointer, not by a zeroed struct (FR-016).
func (a *AvatarReference) IsComplete() bool {
	if a == nil {
		return false
	}
	return strings.TrimSpace(a.PublicID) != "" && strings.TrimSpace(a.URL) != "" &&
		a.Width > 0 && a.Height > 0
}
