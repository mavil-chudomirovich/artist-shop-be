package httpapi

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// deliveryEvidenceUploadOverhead is the room the evidence routes leave for the
// multipart envelope — part headers, boundaries and the form field name — on top
// of the photo ceiling. Each photo is capped again while it is read, so this only
// has to be generous, never exact (the product module's rule).
const deliveryEvidenceUploadOverhead = 64 << 10

// DeliveryEvidenceUploadCeiling is the body ceiling the dispatch and delivery
// routes install: five photos at the documented image ceiling plus the room the
// multipart envelope needs. The routes and the handler read the same domain
// constants, so the limit a route applies cannot drift from the rule the use case
// enforces (FR-006, FR-008, FR-009).
//
// It is exported because the composition root has to compare the shared pipeline
// ceiling against it before it wires anything, and the two must be the same
// number. The route below and RequireDeliveryEvidenceUploadCeiling both read this
// function, so the check cannot drift away from the limit the route really
// installs (research D4, mirroring userhttp.AvatarUploadCeiling).
func DeliveryEvidenceUploadCeiling() int64 {
	return int64(model.MaxDeliveryPhotoBytes)*int64(model.MaxDeliveryPhotos) + deliveryEvidenceUploadOverhead
}

// RequireDeliveryEvidenceUploadCeiling refuses a startup whose shared pipeline
// ceiling is below the ceiling the dispatch and delivery routes install, and
// returns nil otherwise.
//
// The shared wrapper runs before routing, so a ceiling under these routes' own
// means every five-photo dispatch is refused by the pipeline first, with a reason
// that does not point at the setting an operator has to change. Refusing to start
// turns that into one message that does (FR-006, FR-009; mirroring
// userhttp.RequireAvatarUploadCeiling, research D2).
//
// The boundary is deliberate: strictly below refuses, equal starts. Equal is safe
// because the route's own ceiling is the binding constraint at that point, and
// refusing it would block a configuration that works.
//
// mediaConfigured reports whether the provider credentials are present. Without
// them no evidence photo can be stored at all, so the ceiling is irrelevant and
// the startup is allowed: the composition decides how to report that consequence.
//
// The function is pure — it reads no configuration and touches no infrastructure —
// so the composition root can call it before it opens anything and a unit test can
// cover every branch without a database.
func RequireDeliveryEvidenceUploadCeiling(sharedBodyCeilingBytes, deliveryCeilingBytes int64, mediaConfigured bool) error {
	if !mediaConfigured || sharedBodyCeilingBytes >= deliveryCeilingBytes {
		return nil
	}
	// The message carries no credential: it receives only two byte counts and a
	// boolean, so there is nothing secret for it to leak (Constitution V, VI).
	return fmt.Errorf(
		"MAX_BODY_BYTES is %d bytes but a delivery-evidence upload needs at least %d bytes: "+
			"the shared request-size ceiling wraps the body before routing, so every five-photo dispatch would be "+
			"refused before the ship route could apply its own limit. "+
			"Raise MAX_BODY_BYTES to at least %d bytes, or start without media credentials",
		sharedBodyCeilingBytes, deliveryCeilingBytes, deliveryCeilingBytes)
}

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
	// The live stream is a fixed segment, so it sits ahead of the orderId
	// parameter routes; chi prefers static segments, but the order keeps the
	// intent explicit.
	r.Get("/stream", h.StreamMine)
	r.Get("/{orderId}", h.GetMine)
	r.Put("/{orderId}", h.EditMine)
	r.Post("/{orderId}/cancel", h.CancelMine)
	// The owner confirms receipt of a delivered order; the owner is the session's,
	// so a client can only complete its own order (FR-011, FR-018).
	r.Post("/{orderId}/complete", h.ConfirmReceived)
	// The owner sends the order's one feedback once it is delivered or completed;
	// the author is the session's, so a client can only write about its own order
	// (FR-015, FR-018). The feedback may carry up to five photos, so the route
	// raises the same body limit the evidence routes install and accepts
	// multipart/form-data; the handler caps each photo again while it reads
	// (FR-025).
	r.With(middleware.BodyLimitWithRefusal(
		DeliveryEvidenceUploadCeiling(), photosTooLarge(),
	)).Post("/{orderId}/feedback", h.SubmitFeedback)
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
	// The admin live stream is a fixed segment ahead of the orderId parameter
	// routes, mirroring the customer router.
	r.Get("/stream", h.StreamAdmin)
	r.Get("/{orderId}", h.GetByIDAdmin)
	r.Post("/{orderId}/confirm", h.ConfirmByAdmin)
	r.Post("/{orderId}/reject", h.RejectByAdmin)
	// The dispatch route takes up to five evidence photos, so it raises the body
	// limit for its own handler and accepts multipart/form-data; the handler caps
	// each photo again while it reads (FR-006, FR-009).
	r.With(middleware.BodyLimitWithRefusal(
		DeliveryEvidenceUploadCeiling(), photosTooLarge(),
	)).Post("/{orderId}/ship", h.StartShippingByAdmin)
	// The delivery route takes the same up-to-five evidence photos, so it raises
	// the same body limit and accepts multipart/form-data; the handler caps each
	// photo again while it reads (FR-008, FR-009).
	r.With(middleware.BodyLimitWithRefusal(
		DeliveryEvidenceUploadCeiling(), photosTooLarge(),
	)).Post("/{orderId}/deliver", h.DeliverByAdmin)
	// Completion is no longer the operator's act: the customer confirms receipt
	// (POST /orders/{orderId}/complete) or the five-day sweep completes the
	// order, so there is no admin complete route (FR-011, FR-014).
	r.Post("/{orderId}/transfer", h.Transfer)
	return r
}
