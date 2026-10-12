package implement

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

// registrationStatusPendingVerification is the status reported while a
// registration waits for its emailed code to be confirmed. It is a display value
// only: no durable account carries it.
const registrationStatusPendingVerification = "PendingVerification"

// StartRegistration validates the submitted details (email format first, then
// the password policy), rejects an email or username that is already taken, and
// holds the registration in the temporary store only, then emails a one-time
// code. No durable account is created: a visitor who abandons the flow leaves
// nothing behind (FR-001, FR-003, FR-004, FR-005). The returned reference and
// timings drive verify/resend/cancel.
func (s *Service) StartRegistration(ctx context.Context, in dto.StartRegistrationInput) (dto.RegistrationStartedOutput, error) {
	if err := model.ValidateEmail(in.Email); err != nil {
		return dto.RegistrationStartedOutput{}, err
	}
	if err := model.ValidatePasswordPolicy(in.Password); err != nil {
		return dto.RegistrationStartedOutput{}, err
	}
	normalized := model.NormalizeEmail(in.Email)

	if _, err := s.Users.ByEmail(ctx, normalized); err == nil {
		return dto.RegistrationStartedOutput{}, domainerr.ErrEmailTaken
	} else if !errors.Is(err, domainerr.ErrUserNotFound) {
		return dto.RegistrationStartedOutput{}, err
	}

	username := model.NormalizeUsername(derefString(in.Username))
	if username != nil {
		if _, err := s.Users.ByUsername(ctx, *username); err == nil {
			return dto.RegistrationStartedOutput{}, domainerr.ErrUsernameTaken
		} else if !errors.Is(err, domainerr.ErrUserNotFound) {
			return dto.RegistrationStartedOutput{}, err
		}
	}

	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return dto.RegistrationStartedOutput{}, err
	}
	registrationID, err := model.NewRegistrationID()
	if err != nil {
		return dto.RegistrationStartedOutput{}, err
	}
	now := time.Now().UTC()
	reg := model.PendingRegistration{
		RegistrationID: registrationID,
		Email:          normalized,
		Username:       username,
		DisplayName:    normalizeDisplayName(in.DisplayName),
		PasswordHash:   hash,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.Config.PendingRegistrationTTL),
		Status:         model.RegistrationStatusPending,
	}
	if err := s.Registration.Save(ctx, reg); err != nil {
		return dto.RegistrationStartedOutput{}, err
	}

	attempts, err := s.issueRegistrationOTP(ctx, registrationID, normalized)
	if err != nil {
		return dto.RegistrationStartedOutput{}, s.reportRegistrationDeliveryFailure(ctx, registrationID, attempts, err)
	}

	s.Audit.Record(ctx, constant.AuditRegistrationStarted, constant.OutcomeSuccess, nil, string(access.RoleCustomer), "registration", registrationID, nil)
	return dto.RegistrationStartedOutput{
		RegistrationID:    registrationID,
		Status:            registrationStatusPendingVerification,
		OTPExpiresIn:      int(s.Config.OTPTTL.Seconds()),
		ResendAvailableIn: int(s.Config.OTPResendCooldown.Seconds()),
	}, nil
}

// issueRegistrationOTP generates a code, stores only its non-reversible
// representation against the registration reference, and delivers it with the
// shared retry budget (FR-006). It returns the number of delivery attempts made.
func (s *Service) issueRegistrationOTP(ctx context.Context, registrationID, to string) (int, error) {
	otp, err := generateOTP()
	if err != nil {
		return 0, err
	}
	hash, err := s.Hasher.Hash(otp)
	if err != nil {
		return 0, err
	}
	if err := s.Registration.IssueOTP(ctx, registrationID, hash); err != nil {
		return 0, err
	}
	return s.sendWithRetry(ctx, func(ctx context.Context) error {
		return s.Email.Send(ctx, to, "Confirm your email", "Your confirmation code is "+otp)
	})
}

// reportRegistrationDeliveryFailure records the traces a registration whose
// message never arrived must leave, keeps the temporary registration so the
// visitor can retry, and answers the shared delivery failure (FR-010, FR-020).
// The disarm/log helper is keyed by the registration reference because no
// account exists at this point (research D10).
func (s *Service) reportRegistrationDeliveryFailure(ctx context.Context, registrationID string, attempts int, err error) error {
	failure := classifyDeliveryFailure(err)
	logger := logging.WithCorrelation(ctx, s.log())

	if disarmErr := s.Registration.DisarmCooldown(ctx, registrationID); disarmErr != nil {
		logger.ErrorContext(ctx, "resend cooldown could not be disarmed after a failed delivery",
			slog.String("registrationId", registrationID))
	}

	s.Audit.Record(ctx, constant.AuditRegisterDeliveryFailed, constant.OutcomeFailure, nil, "", "registration", registrationID,
		map[string]any{"classification": failure.category})

	logger.ErrorContext(ctx, "verification message could not be delivered",
		slog.String("registrationId", registrationID),
		slog.String("deliveryFailure", failure.category),
		slog.Int("deliveryAttempts", attempts))

	return domainerr.ErrVerificationDeliveryFailed
}

