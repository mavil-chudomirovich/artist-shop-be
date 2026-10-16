package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// AdminRouter builds the `/admin/inventory` group. Mount it under `/api/v1`.
//
// Every route is guarded by the administrator role (FR-005), and the denial is
// recorded through the foundation's OnDenied hook the composition root supplies,
// exactly as the other modules audit their own privilege denials. The resource is
// one variant's stock, addressed by the variant identifier an administrator
// already holds from the product detail (research D8).
func (h *Handler) AdminRouter(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
	r.Get("/{variantId}", h.GetStock)
	r.Get("/{variantId}/movements", h.GetMovements)
	r.Post("/{variantId}/restock", h.Restock)
	r.Post("/{variantId}/damage", h.Damage)
	r.Post("/{variantId}/adjustment", h.Adjustment)
	return r
}
