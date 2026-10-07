package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the public `/categories` group. Mount it under `/api/v1`.
//
// Both routes are readable without a session (FR-001, FR-014). The detail is
// addressed by slug because the slug is what a customer-facing link is built
// from (research D7); the administrator surface, which is addressed by
// identifier, is the separate AdminRouter group below.
func (h *Handler) Router(_ middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.ListPublic)
	r.Get("/{slug}", h.GetPublicBySlug)
	return r
}

// AdminRouter builds the `/admin/categories` group. Mount it under `/api/v1`.
//
// Every route is guarded by the administrator role (FR-014), and the denial is
// recorded through the foundation's OnDenied hook the composition root supplies,
// exactly as module 01 audits its own privilege denial — this group adds no
// second audit mechanism. The surface is addressed by identifier because the
// operator reaches it from the administrator list and an identifier cannot change
// under a client that is mid-edit (research D7).
func (h *Handler) AdminRouter(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
	r.Get("/", h.ListAdmin)
	r.Post("/", h.CreateCategory)
	r.Get("/{categoryId}", h.GetAdmin)
	r.Patch("/{categoryId}", h.UpdateCategory)
	r.Delete("/{categoryId}", h.DeleteCategory)
	return r
}
