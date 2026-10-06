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

// Register creates a pending account and emails an OTP. While delivery works, an
// address that already has an account keeps answering the ordinary accepted
// response, so the endpoint cannot be used to discover whether an email exists
// (FR-014). When delivery fails, both answers become the same failure, because a
// difference between them would let an unauthenticated caller enumerate the
// registered addresses for as long as the outage lasts (FR-008a).
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
		if !errors.Is(err, domainerr.ErrEmailTaken) {
			return err
		}
		return s.deliverToExistingAccount(ctx, normalized)
	}
	attempts, err := s.sendOTP(ctx, normalized)
	if err != nil {
		return s.reportDeliveryFailure(ctx, account, true, attempts, err)
	}
	s.Audit.Record(ctx, constant.AuditRegister, constant.OutcomeSuccess, &account.ID, string(account.Role), "user", account.ID.String(), nil)
	return nil
}

// deliverToExistingAccount answers a registration whose address already has an
// account. It runs the same delivery the new-account path runs and reports the
// same failure through the same helper, so the two answers cannot drift apart
// again (FR-008a). Nothing is created here and nothing stored is touched: the
// stored account keeps its identifier, its password and its state (FR-008).
//
// Three cases, all silent unless the delivery itself fails:
//
//   - The address cannot be found, which happens when the row disappeared between
//     the refused insert and this read. There is nothing to send to.
//   - The account is no longer pending, so it is already confirmed or no longer
//     usable and needs no code. Sending one would change behaviour nobody asked
//     for, and the answer stays the ordinary accepted one.
//   - The account is still pending, so the customer is exactly where a failed
//     delivery left them: a code was never delivered and one can be requested
//     again. Re-sending is the same recovery the resend endpoint offers.
func (s *Service) deliverToExistingAccount(ctx context.Context, normalized string) error {
	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		// Mirrors ResendVerification: an address nobody holds is answered exactly
		// like one that does not exist, so this read cannot become an oracle.
		if errors.Is(err, domainerr.ErrUserNotFound) {
			return nil
		}
		return err
	}
	if account.Status != constant.StatusPending {
		return nil
	}
	attempts, err := s.sendOTP(ctx, normalized)
	if err != nil {
		return s.reportDeliveryFailure(ctx, account, false, attempts, err)
	}
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
// (FR-010, FR-011). Every registration that cannot deliver goes through here, so
// both registration branches answer the identical failure whatever the address
// was (FR-008a).
//
// The account is deliberately kept. It was committed before the send was
// attempted, its pending state is the state the resend flow already operates on,
// and deleting it would discard a registration the customer can still finish on
// their own once delivery recovers (FR-007). A failed delivery never writes the
// success outcome: the row states plainly that the event did not complete
// (FR-013).
//
// created states whether this request is the one that created the account. Both
// branches report a failure for an account, and only one of them created it, so
// the operator-facing line says which - otherwise a failed duplicate is
// indistinguishable from a failed registration at the point where an operator
// decides what to do. It is recorded on the log line only: the audit metadata
// carries the classification and nothing else
// (specs/004-fix-pending-defects/data-model.md), and the action names the
// registration event that did not complete rather than a creation, which is true
// on both branches.
//
// The traces name a registration, so the resend path, which leaves traces of its
// own kind, shares only the cooldown disarm this flow needs and not these two.
func (s *Service) reportDeliveryFailure(ctx context.Context, account *model.Account, created bool, attempts int, err error) error {
	failure := classifyDeliveryFailure(err)
	logger := logging.WithCorrelation(ctx, s.log())

	s.disarmCooldownAfterFailedDelivery(ctx, account.Email, account.ID.String())

	s.Audit.Record(ctx, constant.AuditRegisterDeliveryFailed, constant.OutcomeFailure, nil, "", "user", account.ID.String(),
		map[string]any{"classification": failure.category})

	logger.ErrorContext(ctx, "verification message could not be delivered",
		slog.String("accountId", account.ID.String()),
		slog.String("deliveryFailure", failure.category),
		slog.Bool("accountCreated", created),
		slog.Int("deliveryAttempts", attempts))

	return domainerr.ErrVerificationDeliveryFailed
}
