package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the public `/products` group. Mount it under `/api/v1`.
//
// Both routes are readable without a session (FR-001). The list is at the group
// root and the detail is addressed by slug, because the slug is what a
// customer-facing link is built from (research D11); the administrator surface,
// which is addressed by identifier, is a separate group added with US2.
func (h *Handler) Router(_ middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.ListPublic)
	r.Get("/{slug}", h.GetPublicBySlug)
	return r
}
