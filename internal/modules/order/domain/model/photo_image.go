package model

import (
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// MaxDeliveryPhotoBytes is the documented size ceiling on one delivery-evidence
// photo: 2 MB, the same ceiling an avatar and a product picture already use
// (FR-009). It is a request-shape limit and lives in the domain because the
// domain is the one layer every entry point passes through.
const MaxDeliveryPhotoBytes int64 = 2 << 20

// ValidateDeliveryPhoto decides whether an uploaded delivery-evidence payload may
// be sent to the media provider. It is deliberately a pure function of the bytes
// so it can be unit-tested and reused by any caller, and it is the order module's
// own copy of the rule — no module imports another's validation, and the order's
// domain imports no media package (Constitution I, research D4):
//
//   - the format is decided by sniffing the leading signature, never from a file
//     name or a client-declared media type, both of which the client chooses
//     (FR-009);
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
// oversized is reported as the type error.
func ValidateDeliveryPhoto(content []byte, maxBytes int64) error {
	if !DetectDeliveryImage(content) {
		return domainerr.ErrImageTypeUnsupported
	}
	ceiling := maxBytes
	if ceiling <= 0 {
		ceiling = MaxDeliveryPhotoBytes
	}
	if int64(len(content)) > ceiling {
		return domainerr.ErrImageTooLarge
	}
	return nil
}

// ValidateFeedbackPhoto decides whether an image attached to a customer's
// order-level feedback may be sent to the media provider. It delegates to the
// same rule a delivery-evidence photo uses — the same format sniff and the same
// size ceiling — so the order module keeps one image rule however the image is
// used (FR-025, research D16). It stays stdlib-only and in the order's own
// domain, exactly as ValidateDeliveryPhoto does (Constitution I, research D4).
func ValidateFeedbackPhoto(content []byte) error {
	return ValidateDeliveryPhoto(content, MaxDeliveryPhotoBytes)
}

// DetectDeliveryImage reports whether the payload begins with the signature of one
// of the three accepted formats.
//
// These are the format definitions, not a heuristic: JPEG opens with the SOI
// marker FF D8 followed by a further marker byte, PNG opens with its fixed
// eight-byte header, and WebP is a RIFF container whose form type is "WEBP" at
// offset 8. A GIF, a BMP, an SVG, a PDF and a text file all fail here, which is
// the property FR-009 is really about.
func DetectDeliveryImage(content []byte) bool {
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
