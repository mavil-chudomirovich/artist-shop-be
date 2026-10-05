package implement

import (
	"context"
	"errors"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// VerifyEmail confirms the OTP and activates the account.
func (s *Service) VerifyEmail(ctx context.Context, in dto.VerifyEmailInput) error {
	normalized := model.NormalizeEmail(in.Email)
	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return domainerr.ErrOTPInvalid
		}
		return err
	}
	if account.Status == constant.StatusActive {
		return nil
	}
	if account.Status == constant.StatusDisabled {
		return domainerr.ErrAccountDisabled
	}
	if err := s.OTP.Verify(ctx, normalized, in.OTP); err != nil {
		return err
	}
	if err := s.Users.Activate(ctx, account.ID); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditEmailVerified, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// ResendVerification re-issues an OTP subject to the resend cooldown. The
// response stays generic, so accounts that no longer need a code are skipped
// silently instead of reporting their status.
func (s *Service) ResendVerification(ctx context.Context, in dto.EmailInput) error {
	normalized := model.NormalizeEmail(in.Email)
	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return nil
		}
		return err
	}
	if account.Status != constant.StatusPending {
		return nil
	}
	if err := s.OTP.CanResend(ctx, normalized); err != nil {
		return err
	}
	return s.sendOTP(ctx, normalized)
}
