package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

type stubService struct {
	role       access.Role
	subject    uuid.UUID
	verifyEr   error
	registerEr error

	mu           sync.Mutex
	lastChangePW dto.ChangePasswordInput
}

func newStub(role access.Role) *stubService {
	return &stubService{role: role, subject: uuid.New()}
}

func (s *stubService) Register(context.Context, dto.RegisterInput) error { return s.registerEr }
func (*stubService) VerifyEmail(context.Context, dto.VerifyEmailInput) error {
	return nil
}
func (*stubService) ResendVerification(context.Context, dto.EmailInput) error { return nil }
func (*stubService) Login(context.Context, dto.LoginInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (*stubService) Refresh(context.Context, dto.RefreshInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (*stubService) Logout(context.Context, dto.RefreshInput) error { return nil }
func (*stubService) ForgotPassword(context.Context, dto.EmailInput) error {
	return nil
}
func (*stubService) ResetPassword(context.Context, dto.ResetPasswordInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (s *stubService) ChangePassword(_ context.Context, in dto.ChangePasswordInput) (dto.SessionOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastChangePW = in
	return dto.SessionOutput{}, nil
}
func (s *stubService) VerifyAccessToken(context.Context, string) (appinterface.Claims, error) {
	if s.verifyEr != nil {
		return appinterface.Claims{}, s.verifyEr
	}
	return appinterface.Claims{Subject: s.subject, Role: s.role, ID: "jti", IssuedAt: time.Now()}, nil
}
func (*stubService) Identity(_ context.Context, id uuid.UUID) (dto.IdentityOutput, error) {
	return dto.IdentityOutput{ID: id.String(), Email: "user@example.com", Role: access.RoleCustomer}, nil
}
func (*stubService) ProvisionAdmin(context.Context, dto.ProvisionAdminInput) error { return nil }

var _ appinterface.AuthService = (*stubService)(nil)

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

func testRouter(role access.Role) http.Handler {
	stub := newStub(role)
	return routerFor(stub, &recordingAuditor{}, config.AuthConfig{LoginRatePerMinute: 1000, FlowRatePerMinute: 1000})
}

func routerFor(svc appinterface.AuthService, auditor appinterface.Auditor, cfg config.AuthConfig) http.Handler {
	h := New(svc, auditor, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	return h.Router(cfg)
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

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error.Code
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error.Message
}

func dataMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode success body %q: %v", rec.Body.String(), err)
	}
	return body.Data.Message
}

const registerBody = `{"email":"probe@example.com","password":"Str0ng!Pass"}`

// TestRegisterAnswersServiceUnavailableWhenTheMessageCannotBeDelivered covers
// FR-006: the failure is reported as a service problem, the customer is told in
// the answer itself that no code was sent and that a new one can be requested.
func TestRegisterAnswersServiceUnavailableWhenTheMessageCannotBeDelivered(t *testing.T) {
	stub := newStub(access.RoleCustomer)
	stub.registerEr = domainerr.ErrVerificationDeliveryFailed

	rec := doJSON(routerFor(stub, &recordingAuditor{}, config.AuthConfig{}), http.MethodPost, "/register", registerBody, "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Fatalf("expected %s, got %s", httpx.CodeUnavailable, got)
	}
	message := errorMessage(t, rec)
	if !strings.Contains(message, "could not be sent") {
		t.Fatalf("the answer must say the message was not sent, got %q", message)
	}
	if !strings.Contains(message, "request a new confirmation code") {
		t.Fatalf("the answer must name the next step, got %q", message)
	}
}

// TestTheDeliveryFailureAnswerRevealsNoProviderDetail covers FR-009: even when
// the failure carries a provider sentence, a recipient address and a credential,
// the client sees none of it.
func TestTheDeliveryFailureAnswerRevealsNoProviderDetail(t *testing.T) {
	stub := newStub(access.RoleCustomer)
	stub.registerEr = fmt.Errorf("%w: unauthorized IP address 203.0.113.7, smtp password s3cr3t-value",
		domainerr.ErrVerificationDeliveryFailed)

	rec := doJSON(routerFor(stub, &recordingAuditor{}, config.AuthConfig{}), http.MethodPost, "/register", registerBody, "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{"unauthorized", "203.0.113.7", "s3cr3t", "probe@example.com", "550"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("the answer leaked %q: %s", leak, rec.Body.String())
		}
	}
}

// TestADeliveredMessageKeepsTheGenericAcceptedAnswer is the regression guard for
// FR-014: the endpoint must keep answering the same generic question whatever the
// address is, so registration still cannot be used to discover whether an email
// exists.
func TestADeliveredMessageKeepsTheGenericAcceptedAnswer(t *testing.T) {
	handler := routerFor(newStub(access.RoleCustomer), &recordingAuditor{},
		config.AuthConfig{LoginRatePerMinute: 1000, FlowRatePerMinute: 1000})

	created := doJSON(handler, http.MethodPost, "/register", registerBody, "")
	if created.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", created.Code, created.Body.String())
	}
	duplicate := doJSON(handler, http.MethodPost, "/register", `{"email":"taken@example.com","password":"Str0ng!Pass"}`, "")
	if duplicate.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", duplicate.Code, duplicate.Body.String())
	}
	if dataMessage(t, created) != dataMessage(t, duplicate) {
		t.Fatalf("the answer must not depend on the address: %q vs %q",
			dataMessage(t, created), dataMessage(t, duplicate))
	}
}

