package model

import (
	"net/url"
	"strings"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// AvatarReference points at the customer's stored avatar in the external media
// service. Image bytes are never kept in the database: only this reference is
// (FR-016).
//
// A nil *AvatarReference means the customer has no photo, and the reference
// itself is the single signal for that — there is deliberately no separate
// "hasAvatar" flag that could disagree with the stored columns.
//
// The stored dimensions are the provider's own report after it applied the
// resize, because the provider is the system of record for them (ADR-005,
// research D6). That is why the guard on the width lives here rather than in the
// adapter: nothing else in this service resizes anything, so if the provider
// reports a width the contract cannot describe, the only place that can refuse it
// is the value object every stored reference passes through.
type AvatarReference struct {
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width is the stored pixel width, at most MaxAvatarWidth (FR-015).
	Width int
	// Height is the stored pixel height.
	Height int
}

// The ceilings FR-014 and FR-015 put on an avatar, expressed once here so the
// value object, the composition default and the tests cannot disagree about what
// the contract states.
//
// They are not the same rule enforced twice: MaxAvatarBytes is a request-shape
// limit on what may be read and uploaded, while MaxAvatarWidth is a property of
// what may be stored. Both live in the domain because the domain is the only
// layer every entry point passes through — a limit enforced in presentation could
// be bypassed by any caller that is not HTTP (Constitution I, III).
const (
	// MaxAvatarBytes is the 2 MB ceiling on an uploaded photo (FR-014).
	MaxAvatarBytes int64 = 2 << 20
	// MaxAvatarWidth is the stored pixel width ceiling of FR-015, matching the
	// width the media provider is asked to resize to (ADR-005).
	MaxAvatarWidth = 512
)

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

// NewAvatar builds the reference that will be stored for a photo.
//
// It is the single constructor, so every stored reference passes through the same
// three rules, whatever entry point produced it:
//
//   - all four values must be present, because the four stored columns move
//     together and a half-written avatar is a state no client could render
//     (FR-016). A missing or non-positive dimension, or a blank identifier or
//     link, is refused with domainerr.ErrIncompleteAvatar.
//   - the stored width must fit MaxAvatarWidth. contracts/openapi.yaml documents
//     `Avatar.width` as always 512 at most, and ADR-005 has this service trust the
//     provider to resize; a wider report means the provider did not honour the
//     request, and storing it would put a reference in the database that the
//     contract says cannot exist. The bound is inclusive and the value is
//     refused, never clamped: clamping would store a dimension the image does not
//     have and a client sizing a layout from it would reserve the wrong space.
//   - the stored link must be an absolute HTTPS URL, because the contract declares
//     `Avatar.url` as `format: uri` and the column is plain text, so nothing
//     downstream would refuse a value a client cannot render, and because a
//     cleartext link handed to a browser is a tracking beacon and an in-flight
//     tampering opportunity (see isDisplayableURL).
//
// A rejection carries domainerr.ErrIncompleteAvatar in every case. The reference is
// provider metadata this service asked for and then failed to store, which is an
// internal inconsistency rather than something the customer can fix, so it is
// mapped to the shared INTERNAL_ERROR instead of adding a module code no client
// could act on (contracts/error-codes.md, "Codes deliberately not added"). The
// customer-facing refusals are the ones they caused: an unsupported image type and
// an oversized one, both decided by ValidateAvatarUpload before any byte reaches
// the provider.
func NewAvatar(publicID, link string, width, height int) (*AvatarReference, error) {
	reference := &AvatarReference{
		PublicID: strings.TrimSpace(publicID),
		URL:      strings.TrimSpace(link),
		Width:    width,
		Height:   height,
	}
	if !reference.IsComplete() {
		return nil, domainerr.ErrIncompleteAvatar
	}
	if reference.Width > MaxAvatarWidth {
		return nil, domainerr.ErrIncompleteAvatar
	}
	if !isDisplayableURL(reference.URL) {
		return nil, domainerr.ErrIncompleteAvatar
	}
	return reference, nil
}

// isDisplayableURL reports whether a stored link is one a client can actually
// fetch over a channel that cannot be tampered with in flight.
//
// It has to be absolute and it has to speak HTTPS. A relative path, a
// scheme-relative reference or a foreign scheme would all parse as a URL and still
// be useless in an <img> tag or a fetch.
//
// Plain HTTP is refused even though it would fetch: the value is served to every
// client that reads the profile, and a cleartext link is a beacon for whoever can
// influence the provider's answer — it names a third-party host and discloses the
// viewer, and on a hostile network the bytes that come back are the attacker's,
// not the photo. Requiring TLS removes that channel rather than documenting it as
// acceptable, and the reference is refused outright instead of being silently
// dropped: a link the entity will not vouch for must not be stored, because the
// alternative is a row that claims to hold a photo and renders something else.
//
// The check is on the parsed shape rather than on a prefix, so an uppercase scheme
// and an explicit default port are accepted on the same grounds as the plain form.
func isDisplayableURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	default:
		return false
	}
}

// ValidateAvatarUpload decides whether an uploaded payload may be stored.
//
// It is the whole of FR-014 on the server side, and it is deliberately a pure
// function of the bytes so it can be unit-tested and reused by any caller:
//
//   - the format is decided by sniffing the leading signature, never from the file
//     name or from a client-declared media type, both of which the client chooses
//     (research D7);
//   - only the signature is inspected, not the image. Decoding pixels here would
//     mean an imaging dependency in this binary, which ADR-005 refuses for a
//     capability the media provider already has;
//   - the size ceiling is enforced on the payload itself. A caller that reads a
//     request body must cap the read as well (http.MaxBytesReader) — a post-hoc
//     check on an already buffered body cannot stop a memory-exhaustion upload —
//     but this function is where the rule itself lives, so a caller that forgot
//     the read ceiling still refuses the upload.
//
// maxBytes is the configured ceiling; a value of zero or less means the
// composition left it unset, in which case the documented ceiling applies rather
// than no limit at all.
//
// The type is checked before the size, so an unsupported payload that is also
// oversized is reported as the type error — the problem a customer can actually
// fix by choosing a different file.
func ValidateAvatarUpload(content []byte, maxBytes int64) error {
	if !isSupportedImage(content) {
		return domainerr.ErrAvatarTypeUnsupported
	}
	ceiling := maxBytes
	if ceiling <= 0 {
		ceiling = MaxAvatarBytes
	}
	if int64(len(content)) > ceiling {
		return domainerr.ErrAvatarTooLarge
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
// FR-014 is really about.
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
