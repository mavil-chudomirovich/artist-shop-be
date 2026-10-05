package media

import (
	"context"
	"fmt"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// Unavailable is the explicitly disabled MediaStore.
//
// The real provider adapter lands with the avatar use cases in task T048; until
// then this placeholder keeps the composition root honest. It fails closed: every
// method returns an error wrapping domainerr.ErrMediaUnavailable, so an upload
// is refused with 503 USER_MEDIA_UNAVAILABLE and the current avatar is left
// untouched. It never reports success, because a placeholder that quietly accepted
// an upload would persist a profile pointing at an asset that does not exist
// (FR-017).
//
// It is replaced by the Cloudinary adapter in T048, which also carries the
// provider resize to the stored width ceiling (FR-015, ADR-005).
type Unavailable struct{}

// NewUnavailable creates the disabled media store.
func NewUnavailable() *Unavailable { return &Unavailable{} }

// Upload always fails with domainerr.ErrMediaUnavailable.
func (u *Unavailable) Upload(context.Context, []byte, int) (appinterface.MediaReference, error) {
	return appinterface.MediaReference{}, unavailable("upload")
}

// Remove always fails with domainerr.ErrMediaUnavailable. There is no stored
// asset to release while the adapter is disabled, and reporting success would
// hide a provider that was never configured.
func (u *Unavailable) Remove(context.Context, appinterface.MediaReference) error {
	return unavailable("remove")
}

func unavailable(operation string) error {
	return fmt.Errorf("%w: no media provider adapter is configured (%s)",
		domainerr.ErrMediaUnavailable, operation)
}

var _ appinterface.MediaStore = (*Unavailable)(nil)
