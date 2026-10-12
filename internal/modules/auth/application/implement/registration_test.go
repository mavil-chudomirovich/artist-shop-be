package implement

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// registerPassword is a password that satisfies the policy, shared by the
// registration and start tests.
const registerPassword = "Str0ng!Pass"

// wrongCode returns a 6-digit code guaranteed to differ from actual, so a test
// submits a code the store must reject.
func wrongCode(actual string) string {
	if actual == "000000" {
		return "000001"
	}
	return "000000"
}

// mustStart starts a registration whose message is delivered, so the caller can
// read the code from the fake sender.
func mustStart(t *testing.T, h *harness, email string) appdto.RegistrationStartedOutput {
	t.Helper()
	out, err := h.svc.StartRegistration(context.Background(), appdto.StartRegistrationInput{Email: email, Password: registerPassword})
	if err != nil {
		t.Fatalf("StartRegistration: %v", err)
	}
	return out
}

// TestStartRegistrationStoresTempSendsCodeAndCreatesNoAccount is the core US1
// guarantee: a sign-up holds a temporary registration, emails a code and creates
// no durable account (FR-004).
func TestStartRegistrationStoresTempSendsCodeAndCreatesNoAccount(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	out, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "  New@Example.com  ", Password: registerPassword})
	if err != nil {
		t.Fatalf("StartRegistration: %v", err)
	}
	if out.RegistrationID == "" {
		t.Fatal("expected a registration reference")
	}
	if out.Status != registrationStatusPendingVerification {
		t.Fatalf("status = %q, want %q", out.Status, registrationStatusPendingVerification)
	}
	if out.OTPExpiresIn != int((5 * time.Minute).Seconds()) {
		t.Fatalf("otpExpiresIn = %d", out.OTPExpiresIn)
	}
	if out.ResendAvailableIn != int((60 * time.Second).Seconds()) {
		t.Fatalf("resendAvailableIn = %d", out.ResendAvailableIn)
	}
	if len(h.users.byEmail) != 0 {
		t.Fatal("starting a registration must create no account")
	}
	if h.email.count() != 1 {
		t.Fatalf("expected one confirmation email, got %d", h.email.count())
	}
	reg, err := h.registration.ByID(ctx, out.RegistrationID)
	if err != nil {
		t.Fatalf("expected a stored registration: %v", err)
	}
	if reg.Email != "new@example.com" {
		t.Fatalf("email not normalized: %q", reg.Email)
	}
	if reg.PasswordHash != "h:"+registerPassword {
		t.Fatal("the stored registration must carry the hashed password, never plaintext")
	}
	if !h.audit.has(constant.AuditRegistrationStarted) {
		t.Fatal("expected the successful start to be audited")
	}
}

// TestStartRegistrationRejectsAMalformedEmail covers FR-001: the email format is
// validated before any state is written, so a malformed address is rejected and
// leaves no temporary registration and no sent code.
func TestStartRegistrationRejectsAMalformedEmail(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	malformed := []string{"", "not-an-email", "user@", "@example.com", "user@example", "user example@example.com"}
	for _, email := range malformed {
		_, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: email, Password: registerPassword})
		if !errors.Is(err, domainerr.ErrInvalidEmail) {
			t.Fatalf("email %q: expected ErrInvalidEmail, got %v", email, err)
		}
	}
	if h.registration.count() != 0 {
		t.Fatal("a malformed email must leave no temporary registration")
	}
	if h.email.count() != 0 {
		t.Fatal("a malformed email must not send a code")
	}
	if h.audit.has(constant.AuditRegistrationStarted) {
		t.Fatal("a malformed email must not be audited as a successful start")
	}
}

// TestStartRegistrationAcceptsAWellFormedEmail is the positive counterpart of
// the malformed-email rejection: a valid address still starts the flow and is
// normalized as before (FR-001).
func TestStartRegistrationAcceptsAWellFormedEmail(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	out, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "  Valid.User+tag@Example.com  ", Password: registerPassword})
	if err != nil {
		t.Fatalf("StartRegistration: %v", err)
	}
	if out.RegistrationID == "" {
		t.Fatal("expected a registration reference")
	}
	reg, err := h.registration.ByID(ctx, out.RegistrationID)
	if err != nil {
		t.Fatalf("expected a stored registration: %v", err)
	}
	if reg.Email != "valid.user+tag@example.com" {
		t.Fatalf("email not normalized: %q", reg.Email)
	}
}

