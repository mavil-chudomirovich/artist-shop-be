package implement

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// fakeMedia is a MediaStore that answers without a provider. It records what it
// was asked to do so a test can assert that a refused upload never reached the
// media service at all, and it can be told to fail so the retryable media error
// is exercised.
type fakeMedia struct {
	uploads [][]byte
	widths  []int
	removed []appinterface.MediaReference

	// reference is what a successful upload returns.
	reference appinterface.MediaReference
	// uploadErr and removeErr fail the call, standing in for a provider outage.
	uploadErr error
	removeErr error
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{
		reference: appinterface.MediaReference{
			PublicID: "artist-shop/avatars/one",
			URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
			Width:    512,
			Height:   512,
		},
	}
}

func (m *fakeMedia) Upload(_ context.Context, content []byte, targetWidth int) (appinterface.MediaReference, error) {
	m.uploads = append(m.uploads, append([]byte(nil), content...))
	m.widths = append(m.widths, targetWidth)
	if m.uploadErr != nil {
		return appinterface.MediaReference{}, m.uploadErr
	}
	return m.reference, nil
}

func (m *fakeMedia) Remove(_ context.Context, ref appinterface.MediaReference) error {
	m.removed = append(m.removed, ref)
	return m.removeErr
}

func (m *fakeMedia) uploadCount() int { return len(m.uploads) }

var _ appinterface.MediaStore = (*fakeMedia)(nil)

// avatarHarness is the avatar use cases over in-memory storage, with the audit
// trail and the media service both observable.
type avatarHarness struct {
	svc   *Service
	repo  *memoryProfiles
	audit *recordingAudit
	media *fakeMedia
	owner uuid.UUID
	// other is a second account that must stay invisible to the owner's session.
	other uuid.UUID
}

func newAvatarHarness(t *testing.T, cfg appinterface.Config) *avatarHarness {
	t.Helper()
	owner := uuid.New()
	other := uuid.New()
	repo := newMemoryProfiles(
		model.Profile{ID: owner, Email: "owner@example.com", Role: access.RoleCustomer},
		model.Profile{ID: other, Email: "other@example.com", Role: access.RoleCustomer},
	)
	audit := &recordingAudit{}
	media := newFakeMedia()
	svc := New(Service{
		Profiles: repo,
		Audit:    audit,
		Media:    media,
		Mapper:   mapper.New(nil),
		Config:   cfg,
	})
	return &avatarHarness{svc: svc, repo: repo, audit: audit, media: media, owner: owner, other: other}
}

// avatarImage is a payload the domain accepts: it starts with a PNG signature.
func avatarImage() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, 0x2A, 0x2A, 0x2A)
}

func (h *avatarHarness) stored(t *testing.T, id uuid.UUID) model.Profile {
	t.Helper()
	row, err := h.repo.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read the stored profile: %v", err)
	}
	return *row
}

func TestSetAvatarStoresTheProviderReference(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})

	out, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID:  h.owner,
		Content: avatarImage(),
	})

	if err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}
	if out.Avatar == nil {
		t.Fatal("expected the profile to carry the new avatar")
	}
	if out.Avatar.PublicID != h.media.reference.PublicID ||
		out.Avatar.URL != h.media.reference.URL ||
		out.Avatar.Width != 512 || out.Avatar.Height != 512 {
		t.Fatalf("expected the provider's own reference, got %+v", out.Avatar)
	}

	// The whole reference reached the row, which is what FR-016 asks for.
	stored := h.stored(t, h.owner)
	if stored.Avatar == nil {
		t.Fatal("the stored profile carries no avatar")
	}
	if stored.Avatar.PublicID != out.Avatar.PublicID || stored.Avatar.URL != out.Avatar.URL ||
		stored.Avatar.Width != out.Avatar.Width || stored.Avatar.Height != out.Avatar.Height {
		t.Fatalf("expected the same reference in storage and in the answer, got %+v / %+v",
			stored.Avatar, out.Avatar)
	}

	// The provider is asked to do the resizing, and asked for the documented width.
	if len(h.media.widths) != 1 || h.media.widths[0] != appinterface.DefaultAvatarTargetWidth {
		t.Fatalf("expected one upload at the documented target width, got %v", h.media.widths)
	}
	// The original bytes go up: the provider resizes them, this service does not
	// (ADR-005).
	if len(h.media.uploads) != 1 || string(h.media.uploads[0]) != string(avatarImage()) {
		t.Fatalf("expected the original bytes to be uploaded, got %q", h.media.uploads)
	}
}

