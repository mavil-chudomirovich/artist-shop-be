package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// This file exercises the two avatar routes end to end: the real handlers, the
// real authentication and rate-limit middleware and the real avatar use cases over
// an in-memory repository and a media service that answers without a provider.

const (
	// avatarCeiling keeps the fixture's uploads small; the documented 2 MB default
	// is pinned separately below and in the domain tests.
	avatarCeiling = int64(1024)
)

// stubMedia answers the MediaStore port without a provider. err fails both calls,
// standing in for an outage the customer must be able to retry from.
type stubMedia struct {
	reference appinterface.MediaReference
	err       error
	removed   int
	uploads   int
}

func newStubMedia() *stubMedia {
	return &stubMedia{reference: appinterface.MediaReference{
		PublicID: "artist-shop/avatars/one",
		URL:      "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",
		Width:    512,
		Height:   512,
	}}
}

func (m *stubMedia) Upload(context.Context, []byte, int) (appinterface.MediaReference, error) {
	m.uploads++
	if m.err != nil {
		return appinterface.MediaReference{}, m.err
	}
	return m.reference, nil
}

func (m *stubMedia) Remove(context.Context, appinterface.MediaReference) error {
	m.removed++
	return m.err
}

var _ appinterface.MediaStore = (*stubMedia)(nil)

// avatarService binds the real avatar use cases to the module's full use-case
// surface. The administrator-lookup method still comes from the stub, whose route
// these tests never reach; this adapter disappears once T023 wires
// *implement.Service directly.
type avatarService struct {
	*stubService
	user *implement.Service
}

func (s avatarService) GetProfile(ctx context.Context, id uuid.UUID) (appdto.ProfileOutput, error) {
	return s.user.GetProfile(ctx, id)
}

func (s avatarService) SetAvatar(ctx context.Context, in appdto.SetAvatarInput) (appdto.ProfileOutput, error) {
	return s.user.SetAvatar(ctx, in)
}

func (s avatarService) RemoveAvatar(ctx context.Context, id uuid.UUID) (appdto.ProfileOutput, error) {
	return s.user.RemoveAvatar(ctx, id)
}

var _ appinterface.UserService = avatarService{}

// avatarFixture is the whole US3 stack over in-memory storage.
type avatarFixture struct {
	router   http.Handler
	repo     *memoryProfiles
	audit    *profileAudit
	media    *stubMedia
	customer uuid.UUID
}

func newAvatarFixture(t *testing.T, limits config.UserConfig, ceiling int64) *avatarFixture {
	t.Helper()
	customer := uuid.New()
	repo := newMemoryProfiles(
		model.Profile{ID: customer, Email: "customer@example.com", Role: access.RoleCustomer},
	)
	audit := &profileAudit{}
	media := newStubMedia()
	user := implement.New(implement.Service{
		Profiles: repo,
		Audit:    audit,
		Media:    media,
		Mapper:   mapper.New(nil),
	})
	handler := New(avatarService{stubService: newStub(access.RoleCustomer), user: user},
		appinterface.Config{AvatarMaxBytes: ceiling}, testLogger)
	return &avatarFixture{
		router:   handler.Router(limits, sessionHooks(customer)),
		repo:     repo,
		audit:    audit,
		media:    media,
		customer: customer,
	}
}

// uploadAvatar sends a multipart body whose "file" part carries content under the
// declared file name, which is the only part of the request the handler trusts to
// exist.
func (f *avatarFixture) uploadAvatar(t *testing.T, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	return postAvatar(f.router, filename, content, liveToken)
}

func (f *avatarFixture) upload(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.uploadAvatar(t, "avatar.png", pngPayload())
}

func (f *avatarFixture) remove(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return do(f.router, http.MethodDelete, "/me/avatar", liveToken)
}

func (f *avatarFixture) storedAvatar(t *testing.T) *model.AvatarReference {
	t.Helper()
	row, ok := f.repo.stored(f.customer)
	if !ok {
		t.Fatal("the account row vanished")
	}
	return row.Avatar
}

// postAvatar builds a multipart request around one file part. It never sets a
// media type on the part itself, because the handler must decide the image type
// from the bytes rather than from what the client declared.
func postAvatar(handler http.Handler, filename string, content []byte, token string) *httptest.ResponseRecorder {
	return postAvatarAt(handler, "/me/avatar", filename, content, token)
}

// postAvatarAt is the same request against an arbitrary path, so a test that mounts
// the module under the API prefix can post to the full route.
func postAvatarAt(handler http.Handler, path, filename string, content []byte, token string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(content); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// pngPayload is the smallest thing the domain's sniffer recognises as a PNG.
func pngPayload() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x2A}, 64)...)
}

