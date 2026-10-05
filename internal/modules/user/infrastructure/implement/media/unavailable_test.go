package media

import (
	"context"
	"errors"
	"testing"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// TestUnavailableFailsClosed pins the placeholder's only promise: while the real
// provider adapter is missing, an upload is refused with the module's retryable
// media error and no reference is invented. A placeholder that reported success
// would persist a profile pointing at an asset that does not exist (FR-017).
func TestUnavailableFailsClosed(t *testing.T) {
	store := NewUnavailable()

	reference, err := store.Upload(context.Background(), []byte("image-bytes"), 512)

	if !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
	if reference.PublicID != "" || reference.URL != "" || reference.Width != 0 || reference.Height != 0 {
		t.Fatalf("expected no stored reference, got %+v", reference)
	}
}

func TestUnavailableRefusesToReleaseAReference(t *testing.T) {
	store := NewUnavailable()

	err := store.Remove(context.Background(), appinterface.MediaReference{PublicID: "avatars/1"})

	if !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
}
