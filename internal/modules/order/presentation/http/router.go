package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the `/orders` group. Mount it under `/api/v1`.
//
// The customer surface is addressed at `/orders` with no owner identifier: the
// owner is the session, so a client never names an order's owner (research D10,
// FR-020). Every route is behind the session guard, so a request without one is
// UNAUTHENTICATED. Checkout is the act of creating an order from the caller's
// cart; the list and detail are the caller's own orders, and cancel is the
// transition that returns an unpaid order's goods.
func (h *Handler) Router(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuthentication(hooks))
	r.Get("/", h.ListMine)
	r.Post("/", h.Checkout)
	r.Get("/{orderId}", h.GetMine)
	r.Post("/{orderId}/cancel", h.CancelMine)
	return r
}

// AdminRouter builds the `/admin/orders` group. Mount it under `/api/v1`.
//
// Every route is guarded by the administrator role (FR-023), and the denial is
// recorded through the foundation's OnDenied hook the composition root supplies,
// exactly as the other modules audit their own privilege denial. The operator
// lists and reads every order and advances fulfilment; the state moves are their
// own endpoints, because a transition is not a field edit and it can be refused
// (research D10).
func (h *Handler) AdminRouter(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
	r.Get("/", h.ListAll)
	r.Get("/{orderId}", h.GetByIDAdmin)
	r.Post("/{orderId}/confirm", h.ConfirmByAdmin)
	r.Post("/{orderId}/ship", h.ShipByAdmin)
	r.Post("/{orderId}/complete", h.CompleteByAdmin)
	r.Post("/{orderId}/transfer", h.Transfer)
	return r
}