// TestStartRegistrationRejectsDuplicateEmail pins the early conflict the guided
// form relies on (FR-003, D7).
func TestStartRegistrationRejectsDuplicateEmail(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	if err := h.users.Create(ctx, &model.Account{ID: uuid.New(), Email: "taken@example.com", PasswordHash: "h", Role: access.RoleCustomer, Status: constant.StatusActive}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	_, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "TAKEN@example.com", Password: registerPassword})
	if !errors.Is(err, domainerr.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
	if h.registration.count() != 0 {
		t.Fatal("a taken email must leave no temporary registration")
	}
	if h.email.count() != 0 {
		t.Fatal("a taken email must not send a code")
	}
}

// TestStartRegistrationRejectsDuplicateUsername covers the other early conflict
// (FR-023), compared case-insensitively.
func TestStartRegistrationRejectsDuplicateUsername(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	handle := "artist"
	if err := h.users.Create(ctx, &model.Account{ID: uuid.New(), Email: "a@example.com", Username: &handle, PasswordHash: "h", Role: access.RoleCustomer, Status: constant.StatusActive}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	taken := "ARTIST"
	_, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "b@example.com", Username: &taken, Password: registerPassword})
	if !errors.Is(err, domainerr.ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
	if h.registration.count() != 0 {
		t.Fatal("a taken username must leave no temporary registration")
	}
}

// TestStartRegistrationReplacesAPreviousRegistrationForTheSameEmail covers
// FR-026: the newest registration wins and the previous reference stops working.
func TestStartRegistrationReplacesAPreviousRegistrationForTheSameEmail(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	first, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "same@example.com", Password: registerPassword})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "same@example.com", Password: registerPassword})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.RegistrationID == second.RegistrationID {
		t.Fatal("expected a new reference for the replacement")
	}
	if _, err := h.registration.ByID(ctx, first.RegistrationID); !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("the replaced reference must be gone, got %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatalf("expected exactly one live registration, got %d", h.registration.count())
	}
}

// TestStartRegistrationKeepsTheTempWhenDeliveryFails covers the 503 promise
// (FR-010, D10): the temporary registration survives a failed send and the
// cooldown is disarmed so a new code can be asked for right away.
func TestStartRegistrationKeepsTheTempWhenDeliveryFails(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.email.failNext(1, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	out, err := h.svc.StartRegistration(ctx, appdto.StartRegistrationInput{Email: "recover@example.com", Password: registerPassword})
	if !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected ErrVerificationDeliveryFailed, got %v", err)
	}
	if out.RegistrationID != "" {
		t.Fatal("no reference is returned when the code could not be delivered")
	}
	if h.registration.count() != 1 {
		t.Fatal("the temporary registration must be kept after a failed delivery")
	}
	if h.registration.disarmCount() != 1 {
		t.Fatalf("a failed delivery must disarm the cooldown, got %d", h.registration.disarmCount())
	}
	if !h.audit.has(constant.AuditRegisterDeliveryFailed) {
		t.Fatal("expected the delivery failure to be audited")
	}
	if h.audit.has(constant.AuditRegistrationStarted) {
		t.Fatal("a failed delivery must not be audited as a successful start")
	}
}

// TestVerifyRegistrationCreatesAnActiveCustomer covers the US2 payoff: the
// correct code creates exactly one active customer account and completes the
// temporary registration — the code is discarded and the record is marked
// completed and retained until TTL (FR-009, FR-010).
func TestVerifyRegistrationCreatesAnActiveCustomer(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "v@example.com")
	otp := lastField(h.email.lastBody)

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("VerifyRegistrationOTP: %v", err)
	}
	account, err := h.users.ByEmail(ctx, "v@example.com")
	if err != nil {
		t.Fatalf("expected an account: %v", err)
	}
	if account.Status != constant.StatusActive {
		t.Fatalf("expected an active account, got %s", account.Status)
	}
	if account.Role != access.RoleCustomer {
		t.Fatalf("expected the customer role, got %s", account.Role)
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusCompleted {
		t.Fatalf("the registration must be marked completed, got %q", got)
	}
	if _, err := h.registration.ByID(ctx, out.RegistrationID); err != nil {
		t.Fatalf("the completed record must be retained until TTL, got %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatal("the completed record must stay in the store")
	}
	if err := h.registration.VerifyOTP(ctx, out.RegistrationID, otp); !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("the code must be removed on completion, got %v", err)
	}
	if !h.audit.has(constant.AuditEmailVerified) {
		t.Fatal("expected the verification to be audited")
	}
}

