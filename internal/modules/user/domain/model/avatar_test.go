package model

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// Image headers used by these tests. They are the real magic numbers of the
// three formats the specification accepts, so the sniffer is exercised against
// what a browser would actually send rather than against a stand-in.
var (
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, bytes.Repeat([]byte{0x20}, 32)...)
	pngBytes  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x11}, 32)...)
	webpBytes = append(append([]byte("RIFF"), 0x00, 0x00, 0x00, 0x00), append([]byte("WEBPVP8 "), bytes.Repeat([]byte{0x22}, 32)...)...)
)

// contentAt builds a payload of exactly n bytes that starts with the given image
// header, so a size test does not have to repeat the header inline.
func contentAt(header []byte, n int) []byte {
	out := make([]byte, 0, n)
	out = append(out, header...)
	for len(out) < n {
		out = append(out, 0x2A)
	}
	return out[:n]
}

// FR-014: the three supported formats are recognised from their content, so a
// client cannot pass something else by renaming it or by declaring a media type.
func TestContentSniffingAcceptsJPEGPNGAndWebP(t *testing.T) {
	for name, header := range map[string][]byte{
		"jpeg": jpegBytes,
		"png":  pngBytes,
		"webp": webpBytes,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAvatarUpload(header, MaxAvatarBytes); err != nil {
				t.Fatalf("a %s payload must be accepted, got %v", name, err)
			}
		})
	}
}

// Everything outside the allowlist is refused with the type code, and the payload
// is never inspected beyond its leading bytes: a file that merely starts with the
// right bytes is still rejected, because the rule is a sniff, not a decode.
func TestContentSniffingRefusesEverythingElse(t *testing.T) {
	cases := map[string][]byte{
		"gif":         []byte("GIF89a\x01\x00\x01\x00"),
		"bmp":         {0x42, 0x4D, 0x36, 0x00, 0x00, 0x00},
		"pdf":         []byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"),
		"svg":         []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"plain text":  []byte("this is not an image at all"),
		"empty":       {},
		"only a null": {0x00},
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateAvatarUpload(payload, MaxAvatarBytes)
			if !errors.Is(err, domainerr.ErrAvatarTypeUnsupported) {
				t.Fatalf("expected ErrAvatarTypeUnsupported, got %v", err)
			}
		})
	}
}

// A truncated PNG is not a PNG: the signature alone is eight bytes and the rule is
// checked on exactly those, so a payload that is nothing but a signature is
// accepted as the format it claims and refused by nothing else. This test pins
// that boundary so the rule cannot quietly start decoding images, which would
// need an imaging dependency ADR-005 refuses.
func TestOnlyTheLeadingSignatureIsInspected(t *testing.T) {
	if err := ValidateAvatarUpload(pngBytes[:8], MaxAvatarBytes); err != nil {
		t.Fatalf("the signature alone identifies the format, got %v", err)
	}
	if err := ValidateAvatarUpload(pngBytes[:7], MaxAvatarBytes); !errors.Is(err, domainerr.ErrAvatarTypeUnsupported) {
		t.Fatalf("one byte short of a signature is not that format, got %v", err)
	}
}

// FR-014: the 2 MB ceiling is enforced on the bytes, inclusively — a payload of
// exactly the ceiling is the largest accepted one, and one byte more is refused
// rather than truncated.
func TestTheAvatarSizeCeilingIsInclusiveAndRejectsRatherThanTruncates(t *testing.T) {
	ceiling := int64(1 << 20)

	if err := ValidateAvatarUpload(contentAt(pngBytes, int(ceiling)), ceiling); err != nil {
		t.Fatalf("a payload of exactly the ceiling must be accepted, got %v", err)
	}

	err := ValidateAvatarUpload(contentAt(pngBytes, int(ceiling)+1), ceiling)
	if !errors.Is(err, domainerr.ErrAvatarTooLarge) {
		t.Fatalf("expected ErrAvatarTooLarge one byte over the ceiling, got %v", err)
	}
}

// The documented ceiling is the one FR-014 states, and it is the value the
// composition defaults to, so pinning it here catches a silent change of either.
func TestTheAvatarCeilingsAreTheOnesTheContractStates(t *testing.T) {
	if MaxAvatarBytes != 2<<20 {
		t.Fatalf("FR-014 declares a 2 MB ceiling, got %d", MaxAvatarBytes)
	}
	if MaxAvatarWidth != 512 {
		t.Fatalf("FR-015 declares a 512 px stored width, got %d", MaxAvatarWidth)
	}
}

