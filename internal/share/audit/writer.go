package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// Store is the persistence dependency of the writer.
type Store interface {
	Insert(ctx context.Context, e Event) error
}

// Writer consumes audit events from a bounded queue and persists them with
// idempotent retry. Enqueueing never blocks the caller.
type Writer struct {
	store      Store
	logger     *slog.Logger
	queue      chan Event
	maxRetries int
	workers    int
	wg         sync.WaitGroup
}

// NewWriter creates an audit writer.
func NewWriter(store Store, logger *slog.Logger, queueSize, maxRetries, workers int) *Writer {
	if queueSize <= 0 {
		queueSize = 1024
	}
	if workers <= 0 {
		workers = 1
	}
	return &Writer{
		store:      store,
		logger:     logger,
		queue:      make(chan Event, queueSize),
		maxRetries: maxRetries,
		workers:    workers,
	}
}

// Start launches the worker pool. Workers exit when ctx is cancelled.
func (w *Writer) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go w.loop(ctx)
	}
}

// Emit enqueues an event without blocking the business operation. When the
// queue is full the event is dropped and the condition is made observable.
func (w *Writer) Emit(ctx context.Context, e Event) {
	if e.EventID == uuid.Nil {
		e.EventID = uuid.New()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	if e.CorrelationID == "" {
		e.CorrelationID = reqctx.CorrelationID(ctx)
	}
	select {
	case w.queue <- e:
	default:
		w.logger.ErrorContext(ctx, "audit queue full; event dropped",
			slog.String("action", e.Action),
			slog.String("event_id", e.EventID.String()),
		)
	}
}

// Stop waits for the queue to drain or for ctx to expire.
func (w *Writer) Stop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if len(w.queue) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Writer) loop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-w.queue:
			w.deliver(ctx, e)
		}
	}
}

func (w *Writer) deliver(ctx context.Context, e Event) {
	backoff := 50 * time.Millisecond
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		err := w.store.Insert(ctx, e)
		if err == nil {
			if attempt > 0 {
				w.logger.InfoContext(ctx, "audit event persisted after retry",
					slog.String("event_id", e.EventID.String()),
					slog.Int("attempts", attempt+1),
				)
			}
			return
		}
		w.logger.WarnContext(ctx, "audit write failed; will retry",
			slog.String("event_id", e.EventID.String()),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	w.logger.ErrorContext(ctx, "audit event dropped after retries; manual reconciliation required",
		slog.String("event_id", e.EventID.String()),
		slog.String("action", e.Action),
	)
}
