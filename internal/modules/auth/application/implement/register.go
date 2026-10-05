package implement

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// Register creates a pending account and emails an OTP. An already-registered
// email returns nil so responses do not reveal account existence (FR-011).
func (s *Service) Register(ctx context.Context, in dto.RegisterInput) error {
	if err := model.ValidatePasswordPolicy(in.Password); err != nil {
		return err
	}
	normalized := model.NormalizeEmail(in.Email)
	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return err
	}

	account := &model.Account{
		ID:           uuid.New(),
		Email:        normalized,
		PasswordHash: hash,
		Role:         access.RoleCustomer,
		Status:       constant.StatusPending,
	}
	if err := s.Users.Create(ctx, account); err != nil {
		if errors.Is(err, domainerr.ErrEmailTaken) {
			return nil
		}
		return err
	}
	if err := s.sendOTP(ctx, normalized); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditRegister, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

func (s *Service) sendOTP(ctx context.Context, email string) error {
	otp, err := generateOTP()
	if err != nil {
		return err
	}
	hash, err := s.Hasher.Hash(otp)
	if err != nil {
		return err
	}
	if err := s.OTP.Issue(ctx, email, hash); err != nil {
		return err
	}
	return s.Email.Send(ctx, email, "Confirm your email", "Your confirmation code is "+otp)
}
