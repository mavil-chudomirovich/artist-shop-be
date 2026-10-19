package httpapi

import (
	"net/http"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/sse"
)

// streamsHub is the subscribe side of the shared SSE hub the stream handlers
// need. It keeps presentation decoupled from the hub's publish side.
type streamsHub interface {
	Subscribe(topic string) (<-chan sse.Event, func())
}

// SetStreams attaches the shared SSE hub the stream handlers subscribe to. It
// returns the handler so the composition root can chain the call. A handler
// without a hub answers its streams with 503, so a missing wire is visible
// instead of hanging a connection that never receives anything.
func (h *Handler) SetStreams(hub streamsHub) *Handler {
	h.streams = hub
	return h
}

// StreamMine subscribes the signed-in customer to their own order status
// changes. The topic is the session's subject, never a request value, so one
// customer can never receive another's events (FR-004).
//
// The client reads with fetch + ReadableStream and the Authorization header it
// already uses for REST; EventSource cannot carry the header, so it is not
// supported. Reconnecting re-opens the stream and re-reads GET /orders, because
// the server keeps no replay (specs/020-order-sse-stream).
//
//	@Summary		Subscribe to my order status changes (SSE)
//	@Description	Opens a live SSE stream of order.status_changed for the signed-in customer's own orders. Read with fetch + ReadableStream and the Authorization header; on reconnect, re-open the stream and re-read GET /orders.
//	@Tags			Orders
//	@Produce		text/event-stream
//	@Security		BearerAuth
//	@Success		200	{string}	string	"text/event-stream"
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		429	{object}	httpx.SwaggerError
//	@Failure		503	{object}	httpx.SwaggerError
//	@Router			/orders/stream [get]
func (h *Handler) StreamMine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	if h.streams == nil {
		h.logger.Error("order SSE stream requested without a hub wired")
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnavailable), h.logger)
		return
	}
	ch, unsub := h.streams.Subscribe(appinterface.TopicForUser(actor.ID))
	defer unsub()
	sse.ServeStream(w, r, ch, h.logger)
}

// StreamAdmin subscribes an administrator to the shop-wide order notifications:
// needs_confirmation when an order enters PENDING and feedback_received when a
// customer submits feedback. The route guard already requires ADMIN; the topic
// is fixed, never a request value.
//
//	@Summary		Subscribe to admin order notifications (SSE)
//	@Description	Opens a live SSE stream of order.needs_confirmation and order.feedback_received for administrators. Read with fetch + ReadableStream and the Authorization header; on reconnect, re-open the stream and re-read GET /admin/orders.
//	@Tags			AdminOrders
//	@Produce		text/event-stream
//	@Security		BearerAuth
//	@Success		200	{string}	string	"text/event-stream"
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Failure		429	{object}	httpx.SwaggerError
//	@Failure		503	{object}	httpx.SwaggerError
//	@Router			/admin/orders/stream [get]
func (h *Handler) StreamAdmin(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessionActor(w, r); !ok {
		return
	}
	if h.streams == nil {
		h.logger.Error("admin order SSE stream requested without a hub wired")
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnavailable), h.logger)
		return
	}
	ch, unsub := h.streams.Subscribe(appinterface.TopicAdmins)
	defer unsub()
	sse.ServeStream(w, r, ch, h.logger)
}
