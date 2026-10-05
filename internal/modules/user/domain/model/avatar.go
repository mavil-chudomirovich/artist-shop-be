package model

// AvatarReference points at the customer's stored avatar in the external media
// service. Image bytes are never kept in the database: only this reference is
// (FR-016).
//
// A nil *AvatarReference means the customer has no photo, and the reference
// itself is the single signal for that — there is deliberately no separate
// "hasAvatar" flag that could disagree with the stored columns.
//
// The stored dimensions are the provider's own report after it applied the
// resize; the domain guard that keeps the width within the ceiling, and the
// all-or-nothing rule over the four stored columns, are added with the avatar
// use cases (T044, T047).
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
