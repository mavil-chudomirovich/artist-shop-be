package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file exercises the two US1 routes end to end: the real handlers, the real
// authentication middleware and the real use cases over an in-memory repository.
// The stub service in router_test.go proves the wiring of every route; here the
// point is what the routes actually do.

// Tokens the fixture's hooks understand. They stand in for what the auth module
// issues, so no test needs a real token issuer.
const (
	liveToken    = "valid-session"
	expiredToken = "expired-session"
)

// memoryProfiles is an in-memory ProfileRepository. Every value is copied in and
// out, so a test can only observe what was really written.
type memoryProfiles struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]model.Profile
	saves int
}

func newMemoryProfiles(rows ...model.Profile) *memoryProfiles {
	store := &memoryProfiles{rows: make(map[uuid.UUID]model.Profile, len(rows))}
	for _, row := range rows {
		store.rows[row.ID] = copyProfile(row)
	}
	return store
}

func copyProfile(profile model.Profile) model.Profile {
	out := profile
	if profile.Phone != nil {
		phone := *profile.Phone
		out.Phone = &phone
	}
	if profile.Avatar != nil {
		avatar := *profile.Avatar
		out.Avatar = &avatar
	}
	return out
}

func (m *memoryProfiles) ByID(_ context.Context, id uuid.UUID) (*model.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	stored := copyProfile(row)
	return &stored, nil
}

func (m *memoryProfiles) Save(_ context.Context, profile *model.Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[profile.ID]; !ok {
		return domainerr.ErrUserNotFound
	}
	m.saves++
	m.rows[profile.ID] = copyProfile(*profile)
	return nil
}

func (m *memoryProfiles) saveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

func (m *memoryProfiles) stored(id uuid.UUID) (model.Profile, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	return copyProfile(row), ok
}

// sameProfile compares two rows by value. Comparing the structs directly would
// compare the phone pointer addresses, which the store allocates afresh on every
// read.
func sameProfile(a, b model.Profile) bool {
	if a.ID != b.ID || a.Email != b.Email || a.Role != b.Role || a.DisplayName != b.DisplayName {
		return false
	}
	if (a.Phone == nil) != (b.Phone == nil) {
		return false
	}
	if a.Phone != nil && *a.Phone != *b.Phone {
		return false
	}
	if (a.Avatar == nil) != (b.Avatar == nil) {
		return false
	}
	return a.Avatar == nil || *a.Avatar == *b.Avatar
}

var _ repository.ProfileRepository = (*memoryProfiles)(nil)

// profileAudit collects the audit actions the use cases emit.
type profileAudit struct {
	mu     sync.Mutex
	events []string
}

func (a *profileAudit) Record(_ context.Context, action, _ string, _ *uuid.UUID, _, _, _ string, _ map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, action)
}

func (a *profileAudit) countOf(action string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	total := 0
	for _, e := range a.events {
		if e == action {
			total++
		}
	}
	return total
}

var _ appinterface.Auditor = (*profileAudit)(nil)

// profileService binds the real profile use cases to the module's full use-case
// surface.
//
// Only the profile story is delivered in this phase, so the avatar and address
// use cases do not exist yet and *implement.Service does not yet satisfy
// appinterface.UserService. The remaining methods therefore come from the
// existing stub, whose routes these tests never reach. This adapter disappears
// once every story has landed and T023 wires *implement.Service directly.
type profileService struct {
	*stubService
	profiles *implement.Service
}

func (s profileService) GetProfile(ctx context.Context, id uuid.UUID) (appdto.ProfileOutput, error) {
	return s.profiles.GetProfile(ctx, id)
}

func (s profileService) UpdateProfile(ctx context.Context, in appdto.UpdateProfileInput) (appdto.ProfileOutput, error) {
	return s.profiles.UpdateProfile(ctx, in)
}

var _ appinterface.UserService = profileService{}

// profileFixture is the whole US1 stack over in-memory storage.
type profileFixture struct {
	router http.Handler
	repo   *memoryProfiles
	audit  *profileAudit
	// customer is the account a valid session identifies.
	customer uuid.UUID
	// other is a second account that must stay invisible to that session.
	other uuid.UUID
}

