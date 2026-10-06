package implement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

func lastField(body string) string {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func TestRegisterCreatesPendingAccountAndSendsOTP(t *testing.T) {
	h := newHarness()
	if err := h.svc.Register(context.Background(), appdto.RegisterInput{Email: "User@Example.com ", Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	account, err := h.users.ByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("expected normalized account: %v", err)
	}
	if account.Status != constant.StatusPending {
		t.Fatalf("expected pending, got %s", account.Status)
	}
	if h.email.count() != 1 || !h.audit.has(constant.AuditRegister) {
		t.Fatal("expected otp email and audit event")
	}
}

func TestRegisterRejectsWeakPassword(t *testing.T) {
	h := newHarness()
	if err := h.svc.Register(context.Background(), appdto.RegisterInput{Email: "w@example.com", Password: "password"}); !errors.Is(err, domainerr.ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
}

func TestDuplicateRegisterIsSilent(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: "d@example.com", Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: "d@example.com", Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("duplicate should be silent, got %v", err)
	}
}

func TestVerifyEmailActivates(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_ = h.svc.Register(ctx, appdto.RegisterInput{Email: "v@example.com", Password: "Str0ng!Pass"})
	otp := lastField(h.email.lastBody)

	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: "v@example.com", OTP: otp}); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	account, _ := h.users.ByEmail(ctx, "v@example.com")
	if account.Status != constant.StatusActive {
		t.Fatalf("expected active, got %s", account.Status)
	}
}

func TestVerifyEmailLocksAfterThreeFailures(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_ = h.svc.Register(ctx, appdto.RegisterInput{Email: "l@example.com", Password: "Str0ng!Pass"})

	for i := 0; i < 2; i++ {
		if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: "l@example.com", OTP: "000000"}); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("attempt %d: expected ErrOTPInvalid, got %v", i+1, err)
		}
	}
	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: "l@example.com", OTP: "000000"}); !errors.Is(err, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts, got %v", err)
	}
}

func TestLoginSuccessAndPending(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_ = h.svc.Register(ctx, appdto.RegisterInput{Email: "p@example.com", Password: "Str0ng!Pass"})

	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "p@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"}); !errors.Is(err, domainerr.ErrAccountPending) {
		t.Fatalf("expected ErrAccountPending, got %v", err)
	}

	account, _ := h.users.ByEmail(ctx, "p@example.com")
	_ = h.users.Activate(ctx, account.ID)

	session, err := h.svc.Login(ctx, appdto.LoginInput{Email: "p@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})
	if err != nil || session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("expected session, got %+v err %v", session, err)
	}
}

func TestLoginLockout(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("lock@example.com", "Str0ng!Pass")

	for i := 0; i < 9; i++ {
		_, _ = h.svc.Login(ctx, appdto.LoginInput{Email: "lock@example.com", Password: "wrong", Source: "9.9.9.9"})
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "lock@example.com", Password: "wrong", Source: "9.9.9.9"}); !errors.Is(err, domainerr.ErrAccountLocked) {
		t.Fatalf("expected ErrAccountLocked, got %v", err)
	}
}

func TestRefreshRotatesAndRejectsReuse(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("r@example.com", "Str0ng!Pass")

	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "r@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})
	next, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if next.RefreshToken == session.RefreshToken {
		t.Fatal("expected rotation")
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused, got %v", err)
	}
}

func TestRefreshReuseRevokesEverySessionOfTheAccount(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("replay@example.com", "Str0ng!Pass")

	deviceA, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "replay@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})
	deviceB, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "replay@example.com", Password: "Str0ng!Pass", Source: "2.2.2.2"})

	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceA.RefreshToken}); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceA.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused, got %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceB.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected the other device to be revoked too, got %v", err)
	}
	if !h.audit.has(constant.AuditRefreshReused) {
		t.Fatal("expected the replay to be audited")
	}
}

func TestRefreshRejectsExpiredSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("exp@example.com", "Str0ng!Pass")

	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "exp@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})
	stored, err := h.sessions.ByTokenHash(ctx, testRefresh{}.Hash(session.RefreshToken))
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)

	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken}); !errors.Is(err, domainerr.ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestLogoutRevokesOnlyItsOwnSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("multi@example.com", "Str0ng!Pass")
	deviceA, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "multi@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})
	deviceB, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "multi@example.com", Password: "Str0ng!Pass", Source: "2.2.2.2"})

	if err := h.svc.Logout(ctx, appdto.RefreshInput{RefreshToken: deviceA.RefreshToken}); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceB.RefreshToken}); err != nil {
		t.Fatalf("expected the other device to stay valid, got %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("reset@example.com", "Old!Pass1")

	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: "reset@example.com"}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	token := lastField(h.email.lastBody)

	if _, err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if h.tx.count() == 0 {
		t.Fatal("expected the reset to run inside a transaction")
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "reset@example.com", Password: "Old!Pass1", Source: "2.2.2.2"}); !errors.Is(err, domainerr.ErrInvalidCredentials) {
		t.Fatalf("old password should fail, got %v", err)
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "reset@example.com", Password: "New!Pass2", Source: "2.2.2.2"}); err != nil {
		t.Fatalf("new password should work, got %v", err)
	}
}

func TestResetPasswordRevokesEverySession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("resetall@example.com", "Old!Pass1")
	deviceA, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "resetall@example.com", Password: "Old!Pass1", Source: "1.1.1.1"})
	deviceB, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "resetall@example.com", Password: "Old!Pass1", Source: "2.2.2.2"})

	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: "resetall@example.com"}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if _, err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: lastField(h.email.lastBody), NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceA.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected device A to be revoked, got %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceB.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected device B to be revoked, got %v", err)
	}
}

func TestPasswordWritesAreAtomic(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("tx@example.com", "Old!Pass1")
	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "tx@example.com", Password: "Old!Pass1", Source: "1.1.1.1"})
	account, _ := h.users.ByEmail(ctx, "tx@example.com")
	in := appdto.ChangePasswordInput{
		AccountID:           account.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: session.RefreshToken,
	}

	h.tx.err = errors.New("commit failed")
	if _, err := h.svc.ChangePassword(ctx, in); err == nil {
		t.Fatal("expected the transaction failure to surface")
	}
	if h.audit.has(constant.AuditPasswordChanged) {
		t.Fatal("a rolled back password change must not be audited as success")
	}

	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: "tx@example.com"}); err == nil {
		t.Fatal("expected the transaction failure to surface")
	}
	if _, err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: lastField(h.email.lastBody), NewPassword: "New!Pass3"}); err == nil {
		t.Fatal("expected the transaction failure to surface")
	}
	if h.audit.has(constant.AuditPasswordReset) {
		t.Fatal("a rolled back reset must not be audited as success")
	}
}

func TestChangePasswordRequiresTheCurrentRefreshToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("owner@example.com", "Old!Pass1")
	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "owner@example.com", Password: "Old!Pass1", Source: "1.1.1.1"})
	account, _ := h.users.ByEmail(ctx, "owner@example.com")

	_, err := h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:       account.ID,
		CurrentPassword: "Old!Pass1",
		NewPassword:     "New!Pass2",
	})
	if !errors.Is(err, domainerr.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}

	_, err = h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:           account.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: "refresh:unknown",
	})
	if !errors.Is(err, domainerr.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for an unknown token, got %v", err)
	}

	if err := h.svc.Logout(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken}); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err = h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:           account.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: session.RefreshToken,
	})
	if !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused for a revoked token, got %v", err)
	}
}

func TestChangePasswordRevokesEverySession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("change@example.com", "Old!Pass1")
	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "change@example.com", Password: "Old!Pass1", Source: "3.3.3.3"})
	other, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "change@example.com", Password: "Old!Pass1", Source: "4.4.4.4"})
	account, _ := h.users.ByEmail(ctx, "change@example.com")

	issued, err := h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:           account.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: session.RefreshToken,
		CurrentAccessJTI:    "jti-1",
	})
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if issued.RefreshToken == "" {
		t.Fatal("expected a fresh token pair")
	}
	if !h.blacklist.jtis["jti-1"] {
		t.Fatal("expected jti blacklisted")
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: other.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected every session to be revoked, got %v", err)
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "change@example.com", Password: "New!Pass2", Source: "3.3.3.3"}); err != nil {
		t.Fatalf("new password should work, got %v", err)
	}
}