// FR-019: attaching a photo is a change, so it leaves a trace naming the account.
func TestSettingAnAvatarIsAudited(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})

	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}

	events := h.audit.eventsByAction("USER_AVATAR_SET")
	if len(events) != 1 {
		t.Fatalf("expected exactly one USER_AVATAR_SET event, got %d", len(events))
	}
	event := events[0]
	if event.actorID == nil || *event.actorID != h.owner {
		t.Fatalf("expected the account %s as the actor, got %v", h.owner, event.actorID)
	}
	if event.actorRole != string(access.RoleCustomer) {
		t.Fatalf("expected the actor role, got %q", event.actorRole)
	}
	if event.targetID != h.owner.String() || event.targetType != targetTypeProfile {
		t.Fatalf("expected the account as the target, got %q/%q", event.targetType, event.targetID)
	}
	if event.outcome != "SUCCESS" {
		t.Fatalf("expected a SUCCESS outcome, got %q", event.outcome)
	}
	// The event names what changed, never the stored link or the bytes.
	for key := range event.metadata {
		switch key {
		case "width", "height", "replaced":
		default:
			t.Fatalf("unexpected audit metadata key %q", key)
		}
	}
}

// FR-017: a rejected upload leaves the profile exactly as it was. Nothing is sent
// to the media service, nothing is written, and nothing is audited — a refused
// upload is not a change.
func TestARejectedUploadLeavesThePreviousAvatarUntouched(t *testing.T) {
	oversized := make([]byte, appinterface.DefaultAvatarMaxBytes+1)
	copy(oversized, avatarImage())

	cases := map[string]struct {
		content []byte
		want    error
	}{
		"not an image":     {content: []byte("definitely not an image"), want: domainerr.ErrAvatarTypeUnsupported},
		"over the ceiling": {content: oversized, want: domainerr.ErrAvatarTooLarge},
		"empty":            {content: nil, want: domainerr.ErrAvatarTypeUnsupported},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newAvatarHarness(t, appinterface.Config{})
			if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
				UserID: h.owner, Content: avatarImage(),
			}); err != nil {
				t.Fatalf("seed the avatar: %v", err)
			}
			before := h.stored(t, h.owner)
			uploadsBefore := h.media.uploadCount()
			savesBefore := h.repo.saves

			out, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
				UserID: h.owner, Content: tc.content,
			})

			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if out.ID != uuid.Nil || out.Avatar != nil {
				t.Fatalf("expected no profile in the result, got %+v", out)
			}
			// The file name is attacker-controlled and must not have any say.
			if h.media.uploadCount() != uploadsBefore {
				t.Fatal("a rejected upload must never reach the media service")
			}
			if h.repo.saves != savesBefore {
				t.Fatalf("a rejected upload must not be persisted, saves went from %d to %d",
					savesBefore, h.repo.saves)
			}
			if after := h.stored(t, h.owner); !sameProfile(after, before) {
				t.Fatalf("a rejected upload changed the stored profile: %+v", after)
			}
			if got := h.audit.countOf("USER_AVATAR_SET"); got != 1 {
				t.Fatalf("only the successful seed may be audited, got %d", got)
			}
		})
	}
}

// FR-017 and the contract's 503: a media outage is retryable and changes nothing.
func TestAMediaFailureChangesNothing(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the avatar: %v", err)
	}
	before := h.stored(t, h.owner)
	savesBefore := h.repo.saves
	h.media.uploadErr = errors.New("provider returned 500: {\"error\":{\"message\":\"boom\"}}")

	out, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	})

	if !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
	// Whatever the provider said must not travel further than the sentinel.
	if err.Error() != "media service unavailable" {
		t.Fatalf("expected the bare sentinel, got %q", err.Error())
	}
	if out.ID != uuid.Nil {
		t.Fatalf("expected no profile in the result, got %+v", out)
	}
	if h.repo.saves != savesBefore {
		t.Fatal("a media failure must not be persisted")
	}
	if after := h.stored(t, h.owner); !sameProfile(after, before) {
		t.Fatalf("a media failure changed the stored profile: %+v", after)
	}
	if got := h.audit.countOf("USER_AVATAR_SET"); got != 1 {
		t.Fatalf("a failed upload must not be audited as a change, got %d", got)
	}
}

