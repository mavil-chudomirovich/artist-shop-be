package implement

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

// sixDigits matches the emailed reset code: exactly six decimal digits and
// nothing else (FR-003, FR-017).
var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

// issueResetToken drives the full request -> verify flow and returns the
// single-use reset token, so a reset-consume test starts from a valid token.
func issueResetToken(t *testing.T, h *harness, email string) string {
	t.Helper()
	ctx := context.Background()
	// A previous request for the same address may still hold the send cooldown;
	// tests that are not about the cooldown clear it so they can re-request.
	_ = h.resetOTP.DisarmCooldown(ctx, email)
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	code := lastField(h.email.lastBody)
	out, err := h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: email, OTP: code})
	if err != nil {
		t.Fatalf("VerifyResetOTP: %v", err)
	}
	return out.ResetToken
}

// --- US1: request a reset code (T008) ---

func TestForgotPasswordEmailsACodeToARegisteredAddress(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-request@example.com"
	h.registerActive(email, "Old!Pass1")

	// The submitted address is normalized before the account lookup.
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: "Reset-Request@Example.com "}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if h.email.count() != 1 {
		t.Fatalf("expected exactly one code email, got %d", h.email.count())
	}
	code := lastField(h.email.lastBody)
	if !sixDigits.MatchString(code) {
		t.Fatalf("expected a six-digit code, got %q (body %q)", code, h.email.lastBody)
	}
	if strings.Contains(strings.ToLower(h.email.lastBody), "token") {
		t.Fatalf("the reset email must carry no token, got %q", h.email.lastBody)
	}

	entry, ok := h.resetOTP.codes[email]
	if !ok {
		t.Fatal("expected the code hash to be stored against the normalized address")
	}
	if entry.hash != "h:"+code {
		t.Fatalf("expected only the code's hash to be stored, got %q", entry.hash)
	}
	if !entry.cooldown {
		t.Fatal("expected the send cooldown to be armed after delivering a code")
	}
	if !h.audit.has(constant.AuditPasswordResetReq) {
		t.Fatal("expected the request to be audited")
	}
}

func TestForgotPasswordAnswersGenericallyForAnUnknownAddress(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: "nobody@example.com"}); err != nil {
		t.Fatalf("expected a generic no-op for an unknown address, got %v", err)
	}
	if h.email.count() != 0 {
		t.Fatalf("no code may be sent to an unknown address, got %d emails", h.email.count())
	}
	if len(h.resetOTP.codes) != 0 {
		t.Fatalf("nothing may be stored for an unknown address, got %d entries", len(h.resetOTP.codes))
	}
	if h.audit.has(constant.AuditPasswordResetReq) {
		t.Fatal("an unknown address must not be audited as a request")
	}
}

func TestForgotPasswordRefusesWithinTheCooldown(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "cooldown@example.com"
	h.registerActive(email, "Old!Pass1")

	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected ErrResendCooldown, got %v", err)
	}
	if h.email.count() != 1 {
		t.Fatalf("the refused request must not send a second code, got %d emails", h.email.count())
	}
}

func TestForgotPasswordDisarmsTheCooldownWhenDeliveryFails(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "undeliverable@example.com"
	h.registerActive(email, "Old!Pass1")
	h.email.failNext(1, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email})
	if !errors.Is(err, domainerr.ErrVerificationDeliveryFailed) {
		t.Fatalf("expected ErrVerificationDeliveryFailed, got %v", err)
	}
	if h.resetOTP.disarmCount() != 1 {
		t.Fatalf("expected the failed delivery to disarm the cooldown once, got %d", h.resetOTP.disarmCount())
	}
	if !h.audit.has(constant.AuditPasswordResetDeliveryFailed) {
		t.Fatal("expected the delivery failure to be audited")
	}
	if h.audit.has(constant.AuditPasswordResetReq) {
		t.Fatal("a request that did not deliver must not be audited as a request")
	}
}

// --- US2: verify the code and receive a reset token (T012) ---

