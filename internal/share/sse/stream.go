package sse

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// heartbeatInterval keeps NATs and proxies from closing an idle SSE stream.
// Browsers and fetch readers ignore comment lines, so this carries no payload.
const heartbeatInterval = 25 * time.Second

// SetHeaders applies the SSE response headers. X-Accel-Buffering tells nginx
// not to buffer the stream, or events wait for the buffer to fill.
func SetHeaders(w http.Header) {
	w.Set("Content-Type", "text/event-stream")
	w.Set("Cache-Control", "no-cache")
	w.Set("Connection", "keep-alive")
	w.Set("X-Accel-Buffering", "no")
}

// ServeStream pumps subscribed events to one HTTP response until the client
// goes away. It returns when the request context ends; the caller MUST have
// subscribed before calling and MUST unsubscribe after it returns.
func ServeStream(w http.ResponseWriter, r *http.Request, ch <-chan Event, logger *slog.Logger) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	SetHeaders(w.Header())
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	// The logger is kept so a future change can report abnormal write failures
	// with the request correlation; normal disconnects are silent by design.
	_ = logger
	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\n", evt.Type); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", evt.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