// A size ceiling of zero or less means "no ceiling configured", which must not
// turn into "everything passes": a caller that forgets to resolve the
// configuration gets the documented ceiling rather than an unguarded upload.
func TestAnUnconfiguredCeilingFallsBackToTheDocumentedOne(t *testing.T) {
	payload := contentAt(pngBytes, int(MaxAvatarBytes)+1)

	for _, ceiling := range []int64{0, -1} {
		err := ValidateAvatarUpload(payload, ceiling)
		if !errors.Is(err, domainerr.ErrAvatarTooLarge) {
			t.Fatalf("ceiling %d must still enforce the documented limit, got %v", ceiling, err)
		}
	}
}

// The type rule is checked before the size rule, so an unsupported payload that
// is also oversized reports the type a client can act on first: renaming a 3 MB
// text file to .png does not turn it into a too-large image.
func TestTheTypeRuleIsCheckedBeforeTheSizeRule(t *testing.T) {
	err := ValidateAvatarUpload(bytes.Repeat([]byte("a"), int(MaxAvatarBytes)+1), MaxAvatarBytes)
	if !errors.Is(err, domainerr.ErrAvatarTypeUnsupported) {
		t.Fatalf("expected the type to be reported first, got %v", err)
	}
}

// FR-016: the four stored columns are all populated or all NULL, so a reference
// missing any one of them is refused rather than written half-filled.
func TestAnAvatarReferenceIsAllOrNothing(t *testing.T) {
	complete := AvatarReference{
		PublicID: "artist-shop/avatars/one",
		URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		Width:    512,
		Height:   512,
	}
	if !complete.IsComplete() {
		t.Fatal("a reference carrying every value must be complete")
	}

	cases := map[string]AvatarReference{
		"no public id": {URL: complete.URL, Width: 512, Height: 512},
		"blank public id": {
			PublicID: "   ", URL: complete.URL, Width: 512, Height: 512,
		},
		"no url":       {PublicID: complete.PublicID, Width: 512, Height: 512},
		"no width":     {PublicID: complete.PublicID, URL: complete.URL, Height: 512},
		"no height":    {PublicID: complete.PublicID, URL: complete.URL, Width: 512},
		"zero width":   {PublicID: complete.PublicID, URL: complete.URL, Height: 512},
		"zero height":  {PublicID: complete.PublicID, URL: complete.URL, Width: 512},
		"empty struct": {},
	}

	for name, partial := range cases {
		t.Run(name, func(t *testing.T) {
			if partial.IsComplete() {
				t.Fatalf("a half-filled reference must never be reported complete: %+v", partial)
			}
			if _, err := NewAvatar(partial.PublicID, partial.URL, partial.Width, partial.Height); !errors.Is(err, domainerr.ErrIncompleteAvatar) {
				t.Fatalf("expected ErrIncompleteAvatar, got %v", err)
			}
			profile := Profile{}
			if err := profile.SetAvatar(&partial); !errors.Is(err, domainerr.ErrIncompleteAvatar) {
				t.Fatalf("the entity must refuse a half-filled reference too, got %v", err)
			}
			if profile.Avatar != nil {
				t.Fatal("a refused reference must not be attached")
			}
		})
	}
}

// A nil reference is how "no photo" is expressed, and it is never complete —
// otherwise clearing the four columns would leave a reference the client cannot
// render.
func TestANilReferenceIsNeverComplete(t *testing.T) {
	var reference *AvatarReference
	if reference.IsComplete() {
		t.Fatal("a nil reference is the absence of a photo, never a complete one")
	}
}

// The reference is trimmed on the way in, so a provider that pads its identifier
// cannot store a value no later release can match.
func TestTheStoredReferenceIsTrimmed(t *testing.T) {
	reference, err := NewAvatar(
		"  artist-shop/avatars/one  ",
		"  https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg  ",
		512, 512)
	if err != nil {
		t.Fatalf("NewAvatar: %v", err)
	}
	if reference.PublicID != "artist-shop/avatars/one" ||
		reference.URL != "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg" {
		t.Fatalf("expected the stored values to be trimmed, got %+v", reference)
	}
}

// FR-015 and contracts/openapi.yaml: `Avatar.width` is documented as always 512
// at most. ADR-005 trusts the provider to resize, so the domain refuses whatever
// the provider reports above the ceiling rather than storing a reference the
// contract says cannot exist.
//
// The boundary is inclusive, exactly like the maxLength ceilings on displayName
// and the address members: a width of exactly 512 px is accepted and 513 px is
// rejected outright, never clamped. Clamping would store a dimension the image
// does not have, and a client sizing a layout from it would reserve the wrong
// space. The unit here is a pixel count rather than a character count, so there
// is no rune-versus-byte question to settle — the number itself is the boundary.
func TestTheStoredWidthNeverExceedsTheCeiling(t *testing.T) {
	const url = "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg"

	for _, width := range []int{1, 256, MaxAvatarWidth - 1, MaxAvatarWidth} {
		if _, err := NewAvatar("avatars/one", url, width, 512); err != nil {
			t.Fatalf("a stored width of %d px must be accepted, got %v", width, err)
		}
	}

	reference, err := NewAvatar("avatars/one", url, MaxAvatarWidth+1, 512)
	if !errors.Is(err, domainerr.ErrIncompleteAvatar) {
		t.Fatalf("expected a width above the ceiling to be refused, got %v", err)
	}
	if reference != nil {
		t.Fatalf("an over-wide reference must be refused outright, not corrected: %+v", reference)
	}
}