// FR-017: replacing a photo releases the previous asset, otherwise every upload
// would leave an orphaned image behind in the provider's storage.
func TestReplacingAnAvatarReleasesThePreviousReference(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the avatar: %v", err)
	}
	first := *h.stored(t, h.owner).Avatar
	h.media.reference = appinterface.MediaReference{
		PublicID: "artist-shop/avatars/two",
		URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/two.jpg",
		Width:    512, Height: 640,
	}

	out, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	})

	if err != nil {
		t.Fatalf("replace the avatar: %v", err)
	}
	if out.Avatar == nil || out.Avatar.PublicID != "artist-shop/avatars/two" {
		t.Fatalf("expected the new reference, got %+v", out.Avatar)
	}
	if len(h.media.removed) != 1 {
		t.Fatalf("expected the previous asset to be released once, got %d", len(h.media.removed))
	}
	released := h.media.removed[0]
	if released.PublicID != first.PublicID || released.URL != first.URL {
		t.Fatalf("expected the previous reference to be released, got %+v", released)
	}
	// The stored row now points at the new asset only.
	stored := h.stored(t, h.owner)
	if stored.Avatar == nil || stored.Avatar.PublicID != "artist-shop/avatars/two" {
		t.Fatalf("expected the new reference to be stored, got %+v", stored.Avatar)
	}
	if got := h.audit.countOf("USER_AVATAR_SET"); got != 2 {
		t.Fatalf("expected both uploads to be audited, got %d", got)
	}
}

// A failure to release the replaced asset must not fail the request: the row
// already points at the new image, so the customer's outcome is decided and an
// orphaned file is a storage-cleanup problem, not a user-visible one.
func TestAFailedReleaseOfAReplacedAvatarDoesNotFailTheUpload(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the avatar: %v", err)
	}
	h.media.removeErr = errors.New("provider unreachable")
	h.media.reference = appinterface.MediaReference{
		PublicID: "artist-shop/avatars/two",
		URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/two.jpg",
		Width:    512, Height: 512,
	}

	out, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	})

	if err != nil {
		t.Fatalf("a storage-cleanup failure must not fail the upload, got %v", err)
	}
	if out.Avatar == nil || out.Avatar.PublicID != "artist-shop/avatars/two" {
		t.Fatalf("expected the new reference, got %+v", out.Avatar)
	}
	if stored := h.stored(t, h.owner); stored.Avatar == nil || stored.Avatar.PublicID != "artist-shop/avatars/two" {
		t.Fatalf("expected the new reference to be stored, got %+v", stored.Avatar)
	}
}

// A reference the provider reports outside what the contract can describe is
// refused rather than stored, and the asset that was just uploaded is released
// again so nothing is orphaned by the refusal (FR-015, FR-016).
func TestAReferenceOutsideTheContractIsRefusedAndReleased(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	h.media.reference = appinterface.MediaReference{
		PublicID: "artist-shop/avatars/wide",
		URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/wide.jpg",
		Width:    1024, // the provider did not resize
		Height:   1024,
	}
	savesBefore := h.repo.saves

	_, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	})

	if !errors.Is(err, domainerr.ErrIncompleteAvatar) {
		t.Fatalf("expected an over-wide reference to be refused, got %v", err)
	}
	if h.repo.saves != savesBefore {
		t.Fatal("a refused reference must not be persisted")
	}
	if stored := h.stored(t, h.owner); stored.Avatar != nil {
		t.Fatalf("expected the previous avatar to be untouched, got %+v", stored.Avatar)
	}
	if len(h.media.removed) != 1 || h.media.removed[0].PublicID != "artist-shop/avatars/wide" {
		t.Fatalf("expected the just-uploaded asset to be released, got %+v", h.media.removed)
	}
	if got := h.audit.countOf("USER_AVATAR_SET"); got != 0 {
		t.Fatalf("a refused upload must not be audited, got %d", got)
	}
}

func TestRemoveAvatarClearsTheReferenceAndReleasesIt(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the avatar: %v", err)
	}
	stored := h.stored(t, h.owner)
	released := model.AvatarReference{
		PublicID: stored.Avatar.PublicID,
		URL:      stored.Avatar.URL,
		Width:    stored.Avatar.Width,
		Height:   stored.Avatar.Height,
	}

	out, err := h.svc.RemoveAvatar(context.Background(), h.owner)

	if err != nil {
		t.Fatalf("RemoveAvatar: %v", err)
	}
	if out.Avatar != nil {
		t.Fatalf("expected no avatar in the result, got %+v", out.Avatar)
	}
	if after := h.stored(t, h.owner); after.Avatar != nil {
		t.Fatalf("expected the avatar to be removed from the row, got %+v", after.Avatar)
	}
	if len(h.media.removed) != 1 {
		t.Fatalf("expected the asset to be released once, got %d", len(h.media.removed))
	}
	got := h.media.removed[0]
	if got.PublicID != released.PublicID || got.URL != released.URL ||
		got.Width != released.Width || got.Height != released.Height {
		t.Fatalf("expected the stored reference to be released, got %+v want %+v", got, released)
	}
	events := h.audit.eventsByAction("USER_AVATAR_REMOVED")
	if len(events) != 1 {
		t.Fatalf("expected exactly one USER_AVATAR_REMOVED event, got %d", len(events))
	}
	if events[0].actorID == nil || *events[0].actorID != h.owner {
		t.Fatalf("expected the account as the actor, got %v", events[0].actorID)
	}
	if events[0].targetID != h.owner.String() {
		t.Fatalf("expected the account as the target, got %q", events[0].targetID)
	}
}

