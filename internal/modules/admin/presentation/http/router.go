package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// AdminRouter builds the `/admin/dashboard` group. Mount it under `/api/v1`.
//
// The whole group is guarded by the administrator role (FR-001), and the denial
// is recorded through the foundation's OnDenied hook the composition root
// supplies, exactly as the other administrator groups do — this group adds no
// second audit mechanism. The route is static: there is no resource identifier
// and so no not-found case.
func (h *Handler) AdminRouter(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
	r.Get("/overview", h.Overview)
	return r
}
