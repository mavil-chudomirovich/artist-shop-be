package model

import (
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// Stage is the delivery step a piece of evidence belongs to. A dispatched order
// carries SHIPPING evidence; a delivered one carries DELIVERED evidence. It is
// stored as constrained text in order_delivery_photos.stage.
type Stage string

const (
	// StageShipping is dispatch evidence: the order handed to the carrier
	// (FR-006).
	StageShipping Stage = "SHIPPING"
	// StageDelivered is delivery evidence: the order received by the customer
	// (FR-008).
	StageDelivered Stage = "DELIVERED"
)

// DeliveryPhoto is one piece of evidence attached to an order at a delivery
// step. Image bytes live outside the service, held by the media provider; the
// order keeps only this reference (FR-009, FR-010, research D3).
type DeliveryPhoto struct {
	// ID identifies the photo; the order detail addresses it by this.
	ID uuid.UUID
	// OrderID is the order the photo belongs to.
	OrderID uuid.UUID
	// Stage is the delivery step the photo is evidence for.
	Stage Stage
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width and Height are the stored pixel dimensions the provider reported.
	Width  int
	Height int
	// Position is the order within its stage; insertion order.
	Position int
	// CreatedAt is when the photo was written. Set once.
	CreatedAt time.Time
}

// MaxDeliveryPhotos is the per-order, per-stage evidence ceiling of FR-006 and
// FR-008. It is a count, which no unique index can express, so it is enforced by
// the use case under the order's row lock and re-checked before the insert
// (research D3, D5).
const MaxDeliveryPhotos = 5

// EnsurePhotoCapacity refuses a photo a delivery step has no room for. The caller
// checks this before calling the provider, so a refused upload leaves no orphaned
// asset behind (FR-006, FR-008, FR-009). The ceiling is per order per stage: the
// stage names which step is being filled, and the refusal carries it so the
// response can name the full step.
func EnsurePhotoCapacity(stage Stage, existing int) error {
	if existing >= MaxDeliveryPhotos {
		return domainerr.DeliveryPhotoLimit(string(stage))
	}
	return nil
}
