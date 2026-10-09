package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the `/cart` group. Mount it under `/api/v1`.
//
// The cart is a singleton resource addressed at `/cart` with no identifier: the
// owner is the session, so a client never names a cart and there is no cart or
// owner identifier on any route (research D7, FR-010). A line is addressed by its
// product identifier, because a cart holds at most one line per product. Every
// route is behind the session guard, so a request without one is UNAUTHENTICATED.
func (h *Handler) Router(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuthentication(hooks))
	r.Get("/", h.GetCart)
	r.Post("/items", h.AddItem)
	r.Patch("/items/{productId}", h.ChangeQuantity)
	r.Delete("/items/{productId}", h.RemoveItem)
	return r
}
