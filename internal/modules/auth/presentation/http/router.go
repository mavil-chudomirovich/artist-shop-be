package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// New creates the auth HTTP handler. The auditor is required: without it the
// privilege-denial record required by FR-014 would be silently skipped.
func New(svc appinterface.AuthService, auditor appinterface.Auditor, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, auditor: auditor, logger: logger}
}

// Router builds the `/auth` sub-router. Mount it under `/api/v1`.
func (h *Handler) Router(cfg config.AuthConfig) http.Handler {
	r := chi.NewRouter()

	loginLimit := middleware.RateLimitWindow(cfg.LoginRatePerMinute, time.Minute, h.logger)
	flowLimit := middleware.RateLimitWindow(cfg.FlowRatePerMinute, time.Minute, h.logger)

	r.Group(func(r chi.Router) {
		r.Use(flowLimit)
		r.Post("/register", h.Register)
		r.Post("/verify-email", h.VerifyEmail)
		r.Post("/resend-verification", h.ResendVerification)
		r.Post("/password/forgot", h.ForgotPassword)
		r.Post("/password/reset", h.ResetPassword)
	})

	r.With(loginLimit).Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)

	hooks := h.AuthHooks()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuthentication(hooks))
		r.Get("/me", h.Me)
		r.Post("/password/change", h.ChangePassword)
	})
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
		r.Get("/admin/probe", h.AdminProbe)
	})

	return r
}

// AuthHooks supplies the foundation's authentication hook. Token failures are
// mapped to the module's documented codes instead of a generic 401.
func (h *Handler) AuthHooks() middleware.AuthHooks {
	hooks := middleware.AuthHooks{
		Authenticate: func(ctx context.Context, r *http.Request) (*middleware.Identity, error) {
			raw := bearerToken(r)
			if raw == "" {
				return nil, nil
			}
			claims, err := h.svc.VerifyAccessToken(ctx, raw)
			if err != nil {
				return nil, mapError(err)
			}
			return &middleware.Identity{Subject: claims.Subject.String(), Role: string(claims.Role), TokenID: claims.ID}, nil
		},
	}
	if h.auditor != nil {
		hooks.OnDenied = func(ctx context.Context, id middleware.Identity, r *http.Request) {
			var actor *uuid.UUID
			if parsed, err := uuid.Parse(id.Subject); err == nil {
				actor = &parsed
			}
			h.auditor.Record(ctx, constant.AuditPrivilegeDenied, constant.OutcomeFailure, actor, id.Role, "route", r.URL.Path, nil)
		}
	}
	return hooks
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
