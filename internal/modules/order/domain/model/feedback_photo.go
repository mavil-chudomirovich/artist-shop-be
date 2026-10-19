package model

import (
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// FeedbackPhoto is one image attached to the customer's order-level feedback. It
// is stored exactly like a delivery-evidence photo — the image bytes live outside
// the service, held by the media provider, and the feedback keeps only this
// reference (FR-025, research D16). Unlike a delivery photo it carries no stage:
// a feedback is one message, so its photos are a single ordered set.
type FeedbackPhoto struct {
	// ID identifies the photo; the order detail addresses it by this.
	ID uuid.UUID
	// FeedbackID is the feedback the photo belongs to.
	FeedbackID uuid.UUID
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width and Height are the stored pixel dimensions the provider reported.
	Width  int
	Height int
	// Position is the order within the feedback; insertion order.
	Position int
	// CreatedAt is when the photo was written. Set once.
	CreatedAt time.Time
}

// MaxFeedbackPhotos is the per-feedback ceiling of FR-025: a customer's feedback
// may carry up to five photos. It is a count, which no unique index can express,
// so it is enforced by the use case before anything is stored and re-checked
// before the insert (research D16).
const MaxFeedbackPhotos = 5

// EnsureFeedbackPhotoCapacity refuses a feedback that would carry more than five
// photos. existing is the number of photos the feedback would hold once this
// submission is stored; the caller checks it before calling the provider, so a
// refused upload leaves no orphaned asset behind (FR-025, research D16). It
// returns the same ORDER_IMAGE_LIMIT_REACHED sentinel the delivery steps use, so
// the ceiling is one rule for every order image.
func EnsureFeedbackPhotoCapacity(existing int) error {
	if existing > MaxFeedbackPhotos {
		return domainerr.ErrImageLimitReached
	}
	return nil
}
