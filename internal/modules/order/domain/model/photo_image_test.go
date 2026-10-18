package model

import (
	"bytes"
	"errors"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// The signature payloads FR-009 accepts: JPEG, PNG and WebP. The trailing bytes
// make each payload longer than its signature, which is what a real upload is.
func jpegPhotoPayload() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 16)...)
}

func pngPhotoPayload() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 16)...)
}

func webpPhotoPayload() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WEBP"), bytes.Repeat([]byte{0x00}, 16)...)
}

// FR-009: the three supported formats are decided by their content signature,
// never by a file name or a client-declared media type.
func TestDeliveryPhotoDetectionAcceptsTheThreeSupportedFormats(t *testing.T) {
	cases := map[string][]byte{
		"jpeg": jpegPhotoPayload(),
		"png":  pngPhotoPayload(),
		"webp": webpPhotoPayload(),
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if !DetectDeliveryImage(content) {
				t.Errorf("DetectDeliveryImage(%s) = false, want true", name)
			}
			if err := ValidateDeliveryPhoto(content, MaxDeliveryPhotoBytes); err != nil {
				t.Errorf("ValidateDeliveryPhoto(%s) = %v, want nil", name, err)
			}
		})
	}
}

// FR-009: a payload that is not one of the three formats is refused by its bytes.
func TestDeliveryPhotoRefusesUnsupportedPayloadsBySignature(t *testing.T) {
	cases := map[string][]byte{
		"gif":   []byte("GIF89a...."),
		"pdf":   []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"),
		"text":  []byte("just some text, not an image at all"),
		"bmp":   append([]byte{0x42, 0x4D}, bytes.Repeat([]byte{0x00}, 20)...),
		"empty": {},
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if DetectDeliveryImage(content) {
				t.Errorf("DetectDeliveryImage(%s) = true, want false", name)
			}
			err := ValidateDeliveryPhoto(content, MaxDeliveryPhotoBytes)
			if !errors.Is(err, domainerr.ErrImageTypeUnsupported) {
				t.Errorf("ValidateDeliveryPhoto(%s) = %v, want ErrImageTypeUnsupported", name, err)
			}
		})
	}
}

// FR-009: the payload is bounded at the documented 2 MB ceiling.
func TestDeliveryPhotoRefusesAnOversizedPayload(t *testing.T) {
	oversized := append(pngPhotoPayload(), bytes.Repeat([]byte{0x00}, 32)...)

	err := ValidateDeliveryPhoto(oversized, 8)
	if !errors.Is(err, domainerr.ErrImageTooLarge) {
		t.Fatalf("ValidateDeliveryPhoto(oversized) = %v, want ErrImageTooLarge", err)
	}
}

// The type is checked before the size, so an unsupported payload that is also
// oversized is reported as the type error — the problem the operator can fix by
// choosing a different file (research D4).
func TestDeliveryPhotoChecksTypeBeforeSize(t *testing.T) {
	oversizedPDF := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0x00}, 32)...)

	err := ValidateDeliveryPhoto(oversizedPDF, 8)
	if !errors.Is(err, domainerr.ErrImageTypeUnsupported) {
		t.Fatalf("ValidateDeliveryPhoto(oversized pdf) = %v, want ErrImageTypeUnsupported", err)
	}
}

// A ceiling of zero or less means the composition left it unset, so the documented
// ceiling applies rather than no limit at all.
func TestDeliveryPhotoFallsBackToTheDocumentedCeilingWhenUnset(t *testing.T) {
	oversized := append(pngPhotoPayload(), bytes.Repeat([]byte{0x00}, int(MaxDeliveryPhotoBytes))...)
	if int64(len(oversized)) <= MaxDeliveryPhotoBytes {
		t.Fatalf("test setup: the payload must exceed the documented ceiling")
	}

	err := ValidateDeliveryPhoto(oversized, 0)
	if !errors.Is(err, domainerr.ErrImageTooLarge) {
		t.Fatalf("ValidateDeliveryPhoto(oversized, 0) = %v, want ErrImageTooLarge", err)
	}
}