func TestChangePasswordRejectsAnotherAccountsToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("first@example.com", "Old!Pass1")
	h.registerActive("second@example.com", "Old!Pass1")
	first, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "first@example.com", Password: "Old!Pass1", Source: "1.1.1.1"})
	second, _ := h.users.ByEmail(ctx, "second@example.com")

	_, err := h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:           second.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: first.RefreshToken,
	})
	if !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused, got %v", err)
	}
}

// retryBudgetSpent is the total time the retry loop waits between attempts.
func retryBudgetSpent() time.Duration {
	var total time.Duration
	for _, wait := range deliveryRetryWaits {
		total += wait
	}
	return total
}

// TestRetryBudgetIsSizedToFitTheResponseTarget pins the arithmetic FR-015
// requires: the waits exist for one retry each, the whole budget is smaller than
// the documented response target, and the waits alone cannot exceed it.
func TestRetryBudgetIsSizedToFitTheResponseTarget(t *testing.T) {
	if len(deliveryRetryWaits) != deliveryMaxAttempts-1 {
		t.Fatalf("expected one wait per retry, got %d waits for %d attempts", len(deliveryRetryWaits), deliveryMaxAttempts)
	}
	if spent := retryBudgetSpent(); spent > deliveryRetryBudget {
		t.Fatalf("the waits (%s) exceed the budget (%s)", spent, deliveryRetryBudget)
	}
	if deliveryRetryBudget >= deliveryResponseTarget {
		t.Fatalf("the budget (%s) must leave room inside the response target (%s)", deliveryRetryBudget, deliveryResponseTarget)
	}
}

// TestARetriedDeliveryAnswersInsideTheResponseTarget spends the whole budget for
// real, so the guarantee is measured and not only asserted arithmetically.
func TestARetriedDeliveryAnswersInsideTheResponseTarget(t *testing.T) {
	h := newHarness()
	h.email.failAlwaysWith(func(string) error {
		return fmt.Errorf("%w: the provider reported status 421", domainerr.ErrDeliveryTransient)
	})

	start := time.Now()
	err := h.svc.Register(context.Background(), appdto.RegisterInput{Email: "slow@example.com", Password: "Str0ng!Pass"})
	elapsed := time.Since(start)

	if !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected ErrVerificationDeliveryFailed, got %v", err)
	}
	if h.email.attempts() != deliveryMaxAttempts {
		t.Fatalf("expected the budget to be spent on %d attempts, got %d", deliveryMaxAttempts, h.email.attempts())
	}
	if elapsed < retryBudgetSpent() {
		t.Fatalf("expected the waits to be spent, finished in %s", elapsed)
	}
	if elapsed >= deliveryResponseTarget {
		t.Fatalf("the retried delivery answered in %s, past the %s response target", elapsed, deliveryResponseTarget)
	}
}

// TestTheRetryBudgetStopsRetryingBeforeTheAttemptCountIsSpent proves the cap is
// enforced at runtime: when each attempt costs half the budget, the loop must stop
// after the attempt the waits still fit into instead of always spending every
// attempt.
func TestTheRetryBudgetStopsRetryingBeforeTheAttemptCountIsSpent(t *testing.T) {
	h := newHarness()
	tick := time.Now()
	h.svc.now = func() time.Time { return tick }
	h.email.failAlwaysWith(func(string) error {
		tick = tick.Add(deliveryRetryBudget / 2)
		return fmt.Errorf("%w: the provider reported status 450", domainerr.ErrDeliveryTransient)
	})
	var waited []time.Duration
	h.svc.sleep = func(d time.Duration) { waited = append(waited, d) }

	_ = h.svc.Register(context.Background(), appdto.RegisterInput{Email: "budget@example.com", Password: "Str0ng!Pass"})

	if h.email.attempts() >= deliveryMaxAttempts {
		t.Fatalf("expected the budget to stop the loop, got %d attempts", h.email.attempts())
	}
	if len(waited) == 0 {
		t.Fatal("expected at least one wait before the budget ran out")
	}
	if spent := retryBudgetSpent(); spent > deliveryRetryBudget {
		t.Fatalf("the waits (%s) exceed the budget (%s)", spent, deliveryRetryBudget)
	}
}

