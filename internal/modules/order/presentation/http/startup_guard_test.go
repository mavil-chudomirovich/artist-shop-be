package httpapi

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// --- feature 019-order-delivery: the delivery route body-ceiling guard --------
//
// The dispatch route installs a ceiling of five 2 MiB photos plus the multipart
// envelope, far above the shared pipeline default. The pipeline wraps the body
// before routing, so a MAX_BODY_BYTES below that ceiling would refuse a
// legitimate five-photo dispatch before the ship route could apply its own rule.
// The guard refuses that startup instead (FR-006, FR-009), mirroring
// userhttp.RequireAvatarUploadCeiling.
//
// The guard is a pure function here on purpose: the composition root calls it,
// and every branch can be exercised in this package without a database.

func TestTheDeliveryGuardRefusesASharedCeilingBelowTheRouteCeiling(t *testing.T) {
	required := DeliveryEvidenceUploadCeiling()

	if err := RequireDeliveryEvidenceUploadCeiling(required-1, required, true); err == nil {
		t.Fatalf("a shared ceiling of %d is below the %d the delivery routes need and must refuse to start", required-1, required)
	}
}

func TestTheDeliveryGuardStartsOnAnExactlyEqualSharedCeiling(t *testing.T) {
	required := DeliveryEvidenceUploadCeiling()

	if err := RequireDeliveryEvidenceUploadCeiling(required, required, true); err != nil {
		t.Fatalf("a shared ceiling of %d exactly equals the delivery ceiling and must start: %v", required, err)
	}
}

func TestTheDeliveryGuardStartsWhenMediaIsAbsent(t *testing.T) {
	required := DeliveryEvidenceUploadCeiling()

	if err := RequireDeliveryEvidenceUploadCeiling(1, required, false); err != nil {
		t.Fatalf("without media credentials no evidence photo can be stored, so the ceiling must not refuse the startup: %v", err)
	}
}

func TestTheDeliveryRefusalNamesTheSettingAndBothValues(t *testing.T) {
	const shared = int64(1048576)
	required := DeliveryEvidenceUploadCeiling()

	err := RequireDeliveryEvidenceUploadCeiling(shared, required, true)
	if err == nil {
		t.Fatal("expected a refusal for a shared ceiling below the delivery ceiling")
	}
	msg := err.Error()
	for _, want := range []string{"MAX_BODY_BYTES", strconv.FormatInt(shared, 10), strconv.FormatInt(required, 10)} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q so an operator can correct it without reading source; got %q", want, msg)
		}
	}
}

// The route and the guard read the same exported figure, so the ceiling a startup
// is checked against is exactly the one the route installs, and it leaves room for
// the five photos the route caps.
func TestTheDeliveryRouteAndTheGuardReadTheSameCeiling(t *testing.T) {
	ceiling := DeliveryEvidenceUploadCeiling()
	if ceiling <= int64(model.MaxDeliveryPhotoBytes) {
		t.Fatalf("the delivery route's ceiling %d must leave room for five photos above one photo ceiling of %d",
			ceiling, model.MaxDeliveryPhotoBytes)
	}
}
