package model

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// The signature payloads FR-019 accepts: JPEG, PNG and WebP. The trailing bytes
// make each payload longer than its signature, which is what a real upload is.
func jpegPayload() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 16)...)
}

func pngPayload() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 16)...)
}

func webpPayload() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WEBP"), bytes.Repeat([]byte{0x00}, 16)...)
}

// FR-020: the ceiling is ten; the eleventh is refused with ErrProductImageLimitReached.
func TestPictureCeilingRefusesTheEleventh(t *testing.T) {
	for existing := 0; existing < MaxProductPictures; existing++ {
		if err := EnsurePictureCapacity(existing); err != nil {
			t.Fatalf("EnsurePictureCapacity(%d) = %v, want nil", existing, err)
		}
	}

	for _, existing := range []int{MaxProductPictures, MaxProductPictures + 1} {
		err := EnsurePictureCapacity(existing)
		if !errors.Is(err, domainerr.ErrProductImageLimitReached) {
			t.Fatalf("EnsurePictureCapacity(%d) = %v, want ErrProductImageLimitReached", existing, err)
		}
	}
}

// FR-019: the three supported formats are decided by their content signature.
func TestPictureUploadAcceptsTheThreeSupportedFormats(t *testing.T) {
	cases := map[string][]byte{
		"jpeg": jpegPayload(),
		"png":  pngPayload(),
		"webp": webpPayload(),
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePictureUpload(content, MaxPictureBytes); err != nil {
				t.Errorf("ValidatePictureUpload(%s) = %v, want nil", name, err)
			}
		})
	}
}

// FR-019: a payload that is not an image is refused by its bytes, never by a file
// name or a declared content type.
func TestPictureUploadRefusesUnsupportedPayloadsBySignature(t *testing.T) {
	cases := map[string][]byte{
		"pdf":   []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"),
		"gif":   []byte("GIF89a...."),
		"text":  []byte("just some text, not an image at all"),
		"bmp":   append([]byte{0x42, 0x4D}, bytes.Repeat([]byte{0x00}, 20)...),
		"empty": {},
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidatePictureUpload(content, MaxPictureBytes)
			if !errors.Is(err, domainerr.ErrProductImageTypeUnsupported) {
				t.Errorf("ValidatePictureUpload(%s) = %v, want ErrProductImageTypeUnsupported", name, err)
			}
		})
	}
}

// FR-019: the payload is bounded at the same 2 MB ceiling an avatar uses.
func TestPictureUploadRefusesAnOversizedPayload(t *testing.T) {
	oversized := append(pngPayload(), bytes.Repeat([]byte{0x00}, 32)...)

	err := ValidatePictureUpload(oversized, 8)
	if !errors.Is(err, domainerr.ErrProductImageTooLarge) {
		t.Fatalf("ValidatePictureUpload(oversized) = %v, want ErrProductImageTooLarge", err)
	}
}

// The type is checked before the size, so an unsupported payload that is also
// oversized is reported as the type error (FR-019).
func TestPictureUploadChecksTypeBeforeSize(t *testing.T) {
	oversizedPDF := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0x00}, 32)...)

	err := ValidatePictureUpload(oversizedPDF, 8)
	if !errors.Is(err, domainerr.ErrProductImageTypeUnsupported) {
		t.Fatalf("ValidatePictureUpload(oversized pdf) = %v, want ErrProductImageTypeUnsupported", err)
	}
}

// A ceiling of zero or less means the composition left it unset, so the documented
// ceiling applies rather than no limit at all.
func TestPictureUploadFallsBackToTheDocumentedCeilingWhenUnset(t *testing.T) {
	oversized := append(pngPayload(), bytes.Repeat([]byte{0x00}, int(MaxPictureBytes))...)
	if int64(len(oversized)) <= MaxPictureBytes {
		t.Fatalf("test setup: the payload must exceed the documented ceiling")
	}

	err := ValidatePictureUpload(oversized, 0)
	if !errors.Is(err, domainerr.ErrProductImageTooLarge) {
		t.Fatalf("ValidatePictureUpload(oversized, 0) = %v, want ErrProductImageTooLarge", err)
	}
}