// VerifyRegistrationOTP confirms the submitted code and, on success, creates
// exactly one active customer account and completes the temporary registration:
// the code and the email pointer are removed and the record is retained
// (code-less) until its TTL (FR-009, FR-010). Only after the insert commits is
// the record completed, so a failed creation leaves the registration and its
// code in place to retry. Replaying a confirmation of a completed registration
// reports success without creating a second account (FR-011). A wrong/expired
// code is rejected and audited; an unknown reference is reported generically
// (FR-016).
func (s *Service) VerifyRegistrationOTP(ctx context.Context, in dto.VerifyRegistrationInput) error {
	reg, err := s.Registration.ByID(ctx, in.RegistrationID)
	if err != nil {
		return err
	}
	if reg.Status == model.RegistrationStatusCompleted {
		// This registration was already confirmed; the account exists. Report
		// success without a second insert and without discarding the completed
		// record, so the reference still cannot create a new account (FR-011).
		return nil
	}

	if err := s.Registration.VerifyOTP(ctx, in.RegistrationID, in.OTP); err != nil {
		// The code was already consumed by an earlier confirmation whose
		// response was lost. If the account exists, the confirmation already
		// succeeded and replaying it reports success without a second account
		// (FR-011). Otherwise the code really is expired.
		if errors.Is(err, domainerr.ErrOTPExpired) {
			if account, lookupErr := s.Users.ByEmail(ctx, reg.Email); lookupErr == nil && account != nil {
				return s.completeVerifiedRegistration(ctx, in.RegistrationID, account)
			}
		}
		s.Audit.Record(ctx, constant.AuditRegistrationFailed, constant.OutcomeFailure, nil, "", "registration", in.RegistrationID, nil)
		return err
	}

	account := &model.Account{
		ID:           uuid.New(),
		Email:        reg.Email,
		Username:     reg.Username,
		DisplayName:  reg.DisplayName,
		PasswordHash: reg.PasswordHash,
		Role:         access.RoleCustomer,
		Status:       constant.StatusActive,
	}
	if err := s.Users.Create(ctx, account); err != nil {
		// A unique violation means an account carrying this email or username
		// already exists. Look the registration's email up: when the account is
		// there, an earlier confirmation of this same registration created it,
		// so the repeat is a success; when it is not, the clash is another
		// registration's username and is reported as a conflict (FR-011,
		// FR-023). Any other failure keeps the temporary registration to retry.
		if errors.Is(err, domainerr.ErrEmailTaken) || errors.Is(err, domainerr.ErrUsernameTaken) {
			existing, lookupErr := s.Users.ByEmail(ctx, reg.Email)
			if lookupErr != nil {
				return err
			}
			if existing != nil {
				return s.completeVerifiedRegistration(ctx, in.RegistrationID, existing)
			}
			return domainerr.ErrUsernameTaken
		}
		return err
	}
	return s.completeVerifiedRegistration(ctx, in.RegistrationID, account)
}

// completeVerifiedRegistration marks the temporary registration completed
// (removing its code and email pointer, retaining the record until TTL) and
// records the verification. The completion happens only after the account is
// durably created, so a database failure never loses a registration that can be
// retried, and a retry after a completed registration stays idempotent
// (FR-010, FR-011).
func (s *Service) completeVerifiedRegistration(ctx context.Context, registrationID string, account *model.Account) error {
	if err := s.Registration.Complete(ctx, registrationID); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditEmailVerified, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// ResendRegistrationOTP replaces the code for a live registration after the
// resend cooldown, invalidating the previous code and resetting the attempt
// budget (FR-014, FR-015). The reference is loaded first so an unknown or
// expired one is reported generically, and a delivery failure keeps the
// registration, disarms the cooldown and answers the shared failure (FR-016,
// FR-024).
func (s *Service) ResendRegistrationOTP(ctx context.Context, in dto.RegistrationIDInput) error {
	reg, err := s.Registration.ByID(ctx, in.RegistrationID)
	if err != nil {
		return err
	}
	if reg.Status == model.RegistrationStatusCompleted {
		// A completed registration has no code to replace; stay generic so the
		// response reveals nothing about the reference (FR-016).
		return domainerr.ErrRegistrationNotFound
	}
	if err := s.Registration.CanResend(ctx, in.RegistrationID); err != nil {
		return err
	}
	attempts, err := s.issueRegistrationOTP(ctx, in.RegistrationID, reg.Email)
	if err != nil {
		return s.reportRegistrationDeliveryFailure(ctx, in.RegistrationID, attempts, err)
	}
	s.Audit.Record(ctx, constant.AuditRegistrationResent, constant.OutcomeSuccess, nil, string(access.RoleCustomer), "registration", in.RegistrationID, nil)
	return nil
}

// CancelRegistration removes a temporary registration and invalidates its
// reference and code (FR-017). It is idempotent and generic: an unknown or
// expired reference is a no-op so the response reveals nothing about whether the
// reference existed (FR-016). A completed registration's record is removed too;
// the durable account it produced is untouched.
func (s *Service) CancelRegistration(ctx context.Context, in dto.RegistrationIDInput) error {
	if _, err := s.Registration.ByID(ctx, in.RegistrationID); err != nil {
		if errors.Is(err, domainerr.ErrRegistrationNotFound) {
			return nil
		}
		return err
	}
	if err := s.Registration.Delete(ctx, in.RegistrationID); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditRegistrationCancelled, constant.OutcomeSuccess, nil, string(access.RoleCustomer), "registration", in.RegistrationID, nil)
	return nil
}

// derefString returns the pointed-to string, or "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// normalizeDisplayName trims an optional display name, returning nil when it is
// absent or blank so the created account keeps NULL rather than "".
func normalizeDisplayName(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
