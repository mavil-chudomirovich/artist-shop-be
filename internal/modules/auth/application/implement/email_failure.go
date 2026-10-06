package implement

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

const (
	// deliveryResponseTarget is the target every request is answered inside
	// (specs/004-fix-pending-defects/plan.md, SC-006). The retry budget below is
	// sized against it rather than chosen independently of it, because FR-015
	// requires the time spent retrying to stay inside this figure.
	deliveryResponseTarget = 2 * time.Second

	// deliveryRetryBudget is the total wall-clock time one message may cost,
	// including every wait between attempts. It leaves room inside
	// deliveryResponseTarget for the request's own work and for the provider's
	// answer, so exhausting the budget still answers the customer promptly.
	deliveryRetryBudget = 1200 * time.Millisecond

	// deliveryMaxAttempts counts the first attempt as well as the retries.
	deliveryMaxAttempts = 3
)

// deliveryRetryWaits are the waits between attempts: one entry per retry, in
// order, and always shorter than the budget so waiting can never be what pushes
// a response past the target.
var deliveryRetryWaits = []time.Duration{100 * time.Millisecond, 400 * time.Millisecond}

// deliveryFailure is a refused message reduced to this system's own
// classification. It carries no provider wording, no status text and nothing the
// provider sent back, because both traces that record it must stay free of them
// (FR-012, FR-022).
type deliveryFailure struct {
	category string
	retry    bool
}

// disarmCooldownAfterFailedDelivery clears the resend marker that issuing a code
// armed, because the message it was armed for was never delivered. Every path
// that issues a code and then sends it goes through here, registration and
// resend alike: a marker that records "a message was sent recently" must never
// survive a send that did not happen (FR-024). The code, its lifetime and the
// brute-force counters are untouched, because none of them belongs to delivery
// (FR-018).
//
// accountID identifies the account in the diagnostic, by its reference rather
// than by restating the recipient address (FR-012). A disarm that itself fails
// only leaves the customer inside a cooldown they cannot see, so it is recorded
// and does not replace the error the caller is already returning.
func (s *Service) disarmCooldownAfterFailedDelivery(ctx context.Context, email, accountID string) {
	if err := s.OTP.DisarmCooldown(ctx, email); err != nil {
		logging.WithCorrelation(ctx, s.log()).ErrorContext(ctx, "resend cooldown could not be disarmed after a failed delivery",
			slog.String("accountId", accountID))
	}
}

// classifyDeliveryFailure maps a sender error onto the module's classification.
//
// The classification is read from the error's status category alone. The
// provider's own sentence is never inspected: providers rewrite their copy
// between releases, and keying on it turns a permanent failure into a retried
// one the moment a copy edit lands, which slows every request that hits it
// (FR-017).
//
// An error that carries no category this system recognises is classified
// UNKNOWN and is not retried. Retrying a failure we cannot classify would spend
// the response budget without evidence that it could help, and the customer
// cannot tell the two apart anyway.
func classifyDeliveryFailure(err error) deliveryFailure {
	switch {
	case errors.Is(err, domainerr.ErrDeliveryTransient):
		return deliveryFailure{category: constant.DeliveryTransient, retry: true}
	case errors.Is(err, domainerr.ErrDeliveryUnreachable):
		return deliveryFailure{category: constant.DeliveryUnreachable, retry: true}
	case errors.Is(err, domainerr.ErrDeliveryConfiguration):
		return deliveryFailure{category: constant.DeliveryConfiguration}
	case errors.Is(err, domainerr.ErrDeliveryRefused):
		return deliveryFailure{category: constant.DeliveryRefused}
	default:
		return deliveryFailure{category: constant.DeliveryUnknown}
	}
}

// sendWithRetry delivers one message, retrying only a failure this system has
// classified as one a retry can resolve. It returns the number of attempts made
// and the last failure.
//
// Two bounds apply, and both are needed: deliveryMaxAttempts caps how often the
// provider is asked, and deliveryRetryBudget caps how long the request may spend
// asking. The second is what keeps the answer inside the documented response
// target even when the provider answers slowly (FR-015). A failure classified as
// a configuration problem is never retried, because retrying cannot change the
// outcome (FR-016).
func (s *Service) sendWithRetry(ctx context.Context, send func(context.Context) error) (int, error) {
	start := s.clock()
	var lastErr error

	for attempt := 1; attempt <= deliveryMaxAttempts; attempt++ {
		if attempt > 1 {
			wait := deliveryRetryWaits[attempt-2]
			if s.clock().Sub(start)+wait > deliveryRetryBudget {
				return attempt - 1, lastErr
			}
			if err := s.wait(ctx, wait); err != nil {
				return attempt - 1, lastErr
			}
		}
		if deliveryRetryBudget-s.clock().Sub(start) <= 0 {
			return attempt - 1, lastErr
		}
		attemptCtx, cancel := context.WithTimeout(ctx, deliveryRetryBudget-s.clock().Sub(start))
		err := send(attemptCtx)
		cancel()
		if err == nil {
			return attempt, nil
		}
		lastErr = err
		if !classifyDeliveryFailure(err).retry {
			return attempt, err
		}
	}
	return deliveryMaxAttempts, lastErr
}

// clock reports the current time. Tests replace it so the budget can be
// exhausted deterministically instead of by waiting.
func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// wait sleeps for d, or returns early when the request itself is finished.
func (s *Service) wait(ctx context.Context, d time.Duration) error {
	if s.sleep != nil {
		s.sleep(d)
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
