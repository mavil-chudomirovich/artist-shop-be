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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// NewWithAuditor creates a handler with a privilege-denial auditor.
func NewWithAuditor(svc appinterface.AuthService, auditor appinterface.Auditor, logger *slog.Logger) *Handler {
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
		r.Use(h.requireAdmin(hooks))
		r.Get("/admin/probe", h.AdminProbe)
	})

	return r
}

// AuthHooks supplies the foundation's authentication hook.
func (h *Handler) AuthHooks() middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(ctx context.Context, r *http.Request) (*middleware.Identity, error) {
			raw := bearerToken(r)
			if raw == "" {
				return nil, nil
			}
			claims, err := h.svc.VerifyAccessToken(ctx, raw)
			if err != nil {
				return nil, err
			}
			return &middleware.Identity{Subject: claims.Subject.String(), Role: string(claims.Role), TokenID: claims.ID}, nil
		},
	}
}

// requireAdmin enforces the ADMIN role and records privilege denials (FR-014).
func (h *Handler) requireAdmin(hooks middleware.AuthHooks) func(http.Handler) http.Handler {
	authenticated := middleware.RequireAuthentication(hooks)
	return func(next http.Handler) http.Handler {
		return authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, _ := middleware.IdentityFromContext(r.Context())
			if identity.Role != string(access.RoleAdmin) {
				if h.auditor != nil {
					var actor *uuid.UUID
					if id, err := uuid.Parse(identity.Subject); err == nil {
						actor = &id
					}
					h.auditor.Record(r.Context(), constant.AuditPrivilegeDenied, constant.OutcomeFailure, actor, identity.Role, "route", r.URL.Path, nil)
				}
				httpx.WriteError(w, r, httpx.New(httpx.CodeForbidden), h.logger)
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
