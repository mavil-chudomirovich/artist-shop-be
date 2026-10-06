package implement

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
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
	attempts, err := s.sendOTP(ctx, normalized)
	if err != nil {
		return s.reportDeliveryFailure(ctx, account, attempts, err)
	}
	s.Audit.Record(ctx, constant.AuditRegister, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// sendOTP issues a code and delivers it, retrying only a failure classified as
// one a retry can resolve (FR-015). It returns the number of delivery attempts
// made, which the failure report records for the operator.
func (s *Service) sendOTP(ctx context.Context, email string) (int, error) {
	otp, err := generateOTP()
	if err != nil {
		return 0, err
	}
	hash, err := s.Hasher.Hash(otp)
	if err != nil {
		return 0, err
	}
	if err := s.OTP.Issue(ctx, email, hash); err != nil {
		return 0, err
	}
	return s.sendWithRetry(ctx, func(ctx context.Context) error {
		return s.Email.Send(ctx, email, "Confirm your email", "Your confirmation code is "+otp)
	})
}

// reportDeliveryFailure records the two traces a registration whose message never
// arrived must leave, and returns the error presentation answers 503 with
// (FR-010, FR-011).
//
// The account is deliberately kept. It was committed before the send was
// attempted, its pending state is the state the resend flow already operates on,
// and deleting it would discard a registration the customer can still finish on
// their own once delivery recovers (FR-007). A failed delivery never writes the
// success outcome: the row states plainly that the event did not complete
// (FR-013).
//
// The traces name a registration, so the resend path, which leaves traces of its
// own kind, shares only the cooldown disarm this flow needs and not these two.
func (s *Service) reportDeliveryFailure(ctx context.Context, account *model.Account, attempts int, err error) error {
	failure := classifyDeliveryFailure(err)
	logger := logging.WithCorrelation(ctx, s.log())

	s.disarmCooldownAfterFailedDelivery(ctx, account.Email, account.ID.String())

	s.Audit.Record(ctx, constant.AuditRegisterDeliveryFailed, constant.OutcomeFailure, nil, "", "user", account.ID.String(),
		map[string]any{"classification": failure.category})

	logger.ErrorContext(ctx, "verification message could not be delivered",
		slog.String("accountId", account.ID.String()),
		slog.String("deliveryFailure", failure.category),
		slog.Int("deliveryAttempts", attempts))

	return domainerr.ErrVerificationDeliveryFailed
}