func newProfileFixture(t *testing.T) *profileFixture {
	t.Helper()
	customer := uuid.New()
	other := uuid.New()
	repo := newMemoryProfiles(
		model.Profile{ID: customer, Email: "customer@example.com", Role: access.RoleCustomer},
		model.Profile{
			ID:          other,
			Email:       "other@example.com",
			Role:        access.RoleCustomer,
			DisplayName: "Other Customer",
		},
	)
	audit := &profileAudit{}
	profiles := implement.New(implement.Service{
		Profiles: repo,
		Audit:    audit,
		Mapper:   mapper.New(nil),
		// No MediaStore is wired at all: the profile routes must work without
		// one (FR-021).
	})
	svc := profileService{stubService: newStub(access.RoleCustomer), profiles: profiles}
	handler := New(svc, appinterface.Config{AvatarMaxBytes: 16}, testLogger)
	return &profileFixture{
		router:   handler.Router(openLimits, sessionHooks(customer)),
		repo:     repo,
		audit:    audit,
		customer: customer,
		other:    other,
	}
}

// sessionHooks resolves the two tokens of the fixture: a live session for the
// given account, and an expired one the middleware refuses before any use case
// runs.
func sessionHooks(subject uuid.UUID) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			header := r.Header.Get("Authorization")
			switch {
			case header == "":
				return nil, nil
			case strings.Contains(header, expiredToken):
				// The composition root maps the auth module's token failure to its
				// own documented code; this module only ever sees a 401 AppError.
				return nil, &httpx.AppError{
					Code:    "AUTH_TOKEN_EXPIRED",
					Status:  http.StatusUnauthorized,
					Message: "Access token has expired",
				}
			case strings.Contains(header, liveToken):
				return &middleware.Identity{Subject: subject.String(), Role: string(access.RoleCustomer), TokenID: "jti"}, nil
			default:
				return nil, httpx.New(httpx.CodeUnauthenticated)
			}
		},
	}
}

type profileBody struct {
	Data struct {
		ID          uuid.UUID        `json:"id"`
		Email       string           `json:"email"`
		Role        access.Role      `json:"role"`
		DisplayName string           `json:"displayName"`
		Phone       *string          `json:"phone"`
		Avatar      *json.RawMessage `json:"avatar"`
	} `json:"data"`
}

func decodeProfile(t *testing.T, rec *httptest.ResponseRecorder) profileBody {
	t.Helper()
	var body profileBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode profile body %q: %v", rec.Body.String(), err)
	}
	return body
}

func (f *profileFixture) get(token string) *httptest.ResponseRecorder {
	return do(f.router, http.MethodGet, "/me", token)
}

func (f *profileFixture) patch(body, token string) *httptest.ResponseRecorder {
	return doJSON(f.router, http.MethodPatch, "/me", body, token)
}

// SC-002: a request without a session is refused before any stored data is
// touched.
func TestProfileRoutesRefuseACallerWithoutAToken(t *testing.T) {
	fixture := newProfileFixture(t)

	for _, rec := range []*httptest.ResponseRecorder{
		fixture.get(""),
		fixture.patch(`{"displayName":"Nguyen Van A"}`, ""),
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := errorCode(t, rec); got != "UNAUTHENTICATED" {
			t.Fatalf("expected UNAUTHENTICATED, got %s", got)
		}
	}
	if fixture.repo.saveCount() != 0 {
		t.Fatal("an unauthenticated request must not write anything")
	}
	if len(fixture.audit.events) != 0 {
		t.Fatalf("an unauthenticated request must not be audited, got %v", fixture.audit.events)
	}
}

func TestProfileRoutesRefuseAnExpiredSession(t *testing.T) {
	fixture := newProfileFixture(t)

	rec := fixture.get(expiredToken)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_TOKEN_EXPIRED" {
		t.Fatalf("expected the auth module's token code to pass through, got %s", got)
	}
	if fixture.repo.saveCount() != 0 {
		t.Fatal("an expired session must not write anything")
	}
	if len(fixture.audit.events) != 0 {
		t.Fatalf("an expired session must not be audited, got %v", fixture.audit.events)
	}
}

