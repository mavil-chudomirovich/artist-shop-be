package implement

import (
	"context"
	"errors"
	"strings"
	"testing"

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

func TestLogoutRevokes(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("o@example.com", "Str0ng!Pass")
	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "o@example.com", Password: "Str0ng!Pass", Source: "1.1.1.1"})

	if err := h.svc.Logout(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken}); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := h.svc.Refresh(ctx, appdto.RefreshInput{RefreshToken: session.RefreshToken}); !errors.Is(err, domainerr.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused after logout, got %v", err)
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
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "reset@example.com", Password: "Old!Pass1", Source: "2.2.2.2"}); !errors.Is(err, domainerr.ErrInvalidCredentials) {
		t.Fatalf("old password should fail, got %v", err)
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "reset@example.com", Password: "New!Pass2", Source: "2.2.2.2"}); err != nil {
		t.Fatalf("new password should work, got %v", err)
	}
}

func TestChangePasswordBlacklistsJTI(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.registerActive("change@example.com", "Old!Pass1")
	session, _ := h.svc.Login(ctx, appdto.LoginInput{Email: "change@example.com", Password: "Old!Pass1", Source: "3.3.3.3"})
	account, _ := h.users.ByEmail(ctx, "change@example.com")

	if _, err := h.svc.ChangePassword(ctx, appdto.ChangePasswordInput{
		AccountID:           account.ID,
		CurrentPassword:     "Old!Pass1",
		NewPassword:         "New!Pass2",
		CurrentRefreshToken: session.RefreshToken,
		CurrentAccessJTI:    "jti-1",
	}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if !h.blacklist.jtis["jti-1"] {
		t.Fatal("expected jti blacklisted")
	}
	if _, err := h.svc.Login(ctx, appdto.LoginInput{Email: "change@example.com", Password: "New!Pass2", Source: "3.3.3.3"}); err != nil {
		t.Fatalf("new password should work, got %v", err)
	}
}