// A customer without a photo who presses the button again has already got the
// outcome they asked for: nothing changes, so nothing is written and nothing is
// audited, and no provider call is made for an asset that was never stored.
func TestRemovingAnAvatarThatIsNotThereIsANoOp(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	savesBefore := h.repo.saves

	out, err := h.svc.RemoveAvatar(context.Background(), h.owner)

	if err != nil {
		t.Fatalf("RemoveAvatar on an empty profile must not be an error, got %v", err)
	}
	if out.Avatar != nil {
		t.Fatalf("expected no avatar, got %+v", out.Avatar)
	}
	if h.repo.saves != savesBefore {
		t.Fatal("removing nothing must not write the row")
	}
	if len(h.media.removed) != 0 {
		t.Fatal("removing nothing must not call the media service")
	}
	if got := h.audit.countOf("USER_AVATAR_REMOVED"); got != 0 {
		t.Fatalf("removing nothing must not be audited, got %d", got)
	}
}

// The acting account comes from the session, so one customer can neither replace
// nor release another customer's photo (FR-006, SC-003).
func TestTheAvatarUseCasesOnlyTouchTheSessionAccount(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	stranger := h.other
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: stranger, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the stranger's avatar: %v", err)
	}
	before := h.stored(t, stranger)
	uploadsBefore := h.media.uploadCount()
	removalsBefore := len(h.media.removed)

	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("SetAvatar for the owner: %v", err)
	}
	if after := h.stored(t, stranger); !sameProfile(after, before) {
		t.Fatalf("the owner's upload changed another customer's row: %+v", after)
	}
	// The owner had no photo, so there was nothing of theirs to release: the
	// stranger's asset is untouched.
	if len(h.media.removed) != removalsBefore {
		t.Fatal("no reference may be released for an account the caller does not own")
	}

	if _, err := h.svc.RemoveAvatar(context.Background(), h.owner); err != nil {
		t.Fatalf("RemoveAvatar for the owner: %v", err)
	}
	if after := h.stored(t, stranger); !sameProfile(after, before) {
		t.Fatalf("the owner's removal changed another customer's row: %+v", after)
	}
	if h.media.uploadCount() != uploadsBefore+1 {
		t.Fatalf("expected exactly one further upload, got %d", h.media.uploadCount()-uploadsBefore)
	}
	// Removing the owner's own photo releases exactly that one reference.
	if len(h.media.removed) != removalsBefore+1 {
		t.Fatalf("expected the owner's own asset to be released, got %d releases",
			len(h.media.removed)-removalsBefore)
	}
	if got := h.stored(t, h.owner); got.Avatar != nil {
		t.Fatalf("expected the owner's photo to be gone, got %+v", got.Avatar)
	}
}

// FR-021: reading a profile never needs the media service, so an outage cannot
// fail a read. This is asserted through the avatar paths too, because the upload
// has just been the one operation that did need it.
func TestAProfileReadKeepsWorkingAfterAMediaFailure(t *testing.T) {
	h := newAvatarHarness(t, appinterface.Config{})
	if _, err := h.svc.SetAvatar(context.Background(), appdto.SetAvatarInput{
		UserID: h.owner, Content: avatarImage(),
	}); err != nil {
		t.Fatalf("seed the avatar: %v", err)
	}
	h.media.uploadErr = errors.New("provider unreachable")
	h.media.removeErr = errors.New("provider unreachable")

	out, err := h.svc.GetProfile(context.Background(), h.owner)

	if err != nil {
		t.Fatalf("a media outage must not fail a profile read, got %v", err)
	}
	if out.Avatar == nil || out.Avatar.PublicID != "artist-shop/avatars/one" {
		t.Fatalf("expected the stored reference to be returned as saved, got %+v", out.Avatar)
	}
}