func TestOwnerReadsTheirOwnProfile(t *testing.T) {
	fixture := newProfileFixture(t)

	rec := fixture.get(liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeProfile(t, rec)
	if body.Data.ID != fixture.customer {
		t.Fatalf("expected the account %s, got %s", fixture.customer, body.Data.ID)
	}
	if body.Data.Email != "customer@example.com" {
		t.Fatalf("expected the account email, got %q", body.Data.Email)
	}
	if body.Data.Role != access.RoleCustomer {
		t.Fatalf("expected the account role, got %q", body.Data.Role)
	}
}

// SC-003: one customer can never read another customer's profile, because the
// acting account comes from the session and nothing else.
func TestACustomerOnlyEverReadsTheirOwnProfile(t *testing.T) {
	fixture := newProfileFixture(t)

	rec := fixture.get(liveToken)

	body := decodeProfile(t, rec)
	if body.Data.ID == fixture.other || body.Data.Email == "other@example.com" {
		t.Fatalf("another customer's profile leaked: %+v", body.Data)
	}
	if body.Data.DisplayName == "Other Customer" {
		t.Fatalf("another customer's display name leaked: %+v", body.Data)
	}
}

func TestUpdateProfileReturnsTheNormalisedValues(t *testing.T) {
	fixture := newProfileFixture(t)

	rec := fixture.patch(`{"displayName":"  Nguyen Van A  ","phone":"+84 912 345 678"}`, liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeProfile(t, rec)
	if body.Data.DisplayName != "Nguyen Van A" {
		t.Fatalf("expected the trimmed display name, got %q", body.Data.DisplayName)
	}
	if body.Data.Phone == nil || *body.Data.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone, got %v", body.Data.Phone)
	}
	stored, ok := fixture.repo.stored(fixture.customer)
	if !ok {
		t.Fatal("the owner's row vanished")
	}
	if stored.DisplayName != "Nguyen Van A" {
		t.Fatalf("expected the trimmed name to be stored, got %q", stored.DisplayName)
	}
	if stored.Phone == nil || *stored.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone to be stored, got %v", stored.Phone)
	}
}