// TestVerifyRegistrationReplayAfterSuccessReturnsSuccess covers US2 scenario 5
// and FR-011: a registration already confirmed successfully reports success on
// replay without creating a second account.
func TestVerifyRegistrationReplayAfterSuccessReturnsSuccess(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "replay-ok@example.com")
	otp := lastField(h.email.lastBody)

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// Replay the very same confirmation.
	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("a replayed confirmation must report success, got %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("a replay must create no second account, got %d", len(h.users.byEmail))
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusCompleted {
		t.Fatalf("the registration must stay completed, got %q", got)
	}
}

// TestVerifyRegistrationRejectsAWrongCodeAndKeepsTheTemp covers the rejected
// code path: no account, the failure is counted and the temporary registration
// survives (FR-012).
func TestVerifyRegistrationRejectsAWrongCodeAndKeepsTheTemp(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "wrong@example.com")
	right := lastField(h.email.lastBody)
	wrong := "000000"
	if wrong == right {
		wrong = "000001"
	}

	err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: wrong})
	if !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("expected ErrOTPInvalid, got %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatal("a rejected code must keep the temporary registration")
	}
	if len(h.users.byEmail) != 0 {
		t.Fatal("a rejected code must create no account")
	}
	if !h.audit.has(constant.AuditRegistrationFailed) {
		t.Fatal("expected the rejected code to be audited")
	}
}

// TestVerifyRegistrationRejectsAnUnknownReference covers the generic not-found
// result that reveals nothing (FR-016).
func TestVerifyRegistrationRejectsAnUnknownReference(t *testing.T) {
	h := newHarness()
	err := h.svc.VerifyRegistrationOTP(context.Background(), appdto.VerifyRegistrationInput{RegistrationID: "does-not-exist", OTP: "123456"})
	if !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("expected ErrRegistrationNotFound, got %v", err)
	}
}

// TestVerifyRegistrationRejectsAnExpiredCode covers FR-007: an expired code
// creates no account and needs a resend.
func TestVerifyRegistrationRejectsAnExpiredCode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "expired@example.com")
	otp := lastField(h.email.lastBody)
	h.registration.expireCode(out.RegistrationID)

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("expected ErrOTPExpired, got %v", err)
	}
	if len(h.users.byEmail) != 0 {
		t.Fatal("an expired code must create no account")
	}
}

// TestVerifyRegistrationReplayReturnsSuccessWithoutADuplicate covers FR-011: a
// retry after an earlier confirmation that created the account but lost its
// response reports success and creates no second account.
func TestVerifyRegistrationReplayReturnsSuccessWithoutADuplicate(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "replay@example.com")
	otp := lastField(h.email.lastBody)

	// Simulate the earlier confirmation's durable effect: the account exists and
	// the registration is still live.
	if err := h.users.Create(ctx, &model.Account{ID: uuid.New(), Email: "replay@example.com", PasswordHash: "h:" + registerPassword, Role: access.RoleCustomer, Status: constant.StatusActive}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("a replay must report success, got %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("a replay must create no duplicate, got %d accounts", len(h.users.byEmail))
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusCompleted {
		t.Fatalf("the replay must complete the registration, got %q", got)
	}
	if h.registration.count() != 1 {
		t.Fatal("the completed record must be retained until TTL")
	}
}

// TestVerifyRegistrationRecoversAfterALostResponse covers FR-011 for the
// response that never reached the client: the account was created and the code
// consumed, so the retry must report success and create no second account.
func TestVerifyRegistrationRecoversAfterALostResponse(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "lost@example.com")
	otp := lastField(h.email.lastBody)

	// The earlier confirmation created the account and its code is gone (its
	// completion step never ran), but its response was lost before the temporary
	// registration was marked completed.
	if err := h.registration.InvalidateOTP(ctx, out.RegistrationID); err != nil {
		t.Fatalf("precondition: %v", err)
	}
	if err := h.users.Create(ctx, &model.Account{ID: uuid.New(), Email: "lost@example.com", PasswordHash: "h:" + registerPassword, Role: access.RoleCustomer, Status: constant.StatusActive}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("a retry after a lost response must report success, got %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("the retry must create no duplicate, got %d accounts", len(h.users.byEmail))
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusCompleted {
		t.Fatalf("the retry must complete the registration, got %q", got)
	}
	if h.registration.count() != 1 {
		t.Fatal("the completed record must be retained until TTL")
	}
}

// TestVerifyRegistrationKeepsTheTempWhenTheAccountStoreFails covers FR-010: a
// database failure after a correct code preserves the registration so the
// confirmation can be retried.
func TestVerifyRegistrationKeepsTheTempWhenTheAccountStoreFails(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "dberr@example.com")
	otp := lastField(h.email.lastBody)

	dbErr := errors.New("account store unavailable")
	h.users.createErr = dbErr

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); !errors.Is(err, dbErr) {
		t.Fatalf("expected the store error to surface, got %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatal("a store failure must keep the temporary registration")
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusPending {
		t.Fatalf("a store failure must leave the registration pending, got %q", got)
	}
	if _, err := h.users.ByEmail(ctx, "dberr@example.com"); !errors.Is(err, domainerr.ErrUserNotFound) {
		t.Fatal("no account must be created")
	}
}

// --- US3: resend -----------------------------------------------------------

// TestResendRegistrationIsRefusedWithinTheCooldown covers FR-014: a resend
// before the waiting period is refused and sends nothing.
func TestResendRegistrationIsRefusedWithinTheCooldown(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "cooldown@example.com")

	err := h.svc.ResendRegistrationOTP(ctx, appdto.RegistrationIDInput{RegistrationID: out.RegistrationID})
	if !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected ErrResendCooldown, got %v", err)
	}
	if h.email.count() != 1 {
		t.Fatalf("a refused resend must send nothing, got %d messages", h.email.count())
	}
}

