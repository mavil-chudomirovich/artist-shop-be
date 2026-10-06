package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// stubService records what the handlers passed down, so a test can assert that
// the acting account came from the session rather than from the request.
type stubService struct {
	subject uuid.UUID
	role    access.Role

	mu        sync.Mutex
	called    []string
	profile   uuid.UUID
	update    appdto.UpdateProfileInput
	create    appdto.CreateAddressInput
	edit      appdto.UpdateAddressInput
	ref       appdto.AddressRefInput
	avatar    appdto.SetAvatarInput
	lookup    uuid.UUID
	page      appdto.ListAddressesInput
	lookupErr error
}

func newStub(role access.Role) *stubService {
	return &stubService{subject: uuid.New(), role: role}
}

func (s *stubService) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = append(s.called, call)
}

func (s *stubService) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.called...)
}

func (s *stubService) wasCalled(call string) bool {
	for _, name := range s.calls() {
		if name == call {
			return true
		}
	}
	return false
}

func (s *stubService) GetProfile(_ context.Context, userID uuid.UUID) (appdto.ProfileOutput, error) {
	s.record("GetProfile")
	s.profile = userID
	return s.profileOutput(), nil
}

func (s *stubService) profileOutput() appdto.ProfileOutput {
	return appdto.ProfileOutput{ID: s.subject, Email: "user@example.com", Role: s.role, DisplayName: "Nguyen Van A"}
}

func (s *stubService) UpdateProfile(_ context.Context, in appdto.UpdateProfileInput) (appdto.ProfileOutput, error) {
	s.record("UpdateProfile")
	s.update = in
	return s.profileOutput(), nil
}

func (s *stubService) SetAvatar(_ context.Context, in appdto.SetAvatarInput) (appdto.ProfileOutput, error) {
	s.record("SetAvatar")
	s.avatar = in
	return s.profileOutput(), nil
}

func (s *stubService) RemoveAvatar(_ context.Context, userID uuid.UUID) (appdto.ProfileOutput, error) {
	s.record("RemoveAvatar")
	s.profile = userID
	return s.profileOutput(), nil
}

func (s *stubService) ListAddresses(_ context.Context, in appdto.ListAddressesInput) (appdto.AddressPageOutput, error) {
	s.record("ListAddresses")
	s.page = in
	return appdto.AddressPageOutput{Page: in.Page, PageSize: in.PageSize, Total: 0}, nil
}

func (s *stubService) CreateAddress(_ context.Context, in appdto.CreateAddressInput) (appdto.AddressOutput, error) {
	s.record("CreateAddress")
	s.create = in
	return appdto.AddressOutput{ID: uuid.New(), RecipientName: in.RecipientName}, nil
}

func (s *stubService) UpdateAddress(_ context.Context, in appdto.UpdateAddressInput) (appdto.AddressOutput, error) {
	s.record("UpdateAddress")
	s.edit = in
	return appdto.AddressOutput{ID: in.AddressID}, nil
}

func (s *stubService) DeleteAddress(_ context.Context, in appdto.AddressRefInput) error {
	s.record("DeleteAddress")
	s.ref = in
	return nil
}

func (s *stubService) SetDefaultAddress(_ context.Context, in appdto.AddressRefInput) (appdto.AddressOutput, error) {
	s.record("SetDefaultAddress")
	s.ref = in
	return appdto.AddressOutput{ID: in.AddressID, IsDefault: true}, nil
}

func (s *stubService) LookupCustomer(_ context.Context, userID uuid.UUID) (contracts.Customer, error) {
	s.record("LookupCustomer")
	s.lookup = userID
	if s.lookupErr != nil {
		return contracts.Customer{}, s.lookupErr
	}
	return contracts.Customer{ID: userID, Email: "user@example.com", Role: access.RoleCustomer}, nil
}

var _ appinterface.UserService = (*stubService)(nil)

// recordingAuditor stands in for the audit adapter in the denial path.
type recordingAuditor struct {
	mu     sync.Mutex
	events []string
}

