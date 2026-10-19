//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/email"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	authredis "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/redis"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

type noopAuditor struct{}

func (noopAuditor) Record(context.Context, string, string, *uuid.UUID, string, string, string, map[string]any) {
}

var _ appinterface.Auditor = noopAuditor{}

func integrationRouter(t *testing.T) (http.Handler, *email.FakeSender) {
	t.Helper()
	handler, sender, _ := integrationRouterWithPool(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	return handler, sender
}

// integrationRouterWith builds the router with an explicit configuration, so a
// test that is about a limit can set one.
func integrationRouterWith(t *testing.T, cfg config.AuthConfig) (http.Handler, *email.FakeSender) {
	t.Helper()
	handler, sender, _ := integrationRouterWithPool(t, cfg)
	return handler, sender
}

// integrationRouterWithPool is integrationRouterWith and also hands back the
// pool, so a test can assert on the durable rows directly.
func integrationRouterWithPool(t *testing.T, cfg config.AuthConfig) (http.Handler, *email.FakeSender, *pgxpool.Pool) {
	t.Helper()
	handler, sender, _, pool := integrationRouterFull(t, cfg)
	return handler, sender, pool
}

// integrationRouterFull builds the full stack and also hands back the Redis
// server and the pool, so a test can fast-forward time and inspect durable rows.
func integrationRouterFull(t *testing.T, cfg config.AuthConfig) (http.Handler, *email.FakeSender, *miniredis.Miniredis, *pgxpool.Pool) {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	redisCache, err := cache.New(ctx, config.RedisConfig{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	sender := &email.FakeSender{}
	svc := implement.New(implement.Service{
		Users:         postgres.NewUserRepository(pool),
		Sessions:      postgres.NewSessionRepository(pool),
		Resets:        postgres.NewResetRepository(pool),
		Registration:  authredis.NewRegistrationStore(redisCache, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute),
		ResetOTP:      authredis.NewPasswordResetOTPStore(redisCache, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute),
		Blacklist:     authredis.NewBlacklistStore(redisCache),
		Guard:         authredis.NewLoginGuard(redisCache, 10, 15*time.Minute, 15*time.Minute),
		Hasher:        token.Hasher{},
		Access:        token.NewAccessIssuer("0123456789abcdef0123456789abcdef", 15*time.Minute),
		RefreshTokens: token.Generator{},
		Email:         sender,
		Audit:         noopAuditor{},
		Tx:            &database.DB{Pool: pool},
		Config: implement.Config{
			RefreshTokenTTL:        45 * 24 * time.Hour,
			PasswordResetTTL:       30 * time.Minute,
			PendingRegistrationTTL: 30 * time.Minute,
			OTPResendCooldown:      time.Minute,
			OTPTTL:                 5 * time.Minute,
		},
	})
	h := New(svc, noopAuditor{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	return h.Router(cfg), sender, server, pool
}

func post(handler http.Handler, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

var otpCodePattern = regexp.MustCompile(`data-code="(\d{6})"`)

func lastFieldText(body string) string {
	if match := otpCodePattern.FindStringSubmatch(body); len(match) == 2 {
		return match[1]
	}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

type tokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func login(t *testing.T, handler http.Handler, email, password string) tokenPair {
	t.Helper()
	rec := post(handler, "/login", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data tokenPair `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return body.Data
}

// registrationStarted is the data of a 202 from /register.
type registrationStarted struct {
	RegistrationID    string `json:"registrationId"`
	Status            string `json:"status"`
	OTPExpiresIn      int    `json:"otpExpiresIn"`
	ResendAvailableIn int    `json:"resendAvailableIn"`
}

// startRegistration posts the payload and returns the reference and the code
// that was emailed, so a test can drive the confirmation.
func startRegistration(t *testing.T, handler http.Handler, sender *email.FakeSender, email, password string) (registrationStarted, string) {
	t.Helper()
	rec := post(handler, "/register", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("register: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data registrationStarted `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("expected an OTP email")
	}
	return body.Data, lastFieldText(msg.Body)
}

func registerAndVerify(t *testing.T, handler http.Handler, sender *email.FakeSender, email, password string) {
	t.Helper()
	started, otp := startRegistration(t, handler, sender, email, password)
	rec := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+otp+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthHTTPFlow(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "flow@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	pair := login(t, handler, email, password)

	meReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me: expected 200, got %d", meRec.Code)
	}

	if rec := post(handler, "/refresh", `{"refreshToken":"`+pair.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", rec.Code)
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+pair.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh reuse: expected 401, got %d", rec.Code)
	}
}

func TestMultiDeviceSignOutIsolation(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "multi@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	if rec := post(handler, "/logout", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", rec.Code)
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("other device should still work, got %d", rec.Code)
	}
}

func TestRefreshReplayRevokesEveryDevice(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "replay@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("first refresh: expected 200, got %d", rec.Code)
	}
	rec := post(handler, "/refresh", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay: expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "AUTH_REFRESH_REUSED") {
		t.Fatalf("expected AUTH_REFRESH_REUSED, got %s", rec.Body.String())
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay must revoke the other device too, got %d", rec.Code)
	}
}

func TestChangePasswordRevokesEverySession(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "change@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	body := `{"currentPassword":"` + password + `","newPassword":"Even5tronger!","refreshToken":"` + deviceA.RefreshToken + `"}`
	rec := post(handler, "/password/change", body, deviceA.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected every session to be revoked, got %d", rec.Code)
	}
}

// TestAFailedDeliveryLeavesTheCustomerAWorkingRecoveryPath exercises the story
// against real storage: the refusal is answered as a service problem naming the
// next step, the temporary registration is kept with its cooldown disarmed
// (FR-024), and starting again delivers a fresh code that completes the flow
// (FR-007, FR-026).
func TestAFailedDeliveryLeavesTheCustomerAWorkingRecoveryPath(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "providerdown@example.com"
	const password = "Str0ng!Pass"

	sender.FailNext(1, providerRefuses())

	rec := post(handler, "/register", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register: expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SERVICE_UNAVAILABLE") {
		t.Fatalf("expected the shared service-unavailable code, got %s", rec.Body.String())
	}
	if _, ok := sender.Last(); ok {
		t.Fatal("no message left the system, so none may be recorded as delivered")
	}

	started, otp := startRegistration(t, handler, sender, email, password)
	if rec := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+otp+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// providerRefuses is a refusal a retry cannot change, so exactly one send is
// spent on it (FR-016).
func providerRefuses() error {
	return fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration)
}

func registrationBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `"}`
}

// countUsers reads how many durable accounts carry the email, so a test can
// assert no duplicate was created.
func countUsers(t *testing.T, pool *pgxpool.Pool, email string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

// TestStartRegistrationReportsATakenEmail covers the deliberate early conflict
// (D7): an address that already has an account is answered 409 AUTH_EMAIL_TAKEN
// and no temporary registration is created for it.
func TestStartRegistrationReportsATakenEmail(t *testing.T) {
	handler, sender := integrationRouter(t)
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, "taken@example.com", password)

	rec := post(handler, "/register", registrationBody("TAKEN@example.com", password), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a taken email, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_EMAIL_TAKEN" {
		t.Fatalf("expected AUTH_EMAIL_TAKEN, got %s", got)
	}
}

// TestStartRegistrationReportsATakenUsername covers the other early conflict
// (FR-023): a handle claimed by an existing account is refused at start.
func TestStartRegistrationReportsATakenUsername(t *testing.T) {
	handler, sender := integrationRouter(t)
	const password = "Str0ng!Pass"

	rec := post(handler, "/register", `{"email":"first@example.com","username":"artist","password":"`+password+`"}`, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first register: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data registrationStarted `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	msg, _ := sender.Last()
	if rec := post(handler, "/register/verify-otp", `{"registrationId":"`+body.Data.RegistrationID+`","otp":"`+lastFieldText(msg.Body)+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = post(handler, "/register", `{"email":"second@example.com","username":"ARTIST","password":"`+password+`"}`, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a taken username, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_USERNAME_TAKEN" {
		t.Fatalf("expected AUTH_USERNAME_TAKEN, got %s", got)
	}
}

// TestTheFlowLimitStillBoundsARegistrationFlood covers FR-018: the flow-level
// limit stops a registration flood whatever addresses it names.
func TestTheFlowLimitStillBoundsARegistrationFlood(t *testing.T) {
	handler, _ := integrationRouterWith(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 2})
	const password = "Str0ng!Pass"

	if rec := post(handler, "/register", registrationBody("flood-a@example.com", password), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("first register: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := post(handler, "/register", registrationBody("flood-b@example.com", password), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("second register: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec := post(handler, "/register", registrationBody("flood-c@example.com", password), "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the flow rate limit to refuse the flood, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeRateLimited) {
		t.Fatalf("expected %s, got %s", httpx.CodeRateLimited, got)
	}
}

// TestVerifyRegistrationIsIdempotentAndSignsIn is the MVP end-to-end proof:
// start -> verify -> the account is active and can sign in; replaying the same
// confirmation creates no second account (FR-009, FR-011).
func TestVerifyRegistrationIsIdempotentAndSignsIn(t *testing.T) {
	handler, sender, pool := integrationRouterWithPool(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "idempotent@example.com"
	const password = "Str0ng!Pass"

	started, otp := startRegistration(t, handler, sender, email, password)
	if rec := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+otp+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if n := countUsers(t, pool, email); n != 1 {
		t.Fatalf("expected exactly one account after verification, got %d", n)
	}
	if pair := login(t, handler, email, password); pair.AccessToken == "" {
		t.Fatal("a verified account must be able to sign in")
	}

	// Replaying the confirmation must not create a second account.
	post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+otp+`"}`, "")
	if n := countUsers(t, pool, email); n != 1 {
		t.Fatalf("replaying a confirmation must not create a duplicate, got %d accounts", n)
	}
}

// TestResendCooldownThenSuccessInvalidatesTheOldCode covers US3 against real
// storage: a resend within the cooldown is refused, and after it a new code is
// issued that invalidates the previous one (FR-014, FR-015).
func TestResendCooldownThenSuccessInvalidatesTheOldCode(t *testing.T) {
	handler, sender, server, _ := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "resend-e2e@example.com"
	const password = "Str0ng!Pass"

	started, oldCode := startRegistration(t, handler, sender, email, password)

	rec := post(handler, "/register/resend-otp", `{"registrationId":"`+started.RegistrationID+`"}`, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("resend within cooldown: expected 429, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_RESEND_COOLDOWN" {
		t.Fatalf("expected AUTH_RESEND_COOLDOWN, got %s", got)
	}

	// The cooldown elapses.
	server.FastForward(2 * time.Minute)

	if rec := post(handler, "/register/resend-otp", `{"registrationId":"`+started.RegistrationID+`"}`, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("resend after cooldown: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("expected a fresh OTP email")
	}
	newCode := lastFieldText(msg.Body)
	if newCode == oldCode {
		t.Fatal("expected a different code after a resend")
	}

	// The old code no longer verifies; the new one does.
	if rec := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+oldCode+`"}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("the old code must be rejected, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+newCode+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("the new code must verify, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestConcurrentVerificationCreatesExactlyOneAccount covers US5 / SC-007: two
// simultaneous confirmations of one reference produce exactly one durable row.
func TestConcurrentVerificationCreatesExactlyOneAccount(t *testing.T) {
	handler, sender, _, pool := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "race-e2e@example.com"
	const password = "Str0ng!Pass"

	started, otp := startRegistration(t, handler, sender, email, password)
	body := `{"registrationId":"` + started.RegistrationID + `","otp":"` + otp + `"}`

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < len(codes); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = post(handler, "/register/verify-otp", body, "").Code
		}(i)
	}
	wg.Wait()

	if n := countUsers(t, pool, email); n != 1 {
		t.Fatalf("concurrent confirmations must create exactly one account, got %d", n)
	}
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("confirmation %d returned %d, want 200", i, code)
		}
	}
}

// TestCancelMakesVerifyAndResendGeneric covers US4 against real storage:
// cancelling removes the temporary registration, after which verify and resend
// answer the same generic not-found result (FR-016, FR-017).
func TestCancelMakesVerifyAndResendGeneric(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "cancel-e2e@example.com"
	const password = "Str0ng!Pass"

	started, otp := startRegistration(t, handler, sender, email, password)

	if rec := post(handler, "/register/cancel", `{"registrationId":"`+started.RegistrationID+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	verify := post(handler, "/register/verify-otp", `{"registrationId":"`+started.RegistrationID+`","otp":"`+otp+`"}`, "")
	if verify.Code != http.StatusBadRequest {
		t.Fatalf("verify after cancel: expected 400, got %d", verify.Code)
	}
	if got := errorCode(t, verify); got != "AUTH_REGISTRATION_NOT_FOUND" {
		t.Fatalf("expected AUTH_REGISTRATION_NOT_FOUND, got %s", got)
	}

	resend := post(handler, "/register/resend-otp", `{"registrationId":"`+started.RegistrationID+`"}`, "")
	if resend.Code != http.StatusBadRequest {
		t.Fatalf("resend after cancel: expected 400, got %d", resend.Code)
	}
	if got := errorCode(t, resend); got != "AUTH_REGISTRATION_NOT_FOUND" {
		t.Fatalf("expected AUTH_REGISTRATION_NOT_FOUND, got %s", got)
	}
}

func forgotBody(email string) string {
	return `{"email":"` + email + `"}`
}

// isSixDigitCode reports whether s is exactly six decimal digits.
func isSixDigitCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// TestForgotPasswordEmailsACodeOnlyForRegisteredAddresses covers US1 against
// real storage: a registered address gets the generic 202 and a code by email;
// an unregistered address gets the same 202 and no email at all (FR-002, FR-003).
func TestForgotPasswordEmailsACodeOnlyForRegisteredAddresses(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "forgot-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)
	before := sender.Count()

	rec := post(handler, "/password/forgot", forgotBody(email), "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("registered: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if sender.Count() != before+1 {
		t.Fatalf("expected exactly one code email, got %d new messages", sender.Count()-before)
	}
	msg, _ := sender.Last()
	if code := lastFieldText(msg.Body); !isSixDigitCode(code) {
		t.Fatalf("expected a six-digit code by email, got %q (body %q)", code, msg.Body)
	}

	before = sender.Count()
	rec = post(handler, "/password/forgot", forgotBody("nobody-e2e@example.com"), "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unregistered: expected the same 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if sender.Count() != before {
		t.Fatalf("no code may be sent to an unregistered address, got %d", sender.Count()-before)
	}
}

// TestVerifyResetOTPExchangesTheCodeForAToken covers US2 against real storage:
// a correct code returns a single-use reset token with its lifetime, and the
// code cannot be used a second time (FR-005, FR-006).
func TestVerifyResetOTPExchangesTheCodeForAToken(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "verify-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)

	if rec := post(handler, "/password/forgot", forgotBody(email), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, _ := sender.Last()
	code := lastFieldText(msg.Body)

	rec := post(handler, "/password/forgot/verify-otp", `{"email":"`+email+`","otp":"`+code+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ResetToken string `json:"resetToken"`
			ExpiresIn  int    `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode verify-otp: %v", err)
	}
	if body.Data.ResetToken == "" {
		t.Fatal("expected a reset token")
	}
	if want := int((30 * time.Minute).Seconds()); body.Data.ExpiresIn != want {
		t.Fatalf("expected expiresIn %d, got %d", want, body.Data.ExpiresIn)
	}

	replay := post(handler, "/password/forgot/verify-otp", `{"email":"`+email+`","otp":"`+code+`"}`, "")
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("a used code must be rejected, got %d (%s)", replay.Code, replay.Body.String())
	}
	if got := errorCode(t, replay); got != "AUTH_OTP_EXPIRED" {
		t.Fatalf("expected AUTH_OTP_EXPIRED, got %s", got)
	}
}

// TestPasswordResetFlowEndsAtSignIn covers US3 end to end against real storage:
// request -> verify -> reset returns success with no session; the new password
// signs in, the old one does not, and the token cannot be reused (FR-012,
// FR-013, FR-014).
func TestPasswordResetFlowEndsAtSignIn(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "reset-e2e@example.com"
	const password = "Str0ng!Pass"
	const newPassword = "Even5tronger!"
	registerAndVerify(t, handler, sender, email, password)

	if rec := post(handler, "/password/forgot", forgotBody(email), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, _ := sender.Last()
	code := lastFieldText(msg.Body)

	rec := post(handler, "/password/forgot/verify-otp", `{"email":"`+email+`","otp":"`+code+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var verified struct {
		Data struct {
			ResetToken string `json:"resetToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &verified); err != nil {
		t.Fatalf("decode verify-otp: %v", err)
	}

	rec = post(handler, "/password/reset", `{"token":"`+verified.Data.ResetToken+`","newPassword":"`+newPassword+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "accessToken") || strings.Contains(rec.Body.String(), "refreshToken") {
		t.Fatalf("a successful reset must not issue a session: %s", rec.Body.String())
	}

	if rec := post(handler, "/login", `{"email":"`+email+`","password":"`+password+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the old password must fail, got %d (%s)", rec.Code, rec.Body.String())
	}
	if pair := login(t, handler, email, newPassword); pair.AccessToken == "" {
		t.Fatal("the new password must sign in")
	}

	rec = post(handler, "/password/reset", `{"token":"`+verified.Data.ResetToken+`","newPassword":"Another!Pass9"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("token reuse must be refused, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_RESET_INVALID" {
		t.Fatalf("expected AUTH_RESET_INVALID, got %s", got)
	}
}

// requestResetCode requests a reset code for the address and returns the code
// that was emailed, so a test can drive verification.
func requestResetCode(t *testing.T, handler http.Handler, sender *email.FakeSender, email string) string {
	t.Helper()
	if rec := post(handler, "/password/forgot", forgotBody(email), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("expected a reset code email")
	}
	return lastFieldText(msg.Body)
}

// requestAndVerifyReset drives request -> verify and returns the reset token.
func requestAndVerifyReset(t *testing.T, handler http.Handler, sender *email.FakeSender, email string) string {
	t.Helper()
	code := requestResetCode(t, handler, sender, email)
	rec := post(handler, "/password/forgot/verify-otp", `{"email":"`+email+`","otp":"`+code+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ResetToken string `json:"resetToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode verify-otp: %v", err)
	}
	return body.Data.ResetToken
}

// TestResetRequestCooldownGatesRepeats covers FR-015 against real storage: a
// second request within the cooldown is refused, and once the window passes a
// fresh code may be requested.
func TestResetRequestCooldownGatesRepeats(t *testing.T) {
	handler, sender, server, _ := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "reset-cooldown-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)

	_ = requestResetCode(t, handler, sender, email)

	rec := post(handler, "/password/forgot", forgotBody(email), "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request within cooldown: expected 429, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_RESEND_COOLDOWN" {
		t.Fatalf("expected AUTH_RESEND_COOLDOWN, got %s", got)
	}

	// The cooldown elapses.
	server.FastForward(2 * time.Minute)
	if rec := post(handler, "/password/forgot", forgotBody(email), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("after the cooldown: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestTheFlowLimitBoundsResetRequests covers FR-016 against real storage: the
// flow-level limit stops a reset-request flood from one source.
func TestTheFlowLimitBoundsResetRequests(t *testing.T) {
	handler, _ := integrationRouterWith(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 2})
	// Unknown addresses are answered generically (202) and send nothing.
	if rec := post(handler, "/password/forgot", forgotBody("rl-a@example.com"), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := post(handler, "/password/forgot", forgotBody("rl-b@example.com"), ""); rec.Code != http.StatusAccepted {
		t.Fatalf("second request: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec := post(handler, "/password/forgot", forgotBody("rl-c@example.com"), "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the flow rate limit to refuse the flood, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeRateLimited) {
		t.Fatalf("expected %s, got %s", httpx.CodeRateLimited, got)
	}
}

// TestTheFlowLimitBoundsVerifyResetOTP covers FR-016 on the verification step,
// which lives in the same rate-limited flow group.
func TestTheFlowLimitBoundsVerifyResetOTP(t *testing.T) {
	handler, _ := integrationRouterWith(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 1})
	body := `{"email":"nobody-e2e@example.com","otp":"123456"}`
	// The first call passes the limiter and is refused by the flow logic.
	if rec := post(handler, "/password/forgot/verify-otp", body, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("first verify-otp: expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec := post(handler, "/password/forgot/verify-otp", body, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the flow rate limit on verify-otp, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeRateLimited) {
		t.Fatalf("expected %s, got %s", httpx.CodeRateLimited, got)
	}
}

// TestANewerResetSupersedesAnEarlierToken covers the superseding edge case
// against real storage: a second verified code invalidates the first reset
// token, which is refused at reset with AUTH_RESET_INVALID (D11, FR-014).
func TestANewerResetSupersedesAnEarlierToken(t *testing.T) {
	handler, sender, server, _ := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "reset-supersede-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)

	earlier := requestAndVerifyReset(t, handler, sender, email)
	// The request cooldown elapses so a second code can be issued.
	server.FastForward(2 * time.Minute)
	newer := requestAndVerifyReset(t, handler, sender, email)
	if earlier == newer {
		t.Fatal("expected two distinct reset tokens")
	}

	// The newer token completes the reset.
	if rec := post(handler, "/password/reset", `{"token":"`+newer+`","newPassword":"Even5tronger!"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("newer token: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	// The earlier, superseded token is refused and changes nothing.
	rec := post(handler, "/password/reset", `{"token":"`+earlier+`","newPassword":"Another!Pass9"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("superseded token: expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "AUTH_RESET_INVALID" {
		t.Fatalf("expected AUTH_RESET_INVALID, got %s", got)
	}
}

// resetBody builds the /password/reset payload.
func resetBody(token, newPassword string) string {
	return `{"token":"` + token + `","newPassword":"` + newPassword + `"}`
}

// resetTokenFromBody decodes the reset token a 200 from verify-otp returned.
func resetTokenFromBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data struct {
			ResetToken string `json:"resetToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode verify-otp: %v", err)
	}
	return body.Data.ResetToken
}

// countOpenResetTokens counts the account's still-usable reset tokens, so a test
// can assert the "at most one open token per account" invariant.
func countOpenResetTokens(t *testing.T, pool *pgxpool.Pool, email string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM password_reset_requests r JOIN users u ON u.id = r.user_id WHERE u.email = $1 AND r.used_at IS NULL`,
		email).Scan(&n)
	if err != nil {
		t.Fatalf("count open reset tokens: %v", err)
	}
	return n
}

// TestConcurrentResetVerificationsLeaveExactlyOneUsableToken covers the spec's
// racing edge case against real storage: two simultaneous verifications of the
// same code for the same account may both pass the code check, but the account
// row lock serializes their writes, so exactly one usable reset token survives -
// the later verification supersedes the earlier (research D11, FR-006, FR-014).
func TestConcurrentResetVerificationsLeaveExactlyOneUsableToken(t *testing.T) {
	handler, sender, _, pool := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "reset-verify-race-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)
	code := requestResetCode(t, handler, sender, email)
	body := `{"email":"` + email + `","otp":"` + code + `"}`

	codes := make([]int, 2)
	tokens := make([]string, 2)
	var wg sync.WaitGroup
	for i := 0; i < len(codes); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := post(handler, "/password/forgot/verify-otp", body, "")
			codes[i] = rec.Code
			if rec.Code == http.StatusOK {
				tokens[i] = resetTokenFromBody(t, rec)
			}
		}(i)
	}
	wg.Wait()

	if n := countOpenResetTokens(t, pool, email); n != 1 {
		t.Fatalf("concurrent verifications must leave exactly one usable reset token, got %d", n)
	}

	// At least one verification must have succeeded, and among the tokens it
	// returned exactly one completes a reset; any other was superseded by the
	// newer verification and is refused.
	issued := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if tok != "" {
			issued = append(issued, tok)
		}
	}
	if len(issued) == 0 {
		t.Fatalf("expected at least one verification to succeed, got statuses %v", codes)
	}
	passwords := []string{"ResetRace!Pass1", "ResetRace!Pass2"}
	successes, refused := 0, 0
	for i, tok := range issued {
		rec := post(handler, "/password/reset", resetBody(tok, passwords[i]), "")
		switch {
		case rec.Code == http.StatusOK:
			successes++
		case rec.Code == http.StatusBadRequest && errorCode(t, rec) == "AUTH_RESET_INVALID":
			refused++
		default:
			t.Fatalf("a superseded token must be refused with AUTH_RESET_INVALID, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
	if successes != 1 || refused != len(issued)-1 {
		t.Fatalf("exactly one issued token must be usable, got %d success / %d refused", successes, refused)
	}
}

// TestConcurrentResetsOnTheSameTokenLeaveExactlyOneSuccess covers the racing edge
// case on the consuming side against real storage: two simultaneous resets on
// one token cannot both succeed, because Consume is conditional and the loser's
// transaction rolls back. One response is 200 and the other 400
// AUTH_RESET_INVALID, and only the winner's password is applied (research D11,
// FR-012, FR-014).
func TestConcurrentResetsOnTheSameTokenLeaveExactlyOneSuccess(t *testing.T) {
	handler, sender, _, _ := integrationRouterFull(t, config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100})
	const email = "reset-race-e2e@example.com"
	const password = "Str0ng!Pass"
	registerAndVerify(t, handler, sender, email, password)
	token := requestAndVerifyReset(t, handler, sender, email)

	newPasswords := []string{"Concurrent!Pass1", "Concurrent!Pass2"}
	recs := make([]*httptest.ResponseRecorder, len(newPasswords))
	var wg sync.WaitGroup
	for i := 0; i < len(newPasswords); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i] = post(handler, "/password/reset", resetBody(token, newPasswords[i]), "")
		}(i)
	}
	wg.Wait()

	successes, refused := 0, 0
	for _, rec := range recs {
		switch {
		case rec.Code == http.StatusOK:
			successes++
		case rec.Code == http.StatusBadRequest && errorCode(t, rec) == "AUTH_RESET_INVALID":
			refused++
		default:
			t.Fatalf("unexpected status from a concurrent reset: %d (%s)", rec.Code, rec.Body.String())
		}
	}
	if successes != 1 || refused != 1 {
		t.Fatalf("expected exactly one success and one refusal, got %d success / %d refused", successes, refused)
	}

	// The winner's password is the only one applied: the loser's transaction was
	// rolled back, so the old password fails and exactly one new password signs in.
	if rec := post(handler, "/login", `{"email":"`+email+`","password":"`+password+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the old password must fail after a reset, got %d (%s)", rec.Code, rec.Body.String())
	}
	signedIn := 0
	for _, pw := range newPasswords {
		if rec := post(handler, "/login", `{"email":"`+email+`","password":"`+pw+`"}`, ""); rec.Code == http.StatusOK {
			signedIn++
		}
	}
	if signedIn != 1 {
		t.Fatalf("exactly one new password must sign in, got %d", signedIn)
	}
}
