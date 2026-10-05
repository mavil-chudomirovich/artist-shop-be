package implement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// ForgotPassword emails a single-use reset token. Unknown emails return nil.
func (s *Service) ForgotPassword(ctx context.Context, in dto.EmailInput) error {
	normalized := model.NormalizeEmail(in.Email)
	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return nil
		}
		return err
	}

	rawToken, tokenHash, err := s.RefreshTokens.Generate()
	if err != nil {
		return err
	}
	req := &model.ResetRequest{
		ID:        uuid.New(),
		UserID:    account.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().UTC().Add(s.Config.PasswordResetTTL),
	}
	if err := s.Resets.Create(ctx, req); err != nil {
		return err
	}
	if err := s.Email.Send(ctx, normalized, "Reset your password", "Use this token to reset your password: "+rawToken); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditPasswordResetReq, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// ResetPassword consumes a reset token, revokes all sessions, blacklists prior
// access tokens, and issues a fresh token pair.
func (s *Service) ResetPassword(ctx context.Context, in dto.ResetPasswordInput) (dto.SessionOutput, error) {
	if err := model.ValidatePasswordPolicy(in.NewPassword); err != nil {
		return dto.SessionOutput{}, err
	}
	req, err := s.Resets.ByTokenHash(ctx, s.RefreshTokens.Hash(in.Token))
	if err != nil {
		if errors.Is(err, domainerr.ErrResetNotFound) {
			return dto.SessionOutput{}, domainerr.ErrResetInvalid
		}
		return dto.SessionOutput{}, err
	}
	if req.UsedAt != nil || time.Now().UTC().After(req.ExpiresAt) {
		return dto.SessionOutput{}, domainerr.ErrResetInvalid
	}

	account, err := s.Users.ByID(ctx, req.UserID)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	newHash, err := s.Hasher.Hash(in.NewPassword)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	if err := s.Users.UpdatePassword(ctx, account.ID, newHash); err != nil {
		return dto.SessionOutput{}, err
	}
	if err := s.Resets.Consume(ctx, req.ID); err != nil {
		return dto.SessionOutput{}, err
	}
	if err := s.Sessions.RevokeAllForUser(ctx, account.ID); err != nil {
		return dto.SessionOutput{}, err
	}
	now := time.Now().UTC()
	if err := s.Blacklist.RevokeUserBefore(ctx, account.ID, now, s.Access.TTL()); err != nil {
		return dto.SessionOutput{}, err
	}
	session, err := s.issueSession(ctx, account, now.Add(s.Config.RefreshTokenTTL), "", "")
	if err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditPasswordReset, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return session, nil
}

// ChangePassword updates the password of a signed-in account, blacklisting prior
// access tokens and keeping the replaced session's expiry when supplied.
func (s *Service) ChangePassword(ctx context.Context, in dto.ChangePasswordInput) (dto.SessionOutput, error) {
	account, err := s.Users.ByID(ctx, in.AccountID)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	ok, err := s.Hasher.Verify(account.PasswordHash, in.CurrentPassword)
	if err != nil || !ok {
		return dto.SessionOutput{}, domainerr.ErrInvalidCredentials
	}
	if err := model.ValidatePasswordPolicy(in.NewPassword); err != nil {
		return dto.SessionOutput{}, err
	}
	newHash, err := s.Hasher.Hash(in.NewPassword)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	if err := s.Users.UpdatePassword(ctx, account.ID, newHash); err != nil {
		return dto.SessionOutput{}, err
	}

	now := time.Now().UTC()
	if err := s.Blacklist.RevokeUserBefore(ctx, account.ID, now, s.Access.TTL()); err != nil {
		return dto.SessionOutput{}, err
	}
	if in.CurrentAccessJTI != "" {
		if err := s.Blacklist.RevokeJTI(ctx, in.CurrentAccessJTI, s.Access.TTL()); err != nil {
			return dto.SessionOutput{}, err
		}
	}

	expiresAt := now.Add(s.Config.RefreshTokenTTL)
	var replace *model.Session
	if in.CurrentRefreshToken != "" {
		if existing, err := s.Sessions.ByTokenHash(ctx, s.RefreshTokens.Hash(in.CurrentRefreshToken)); err == nil && existing.RevokedAt == nil {
			expiresAt = existing.ExpiresAt
			replace = existing
		}
	}

	accessToken, _, err := s.Access.Issue(account.ID, account.Role)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	rawRefresh, refreshHash, err := s.RefreshTokens.Generate()
	if err != nil {
		return dto.SessionOutput{}, err
	}
	next := &model.Session{
		ID:               uuid.New(),
		UserID:           account.ID,
		RefreshTokenHash: refreshHash,
		ExpiresAt:        expiresAt,
		UserAgent:        in.UserAgent,
		IP:               in.IP,
	}
	if replace != nil {
		if err := s.Sessions.Rotate(ctx, replace.ID, next); err != nil {
			return dto.SessionOutput{}, err
		}
	} else if err := s.Sessions.Create(ctx, next); err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditPasswordChanged, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return dto.SessionOutput{AccessToken: accessToken, RefreshToken: rawRefresh, ExpiresIn: int(s.Access.TTL().Seconds())}, nil
}