// The height is the provider's own report and the contract sets no ceiling on it,
// so only the structural rule applies: a missing or non-positive height is refused.
func TestTheStoredHeightMustBePositive(t *testing.T) {
	const url = "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg"

	if _, err := NewAvatar("avatars/one", url, 512, 4096); err != nil {
		t.Fatalf("any positive height must be accepted, got %v", err)
	}
	for _, height := range []int{0, -1} {
		if _, err := NewAvatar("avatars/one", url, 512, height); !errors.Is(err, domainerr.ErrIncompleteAvatar) {
			t.Fatalf("height %d must be refused, got %v", height, err)
		}
	}
}

// contracts/openapi.yaml declares `Avatar.url` as `format: uri`. The database
// column is plain text, so nothing else would refuse a value a client cannot
// render, and a stored link that is not a link breaks the one screen the photo
// exists for.
//
// HTTPS only. A cleartext link would fetch, so shape alone cannot tell it apart
// from the real thing, but it is a beacon naming a third-party host to whoever
// reads the profile and it hands the bytes of the response to the network in
// between. The refusal is deliberate and the asset is released by the caller, so
// a provider answer this service will not vouch for never reaches the row.
func TestTheStoredURLMustBeAnAbsoluteHTTPSLink(t *testing.T) {
	accepted := []string{
		"https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		"HTTPS://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		"https://res.cloudinary.com:443/image/upload/a.png?v=1#top",
	}
	for _, url := range accepted {
		if _, err := NewAvatar("avatars/one", url, 512, 512); err != nil {
			t.Fatalf("expected %q to be accepted, got %v", url, err)
		}
	}

	refused := map[string]string{
		"empty":             "",
		"blank":             "   ",
		"plain http":        "http://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		"uppercase http":    "HTTP://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		"http on any host":  "http://attacker.example/beacon.png",
		"not a url":         "res.cloudinary.com/demo/one.jpg",
		"relative path":     "/image/upload/one.jpg",
		"scheme relative":   "//res.cloudinary.com/demo/one.jpg",
		"other scheme":      "ftp://res.cloudinary.com/demo/one.jpg",
		"file scheme":       "file:///etc/passwd",
		"javascript scheme": "javascript:alert(1)",
		"control char":      "https://res.example/one\x00.jpg",
	}
	for name, url := range refused {
		t.Run(name, func(t *testing.T) {
			reference, err := NewAvatar("avatars/one", url, 512, 512)
			if !errors.Is(err, domainerr.ErrIncompleteAvatar) {
				t.Fatalf("expected an unusable link to be refused, got %v", err)
			}
			if reference != nil {
				t.Fatalf("expected no reference, got %+v", reference)
			}
		})
	}
}

// A long but perfectly valid provider link is not the length rule's business:
// there is no maxLength on Avatar.url in the contract, so refusing one would
// invent a limit the contract does not declare.
func TestTheStoredURLHasNoLengthCeiling(t *testing.T) {
	long := "https://res.cloudinary.com/demo/image/upload/" + strings.Repeat("folder/", 40) + "one.jpg"
	if _, err := NewAvatar("avatars/one", long, 512, 512); err != nil {
		t.Fatalf("expected a long but valid link to be accepted, got %v", err)
	}
}

// The entity attaches a complete reference and clears it again. The width and
// link rules belong to NewAvatar, which is the only way a reference is built for
// storage, so the entity deliberately re-checks only the all-or-nothing rule that
// also protects the four stored columns.
func TestSetAvatarAcceptsWhatTheValueObjectAccepted(t *testing.T) {
	reference, err := NewAvatar("avatars/one", "https://res.example/one.jpg", 512, 512)
	if err != nil {
		t.Fatalf("NewAvatar: %v", err)
	}

	profile := Profile{}
	if err := profile.SetAvatar(reference); err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}
	if profile.Avatar != reference {
		t.Fatal("expected the reference to be attached")
	}

	if err := profile.SetAvatar(nil); err != nil {
		t.Fatalf("clearing the reference must not be an error, got %v", err)
	}
	if profile.Avatar != nil {
		t.Fatal("expected the reference to be cleared")
	}
}
