package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the `/orders` group. Mount it under `/api/v1`.
//
// The customer surface is addressed at `/orders` with no owner identifier: the
// owner is the session, so a client never names an order's owner (research D10,
// FR-020). Every route is behind the session guard, so a request without one is
// UNAUTHENTICATED. Checkout is the act of creating an order from the caller's
// cart.
func (h *Handler) Router(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuthentication(hooks))
	r.Post("/", h.Checkout)
	return r
}