func TestVerifyResetOTPExchangesACodeForAResetToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "verify@example.com"
	h.registerActive(email, "Old!Pass1")
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	code := lastField(h.email.lastBody)
	account, _ := h.users.ByEmail(ctx, email)

	out, err := h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: email, OTP: code})
	if err != nil {
		t.Fatalf("VerifyResetOTP: %v", err)
	}
	if out.ResetToken == "" {
		t.Fatal("expected a reset token to be issued")
	}
	if want := int(h.svc.Config.PasswordResetTTL.Seconds()); out.ExpiresIn != want {
		t.Fatalf("expected expiresIn %d, got %d", want, out.ExpiresIn)
	}
	stored, err := h.resets.ByTokenHash(ctx, testRefresh{}.Hash(out.ResetToken))
	if err != nil {
		t.Fatalf("the issued reset token must be stored: %v", err)
	}
	if stored.UserID != account.ID {
		t.Fatalf("expected the token to belong to the account (%s), got %s", account.ID, stored.UserID)
	}
	if _, ok := h.resetOTP.codes[email]; ok {
		t.Fatal("the code must be invalidated after a successful verification")
	}
	if !h.audit.has(constant.AuditPasswordResetCodeVerified) {
		t.Fatal("expected the verified code to be audited")
	}
}

func TestVerifyResetOTPRejectsAWrongCode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "verify-wrong@example.com"
	h.registerActive(email, "Old!Pass1")
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	code := lastField(h.email.lastBody)

	out, err := h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: email, OTP: wrongCode(code)})
	if !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("expected ErrOTPInvalid, got %v", err)
	}
	if out.ResetToken != "" {
		t.Fatal("no token may be issued for a wrong code")
	}
	if len(h.resets.byHash) != 0 {
		t.Fatal("no reset request may be created for a wrong code")
	}
	if !h.audit.has(constant.AuditPasswordResetCodeFailed) {
		t.Fatal("expected the rejected code to be audited")
	}
}

func TestVerifyResetOTPBlocksAfterTheAttemptLimit(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "verify-limit@example.com"
	h.registerActive(email, "Old!Pass1")
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	code := lastField(h.email.lastBody)
	wrong := wrongCode(code)

	// The fake blocks after maxAttempts (3) failures; the failure that reaches
	// the limit is reported as ErrOTPTooManyAttempts.
	var last error
	for i := 0; i < 3; i++ {
		_, last = h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: email, OTP: wrong})
	}
	if !errors.Is(last, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts at the limit, got %v", last)
	}
	if len(h.resets.byHash) != 0 {
		t.Fatal("a blocked code must never yield a token")
	}
}

func TestVerifyResetOTPRejectsAnExpiredOrUnknownCode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "verify-expired@example.com"
	h.registerActive(email, "Old!Pass1")
	if err := h.svc.ForgotPassword(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	// The code's lifetime elapses.
	h.resetOTP.expireCode(email)

	out, err := h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: email, OTP: "123456"})
	if !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("expected ErrOTPExpired for an expired code, got %v", err)
	}
	if out.ResetToken != "" {
		t.Fatal("no token may be issued for an expired code")
	}

	// An address with no active code (never requested / unregistered) is
	// refused the same way.
	out, err = h.svc.VerifyResetOTP(ctx, appdto.VerifyResetOTPInput{Email: "nobody@example.com", OTP: "123456"})
	if !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("expected ErrOTPExpired for an unknown address, got %v", err)
	}
	if out.ResetToken != "" {
		t.Fatal("no token may be issued for an address with no active code")
	}
}

// --- US3: set the new password (T017) ---

func TestResetPasswordChangesThePasswordAndEndsEverySession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-ok@example.com"
	h.registerActive(email, "Old!Pass1")
	deviceA, _ := h.svc.Login(ctx, appdto.LoginInput{Email: email, Password: "Old!Pass1", Source: "1.1.1.1"})
	deviceB, _ := h.svc.Login(ctx, appdto.LoginInput{Email: email, Password: "Old!Pass1", Source: "2.2.2.2"})
	account, _ := h.users.ByEmail(ctx, email)

	token := issueResetToken(t, h, email)

	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if h.tx.count() == 0 {
		t.Fatal("expected the reset to run inside a transaction")
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: email, Password: "Old!Pass1", Source: "3.3.3.3"}); !errors.Is(err, domainerr.ErrInvalidCredentials) {
		t.Fatalf("old password must fail, got %v", err)
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: email, Password: "New!Pass2", Source: "3.3.3.3"}); err != nil {
		t.Fatalf("new password must sign in, got %v", err)
	}

	stored, err := h.resets.ByTokenHash(ctx, testRefresh{}.Hash(token))
	if err != nil {
		t.Fatalf("load the consumed reset request: %v", err)
	}
	if stored.UsedAt == nil {
		t.Fatal("expected the reset token to be consumed")
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceA.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected device A's session to be revoked, got %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: deviceB.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected device B's session to be revoked, got %v", err)
	}
	if _, ok := h.blacklist.userMin[account.ID]; !ok {
		t.Fatal("expected access tokens issued before the reset to be blacklisted")
	}
	if !h.audit.has(constant.AuditPasswordReset) {
		t.Fatal("expected the reset to be audited")
	}
}

func TestResetPasswordRejectsAnUnknownToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("reset-unknown@example.com", "Old!Pass1")

	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: "refresh:unknown", NewPassword: "New!Pass2"}); !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected ErrResetInvalid for an unknown token, got %v", err)
	}
}

func TestResetPasswordRejectsAnExpiredToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-expired@example.com"
	h.registerActive(email, "Old!Pass1")
	token := issueResetToken(t, h, email)

	stored, err := h.resets.ByTokenHash(ctx, testRefresh{}.Hash(token))
	if err != nil {
		t.Fatalf("load reset request: %v", err)
	}
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)

	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"}); !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected ErrResetInvalid for an expired token, got %v", err)
	}
}

func TestResetPasswordRejectsAUsedToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-used@example.com"
	h.registerActive(email, "Old!Pass1")
	token := issueResetToken(t, h, email)

	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("first reset: %v", err)
	}
	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "Other!Pass3"}); !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected ErrResetInvalid reusing a token, got %v", err)
	}
}

// TestResetPasswordRejectsATokenConsumedByARacingResetAndRollsBack covers the
// spec's racing edge case on the consuming side (D11): the token is read as open,
// but a concurrent reset consumes it before this transaction reaches Consume.
// The conditional update affects no row, so the reset is refused and the whole
// transaction - including the password change already made - rolls back; the
// account keeps its old password.
func TestResetPasswordRejectsATokenConsumedByARacingResetAndRollsBack(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-race@example.com"
	h.registerActive(email, "Old!Pass1")
	account, _ := h.users.ByEmail(ctx, email)
	token := issueResetToken(t, h, email)

	// The competing transaction consumes the same token first: the read below
	// still sees it open, but the conditional write finds it used.
	h.svc.Tx = &rollbackTx{users: h.users}
	h.resets.consumeConflict = true

	err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"})
	if !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected ErrResetInvalid, got %v", err)
	}
	if account.PasswordHash != "h:Old!Pass1" {
		t.Fatalf("the refused reset must roll the password change back, got %q", account.PasswordHash)
	}
}

// TestConsumeRejectsASecondUseOfTheSameToken pins the repository contract the
// PostgreSQL adapter implements: Consume is conditional, so a token can be
// consumed exactly once (research D11).
func TestConsumeRejectsASecondUseOfTheSameToken(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "consume-twice@example.com"
	h.registerActive(email, "Old!Pass1")
	token := issueResetToken(t, h, email)

	stored, err := h.resets.ByTokenHash(ctx, testRefresh{}.Hash(token))
	if err != nil {
		t.Fatalf("load reset request: %v", err)
	}
	if err := h.resets.Consume(ctx, stored.ID); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := h.resets.Consume(ctx, stored.ID); !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected a second consume to be refused, got %v", err)
	}
}

func TestResetPasswordKeepsTheTokenWhenThePasswordIsWeak(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "reset-weak@example.com"
	h.registerActive(email, "Old!Pass1")
	token := issueResetToken(t, h, email)

	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "weak"}); !errors.Is(err, domainerr.ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
	// The token was never consumed, so the user can retry with a valid password.
	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: token, NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("the token must stay usable after a policy rejection: %v", err)
	}
}

// --- US4: abuse and interruption handling (T023) ---

// TestANewerResetTokenSupersedesTheEarlierOne covers the superseding edge case:
// issuing a second reset token for the account invalidates the first, so a
// presented superseded token is refused at reset (D11, FR-014).
func TestANewerResetTokenSupersedesTheEarlierOne(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "supersede@example.com"
	h.registerActive(email, "Old!Pass1")

	earlier := issueResetToken(t, h, email)
	newer := issueResetToken(t, h, email)
	if earlier == newer {
		t.Fatal("expected two distinct reset tokens")
	}

	// The newer token is the valid one.
	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: newer, NewPassword: "New!Pass2"}); err != nil {
		t.Fatalf("the newer token must be accepted: %v", err)
	}
	// The earlier token was superseded when the newer one was issued.
	if err := h.svc.ResetPassword(ctx, appdto.ResetPasswordInput{Token: earlier, NewPassword: "Other!Pass3"}); !errors.Is(err, domainerr.ErrResetInvalid) {
		t.Fatalf("expected ErrResetInvalid for a superseded token, got %v", err)
	}
}
