package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

func TestCorrelationGeneratesWhenMissing(t *testing.T) {
	var captured string
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = reqctx.CorrelationID(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get(CorrelationHeader) == "" {
		t.Fatal("expected generated correlation header")
	}
	if captured == "" || captured != rec.Header().Get(CorrelationHeader) {
		t.Fatalf("context id %q does not match header %q", captured, rec.Header().Get(CorrelationHeader))
	}
}

func TestCorrelationPropagatesValidValue(t *testing.T) {
	const incoming = "123e4567-e89b-12d3-a456-426614174000"
	handler := Correlation(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CorrelationHeader, incoming)
	handler.ServeHTTP(rec, req)

	if rec.Header().Get(CorrelationHeader) != incoming {
		t.Fatalf("expected propagated id, got %q", rec.Header().Get(CorrelationHeader))
	}
}

func TestCorrelationConcurrentIsolation(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the context value so each response can be checked against its header.
		w.Header().Set("X-Context-Id", reqctx.CorrelationID(r.Context()))
	}))

	const requests = 64
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		seen   = make(map[string]struct{}, requests)
		failed []string
	)

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			headerID := rec.Header().Get(CorrelationHeader)
			contextID := rec.Header().Get("X-Context-Id")

			mu.Lock()
			defer mu.Unlock()
			if headerID == "" || headerID != contextID {
				failed = append(failed, "header/context mismatch: "+headerID+" != "+contextID)
				return
			}
			if _, dup := seen[headerID]; dup {
				failed = append(failed, "duplicate correlation id: "+headerID)
				return
			}
			seen[headerID] = struct{}{}
		}()
	}
	wg.Wait()

	if len(failed) > 0 {
		t.Fatalf("correlation isolation failures (%d): %v", len(failed), failed)
	}
	if len(seen) != requests {
		t.Fatalf("expected %d unique correlation ids, got %d", requests, len(seen))
	}
}

func TestCorrelationReplacesMalformedValue(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CorrelationHeader, "not-a-uuid")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(CorrelationHeader); got == "not-a-uuid" || got == "" {
		t.Fatalf("expected replacement UUID, got %q", got)
	}
}