func (a *recordingAuditor) Record(_ context.Context, action, _ string, _ *uuid.UUID, _, _, _ string, _ map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, action)
}

func (a *recordingAuditor) has(action string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.events {
		if e == action {
			return true
		}
	}
	return false
}

var _ appinterface.Auditor = (*recordingAuditor)(nil)

// stubDivisions serves two provinces so the reference-data routes can be checked
// without reading the bundled dataset.
type stubDivisions struct {
	unknownProvince bool
}

func (s *stubDivisions) Provinces(context.Context) ([]appinterface.Province, error) {
	return []appinterface.Province{{Code: "79", Name: "Ho Chi Minh"}, {Code: "01", Name: "Ha Noi"}}, nil
}

func (s *stubDivisions) Wards(_ context.Context, provinceCode string) ([]appinterface.Ward, error) {
	if s.unknownProvince || provinceCode != "79" {
		return nil, unknownProvinceError()
	}
	return []appinterface.Ward{{Code: "26734", Name: "Ward 1", ProvinceCode: provinceCode}}, nil
}

func (s *stubDivisions) ValidateAddressDivisions(context.Context, string, string) error { return nil }

var _ appinterface.Divisions = (*stubDivisions)(nil)

func unknownProvinceError() error {
	return fmt.Errorf("%w: %q", administrative.ErrUnknownProvince, "does-not-exist")
}

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// openLimits keeps both module limits wide so a test only trips the one it means
// to exercise.
var openLimits = config.UserConfig{AvatarUploadRatePerHour: 1000, AddressWriteRatePerMinute: 1000}

// testHooks resolves a signed-in caller from a bearer token, exactly as the
// composition root does, and records role denials through OnDenied.
func testHooks(svc *stubService, auditor *recordingAuditor) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			if r.Header.Get("Authorization") == "" {
				return nil, nil
			}
			return &middleware.Identity{Subject: svc.subject.String(), Role: string(svc.role), TokenID: "jti"}, nil
		},
		OnDenied: func(_ context.Context, id middleware.Identity, r *http.Request) {
			auditor.Record(context.Background(), "PRIVILEGE_DENIED", "FAILURE", nil, id.Role, "route", r.URL.Path, nil)
		},
	}
}

func usersRouter(svc *stubService, auditor *recordingAuditor, limits config.UserConfig) http.Handler {
	h := New(svc, appinterface.Config{AvatarMaxBytes: 16}, testLogger)
	return h.Router(limits, testHooks(svc, auditor))
}

func divisionsRouter() http.Handler {
	return NewDivisionsHandler(&stubDivisions{}, testLogger).Router(
		testHooks(newStub(access.RoleCustomer), &recordingAuditor{}))
}

