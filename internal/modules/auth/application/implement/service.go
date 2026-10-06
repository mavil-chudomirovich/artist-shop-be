// Package implement contains the auth module's use-case implementations. It
// depends only on application/interface and domain.
package implement

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// Config carries use-case timeouts and TTLs.
type Config struct {
	RefreshTokenTTL  time.Duration
	PasswordResetTTL time.Duration
}

// Service implements appinterface.AuthService.
type Service struct {
	Users         repository.UserRepository
	Sessions      repository.SessionRepository
	Resets        repository.ResetRepository
	OTP           appinterface.OTPStore
	Blacklist     appinterface.Blacklist
	Guard         appinterface.LoginGuard
	Hasher        appinterface.PasswordHasher
	Access        appinterface.AccessTokens
	RefreshTokens appinterface.RefreshTokens
	Email         appinterface.EmailSender
	Audit         appinterface.Auditor
	Tx            appinterface.UnitOfWork
	// Logger receives the classified diagnostic an operator acts on when a
	// message cannot be delivered. It must be the composition's own structured
	// logger so the line carries the request correlation id (Constitution VI).
	Logger *slog.Logger
	Config Config

	// now and sleep are the retry budget's clock and timer. They are nil in
	// production and replaced in tests, so the budget can be exercised without
	// waiting on it.
	now   func() time.Time
	sleep func(time.Duration)
}

var _ appinterface.AuthService = (*Service)(nil)

// New creates the service.
func New(deps Service) *Service { return &deps }

// log returns the service's logger, falling back to the default one so a missing
// logger never turns a diagnostic into a panic.
func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// issueSession creates a session row and returns the token pair together with
// the persisted session so callers can reference it in audit events.
func (s *Service) issueSession(ctx context.Context, account *model.Account, expiresAt time.Time, userAgent, ip string) (dto.SessionOutput, *model.Session, error) {
	accessToken, _, err := s.Access.Issue(account.ID, account.Role)
	if err != nil {
		return dto.SessionOutput{}, nil, fmt.Errorf("issue access token: %w", err)
	}
	rawRefresh, refreshHash, err := s.RefreshTokens.Generate()
	if err != nil {
		return dto.SessionOutput{}, nil, fmt.Errorf("generate refresh token: %w", err)
	}
	session := &model.Session{
		ID:               uuid.New(),
		UserID:           account.ID,
		RefreshTokenHash: refreshHash,
		ExpiresAt:        expiresAt,
		UserAgent:        userAgent,
		IP:               ip,
	}
	if err := s.Sessions.Create(ctx, session); err != nil {
		return dto.SessionOutput{}, nil, err
	}
	out := dto.SessionOutput{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    int(s.Access.TTL().Seconds()),
	}
	return out, session, nil
}

// VerifyAccessToken parses an access token and rejects revoked/blacklisted ones.
func (s *Service) VerifyAccessToken(ctx context.Context, raw string) (appinterface.Claims, error) {
	claims, err := s.Access.Parse(raw)
	if err != nil {
		return appinterface.Claims{}, err
	}
	revoked, err := s.Blacklist.IsJTIRevoked(ctx, claims.ID)
	if err != nil {
		return appinterface.Claims{}, err
	}
	if revoked {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}
	stale, err := s.Blacklist.IsIssuedBefore(ctx, claims.Subject, claims.IssuedAt)
	if err != nil {
		return appinterface.Claims{}, err
	}
	if stale {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}
	return claims, nil
}

// Identity loads the account behind a subject.
func (s *Service) Identity(ctx context.Context, id uuid.UUID) (dto.IdentityOutput, error) {
	account, err := s.Users.ByID(ctx, id)
	if err != nil {
		return dto.IdentityOutput{}, err
	}
	return mapper.ToIdentity(account), nil
}

// ProvisionAdmin creates or updates the single admin account. It is an
// administrative mutation, so it is audited (Constitution VI).
func (s *Service) ProvisionAdmin(ctx context.Context, in dto.ProvisionAdminInput) error {
	if err := model.ValidatePasswordPolicy(in.Password); err != nil {
		return err
	}
	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return err
	}
	account := &model.Account{
		ID:           uuid.New(),
		Email:        model.NormalizeEmail(in.Email),
		PasswordHash: hash,
		Role:         access.RoleAdmin,
		Status:       constant.StatusActive,
	}
	if err := s.Users.UpsertAdmin(ctx, account); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditAdminProvisioned, constant.OutcomeSuccess, nil, string(access.RoleAdmin), "user", account.Email, nil)
	return nil
}

// generateOTP returns a random 6-digit code.
func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