// TestResendRegistrationIssuesANewCodeAfterTheCooldown covers FR-015: after the
// cooldown a new code replaces the old one and the attempt budget is reset.
func TestResendRegistrationIssuesANewCodeAfterTheCooldown(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "resend@example.com")
	oldCode := lastField(h.email.lastBody)

	// Two wrong submissions first, so the budget reset is observable against the
	// harness limit of 3 (a third failure would block the code).
	for i := 0; i < 2; i++ {
		if err := h.registration.VerifyOTP(ctx, out.RegistrationID, wrongCode(oldCode)); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("precondition wrong attempt %d: %v", i+1, err)
		}
	}

	// The cooldown has elapsed (its marker is gone), so the resend is allowed.
	if err := h.registration.DisarmCooldown(ctx, out.RegistrationID); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if err := h.svc.ResendRegistrationOTP(ctx, appdto.RegistrationIDInput{RegistrationID: out.RegistrationID}); err != nil {
		t.Fatalf("ResendRegistrationOTP: %v", err)
	}
	newCode := lastField(h.email.lastBody)
	if newCode == oldCode {
		t.Fatal("expected a new code")
	}
	if h.email.count() != 2 {
		t.Fatalf("expected a second message, got %d", h.email.count())
	}
	// The old code no longer works.
	if err := h.registration.VerifyOTP(ctx, out.RegistrationID, oldCode); !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("the old code must be invalid, got %v", err)
	}
	// The budget was reset: one wrong attempt does not block the new code.
	if err := h.registration.VerifyOTP(ctx, out.RegistrationID, wrongCode(newCode)); !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("the attempt budget must be reset, got %v", err)
	}
	// And the new code still verifies.
	if err := h.registration.VerifyOTP(ctx, out.RegistrationID, newCode); err != nil {
		t.Fatalf("the new code must verify, got %v", err)
	}
	if !h.audit.has(constant.AuditRegistrationResent) {
		t.Fatal("expected the resend to be audited")
	}
}

// TestResendRegistrationForAnUnknownReferenceIsGeneric covers FR-016: the
// response reveals nothing about whether the reference ever existed.
func TestResendRegistrationForAnUnknownReferenceIsGeneric(t *testing.T) {
	h := newHarness()
	err := h.svc.ResendRegistrationOTP(context.Background(), appdto.RegistrationIDInput{RegistrationID: "does-not-exist"})
	if !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("expected ErrRegistrationNotFound, got %v", err)
	}
}

// --- US5: resilience and abuse --------------------------------------------

// TestVerifyRegistrationRetriesToExactlyOneAccountAfterAStoreFailure covers
// FR-010 and US5 scenario 1: a failing account store keeps the registration,
// and a later retry creates exactly one account.
func TestVerifyRegistrationRetriesToExactlyOneAccountAfterAStoreFailure(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "retry@example.com")
	otp := lastField(h.email.lastBody)

	dbErr := errors.New("account store unavailable")
	h.users.createErr = dbErr

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); !errors.Is(err, dbErr) {
		t.Fatalf("expected the store error to surface, got %v", err)
	}
	if got := h.registration.status(out.RegistrationID); got != model.RegistrationStatusPending {
		t.Fatalf("a store failure must leave the registration pending, got %q", got)
	}

	h.users.createErr = nil
	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp}); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("the retry must create exactly one account, got %d", len(h.users.byEmail))
	}
}