func do(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func doJSON(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func doAvatar(handler http.Handler, content []byte, token string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(content); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Details []struct {
			Field string `json:"field"`
		} `json:"details"`
	} `json:"error"`
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return errorBody(t, rec).Error.Code
}

func detailField(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	details := errorBody(t, rec).Error.Details
	if len(details) != 1 {
		t.Fatalf("expected exactly one field detail, got %+v", details)
	}
	return details[0].Field
}

func TestEveryRouteRefusesAnUnauthenticatedCaller(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)
	body := `{"recipientName":"A","recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`
	targetID := uuid.New().String()

	cases := []struct {
		name   string
		invoke func() *httptest.ResponseRecorder
	}{
		{"read own profile", func() *httptest.ResponseRecorder { return do(router, http.MethodGet, "/me", "") }},
		{"update own profile", func() *httptest.ResponseRecorder {
			return doJSON(router, http.MethodPatch, "/me", `{"displayName":"A"}`, "")
		}},
		{"remove avatar", func() *httptest.ResponseRecorder { return do(router, http.MethodDelete, "/me/avatar", "") }},
		{"upload avatar", func() *httptest.ResponseRecorder { return doAvatar(router, []byte("image-bytes"), "") }},
		{"list addresses", func() *httptest.ResponseRecorder { return do(router, http.MethodGet, "/me/addresses", "") }},
		{"create address", func() *httptest.ResponseRecorder {
			return doJSON(router, http.MethodPost, "/me/addresses", body, "")
		}},
		{"edit address", func() *httptest.ResponseRecorder {
			return doJSON(router, http.MethodPatch, "/me/addresses/"+targetID, `{"streetAddress":"1 Le Loi"}`, "")
		}},
		{"hide address", func() *httptest.ResponseRecorder {
			return do(router, http.MethodDelete, "/me/addresses/"+targetID, "")
		}},
		{"mark default", func() *httptest.ResponseRecorder {
			return do(router, http.MethodPost, "/me/addresses/"+targetID+"/default", "")
		}},
		{"administrator lookup", func() *httptest.ResponseRecorder {
			return do(router, http.MethodGet, "/"+targetID, "")
		}},
		{"provinces", func() *httptest.ResponseRecorder { return do(divisionsRouter(), http.MethodGet, "/provinces", "") }},
		{"wards", func() *httptest.ResponseRecorder {
			return do(divisionsRouter(), http.MethodGet, "/provinces/79/wards", "")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.invoke()
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != "UNAUTHENTICATED" {
				t.Fatalf("expected UNAUTHENTICATED, got %s", got)
			}
		})
	}
	if calls := svc.calls(); len(calls) != 0 {
		t.Fatalf("expected no use case to run without a session, got %v", calls)
	}
}

func TestAdministratorLookupRefusesACustomerAndAuditsTheDenial(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	auditor := &recordingAuditor{}
	router := usersRouter(svc, auditor, openLimits)

	rec := do(router, http.MethodGet, "/"+uuid.New().String(), "token")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if got := errorCode(t, rec); got != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", got)
	}
	if !auditor.has("PRIVILEGE_DENIED") {
		t.Fatal("expected the privilege denial to be audited")
	}
	if svc.wasCalled("LookupCustomer") {
		t.Fatal("a customer must not reach the lookup use case")
	}
}

func TestAdministratorLookupLetsAnAdministratorReadTheCustomer(t *testing.T) {
	svc := newStub(access.RoleAdmin)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)
	target := uuid.New()

	rec := do(router, http.MethodGet, "/"+target.String(), "token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.lookup != target {
		t.Fatalf("expected the path identifier %s, got %s", target, svc.lookup)
	}
}

// chi resolves the static /me segment before the /{userId} parameter, so a
// customer reading their own profile never hits the operator lookup.
func TestSelfServiceRoutesWinOverTheAdministratorLookup(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	if rec := do(router, http.MethodGet, "/me", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /me, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.wasCalled("LookupCustomer") {
		t.Fatal("/me must not be routed to the administrator lookup")
	}
	if svc.profile != svc.subject {
		t.Fatalf("expected the account %s from the session, got %s", svc.subject, svc.profile)
	}
}

func TestNonUUIDIdentifiersAreRejectedWithTheFieldNamed(t *testing.T) {
	// The administrator lookup is role-guarded, so its identifier is only parsed
	// for a caller that already passed the role check; the self-service routes
	// need no role.
	cases := []struct {
		name   string
		invoke func(router http.Handler) *httptest.ResponseRecorder
		field  string
	}{
		{
			name: "administrator lookup",
			invoke: func(router http.Handler) *httptest.ResponseRecorder {
				return do(router, http.MethodGet, "/not-a-uuid", "token")
			},
			field: fieldUserID,
		},
		{
			name: "edit address",
			invoke: func(router http.Handler) *httptest.ResponseRecorder {
				return doJSON(router, http.MethodPatch, "/me/addresses/not-a-uuid", `{"streetAddress":"1 Le Loi"}`, "token")
			},
			field: fieldAddressID,
		},
		{
			name: "hide address",
			invoke: func(router http.Handler) *httptest.ResponseRecorder {
				return do(router, http.MethodDelete, "/me/addresses/42", "token")
			},
			field: fieldAddressID,
		},
		{
			name: "mark default",
			invoke: func(router http.Handler) *httptest.ResponseRecorder {
				return do(router, http.MethodPost, "/me/addresses/42/default", "token")
			},
			field: fieldAddressID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newStub(access.RoleAdmin)
			rec := tc.invoke(usersRouter(svc, &recordingAuditor{}, openLimits))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", got)
			}
			if got := detailField(t, rec); got != tc.field {
				t.Fatalf("expected the detail to name %q, got %q", tc.field, got)
			}
			if calls := svc.calls(); len(calls) != 0 {
				t.Fatalf("a malformed identifier must not reach a use case, got %v", calls)
			}
		})
	}
}