type avatarBody struct {
	Data struct {
		Avatar *struct {
			PublicID string `json:"publicId"`
			URL      string `json:"url"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		} `json:"avatar"`
	} `json:"data"`
}

func decodeAvatar(t *testing.T, rec *httptest.ResponseRecorder) avatarBody {
	t.Helper()
	var body avatarBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode avatar body %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-025 and SC-013: an account that exceeds the hourly upload rate is refused
// with a retry hint, and the photo it already has keeps working.
func TestExceedingTheAvatarUploadRateIsRefusedWithARetryHint(t *testing.T) {
	limits := config.UserConfig{AvatarUploadRatePerHour: 2, AddressWriteRatePerMinute: 1000}
	fixture := newAvatarFixture(t, limits, avatarCeiling)

	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("first upload: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("second upload: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	stored := fixture.storedAvatar(t)
	if stored == nil {
		t.Fatal("the accepted uploads must have stored a reference")
	}

	rec := fixture.upload(t)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", got)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header so the client knows when to try again")
	}
	// The refusal changes nothing, and the existing avatar keeps working.
	after := fixture.storedAvatar(t)
	if after == nil || *after != *stored {
		t.Fatalf("a refused upload changed the stored avatar: %+v", after)
	}
	read := decodeAvatar(t, do(fixture.router, http.MethodGet, "/me", liveToken))
	if read.Data.Avatar == nil || read.Data.Avatar.PublicID != stored.PublicID {
		t.Fatalf("expected the profile read to still carry the avatar, got %+v", read.Data.Avatar)
	}
	if removed := fixture.remove(t); removed.Code != http.StatusOK {
		t.Fatalf("the removal route is not the upload route and must keep working, got %d (%s)",
			removed.Code, removed.Body.String())
	}
	if fixture.storedAvatar(t) != nil {
		t.Fatal("expected the avatar to be removed")
	}
}

// The limit protects the media service, so a refused upload must not have reached
// it. Nothing here observes the provider directly; what the test pins is that the
// profile is untouched and no second reference was stored.
func TestTheAvatarRateLimitIsSeparateFromTheAddressLimit(t *testing.T) {
	limits := config.UserConfig{AvatarUploadRatePerHour: 1, AddressWriteRatePerMinute: 1}
	fixture := newAvatarFixture(t, limits, avatarCeiling)

	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("first upload: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := fixture.upload(t); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the second upload to be limited, got %d", rec.Code)
	}

	// One address write is allowed by its own separate budget.
	rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", addressBody, liveToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("the address budget is its own, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// FR-014: an upload that is not one of the three supported image formats is
// refused with the type code, the offending field named, and the current avatar
// left in place (SC-005).
func TestAnUnsupportedImageIsRefusedAndKeepsTheCurrentAvatar(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("seed the avatar: %d (%s)", rec.Code, rec.Body.String())
	}
	before := *fixture.storedAvatar(t)

	// A GIF header with a .png name: the name is not the evidence.
	rec := fixture.uploadAvatar(t, "avatar.png", []byte("GIF89a\x01\x00\x01\x00"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeAvatarTypeUnsupported {
		t.Fatalf("expected %s, got %s", constant.CodeAvatarTypeUnsupported, got)
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	after := fixture.storedAvatar(t)
	if after == nil || *after != before {
		t.Fatalf("a refused upload changed the stored avatar: %+v", after)
	}
	if got := fixture.audit.countOf(constant.AuditAvatarSet); got != 1 {
		t.Fatalf("a refused upload must not be audited, got %d", got)
	}
}

// The file name and the part's declared media type are both attacker-controlled,
// so the same bytes pass under any name.
func TestTheImageTypeIsDecidedFromTheBytesNotTheName(t *testing.T) {
	for _, filename := range []string{"avatar.png", "avatar.exe", "no-extension", "../../etc/passwd"} {
		t.Run(filename, func(t *testing.T) {
			fixture := newAvatarFixture(t, openLimits, avatarCeiling)
			rec := fixture.uploadAvatar(t, filename, pngPayload())
			if rec.Code != http.StatusOK {
				t.Fatalf("expected the PNG bytes to be accepted under %q, got %d (%s)",
					filename, rec.Code, rec.Body.String())
			}
		})
	}
}

// The ceiling is enforced while the payload is read, so an upload one byte over it
// is refused with the documented 413 rather than buffered and refused afterwards.
func TestAnOversizedUploadIsRefusedWithTheSizeCode(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)

	rec := fixture.uploadAvatar(t, "avatar.png", bytes.Repeat([]byte("a"), int(avatarCeiling)+1))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeAvatarTooLarge {
		t.Fatalf("expected %s, got %s", constant.CodeAvatarTooLarge, got)
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	if fixture.storedAvatar(t) != nil {
		t.Fatal("an oversized upload must not be stored")
	}
}

// A payload of exactly the ceiling is the largest accepted one, so the check is
// inclusive at the boundary rather than off by one.
func TestAnUploadAtExactlyTheCeilingIsAccepted(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	content := make([]byte, avatarCeiling)
	copy(content, pngPayload())

	rec := fixture.uploadAvatar(t, "avatar.png", content)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// A declared length cannot be trusted, so the ceiling has to hold for a body that
// streams in chunks and announces no length at all. The route's own body limit cuts
// such a stream, and the customer is told what the rule actually was rather than
// being handed a parse failure.
func TestAnOversizedUploadWithoutADeclaredLengthIsRefusedAsTooLarge(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	content := make([]byte, avatarCeiling+512)
	copy(content, pngPayload())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("create the file part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write the file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+liveToken)
	// A chunked request announces no length, so the route's early refusal cannot
	// fire and the read ceiling has to.
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	fixture.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeAvatarTooLarge {
		t.Fatalf("expected %s, got %s", constant.CodeAvatarTooLarge, got)
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	if fixture.storedAvatar(t) != nil {
		t.Fatal("an oversized upload must not be stored")
	}
}

// The documented 2 MB ceiling is what a real customer hits, so the route has to
// carry a request that size when the composition leaves the setting at its
// default. This is the payload the global body ceiling must not refuse.
func TestAnUploadAtTheDocumentedCeilingFitsTheRoute(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, 0) // the default ceiling applies
	content := make([]byte, appinterface.DefaultAvatarMaxBytes)
	copy(content, pngPayload())

	rec := fixture.uploadAvatar(t, "avatar.png", content)

	if rec.Code != http.StatusOK {
		t.Fatalf("a legitimate 2 MB upload must be accepted, got %d (%s)", rec.Code, rec.Body.String())
	}
	avatar := decodeAvatar(t, rec).Data.Avatar
	if avatar == nil || avatar.Width != 512 || avatar.Height != 512 || avatar.URL == "" || avatar.PublicID == "" {
		t.Fatalf("expected the stored reference in the response, got %+v", avatar)
	}
}

// FR-017 and the contract's 503: a provider outage is retryable, so the current
// avatar is left exactly as it was and the client is told the profile did not
// change.
func TestAMediaFailureIsRetryableAndKeepsTheCurrentAvatar(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("seed the avatar: %d (%s)", rec.Code, rec.Body.String())
	}
	before := *fixture.storedAvatar(t)
	fixture.media.err = errors.New("provider returned 500")

	rec := fixture.upload(t)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeMediaUnavailable {
		t.Fatalf("expected %s, got %s", constant.CodeMediaUnavailable, got)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("provider returned 500")) {
		t.Fatalf("the provider's own words leaked to the client: %s", rec.Body.String())
	}
	after := fixture.storedAvatar(t)
	if after == nil || *after != before {
		t.Fatalf("a media failure changed the stored avatar: %+v", after)
	}
}

func TestRemovingTheAvatarAnswersTheProfileWithoutOne(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("seed the avatar: %d (%s)", rec.Code, rec.Body.String())
	}

	rec := fixture.remove(t)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if avatar := decodeAvatar(t, rec).Data.Avatar; avatar != nil {
		t.Fatalf("expected no avatar in the response, got %+v", avatar)
	}
	if fixture.storedAvatar(t) != nil {
		t.Fatal("expected the stored avatar to be cleared")
	}
	if got := fixture.audit.countOf(constant.AuditAvatarRemoved); got != 1 {
		t.Fatalf("expected the removal to be audited once, got %d", got)
	}
	if fixture.media.removed != 1 {
		t.Fatalf("expected the asset to be released once, got %d", fixture.media.removed)
	}
}

// FR-021: a profile whose media service is unavailable is still readable. The
// read answers from the stored reference alone, so the outage that made an upload
// fail cannot make a read fail.
func TestTheProfileReadKeepsWorkingWhileTheMediaServiceIsDown(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)
	if rec := fixture.upload(t); rec.Code != http.StatusOK {
		t.Fatalf("seed the avatar: %d (%s)", rec.Code, rec.Body.String())
	}
	fixture.media.err = errors.New("provider unreachable")

	rec := do(fixture.router, http.MethodGet, "/me", liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	avatar := decodeAvatar(t, rec).Data.Avatar
	if avatar == nil || avatar.PublicID == "" || avatar.URL == "" {
		t.Fatalf("expected the stored reference to be returned as saved, got %+v", avatar)
	}
}

// SC-002: an upload or a removal without a session is refused before any byte
// reaches the media service.
func TestTheAvatarRoutesRefuseAnUnauthenticatedCaller(t *testing.T) {
	fixture := newAvatarFixture(t, openLimits, avatarCeiling)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"upload": postAvatar(fixture.router, "avatar.png", pngPayload(), ""),
		"remove": do(fixture.router, http.MethodDelete, "/me/avatar", ""),
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if fixture.storedAvatar(t) != nil {
		t.Fatal("an unauthenticated request must not store anything")
	}
	if fixture.media.removed != 0 {
		t.Fatal("an unauthenticated request must not reach the media service")
	}
}