// TestRegisterDoesNotRetryAConfigurationClassifiedFailure covers FR-016: a
// failure a retry cannot change is refused immediately, so the customer is not
// kept waiting for an outcome that was never going to differ.
func TestRegisterDoesNotRetryAConfigurationClassifiedFailure(t *testing.T) {
	h := newHarness()
	h.email.failNext(deliveryMaxAttempts, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	err := h.svc.Register(context.Background(), appdto.RegisterInput{Email: "config@example.com", Password: "Str0ng!Pass"})

	if !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected ErrVerificationDeliveryFailed, got %v", err)
	}
	if h.email.attempts() != 1 {
		t.Fatalf("a configuration failure must not be retried, sent %d times", h.email.attempts())
	}
	if h.email.count() != 0 {
		t.Fatalf("no message was delivered, got %d", h.email.count())
	}
}

// TestRegisterDoesNotRetryAnUnclassifiedFailure keeps the budget for failures
// this system can actually vouch for: an error with no category is not assumed
// transient.
func TestRegisterDoesNotRetryAnUnclassifiedFailure(t *testing.T) {
	h := newHarness()
	h.email.failNext(deliveryMaxAttempts, errors.New("the sender returned something unexpected"))

	err := h.svc.Register(context.Background(), appdto.RegisterInput{Email: "unknown@example.com", Password: "Str0ng!Pass"})

	if !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected ErrVerificationDeliveryFailed, got %v", err)
	}
	if h.email.attempts() != 1 {
		t.Fatalf("an unclassified failure must not be retried, sent %d times", h.email.attempts())
	}
}

// TestRegisterRecoversWhenATransientFailureIsRetried covers the transient half of
// FR-015: one delivered message, no duplicate account, and a customer who can
// finish registering without anyone else's help.
func TestRegisterRecoversWhenATransientFailureIsRetried(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.email.failNext(1, fmt.Errorf("%w: the provider reported status 421", domainerr.ErrDeliveryTransient))

	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: "retry@example.com", Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("a transient failure inside the budget must not fail the request: %v", err)
	}
	if h.email.attempts() != 2 {
		t.Fatalf("expected one retry, got %d sends", h.email.attempts())
	}
	if h.email.count() != 1 {
		t.Fatalf("expected exactly one delivered message, got %d", h.email.count())
	}
	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: "retry@example.com", OTP: lastField(h.email.lastBody)}); err != nil {
		t.Fatalf("the delivered code must complete the registration: %v", err)
	}
}

// TestAFailedRegistrationIsCompletedByRequestingANewCode is the promise the 503
// makes, proved end to end: the account survives, the failed send leaves no
// cooldown in the way (FR-024), the new code completes the registration (FR-007)
// and no second account appears (FR-008).
func TestAFailedRegistrationIsCompletedByRequestingANewCode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "recover@example.com"
	h.email.failNext(1, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: email, Password: "Str0ng!Pass"}); !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected the delivery failure to surface, got %v", err)
	}
	if _, err := h.users.ByEmail(ctx, email); err != nil {
		t.Fatalf("the account must be kept: %v", err)
	}
	if h.otp.disarmCount() != 1 {
		t.Fatalf("expected the cooldown to be disarmed once, got %d", h.otp.disarmCount())
	}
	if err := h.otp.CanResend(ctx, email); err != nil {
		t.Fatalf("a send that did not happen must not consume the cooldown: %v", err)
	}
	if err := h.svc.ResendVerification(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("a new code must be requestable right after a failed delivery: %v", err)
	}
	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: email, OTP: lastField(h.email.lastBody)}); err != nil {
		t.Fatalf("the customer must be able to complete registration: %v", err)
	}
	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: email, Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("a repeated registration must stay silent, got %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("expected exactly one account, got %d", len(h.users.byEmail))
	}
}

