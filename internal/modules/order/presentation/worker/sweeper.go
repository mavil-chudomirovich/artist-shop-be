package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultInterval is how often the sweeper runs the order module's background
// sweeps. Sixty minutes is the order's payment window; a sweep every thirty
// seconds keeps eventual cancellation tight without turning the loop into a busy
// wait (research D5).
const DefaultInterval = 30 * time.Second

// SweepUseCases is the application capability the sweeper drives: both of the
// order module's background sweeps. Declaring it here rather than importing the
// module's service keeps the worker free of any business rule: the sweeper only
// knows how to call these on a schedule.
type SweepUseCases interface {
	// ExpireOrders cancels every awaiting-payment order whose window has passed
	// and returns its goods.
	ExpireOrders(ctx context.Context) error
	// AutoCompleteDelivered completes every delivered order whose five-day
	// window has passed, with no acting account.
	AutoCompleteDelivered(ctx context.Context) error
}

// Sweeper runs the order module's background sweeps on a fixed interval. It
// carries no business rule of its own: on every tick it calls the expiry sweep and
// then the completion sweep, one tick at a time, and stops when its context is
// cancelled (FR-008, FR-012, research D6).
type Sweeper struct {
	sweeps   SweepUseCases
	interval time.Duration
	logger   *slog.Logger
	wg       sync.WaitGroup
}

// New creates a sweeper on the default interval.
func New(sweeps SweepUseCases, logger *slog.Logger) *Sweeper {
	return NewWithInterval(sweeps, DefaultInterval, logger)
}

// NewWithInterval creates a sweeper on an explicit interval. It is what the
// composition root uses if it ever needs a different period, and what the tests
// use to drive real ticks in milliseconds; a non-positive interval falls back to
// the default rather than spinning.
func NewWithInterval(sweeps SweepUseCases, interval time.Duration, logger *slog.Logger) *Sweeper {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{sweeps: sweeps, interval: interval, logger: logger}
}

// Start runs the ticker loop until ctx is cancelled. It blocks, so the
// composition root runs it in its own goroutine; a shutdown cancels the same
// context the server and the audit writer share, which stops this loop too.
//
// Each tick runs its sweeps synchronously, so a slow sweep cannot overlap the next
// tick: a tick that arrives during a sweep is dropped by the ticker rather than
// starting a second one.
func (s *Sweeper) Start(ctx context.Context) {
	s.wg.Add(1)
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// Stop waits for the loop Start runs to return. It is called after the context
// has been cancelled, so it joins the loop rather than cutting it short.
func (s *Sweeper) Stop() { s.wg.Wait() }

// sweep runs one pass of each sweep the order module needs: the expiry sweep, then
// the completion sweep. Both are idempotent, so a repeated tick changes nothing a
// previous one already applied. A failure is logged rather than propagated — a
// transient storage error must not kill the loop — and the sweeps run
// independently, so a failure of one does not stop the other and the next tick
// retries both. A cancellation is expected during shutdown and is not logged.
func (s *Sweeper) sweep(ctx context.Context) {
	s.runSweep(ctx, "order expiry sweep failed", s.sweeps.ExpireOrders)
	s.runSweep(ctx, "order completion sweep failed", s.sweeps.AutoCompleteDelivered)
}

// runSweep runs one sweep, logging (never propagating) a failure so the caller can
// carry on to the next sweep and the loop can carry on to the next tick.
func (s *Sweeper) runSweep(ctx context.Context, message string, sweep func(context.Context) error) {
	if err := sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.ErrorContext(ctx, message, slog.String("error", err.Error()))
	}
}