// TestVerifyRegistrationBlocksAfterTooManyWrongAttempts covers FR-012/FR-013:
// wrong codes past the limit are refused and the correct code is blocked until a
// resend issues a new one.
func TestVerifyRegistrationBlocksAfterTooManyWrongAttempts(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "brute@example.com")
	right := lastField(h.email.lastBody)
	wrong := wrongCode(right)

	// The harness limit is 3, so the third wrong submission trips it.
	var last error
	for i := 0; i < 3; i++ {
		last = h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: wrong})
	}
	if !errors.Is(last, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts after the limit, got %v", last)
	}
	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: right}); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected the code to be blocked, got %v", err)
	}
	if len(h.users.byEmail) != 0 {
		t.Fatal("a blocked code must create no account")
	}
}

// TestVerifyRegistrationConcurrentCallsCreateOneAccount covers US5 scenario 4:
// two concurrent confirmations of one reference produce exactly one account.
func TestVerifyRegistrationConcurrentCallsCreateOneAccount(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "concurrent@example.com")
	otp := lastField(h.email.lastBody)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < len(errs); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: otp})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent confirmation %d: %v", i, err)
		}
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("concurrent confirmations must create exactly one account, got %d", len(h.users.byEmail))
	}
}

// --- US4: cancel -----------------------------------------------------------

// TestCancelRegistrationRemovesTheTempAndInvalidatesTheCode covers FR-017: a
// cancel removes the temporary registration, so its reference and code are
// unusable.
func TestCancelRegistrationRemovesTheTempAndInvalidatesTheCode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "cancel@example.com")
	code := lastField(h.email.lastBody)

	if err := h.svc.CancelRegistration(ctx, appdto.RegistrationIDInput{RegistrationID: out.RegistrationID}); err != nil {
		t.Fatalf("CancelRegistration: %v", err)
	}
	if _, err := h.registration.ByID(ctx, out.RegistrationID); !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("cancel must remove the record, got %v", err)
	}
	if err := h.registration.VerifyOTP(ctx, out.RegistrationID, code); !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("cancel must invalidate the code, got %v", err)
	}
	if !h.audit.has(constant.AuditRegistrationCancelled) {
		t.Fatal("expected the cancellation to be audited")
	}
}

// TestCancelRegistrationForAnUnknownReferenceIsANoOp covers FR-016: cancelling
// an unknown or expired reference is a generic no-op.
func TestCancelRegistrationForAnUnknownReferenceIsANoOp(t *testing.T) {
	h := newHarness()
	if err := h.svc.CancelRegistration(context.Background(), appdto.RegistrationIDInput{RegistrationID: "does-not-exist"}); err != nil {
		t.Fatalf("cancelling an unknown reference must be a no-op, got %v", err)
	}
	if h.audit.has(constant.AuditRegistrationCancelled) {
		t.Fatal("an unknown reference must not produce a cancellation trace")
	}
}

// TestTheTempRegistrationSurvivesEverythingButCancel covers US4 scenario 1 and
// SC-008: the temporary record is only removed by cancel (or expiry), not by a
// wrong code or a resend.
func TestTheTempRegistrationSurvivesEverythingButCancel(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	out := mustStart(t, h, "survive@example.com")
	right := lastField(h.email.lastBody)

	if err := h.svc.VerifyRegistrationOTP(ctx, appdto.VerifyRegistrationInput{RegistrationID: out.RegistrationID, OTP: wrongCode(right)}); !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("precondition wrong code: %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatal("a wrong code must keep the temporary registration")
	}

	if err := h.registration.DisarmCooldown(ctx, out.RegistrationID); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if err := h.svc.ResendRegistrationOTP(ctx, appdto.RegistrationIDInput{RegistrationID: out.RegistrationID}); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if h.registration.count() != 1 {
		t.Fatal("a resend must keep the temporary registration")
	}

	if err := h.svc.CancelRegistration(ctx, appdto.RegistrationIDInput{RegistrationID: out.RegistrationID}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if h.registration.count() != 0 {
		t.Fatal("cancel must remove the temporary registration")
	}
}