func TestAddressRoutesTakeTheAccountFromTheSession(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)
	otherCustomerAddress := uuid.New()
	body := `{"recipientName":"Nguyen Van A","recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`

	rec := doJSON(router, http.MethodPost, "/me/addresses", body, "token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.create.UserID != svc.subject {
		t.Fatalf("expected the account %s from the session, got %s", svc.subject, svc.create.UserID)
	}

	if rec := doJSON(router, http.MethodPatch, "/me/addresses/"+otherCustomerAddress.String(), `{"streetAddress":"1 Le Loi"}`, "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.edit.UserID != svc.subject || svc.edit.AddressID != otherCustomerAddress {
		t.Fatalf("expected account %s and address %s, got account %s and address %s",
			svc.subject, otherCustomerAddress, svc.edit.UserID, svc.edit.AddressID)
	}

	if rec := do(router, http.MethodDelete, "/me/addresses/"+otherCustomerAddress.String(), "token"); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if svc.ref.UserID != svc.subject || svc.ref.AddressID != otherCustomerAddress {
		t.Fatal("expected the hide to be scoped to the session account")
	}
}

// A body that tries to name the owner is rejected outright: the session is the
// only source of the acting account (FR-006).
func TestAClientSuppliedOwnerIsRejected(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)
	body := `{"recipientName":"Nguyen Van A","recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734","streetAddress":"12 Nguyen Hue","userId":"` + uuid.New().String() + `"}`

	rec := doJSON(router, http.MethodPost, "/me/addresses", body, "token")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "MALFORMED_REQUEST" {
		t.Fatalf("expected MALFORMED_REQUEST for an unknown member, got %s", got)
	}
	if svc.wasCalled("CreateAddress") {
		t.Fatal("a body carrying an owner identifier must not reach the use case")
	}
}

func TestProfileUpdateDistinguishesOmittedAndClearedPhone(t *testing.T) {
	omitted := newStub(access.RoleCustomer)
	router := usersRouter(omitted, &recordingAuditor{}, openLimits)

	if rec := doJSON(router, http.MethodPatch, "/me", `{"displayName":"Nguyen Van A"}`, "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if omitted.update.Phone != nil {
		t.Fatalf("an omitted phone must keep the current value, got %v", *omitted.update.Phone)
	}
	if omitted.update.DisplayName == nil || *omitted.update.DisplayName != "Nguyen Van A" {
		t.Fatal("expected the sent display name to be passed down")
	}

	cleared := newStub(access.RoleCustomer)
	router = usersRouter(cleared, &recordingAuditor{}, openLimits)

	if rec := doJSON(router, http.MethodPatch, "/me", `{"phone":null}`, "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if cleared.update.Phone == nil || *cleared.update.Phone != "" {
		t.Fatal("an explicit null must clear the phone, not keep it")
	}
}

func TestProfileResponseCarriesANullAvatarWhenThereIsNoPhoto(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	rec := do(router, http.MethodGet, "/me", "token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Data httpdto.ProfileResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode profile body %q: %v", rec.Body.String(), err)
	}
	if body.Data.Avatar != nil {
		t.Fatalf("expected a null avatar, got %+v", body.Data.Avatar)
	}
	if body.Data.Role != access.RoleCustomer || body.Data.Email == "" {
		t.Fatalf("unexpected profile payload: %+v", body.Data)
	}
}