// TestTheFlowRateLimitStillAppliesAfterADeliveryFailure covers FR-025: once the
// cooldown is disarmed, the flow-level limit is the only thing left in the way
// and it must still refuse a customer who retries immediately. Without this the
// disarm would be an unbounded sending path.
func TestTheFlowRateLimitStillAppliesAfterADeliveryFailure(t *testing.T) {
	stub := newStub(access.RoleCustomer)
	stub.registerEr = domainerr.ErrVerificationDeliveryFailed
	handler := routerFor(stub, &recordingAuditor{}, config.AuthConfig{FlowRatePerMinute: 1})

	if rec := doJSON(handler, http.MethodPost, "/register", registerBody, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	// The 503 tells the customer to request a new code, so the retry must not be
	// refused by a cooldown - only by the flow limit.
	rec := doJSON(handler, http.MethodPost, "/resend-verification", `{"email":"probe@example.com"}`, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the flow rate limit to refuse the immediate retry, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", got)
	}
}

func TestMeRequiresAuthentication(t *testing.T) {
	if rec := do(testRouter(access.RoleCustomer), http.MethodGet, "/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMeReturnsIdentity(t *testing.T) {
	if rec := do(testRouter(access.RoleCustomer), http.MethodGet, "/me", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMeMapsTokenFailuresToAuthCodes(t *testing.T) {
	cases := map[error]string{
		domainerr.ErrInvalidToken: constant.CodeTokenInvalid,
		domainerr.ErrExpiredToken: constant.CodeTokenExpired,
	}
	for cause, want := range cases {
		stub := newStub(access.RoleCustomer)
		stub.verifyEr = cause
		rec := do(routerFor(stub, &recordingAuditor{}, config.AuthConfig{}), http.MethodGet, "/me", "token")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for %v, got %d", cause, rec.Code)
		}
		if got := errorCode(t, rec); got != want {
			t.Fatalf("expected %s for %v, got %s", want, cause, got)
		}
	}
}

func TestAdminProbeDeniesCustomerAndAudits(t *testing.T) {
	auditor := &recordingAuditor{}
	rec := do(routerFor(newStub(access.RoleCustomer), auditor, config.AuthConfig{}), http.MethodGet, "/admin/probe", "token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if got := errorCode(t, rec); got != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", got)
	}
	if !auditor.has(constant.AuditPrivilegeDenied) {
		t.Fatal("expected the privilege denial to be audited")
	}
}

func TestAdminProbeAllowsAdmin(t *testing.T) {
	if rec := do(testRouter(access.RoleAdmin), http.MethodGet, "/admin/probe", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestChangePasswordRequiresAuthentication(t *testing.T) {
	body := `{"currentPassword":"a","newPassword":"b","refreshToken":"c"}`
	if rec := doJSON(testRouter(access.RoleCustomer), http.MethodPost, "/password/change", body, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestChangePasswordRequiresRefreshToken(t *testing.T) {
	body := `{"currentPassword":"Str0ng!Pass","newPassword":"Even5tronger!"}`
	rec := doJSON(testRouter(access.RoleCustomer), http.MethodPost, "/password/change", body, "token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %s", got)
	}
}

func TestChangePasswordTakesTheAccountFromTheSession(t *testing.T) {
	stub := newStub(access.RoleCustomer)
	body := `{"currentPassword":"Str0ng!Pass","newPassword":"Even5tronger!","refreshToken":"refresh-1"}`
	if rec := doJSON(routerFor(stub, &recordingAuditor{}, config.AuthConfig{}), http.MethodPost, "/password/change", body, "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.lastChangePW.AccountID != stub.subject {
		t.Fatalf("expected the account id from the session (%s), got %s", stub.subject, stub.lastChangePW.AccountID)
	}
}

func TestLoginRateLimitReturns429(t *testing.T) {
	handler := routerFor(newStub(access.RoleCustomer), &recordingAuditor{}, config.AuthConfig{LoginRatePerMinute: 1, FlowRatePerMinute: 1000})
	body := `{"email":"user@example.com","password":"Str0ng!Pass"}`
	if rec := doJSON(handler, http.MethodPost, "/login", body, ""); rec.Code != http.StatusOK {
		t.Fatalf("first login: expected 200, got %d", rec.Code)
	}
	rec := doJSON(handler, http.MethodPost, "/login", body, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second login: expected 429, got %d", rec.Code)
	}
	if got := errorCode(t, rec); got != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED, got %s", got)
	}
}

func TestMapErrorCodes(t *testing.T) {
	cases := map[error]string{
		domainerr.ErrWeakPassword:       constant.CodeWeakPassword,
		domainerr.ErrInvalidCredentials: constant.CodeInvalidCredentials,
		domainerr.ErrAccountPending:     constant.CodeAccountPending,
		domainerr.ErrAccountDisabled:    constant.CodeAccountDisabled,
		domainerr.ErrOTPInvalid:         constant.CodeOTPInvalid,
		domainerr.ErrAccountLocked:      constant.CodeLoginLocked,
		domainerr.ErrExpiredToken:       constant.CodeTokenExpired,
		domainerr.ErrInvalidToken:       constant.CodeTokenInvalid,
		domainerr.ErrRefreshReused:      constant.CodeRefreshReused,
		domainerr.ErrResetInvalid:       constant.CodeResetInvalid,
	}
	for err, want := range cases {
		if got := mapError(err); string(got.Code) != want {
			t.Errorf("mapError(%v): expected %s, got %s", err, want, got.Code)
		}
	}
	if got := mapError(errors.New("boom")); string(got.Code) != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", got.Code)
	}
}

func TestBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer abc.def")
	if got := bearerToken(req); got != "abc.def" {
		t.Fatalf("unexpected token %q", got)
	}
	req.Header.Set("Authorization", "Basic x")
	if got := bearerToken(req); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}
