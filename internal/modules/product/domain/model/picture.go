package model

import (
	"sort"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// Picture is an image attached to a product, held outside the service by the media
// provider (FR-018). The service keeps the reference, never the bytes.
//
// A product carries several pictures in an order the operator does not edit
// (research D11), with at most one of them the main picture. The picture collection
// is the product's, so the rules below are pure functions over it.
type Picture struct {
	// ID identifies the picture; the administrator surface addresses it by this.
	ID uuid.UUID
	// ProductID is the product it belongs to.
	ProductID uuid.UUID
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients. It must be HTTPS.
	URL string
	// Width and Height are the stored pixel dimensions the provider reported.
	Width  int
	Height int
	// Position is the display order. Insertion order; the operator does not edit
	// it.
	Position int
	// IsPrimary reports whether this is the product's main picture.
	IsPrimary bool
	// CreatedAt is when the picture was added. Set once.
	CreatedAt time.Time
}

// MaxProductPictures is the per-product picture ceiling of FR-020. It is enforced
// before storage, so an eleventh upload never reaches the provider.
const MaxProductPictures = 10

// MaxPictureBytes is the 2 MB upload ceiling of FR-019, the same one an avatar
// upload already uses. It is the request-shape limit; the count ceiling above is a
// property of the product.
const MaxPictureBytes int64 = 2 << 20

// EnsurePictureCapacity refuses a picture the product has no room for. The caller
// checks this before calling the provider, so a refused upload leaves no orphaned
// asset behind (FR-020).
func EnsurePictureCapacity(existing int) error {
	if existing >= MaxProductPictures {
		return domainerr.ErrProductImageLimitReached
	}
	return nil
}

// ValidatePictureUpload decides whether an uploaded payload may be sent to the
// provider. It is deliberately a pure function of the bytes so it can be unit
// tested and reused by any caller.
//
//   - the format is decided by sniffing the leading signature, never from a file
//     name or a client-declared media type, both of which the client chooses
//     (FR-019);
//   - only the signature is inspected, not the image: decoding pixels here would
//     pull an imaging dependency into this binary for a capability the provider
//     already has;
//   - the size ceiling is enforced on the payload itself. A caller that reads a
//     request body must cap the read as well (http.MaxBytesReader) — a post-hoc
//     check on an already buffered body cannot stop a memory-exhaustion upload —
//     but this function is where the rule lives.
//
// maxBytes is the configured ceiling; zero or less means the composition left it
// unset, in which case the documented ceiling applies rather than no limit at all.
// The type is checked before the size, so an unsupported payload that is also
// oversized is reported as the type error — the problem the operator can fix by
// choosing a different file.
func ValidatePictureUpload(content []byte, maxBytes int64) error {
	if !isSupportedImage(content) {
		return domainerr.ErrProductImageTypeUnsupported
	}
	ceiling := maxBytes
	if ceiling <= 0 {
		ceiling = MaxPictureBytes
	}
	if int64(len(content)) > ceiling {
		return domainerr.ErrProductImageTooLarge
	}
	return nil
}

// isSupportedImage reports whether the payload begins with the signature of one of
// the three accepted formats.
//
// These are the format definitions, not a heuristic: JPEG opens with the SOI marker
// FF D8 followed by a further marker byte, PNG opens with its fixed eight-byte
// header, and WebP is a RIFF container whose form type is "WEBP" at offset 8. A
// GIF, a BMP, an SVG, a PDF and a text file all fail here, which is the property
// FR-019 is really about.
func isSupportedImage(content []byte) bool {
	switch {
	case len(content) >= 3 && content[0] == 0xFF && content[1] == 0xD8 && content[2] == 0xFF:
		return true
	case len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP":
		return true
	default:
		return false
	}
}

// HasAtMostOnePrimary reports whether no more than one picture is flagged as the
// main one (FR-016, research D7).
//
// The partial unique index product_images_one_primary_key is the real guarantee,
// because it cannot be bypassed by any code path; this function is the domain's own
// check, so a caller can detect a malformed collection before it tries to store it.
func HasAtMostOnePrimary(pictures []Picture) bool {
	primary := 0
	for _, picture := range pictures {
		if picture.IsPrimary {
			primary++
		}
	}
	return primary <= 1
}

// OrderPictures returns the pictures in the order a client shows them: position
// first, then the identifier to break a tie deterministically (FR-017, research
// D17). A new picture never overwrites another's place, because its position is
// taken after every existing one.
func OrderPictures(pictures []Picture) []Picture {
	ordered := append([]Picture(nil), pictures...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return ordered[i].ID.String() < ordered[j].ID.String()
	})
	return ordered
}

// PrimaryPicture returns the product's current main picture, if one exists. A
// product with no pictures, or one whose main picture was removed, reports none —
// which is a valid product, not an error (research D7).
func PrimaryPicture(pictures []Picture) (Picture, bool) {
	for _, picture := range pictures {
		if picture.IsPrimary {
			return picture, true
		}
	}
	return Picture{}, false
}

// NextPicturePosition reports the position a newly added picture takes: after
// every existing one, so insertion order is preserved even after removals
// (research D11).
func NextPicturePosition(pictures []Picture) int {
	next := 0
	for i, picture := range pictures {
		if i == 0 || picture.Position >= next {
			next = picture.Position + 1
		}
	}
	return next
}

// PromotionAfterRemoval reports which remaining picture must become the main one
// when removedID is removed, or false when nobody should (research D7).
//
// Promotion happens only when the removed picture was the main one and pictures
// remain; the promoted picture is the next by position. Removing a picture that was
// not the main one, removing the last picture, or naming an unknown identifier all
// answer false — "at least one main picture while any picture exists" is a
// transition the use case applies, not a constraint (FR-016).
func PromotionAfterRemoval(pictures []Picture, removedID uuid.UUID) (uuid.UUID, bool) {
	var removed *Picture
	for i := range pictures {
		if pictures[i].ID == removedID {
			removed = &pictures[i]
			break
		}
	}
	if removed == nil || !removed.IsPrimary {
		return uuid.Nil, false
	}

	remaining := make([]Picture, 0, len(pictures))
	for _, picture := range pictures {
		if picture.ID == removedID {
			continue
		}
		remaining = append(remaining, picture)
	}
	if len(remaining) == 0 {
		return uuid.Nil, false
	}
	return OrderPictures(remaining)[0].ID, true
}
