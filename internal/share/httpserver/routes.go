package httpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/health"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Dependencies are the collaborators required to build the router.
type Dependencies struct {
	Config  *config.Config
	Logger  *slog.Logger
	DB      *database.DB
	Cache   health.Pinger
	Auth    middleware.AuthHooks
	Version func(ctx context.Context) (int64, error)
	// Mount registers module sub-routers under /api/v1.
	Mount func(r chi.Router)
}

// NewRouter builds the shared router with the foundation middleware pipeline.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP(deps.Config.HTTP.TrustedProxies))
	r.Use(middleware.Correlation)
	r.Use(middleware.RequestLogger(deps.Logger))
	r.Use(middleware.Recovery(deps.Logger))
	r.Use(middleware.CORS(deps.Config.CORS.AllowedOrigins))
	r.Use(middleware.BodyLimit(deps.Config.MaxBodyBytes))
	r.Use(middleware.JSONContentType)
	// Resolve the caller identity when possible. Enforcement is applied per
	// route by business modules via RequireAuthentication / RequireRole.
	r.Use(middleware.Authentication(deps.Auth))

	healthHandler := health.New(deps.DB, deps.Version).WithCache(deps.Cache)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.RateLimit(
			deps.Config.RateLimit.RequestsPerSecond,
			deps.Config.RateLimit.Burst,
			deps.Logger,
		))
		if deps.Mount != nil {
			deps.Mount(r)
		}
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.New(httpx.CodeNotFound), deps.Logger)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMethodNotAllowed), deps.Logger)
	})

	return r
}
