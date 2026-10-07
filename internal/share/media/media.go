package media

import (
	"context"
	"errors"
)

// Store stores binary media outside the service and hands back a reference to
// keep in the database. It is named for the capability rather than the vendor,
// so swapping the provider touches only the adapter (ADR-005).
//
// It lives in internal/share/media rather than in one module's
// application/interface because two modules need it and Constitution I sends
// code shared by more than one module to internal/share (research D2). There is
// one declaration, one adapter and one behaviour, so the Cloudinary signature
// scheme, the multipart upload shape and the sanitised failure classification
// cannot drift between the modules that store media.
type Store interface {
	// Upload stores content and returns the stored reference. targetWidth asks
	// the provider to resize during the upload; the provider, not this service,
	// is the system of record for the resulting dimensions.
	Upload(ctx context.Context, content []byte, targetWidth int) (Reference, error)
	// Remove releases a previously stored reference. Removing an unknown
	// reference must succeed so a retry never fails on an already-freed asset.
	Remove(ctx context.Context, ref Reference) error
}

// Reference identifies one stored media asset. Media bytes are never kept in
// the database; only this reference is (Constitution, Media constraint).
type Reference struct {
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients.
	URL string
	// Width and Height are the stored pixel dimensions as reported by the
	// provider after resizing.
	Width  int
	Height int
}

// ErrUnavailable is returned when the media provider rejects a call or cannot
// be reached, or when the adapter has no credentials to call it with. It is the
// one error every provider failure is flattened to, so a consumer can branch on
// a retryable outage without importing the adapter's internals. The error
// carries no provider prose, no endpoint and no credential: presentation logs
// whatever error it is handed, so anything attached here would reach a log line
// (Constitution V, VI).
var ErrUnavailable = errors.New("media service unavailable")