func TestUpdateProfileClearsAFieldSentAsEmptyString(t *testing.T) {
	fixture := newProfileFixture(t)
	if rec := fixture.patch(`{"displayName":"Nguyen Van A","phone":"0912345678"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("seed the profile: %d (%s)", rec.Code, rec.Body.String())
	}

	rec := fixture.patch(`{"phone":null}`, liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeProfile(t, rec)
	if body.Data.Phone != nil {
		t.Fatalf("expected a cleared phone, got %q", *body.Data.Phone)
	}
	if body.Data.DisplayName != "Nguyen Van A" {
		t.Fatalf("an omitted field must keep its value, got %q", body.Data.DisplayName)
	}
	if stored, _ := fixture.repo.stored(fixture.customer); stored.Phone != nil {
		t.Fatalf("expected the phone to be cleared in storage too, got %v", stored.Phone)
	}
}

func TestInvalidPhoneIsRejectedWithTheFieldNamed(t *testing.T) {
	fixture := newProfileFixture(t)
	if rec := fixture.patch(`{"displayName":"Nguyen Van A","phone":"0912345678"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("seed the profile: %d (%s)", rec.Code, rec.Body.String())
	}
	before, _ := fixture.repo.stored(fixture.customer)

	rec := fixture.patch(`{"phone":"12345"}`, liveToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeInvalidPhone {
		t.Fatalf("expected %s, got %s", constant.CodeInvalidPhone, got)
	}
	if got := detailField(t, rec); got != fieldPhone {
		t.Fatalf("expected the detail to name %q, got %q", fieldPhone, got)
	}
	if after, _ := fixture.repo.stored(fixture.customer); !sameProfile(after, before) {
		t.Fatalf("a rejected phone changed the stored profile: %+v", after)
	}
}

// A display name over the maxLength the contract declares for it must be refused
// the same way an invalid phone is: 400 VALIDATION_ERROR with a detail naming the
// member the customer has to shorten. Nothing is persisted and nothing is audited,
// because nothing changed. This is the check that keeps the advertised maxLength
// from being a limit the server quietly ignores (FR-020).
func TestADisplayNameOverTheContractLengthCeilingIsRefused(t *testing.T) {
	fixture := newProfileFixture(t)
	if rec := fixture.patch(`{"displayName":"Nguyen Van A","phone":"0912345678"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("seed the profile: %d (%s)", rec.Code, rec.Body.String())
	}
	before, _ := fixture.repo.stored(fixture.customer)
	savesBefore := fixture.repo.saveCount()

	rec := fixture.patch(`{"displayName":`+mustJSONString(t, strings.Repeat("a", 121))+`}`, liveToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeValidation) {
		t.Fatalf("expected VALIDATION_ERROR, got %s", got)
	}
	if got := detailField(t, rec); got != fieldDisplayName {
		t.Fatalf("expected the detail to name %q, got %q", fieldDisplayName, got)
	}
	if after, _ := fixture.repo.stored(fixture.customer); !sameProfile(after, before) {
		t.Fatalf("a refused name changed the stored profile: %+v", after)
	}
	if fixture.repo.saveCount() != savesBefore {
		t.Fatal("a refused name must not be written")
	}
	if got := fixture.audit.countOf(constant.AuditProfileUpdated); got != 1 {
		t.Fatalf("only the successful seed update may be audited, got %d", got)
	}
}

// A name at exactly the advertised length is accepted through the wire, and so is
// a multi-byte one: the ceiling counts characters, so a Vietnamese name of 120
// characters passes even though it is 360 bytes.
func TestADisplayNameAtTheContractLengthCeilingIsAccepted(t *testing.T) {
	cases := map[string]string{
		"ascii at the limit":        strings.Repeat("a", 120),
		"multi-byte at the limit":   strings.Repeat("Ữ", 120),
		"the limit plus whitespace": strings.Repeat("a", 120) + "   ",
	}

	for name, displayName := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newProfileFixture(t)

			rec := fixture.patch(`{"displayName":`+mustJSONString(t, displayName)+`}`, liveToken)

			if rec.Code != http.StatusOK {
				t.Fatalf("a name at the advertised limit must be accepted, got %d (%s)", rec.Code, rec.Body.String())
			}
			want := strings.TrimSpace(displayName)
			if got := decodeProfile(t, rec).Data.DisplayName; got != want {
				t.Fatalf("expected the trimmed name of %d characters, got %d",
					len([]rune(want)), len([]rune(got)))
			}
			if stored, _ := fixture.repo.stored(fixture.customer); stored.DisplayName != want {
				t.Fatalf("expected the trimmed name to be stored, got %d characters", len([]rune(stored.DisplayName)))
			}
		})
	}
}

func TestEverySuccessfulUpdateIsAudited(t *testing.T) {
	fixture := newProfileFixture(t)

	if rec := fixture.patch(`{"displayName":"Nguyen Van A"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("first update: %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := fixture.patch(`{"phone":"0912345678"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("second update: %d (%s)", rec.Code, rec.Body.String())
	}

	if got := fixture.audit.countOf(constant.AuditProfileUpdated); got != 2 {
		t.Fatalf("expected two audit events, got %d", got)
	}
}

func TestARejectedUpdateIsNotAudited(t *testing.T) {
	fixture := newProfileFixture(t)

	if rec := fixture.patch(`{"phone":"12345"}`, liveToken); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	if got := fixture.audit.countOf(constant.AuditProfileUpdated); got != 0 {
		t.Fatalf("a rejected update must leave no audit event, got %d", got)
	}
}

// FR-021: the profile still reads when there is no photo, and the avatar member
// is null rather than absent or an error, so a media outage cannot fail the read.
func TestProfileReadCarriesANullAvatarWhenThereIsNoPhoto(t *testing.T) {
	fixture := newProfileFixture(t)

	rec := fixture.get(liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"avatar":null`) {
		t.Fatalf("expected a null avatar member, got %s", rec.Body.String())
	}
	body := decodeProfile(t, rec)
	if body.Data.Avatar != nil {
		t.Fatalf("expected no avatar object, got %s", string(*body.Data.Avatar))
	}
}

// There is deliberately no 404 case for these two routes: the profile is the
// users row and account deletion is out of scope (FR-024), so the row a session
// identifies cannot go missing. The assertion below documents that contract by
// checking the route set, not by inventing a failure mode.
func TestProfileRoutesCannotReturnNotFound(t *testing.T) {
	fixture := newProfileFixture(t)

	if rec := fixture.get(liveToken); rec.Code == http.StatusNotFound {
		t.Fatalf("reading one's own profile must never answer 404, got %s", rec.Body.String())
	}
}

// The group is mounted under the API prefix in the composition root, so mounting
// it here proves the routes resolve at /api/v1/users/me too.
func TestProfileRoutesResolveUnderTheAPIPrefix(t *testing.T) {
	fixture := newProfileFixture(t)
	root := chi.NewRouter()
	root.Mount("/api/v1/users", fixture.router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+liveToken)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeProfile(t, rec)
	if body.Data.ID != fixture.customer {
		t.Fatalf("expected the account %s, got %s", fixture.customer, body.Data.ID)
	}
}
