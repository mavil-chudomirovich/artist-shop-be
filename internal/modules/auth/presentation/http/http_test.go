package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

type stubService struct{ role access.Role }

func (stubService) Register(context.Context, dto.RegisterInput) error { return nil }
func (stubService) VerifyEmail(context.Context, dto.VerifyEmailInput) error {
	return nil
}
func (stubService) ResendVerification(context.Context, dto.EmailInput) error { return nil }
func (stubService) Login(context.Context, dto.LoginInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (stubService) Refresh(context.Context, dto.RefreshInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (stubService) Logout(context.Context, dto.RefreshInput) error { return nil }
func (stubService) ForgotPassword(context.Context, dto.EmailInput) error {
	return nil
}
func (stubService) ResetPassword(context.Context, dto.ResetPasswordInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (stubService) ChangePassword(context.Context, dto.ChangePasswordInput) (dto.SessionOutput, error) {
	return dto.SessionOutput{}, nil
}
func (s stubService) VerifyAccessToken(context.Context, string) (appinterface.Claims, error) {
	return appinterface.Claims{Subject: uuid.New(), Role: s.role, ID: "jti", IssuedAt: time.Now()}, nil
}
func (stubService) Identity(_ context.Context, id uuid.UUID) (dto.IdentityOutput, error) {
	return dto.IdentityOutput{ID: id.String(), Email: "user@example.com", Role: access.RoleCustomer}, nil
}
func (stubService) ProvisionAdmin(context.Context, dto.ProvisionAdminInput) error { return nil }

var _ appinterface.AuthService = stubService{}

func testRouter(role access.Role) http.Handler {
	h := New(stubService{role: role}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	return h.Router(config.AuthConfig{LoginRatePerMinute: 1000, FlowRatePerMinute: 1000})
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

func TestAdminProbeDeniesCustomer(t *testing.T) {
	if rec := do(testRouter(access.RoleCustomer), http.MethodGet, "/admin/probe", "token"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestAdminProbeAllowsAdmin(t *testing.T) {
	if rec := do(testRouter(access.RoleAdmin), http.MethodGet, "/admin/probe", "token"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMapErrorCodes(t *testing.T) {
	cases := map[error]string{
		domainerr.ErrWeakPassword:       constant.CodeWeakPassword,
		domainerr.ErrInvalidCredentials: constant.CodeInvalidCredentials,
		domainerr.ErrAccountPending:     constant.CodeAccountPending,
		domainerr.ErrOTPInvalid:         constant.CodeOTPInvalid,
		domainerr.ErrAccountLocked:      constant.CodeLoginLocked,
		domainerr.ErrExpiredToken:       constant.CodeTokenExpired,
		domainerr.ErrRefreshReused:      constant.CodeRefreshReused,
		domainerr.ErrResetInvalid:       constant.CodeResetInvalid,
	}
	for err, want := range cases {
		if got := mapError(err); string(got.Code) != want {
			t.Errorf("mapError(%v): expected %s, got %s", err, want, got.Code)
		}
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
