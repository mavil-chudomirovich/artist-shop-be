package implement

import (
	"context"
	"errors"
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
