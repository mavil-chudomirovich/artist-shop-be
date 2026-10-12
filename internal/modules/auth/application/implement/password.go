package implement

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

// ForgotPassword requests a password reset by emailing a short one-time code.
// The answer is generic: an unknown address returns nil without creating any
// state or sending anything, so the endpoint never reveals whether an account
// exists (FR-002). A registered address is refused while the resend cooldown is
// still active (FR-015) and, once a code is issued, receives an email whose body
// is only the code - never the reset token (FR-003, FR-017). A delivery that
// cannot be retried away disarms the cooldown (so the customer can try again
// immediately), records the failure and answers the shared delivery error
// (research D8).
func (s *Service) ForgotPassword(ctx context.Context, in dto.EmailInput) error {
	if err := model.ValidateEmail(in.Email); err != nil {
		return err
	}
	normalized := model.NormalizeEmail(in.Email)
	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return nil
		}
		return err
	}
	if err := s.ResetOTP.CanResend(ctx, normalized); err != nil {
		return err
	}

	attempts, err := s.issueResetOTP(ctx, normalized)
	if err != nil {
		return s.reportResetDeliveryFailure(ctx, account, attempts, err)
	}
	s.Audit.Record(ctx, constant.AuditPasswordResetReq, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// issueResetOTP generates a code, stores only its non-reversible representation
// against the normalized address, and delivers it with the shared retry budget.
// It returns the number of delivery attempts made.
func (s *Service) issueResetOTP(ctx context.Context, to string) (int, error) {
	otp, err := generateOTP()
	if err != nil {
		return 0, err
	}
	hash, err := s.Hasher.Hash(otp)
	if err != nil {
		return 0, err
	}
	if err := s.ResetOTP.Issue(ctx, to, hash); err != nil {
		return 0, err
	}
	return s.sendWithRetry(ctx, func(ctx context.Context) error {
		return s.Email.Send(ctx, to, "Reset your password", "Your password reset code is "+otp)
	})
}

// reportResetDeliveryFailure records what a reset request whose message never
// arrived must leave - a disarmed cooldown so the request can be retried, a
// failure audit entry carrying only the classification, and one classified
// operator log line - and answers the shared delivery failure (FR-018). It is
// keyed by the account, never the address, so no recipient PII reaches the logs
// or the audit trail.
func (s *Service) reportResetDeliveryFailure(ctx context.Context, account *model.Account, attempts int, err error) error {
	failure := classifyDeliveryFailure(err)
	logger := logging.WithCorrelation(ctx, s.log())

	if disarmErr := s.ResetOTP.DisarmCooldown(ctx, model.NormalizeEmail(account.Email)); disarmErr != nil {
		logger.ErrorContext(ctx, "reset resend cooldown could not be disarmed after a failed delivery",
			slog.String("userId", account.ID.String()))
	}

	s.Audit.Record(ctx, constant.AuditPasswordResetDeliveryFailed, constant.OutcomeFailure, &account.ID, string(account.Role), "user", account.ID.String(),
		map[string]any{"classification": failure.category})

	logger.ErrorContext(ctx, "reset code could not be delivered",
		slog.String("userId", account.ID.String()),
		slog.String("deliveryFailure", failure.category),
		slog.Int("deliveryAttempts", attempts))

	return domainerr.ErrVerificationDeliveryFailed
}

// VerifyResetOTP verifies the emailed reset code and, on success, issues a
// separate single-use reset token whose only purpose is to set a new password
// (FR-005, FR-006). The token is created durably before the code is invalidated,
// so a transient failure between the two can be retried with the same code
// (research D3). A rejected code records a failure and, like the request step,
// never reveals whether the address is registered (FR-009). The returned token
// is not a session and cannot authenticate a request.
func (s *Service) VerifyResetOTP(ctx context.Context, in dto.VerifyResetOTPInput) (dto.ResetTokenOutput, error) {
	if err := model.ValidateEmail(in.Email); err != nil {
		return dto.ResetTokenOutput{}, err
	}
	normalized := model.NormalizeEmail(in.Email)
	if err := s.ResetOTP.Verify(ctx, normalized, in.OTP); err != nil {
		s.Audit.Record(ctx, constant.AuditPasswordResetCodeFailed, constant.OutcomeFailure, nil, "", "reset_code", "", nil)
		return dto.ResetTokenOutput{}, err
	}

	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			// A code only ever exists for a registered address, so this branch is
			// defensive; it is reported as an expired code so nothing about the
			// address leaks (FR-009).
			return dto.ResetTokenOutput{}, domainerr.ErrOTPExpired
		}
		return dto.ResetTokenOutput{}, err
	}

	rawToken, tokenHash, err := s.RefreshTokens.Generate()
	if err != nil {
		return dto.ResetTokenOutput{}, err
	}
	now := time.Now().UTC()
	req := &model.ResetRequest{
		ID:        uuid.New(),
		UserID:    account.ID,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(s.Config.PasswordResetTTL),
	}
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		return s.Resets.Create(txCtx, req)
	}); err != nil {
		return dto.ResetTokenOutput{}, err
	}
	if err := s.ResetOTP.Invalidate(ctx, normalized); err != nil {
		return dto.ResetTokenOutput{}, err
	}

	s.Audit.Record(ctx, constant.AuditPasswordResetCodeVerified, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return dto.ResetTokenOutput{
		ResetToken: rawToken,
		ExpiresIn:  int(s.Config.PasswordResetTTL.Seconds()),
	}, nil
}

// ResetPassword consumes a reset token and replaces the password. A successful
// reset ends every existing session and blacklists the account's prior access
// tokens, and reports success only: it does not issue a session, so the user
// signs in again with the new password (FR-012, FR-013). Every write runs in one
// transaction so a partial failure cannot leave the account locked out. The
// policy is checked before the token is consumed, so a weak password leaves the
// token usable for a retry (FR-011).
func (s *Service) ResetPassword(ctx context.Context, in dto.ResetPasswordInput) error {
	if err := model.ValidatePasswordPolicy(in.NewPassword); err != nil {
		return err
	}
	req, err := s.Resets.ByTokenHash(ctx, s.RefreshTokens.Hash(in.Token))
	if err != nil {
		if errors.Is(err, domainerr.ErrResetNotFound) {
			return domainerr.ErrResetInvalid
		}
		return err
	}
	if req.UsedAt != nil || time.Now().UTC().After(req.ExpiresAt) {
		return domainerr.ErrResetInvalid
	}

	account, err := s.Users.ByID(ctx, req.UserID)
	if err != nil {
		return err
	}
	newHash, err := s.Hasher.Hash(in.NewPassword)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
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
		return nil
	})
	if err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditPasswordReset, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
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
