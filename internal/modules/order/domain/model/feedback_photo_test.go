package model

import (
	"bytes"
	"errors"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// FR-025, research D16: a feedback may carry up to five photos; the sixth is
// refused with the same ORDER_IMAGE_LIMIT_REACHED sentinel the delivery steps use,
// and nothing is stored.
func TestEnsureFeedbackPhotoCapacityRefusesMoreThanFive(t *testing.T) {
	for _, existing := range []int{0, 1, 5} {
		if err := EnsureFeedbackPhotoCapacity(existing); err != nil {
			t.Errorf("EnsureFeedbackPhotoCapacity(%d) = %v, want nil", existing, err)
		}
	}
	for _, existing := range []int{6, 7} {
		if err := EnsureFeedbackPhotoCapacity(existing); !errors.Is(err, domainerr.ErrImageLimitReached) {
			t.Errorf("EnsureFeedbackPhotoCapacity(%d) = %v, want ErrImageLimitReached", existing, err)
		}
	}
}

// FR-025, research D16: a feedback photo is decided by exactly the same image rule
// a delivery-evidence photo uses — the format sniff first, then the size ceiling.
func TestValidateFeedbackPhotoReusesTheDeliveryImageRule(t *testing.T) {
	for name, content := range map[string][]byte{
		"jpeg": jpegPhotoPayload(),
		"png":  pngPhotoPayload(),
		"webp": webpPhotoPayload(),
	} {
		if err := ValidateFeedbackPhoto(content); err != nil {
			t.Errorf("ValidateFeedbackPhoto(%s) = %v, want nil", name, err)
		}
	}
}

// FR-025, research D16: a payload that is not one of the three formats is refused
// as the type error, and a payload over the documented ceiling as the size error.
func TestValidateFeedbackPhotoRefusesUnsupportedAndOversized(t *testing.T) {
	err := ValidateFeedbackPhoto([]byte("GIF89a...."))
	if !errors.Is(err, domainerr.ErrImageTypeUnsupported) {
		t.Errorf("ValidateFeedbackPhoto(gif) = %v, want ErrImageTypeUnsupported", err)
	}

	oversized := append(pngPhotoPayload(), bytes.Repeat([]byte{0x00}, int(MaxDeliveryPhotoBytes))...)
	err = ValidateFeedbackPhoto(oversized)
	if !errors.Is(err, domainerr.ErrImageTooLarge) {
		t.Errorf("ValidateFeedbackPhoto(oversized) = %v, want ErrImageTooLarge", err)
	}
}
