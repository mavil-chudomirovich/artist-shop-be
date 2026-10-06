package httpapi

import (
	"strconv"
	"strings"
	"testing"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
)

// --- feature 004-fix-pending-defects, US3 (T016, T017) ---------------------
// An operator who sets MAX_BODY_BYTES below what an avatar upload needs gets no
// warning today: the service starts and every avatar upload then fails with a
// message that does not point at the setting. The guard refuses that startup
// instead (FR-019), and the refusal names the setting and both values so it can
// be corrected without reading source (FR-020).
//
// The guard is a pure function here on purpose: the composition root calls it,
// and every branch can be exercised in this package without a database, which a
// test in cmd/api could not do, because run() opens the database and runs
// migrations before it reaches any wiring (research D2, plan.md Complexity
// Tracking).

// requiredAvatarCeiling is the figure the avatar route installs in the
// composition this service actually builds.
func requiredAvatarCeiling() int64 {
	return AvatarUploadCeiling(appinterface.Config{}.AvatarMaxBytesOrDefault())
}

func TestTheStartupGuardRefusesASharedCeilingBelowTheAvatarCeiling(t *testing.T) {
	required := requiredAvatarCeiling()

	err := RequireAvatarUploadCeiling(required-1, required, true)

	if err == nil {
		t.Fatalf("a shared ceiling of %d is below the %d the avatar route needs and must refuse to start", required-1, required)
	}
}

func TestTheStartupGuardStartsOnAnExactlyEqualSharedCeiling(t *testing.T) {
	required := requiredAvatarCeiling()

	if err := RequireAvatarUploadCeiling(required, required, true); err != nil {
		t.Fatalf("a shared ceiling of %d exactly equals the avatar ceiling and must start: %v", required, err)
	}
}

func TestTheStartupGuardStartsOnASharedCeilingAboveTheAvatarCeiling(t *testing.T) {
	required := requiredAvatarCeiling()

	if err := RequireAvatarUploadCeiling(required+1, required, true); err != nil {
		t.Fatalf("a shared ceiling above the %d the avatar route needs must start: %v", required, err)
	}
}

func TestTheStartupGuardStartsWhenMediaIsAbsent(t *testing.T) {
	required := requiredAvatarCeiling()

	if err := RequireAvatarUploadCeiling(1, required, false); err != nil {
		t.Fatalf("without media credentials no avatar upload can be served, so the ceiling must not refuse the startup: %v", err)
	}
}

func TestTheRefusalNamesTheSettingAndBothValues(t *testing.T) {
	const shared = int64(1048576)
	required := requiredAvatarCeiling()

	err := RequireAvatarUploadCeiling(shared, required, true)
	if err == nil {
		t.Fatal("expected a refusal for a shared ceiling below the avatar ceiling")
	}

	msg := err.Error()
	for _, want := range []string{"MAX_BODY_BYTES", strconv.FormatInt(shared, 10), strconv.FormatInt(required, 10)} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q so an operator can correct it without reading source; got %q", want, msg)
		}
	}
}

func TestTheRefusalCarriesNoCredentialValue(t *testing.T) {
	err := RequireAvatarUploadCeiling(1048576, requiredAvatarCeiling(), true)
	if err == nil {
		t.Fatal("expected a refusal for a shared ceiling below the avatar ceiling")
	}

	msg := err.Error()
	for _, forbidden := range []string{
		"SECRET", "API_KEY", "CLOUD_NAME", "PASSWORD", "TOKEN", "cloudinary",
	} {
		if strings.Contains(strings.ToUpper(msg), forbidden) {
			t.Errorf("the refusal must carry no credential material, but it mentions %q: %q", forbidden, msg)
		}
	}
}

func TestTheRouteAndTheGuardReadTheSameCeiling(t *testing.T) {
	image := appinterface.Config{}.AvatarMaxBytesOrDefault()

	if got := AvatarUploadCeiling(image); got <= image {
		t.Fatalf("the avatar route's ceiling %d must leave room for the multipart envelope above the image ceiling %d", got, image)
	}
	// Whatever room the route adds, the guard's figure has to add the same room,
	// otherwise the startup check protects a number the route never installs.
	if got, want := AvatarUploadCeiling(4096)-4096, AvatarUploadCeiling(image)-image; got != want {
		t.Fatalf("the overhead the exported figure adds is %d at 4096 but %d at the configured image ceiling", got, want)
	}
}

// --- end feature 004-fix-pending-defects, US3 -------------------------------