func TestAvatarUploadIsRefusedAboveTheCeilingBeforeTheUseCase(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits) // AvatarMaxBytes is 16

	rec := doAvatar(router, bytes.Repeat([]byte("a"), 64), "token")

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "USER_AVATAR_TOO_LARGE" {
		t.Fatalf("expected USER_AVATAR_TOO_LARGE, got %s", got)
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	if svc.wasCalled("SetAvatar") {
		t.Fatal("an oversized upload must not reach the media service")
	}
}

// --- feature 004-fix-pending-defects, US1 (T007) ---------------------------
// The route installs its own body limit above the image ceiling, so a request that
// declares a length over that limit is refused before the handler reads a byte.
// That early refusal has to name the image like the handler's own read ceiling
// does, or the same mistake answers two different codes depending on the client
// (FR-001, FR-002, research D1). This is the test that pins the middleware's
// caller-supplied refusal at the route that uses it.

// usersRouter declares AvatarMaxBytes 16, so the route's own limit is 16 + 64 KB;
// a body larger than that is refused by the route rather than by the handler.
func TestTheAvatarRouteRefusesADeclaredOversizedRequestWithTheAvatarCode(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	rec := doAvatar(router, bytes.Repeat([]byte("a"), 64<<10), "token")

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "USER_AVATAR_TOO_LARGE" {
		t.Fatalf("expected USER_AVATAR_TOO_LARGE, got %s", got)
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	if svc.wasCalled("SetAvatar") {
		t.Fatal("an oversized upload must not reach the media service")
	}
}

// --- end feature 004-fix-pending-defects, US1 -------------------------------

func TestAvatarUploadHandsTheBytesToTheUseCase(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	rec := doAvatar(router, []byte("tiny-image"), "token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if string(svc.avatar.Content) != "tiny-image" {
		t.Fatalf("expected the uploaded bytes, got %q", svc.avatar.Content)
	}
	if svc.avatar.UserID != svc.subject {
		t.Fatal("expected the account from the session")
	}
}

func TestMissingAvatarPartNamesTheField(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	// A well-formed multipart body that carries something other than the file
	// part: the request is missing what it needs, not malformed.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("note", "no image here"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := detailField(t, rec); got != fieldFile {
		t.Fatalf("expected the detail to name %q, got %q", fieldFile, got)
	}
	if svc.wasCalled("SetAvatar") {
		t.Fatal("a body without a file part must not reach the use case")
	}
}

func TestAddressCreateRequiresEveryMandatoryMember(t *testing.T) {
	complete := map[string]string{
		"recipientName":  "Nguyen Van A",
		"recipientPhone": "0912345678",
		"provinceCode":   "79",
		"wardCode":       "26734",
		"streetAddress":  "12 Nguyen Hue",
	}
	for field := range complete {
		t.Run(field, func(t *testing.T) {
			body := map[string]string{}
			for name, value := range complete {
				if name != field {
					body[name] = value
				}
			}
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			svc := newStub(access.RoleCustomer)
			rec := doJSON(usersRouter(svc, &recordingAuditor{}, openLimits), http.MethodPost, "/me/addresses", string(payload), "token")

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", got)
			}
			if got := detailField(t, rec); got != field {
				t.Fatalf("expected the detail to name %q, got %q", field, got)
			}
		})
	}
}

func TestAddressListValidatesThePageWindow(t *testing.T) {
	cases := map[string]string{
		"page=0":            fieldPage,
		"page=abc":          fieldPage,
		"pageSize=0":        fieldPageSize,
		"pageSize=101":      fieldPageSize,
		"pageSize=not-a-no": fieldPageSize,
	}
	for query, field := range cases {
		t.Run(query, func(t *testing.T) {
			svc := newStub(access.RoleCustomer)
			rec := do(usersRouter(svc, &recordingAuditor{}, openLimits), http.MethodGet, "/me/addresses?"+query, "token")

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := detailField(t, rec); got != field {
				t.Fatalf("expected the detail to name %q, got %q", field, got)
			}
			if svc.wasCalled("ListAddresses") {
				t.Fatal("an invalid page window must not reach the use case")
			}
		})
	}
}

