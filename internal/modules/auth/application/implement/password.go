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
// The open-request invalidation plus the insert run in one transaction.
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
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		return s.Resets.Create(txCtx, req)
	}); err != nil {
		return err
	}
	if err := s.Email.Send(ctx, normalized, "Reset your password", "Use this token to reset your password: "+rawToken); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditPasswordResetReq, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// ResetPassword consumes a reset token, revokes all sessions, blacklists prior
// access tokens, and issues a fresh token pair. Every write runs in one
// transaction so a partial failure cannot leave the account locked out.
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

	now := time.Now().UTC()
	var issued dto.SessionOutput
	err = s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Users.UpdatePassword(txCtx, account.ID, newHash); err != nil {
			return err
		}
		if err := s.Resets.Consume(txCtx, req.ID); err != nil {
			return err
		}
		if err := s.Sessions.RevokeAllForUser(txCtx, account.ID); err != nil {
			return err
		}
		if err := s.Blacklist.RevokeUserBefore(txCtx, account.ID, now, s.Access.TTL()); err != nil {
			return err
		}
		out, _, err := s.issueSession(txCtx, account, now.Add(s.Config.RefreshTokenTTL), "", "")
		if err != nil {
			return err
		}
		issued = out
		return nil
	})
	if err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditPasswordReset, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return issued, nil
}

// ChangePassword updates the password of a signed-in account. The supplied
// refresh token must be an active session of that account; afterwards every
// session is revoked and a new token pair is issued.
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
	current, err := s.Sessions.ByTokenHash(ctx, s.RefreshTokens.Hash(in.CurrentRefreshToken))
	if err != nil {
		if errors.Is(err, domainerr.ErrSessionNotFound) {
			return dto.SessionOutput{}, domainerr.ErrInvalidToken
		}
		return dto.SessionOutput{}, err
	}
	if current.UserID != account.ID || current.RevokedAt != nil {
		return dto.SessionOutput{}, domainerr.ErrRefreshReused
	}

	now := time.Now().UTC()
	var issued dto.SessionOutput
	err = s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Users.UpdatePassword(txCtx, account.ID, newHash); err != nil {
			return err
		}
		if err := s.Sessions.RevokeAllForUser(txCtx, account.ID); err != nil {
			return err
		}
		if err := s.Blacklist.RevokeUserBefore(txCtx, account.ID, now, s.Access.TTL()); err != nil {
			return err
		}
		if in.CurrentAccessJTI != "" {
			if err := s.Blacklist.RevokeJTI(txCtx, in.CurrentAccessJTI, s.Access.TTL()); err != nil {
				return err
			}
		}
		out, _, err := s.issueSession(txCtx, account, now.Add(s.Config.RefreshTokenTTL), in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		issued = out
		return nil
	})
	if err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditPasswordChanged, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return issued, nil
}