// FR-016, research D7: at most one picture may be the main one.
func TestHasAtMostOnePrimary(t *testing.T) {
	if !HasAtMostOnePrimary(nil) {
		t.Errorf("a product with no pictures must pass the invariant")
	}
	if !HasAtMostOnePrimary([]Picture{{ID: uuid.New(), IsPrimary: true}, {ID: uuid.New()}}) {
		t.Errorf("exactly one main picture must pass the invariant")
	}
	if HasAtMostOnePrimary([]Picture{{ID: uuid.New(), IsPrimary: true}, {ID: uuid.New(), IsPrimary: true}}) {
		t.Errorf("two main pictures must fail the invariant")
	}
}

// Pictures are shown in order: position first, then the identifier to break a tie.
func TestOrderPicturesSortsByPositionThenIdentifier(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	last := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	pictures := []Picture{
		{ID: last, Position: 1},
		{ID: second, Position: 0},
		{ID: first, Position: 0},
	}

	ordered := OrderPictures(pictures)
	if len(ordered) != 3 {
		t.Fatalf("OrderPictures dropped a picture: %+v", ordered)
	}
	if ordered[0].ID != first || ordered[1].ID != second || ordered[2].ID != last {
		t.Fatalf("unexpected order: %+v", ordered)
	}
}

// A product's main picture is the one flagged as primary, if any (FR-016).
func TestPrimaryPictureReturnsTheFlaggedOne(t *testing.T) {
	primary := Picture{ID: uuid.New(), Position: 1, IsPrimary: true}
	pictures := []Picture{
		{ID: uuid.New(), Position: 0},
		primary,
	}

	got, ok := PrimaryPicture(pictures)
	if !ok || got.ID != primary.ID {
		t.Fatalf("PrimaryPicture = (%+v, %v), want the primary picture", got, ok)
	}

	if _, ok := PrimaryPicture([]Picture{{ID: uuid.New()}}); ok {
		t.Errorf("a product with no primary picture must report none")
	}
}

// FR-016, research D7: removing the main picture promotes the next one by
// position; removing any other leaves the main one alone.
func TestPromotionAfterRemovalPromotesTheNextByPosition(t *testing.T) {
	primary := Picture{ID: uuid.New(), Position: 0, IsPrimary: true}
	next := Picture{ID: uuid.New(), Position: 1}
	later := Picture{ID: uuid.New(), Position: 2}
	pictures := []Picture{primary, next, later}

	promoted, ok := PromotionAfterRemoval(pictures, primary.ID)
	if !ok || promoted != next.ID {
		t.Fatalf("PromotionAfterRemoval(primary) = (%s, %v), want the next by position", promoted, ok)
	}

	if _, ok := PromotionAfterRemoval(pictures, later.ID); ok {
		t.Errorf("removing a non-primary picture must not promote anyone")
	}

	if _, ok := PromotionAfterRemoval([]Picture{primary}, primary.ID); ok {
		t.Errorf("removing the only picture must leave no main picture")
	}

	if _, ok := PromotionAfterRemoval(pictures, uuid.New()); ok {
		t.Errorf("an unknown picture identifier must not promote anyone")
	}
}

// A newly added picture goes after every existing one.
func TestNextPicturePositionFollowsTheHighest(t *testing.T) {
	if got := NextPicturePosition(nil); got != 0 {
		t.Errorf("NextPicturePosition(nil) = %d, want 0", got)
	}

	pictures := []Picture{{Position: 0}, {Position: 5}, {Position: 2}}
	if got := NextPicturePosition(pictures); got != 6 {
		t.Errorf("NextPicturePosition = %d, want 6", got)
	}
}
