package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultInterval is how often the sweeper cancels expired orders. Fifteen
// minutes is the order's hold window; a sweep every thirty seconds keeps eventual
// cancellation tight without turning the loop into a busy wait (research D6).
const DefaultInterval = 30 * time.Second

// ExpireUseCase is the one application capability the sweeper drives. Declaring
// it here rather than importing the module's service keeps the worker free of any
// business rule: the sweeper only knows how to call it on a schedule.
type ExpireUseCase interface {
	// ExpireOrders cancels every awaiting-payment order whose window has passed
	// and returns its goods.
	ExpireOrders(ctx context.Context) error
}

// Sweeper cancels expired orders on a fixed interval. It carries no business rule
// of its own: it calls the expiry use case every tick, one sweep at a time, and
// stops when its context is cancelled (FR-012, research D6).
type Sweeper struct {
	expire   ExpireUseCase
	interval time.Duration
	logger   *slog.Logger
	wg       sync.WaitGroup
}

// New creates a sweeper on the default interval.
func New(expire ExpireUseCase, logger *slog.Logger) *Sweeper {
	return NewWithInterval(expire, DefaultInterval, logger)
}

// NewWithInterval creates a sweeper on an explicit interval. It is what the
// composition root uses if it ever needs a different period, and what the tests
// use to drive real ticks in milliseconds; a non-positive interval falls back to
// the default rather than spinning.
func NewWithInterval(expire ExpireUseCase, interval time.Duration, logger *slog.Logger) *Sweeper {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{expire: expire, interval: interval, logger: logger}
}

// Start runs the ticker loop until ctx is cancelled. It blocks, so the
// composition root runs it in its own goroutine; a shutdown cancels the same
// context the server and the audit writer share, which stops this loop too.
//
// Each sweep runs synchronously, so a slow sweep cannot overlap the next tick: a
// tick that arrives during a sweep is dropped by the ticker rather than starting
// a second one.
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

// sweep runs one pass of the expiry use case. A failure is logged rather than
// propagated: a transient storage error must not kill the loop, and the next tick
// retries. A cancellation is expected during shutdown and is not logged.
func (s *Sweeper) sweep(ctx context.Context) {
	if err := s.expire.ExpireOrders(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.ErrorContext(ctx, "order expiry sweep failed", slog.String("error", err.Error()))
	}
}