func TestAddressListDefaultsAndForwardsTheWindow(t *testing.T) {
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, openLimits)

	if rec := do(router, http.MethodGet, "/me/addresses", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.page.Page != 1 || svc.page.PageSize != defaultPageSize {
		t.Fatalf("expected the default window, got page %d size %d", svc.page.Page, svc.page.PageSize)
	}

	if rec := do(router, http.MethodGet, "/me/addresses?page=2&pageSize=100", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.page.Page != 2 || svc.page.PageSize != maxPageSize {
		t.Fatalf("expected the requested window, got page %d size %d", svc.page.Page, svc.page.PageSize)
	}
	if svc.page.UserID != svc.subject {
		t.Fatal("expected the account from the session")
	}
}

func TestAddressWriteRoutesAreRateLimited(t *testing.T) {
	body := `{"recipientName":"Nguyen Van A","recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`
	limits := config.UserConfig{AvatarUploadRatePerHour: 1000, AddressWriteRatePerMinute: 1}
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, limits)

	if rec := doJSON(router, http.MethodPost, "/me/addresses", body, "token"); rec.Code != http.StatusCreated {
		t.Fatalf("first write: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec := doJSON(router, http.MethodPost, "/me/addresses", body, "token")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second write: expected 429, got %d", rec.Code)
	}
	if got := errorCode(t, rec); got != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", got)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header on the limited response")
	}
}

func TestAvatarUploadIsRateLimitedSeparately(t *testing.T) {
	limits := config.UserConfig{AvatarUploadRatePerHour: 1, AddressWriteRatePerMinute: 1000}
	svc := newStub(access.RoleCustomer)
	router := usersRouter(svc, &recordingAuditor{}, limits)

	if rec := doAvatar(router, []byte("tiny-image"), "token"); rec.Code != http.StatusOK {
		t.Fatalf("first upload: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec := doAvatar(router, []byte("tiny-image"), "token")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload: expected 429, got %d", rec.Code)
	}
	if got := errorCode(t, rec); got != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", got)
	}
	// The limit protects storage and the media service; reading the profile must
	// keep working (SC-013).
	if rec := do(router, http.MethodGet, "/me", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected the profile read to keep working, got %d", rec.Code)
	}
}

func TestDivisionsServeTheCascadingSelect(t *testing.T) {
	router := divisionsRouter()

	rec := do(router, http.MethodGet, "/provinces", "token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var provinces struct {
		Data []httpdto.ProvinceResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provinces); err != nil {
		t.Fatalf("decode provinces: %v", err)
	}
	if len(provinces.Data) != 2 || provinces.Data[0].Code != "79" {
		t.Fatalf("unexpected provinces payload: %+v", provinces.Data)
	}

	rec = do(router, http.MethodGet, "/provinces/79/wards", "token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var wards struct {
		Data []httpdto.WardResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wards); err != nil {
		t.Fatalf("decode wards: %v", err)
	}
	if len(wards.Data) != 1 || wards.Data[0].ProvinceCode != "79" {
		t.Fatalf("a ward list must stay scoped to one province: %+v", wards.Data)
	}
}

// An unknown province on the ward route carries the module's own code, whose
// documented status is 400 in contracts/error-codes.md, so a client can handle
// one code the same way wherever a stale code arrives.
func TestWardsOfAnUnknownProvinceReportTheModuleCode(t *testing.T) {
	rec := do(divisionsRouter(), http.MethodGet, "/provinces/does-not-exist/wards", "token")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "USER_UNKNOWN_PROVINCE" {
		t.Fatalf("expected USER_UNKNOWN_PROVINCE, got %s", got)
	}
	if got := detailField(t, rec); got != fieldProvinceCode {
		t.Fatalf("expected the detail to name %q, got %q", fieldProvinceCode, got)
	}
}