// TestAFailedDeliveryLeavesTwoTracesCoveringTheAccount covers FR-010 and FR-011:
// an audit entry with a failure outcome that names the account it created, and no
// success outcome for a registration that did not deliver.
func TestAFailedDeliveryLeavesTwoTracesCoveringTheAccount(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "audited@example.com"
	h.email.failNext(deliveryMaxAttempts, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	_ = h.svc.Register(ctx, appdto.RegisterInput{Email: email, Password: "Str0ng!Pass"})

	if h.audit.has(constant.AuditRegister) {
		t.Fatal("a registration that did not deliver must not be audited as a success (FR-013)")
	}
	event, ok := h.audit.find(constant.AuditRegisterDeliveryFailed)
	if !ok {
		t.Fatalf("expected a %s entry, got %s", constant.AuditRegisterDeliveryFailed, h.audit.traces())
	}
	if event.outcome != constant.OutcomeFailure {
		t.Fatalf("expected outcome %s, got %s", constant.OutcomeFailure, event.outcome)
	}
	account, err := h.users.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("the account must be kept: %v", err)
	}
	if event.targetID != account.ID.String() {
		t.Fatalf("expected the entry to identify account %s, got %q", account.ID, event.targetID)
	}
	if !strings.Contains(fmt.Sprint(event.metadata), constant.DeliveryConfiguration) {
		t.Fatalf("expected the failure classification in the metadata, got %v", event.metadata)
	}
	if logs := h.logs.String(); !strings.Contains(logs, constant.DeliveryConfiguration) {
		t.Fatalf("expected one classified log line for the operator, got %q", logs)
	}
}

// TestTheFailureTracesCarryNoCodeCredentialOrProviderWording covers FR-012 and
// FR-022 with a provider that quotes everything it was given: neither trace may
// keep the code, the credential, the recipient address or the provider's own
// sentence.
func TestTheFailureTracesCarryNoCodeCredentialOrProviderWording(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const (
		email      = "leak@example.com"
		credential = "smtp-credential-value-do-not-log"
		password   = "Str0ng!Pass"
	)
	var issued string
	h.email.failAlwaysWith(func(body string) error {
		issued = lastField(body)
		return fmt.Errorf("%w: unauthorized IP address 203.0.113.7, code %s, credential %s",
			domainerr.ErrDeliveryConfiguration, issued, credential)
	})

	_ = h.svc.Register(ctx, appdto.RegisterInput{Email: email, Password: password})

	if issued == "" {
		t.Fatal("expected the sender to have been given a verification code")
	}
	traces := h.audit.traces() + "\n" + h.logs.String()
	for _, secret := range []string{issued, credential, password, email, "203.0.113.7", "unauthorized IP address"} {
		if strings.Contains(traces, secret) {
			t.Fatalf("a trace leaked %q: %s", secret, traces)
		}
	}
	if !strings.Contains(h.logs.String(), constant.DeliveryConfiguration) {
		t.Fatalf("the operator must still learn the classification, got %q", h.logs.String())
	}
}

// TestASuccessfulRegistrationLeavesTheSuccessTraceAlone covers FR-013: the
// successful path keeps exactly the audit entry it always wrote.
func TestASuccessfulRegistrationLeavesTheSuccessTraceAlone(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: "ok@example.com", Password: "Str0ng!Pass"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	event, ok := h.audit.find(constant.AuditRegister)
	if !ok {
		t.Fatalf("expected a %s entry, got %s", constant.AuditRegister, h.audit.traces())
	}
	if event.outcome != constant.OutcomeSuccess {
		t.Fatalf("expected outcome %s, got %s", constant.OutcomeSuccess, event.outcome)
	}
	if h.audit.has(constant.AuditRegisterDeliveryFailed) {
		t.Fatal("a delivered message must not leave a delivery-failure entry")
	}
	if h.otp.disarmCount() != 0 {
		t.Fatalf("a delivered message must leave the cooldown armed, got %d disarms", h.otp.disarmCount())
	}
}
