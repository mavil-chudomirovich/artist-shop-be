package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// RateLimit enforces an in-process token bucket per client IP and route class,
// with the configured requests-per-second and burst (FR-021). No external store is
// used.
func RateLimit(rps, burst int, logger *slog.Logger) func(http.Handler) http.Handler {
	return rateLimit(rate.Limit(rps), burst, logger)
}

// RateLimitWindow limits one client IP and route class to a number of requests per
// time window. It is a convenience wrapper over RateLimit for per-minute style
// thresholds.
func RateLimitWindow(requests int, window time.Duration, logger *slog.Logger) func(http.Handler) http.Handler {
	if window <= 0 {
		window = time.Minute
	}
	if requests <= 0 {
		requests = 1
	}
	return rateLimit(rate.Limit(float64(requests)/window.Seconds()), requests, logger)
}

func rateLimit(limit rate.Limit, burst int, logger *slog.Logger) func(http.Handler) http.Handler {
	type visitor struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	var (
		mu       sync.Mutex
		visitors = make(map[string]*visitor)
	)

	// Evict idle limiter keys to bound memory.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			for key, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, key)
				}
			}
			mu.Unlock()
		}
	}()

	get := func(key string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		v, ok := visitors[key]
		if !ok {
			v = &visitor{limiter: rate.NewLimiter(limit, burst)}
			visitors[key] = v
		}
		v.lastSeen = time.Now()
		return v.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r) + "|" + routeClass(r)
			if !get(key).Allow() {
				logger.WarnContext(r.Context(), "rate limit exceeded", slog.String("client", key))
				w.Header().Set("Retry-After", "1")
				httpx.WriteError(w, r, httpx.New(httpx.CodeRateLimited), logger)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// routeClass buckets requests so reads and writes (and authentication traffic)
// are limited independently per client IP (FR-021).
func routeClass(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		return "auth"
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return "write"
	default:
		return "read"
	}
}
