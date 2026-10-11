package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Router builds the payment customer routes. It is mounted once at
// `/<orders>/{orderId}/payment` by the composition root: the routes are relative
// (`POST /` and `GET /`), and the handler reads `orderId` with chi.URLParam from
// the mount pattern, so the sub-resource always names the order it belongs to
// (research D12, plan.md "On the route mount").
//
// Every route is behind the session guard, so a request without one is
// UNAUTHENTICATED; the acting customer always comes from the session, never from
// the request (FR-003, FR-017).
func (h *Handler) Router(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuthentication(hooks))
	r.Post("/", h.StartPayment)
	r.Get("/", h.ReadPayment)
	return r
}
