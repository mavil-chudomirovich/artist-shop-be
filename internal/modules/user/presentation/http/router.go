package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// avatarUploadOverhead is the room the router leaves for the multipart envelope —
// part headers, boundaries and the form field name — on top of the image ceiling.
// The image itself is capped again while it is read, so this only has to be
// generous, never exact. It also has to stay under the shared pipeline ceiling,
// which wraps the body before routing and therefore cannot be lifted here;
// config.MaxBodyBytes ships at twice the image ceiling for that reason.
const avatarUploadOverhead = 64 << 10

// Router builds the `/users` group. Mount it under `/api/v1`.
//
// Ownership is decided in the routing shape itself: no route takes an owner
// identifier, the acting account always comes from the session middleware, and
// the only path parameter that names an account is the ADMIN-guarded
// administrator lookup, where the account is the subject of the request rather
// than the actor (FR-006, FR-013, research D5).
//
// chi resolves the static `/me` segment before the `/{userId}` parameter, so the
// self-service routes keep their own handlers while `/{userId}` stays free for
// the lookup.
func (h *Handler) Router(limits config.UserConfig, hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()

	// FR-025: the module carries its own thresholds for the write-heavy routes,
	// so a single account cannot monopolise the media service or the address
	// table by hammering one endpoint, and the shared global limit stays a
	// coarse safety net.
	avatarLimit := middleware.RateLimitWindow(limits.AvatarUploadRatePerHour, time.Hour, h.logger)
	addressWriteLimit := middleware.RateLimitWindow(limits.AddressWriteRatePerMinute, time.Minute, h.logger)
	authenticated := middleware.RequireAuthentication(hooks)

	// Self-service routes: the account comes from the session, the path carries
	// no owner identifier.
	r.With(authenticated).Get("/me", h.GetProfile)
	// The routes that decode a JSON body carry the JSON-only content-type check, so
	// a request that sends another media type is refused with 415 here rather than
	// failing later as a parse error (FR-014 in spirit: the media type is checked
	// where the route knows what it expects).
	r.With(authenticated, middleware.JSONContentType).Patch("/me", h.UpdateProfile)
	r.With(authenticated).Delete("/me/avatar", h.RemoveAvatar)
	// The upload route raises the body limit for its own handler: the image ceiling
	// is above the shared pipeline ceiling, which wraps the body before routing and
	// so cannot be lifted by a route. The pipeline ceiling still caps the request
	// early at twice the avatar ceiling, and this route keeps the precise FR-015
	// check — the multipart envelope is allowed for on top of the image, and the
	// image itself is capped again while it is read, so a body that is over the
	// limit is refused without being buffered whole (research D7).
	//
	// The route reports the refusal its handler would have reported, so a request
	// whose declared length is over this limit is answered with the avatar-specific
	// code rather than the generic one the JSON routes keep. That is what makes the
	// answer independent of whether the client declared its length (FR-001,
	// research D1).
	r.With(authenticated, avatarLimit,
		middleware.BodyLimitWithRefusal(h.cfg.AvatarMaxBytesOrDefault()+avatarUploadOverhead, avatarTooLarge())).
		Post("/me/avatar", h.SetAvatar)

	r.With(authenticated).Get("/me/addresses", h.ListAddresses)
	r.With(authenticated, middleware.JSONContentType, addressWriteLimit).
		Post("/me/addresses", h.CreateAddress)
	r.With(authenticated, middleware.JSONContentType, addressWriteLimit).
		Patch("/me/addresses/{addressId}", h.UpdateAddress)
	r.With(authenticated, addressWriteLimit).Delete("/me/addresses/{addressId}", h.DeleteAddress)
	r.With(authenticated, addressWriteLimit).Post("/me/addresses/{addressId}/default", h.SetDefaultAddress)

	// The operator lookup is a single path, not a separate /admin prefix: the
	// account is the subject of the request rather than the actor. RequireRole
	// resolves the session itself and records the denial through the hooks'
	// OnDenied, exactly as the auth module does, so a customer probing this route
	// leaves a trace.
	r.With(middleware.RequireRole(hooks, string(access.RoleAdmin))).
		Get("/{userId}", h.LookupCustomer)

	return r
}

// DivisionsHandler serves the cascading-select reference data of the official
// administrative dataset.
//
// It is a separate handler from the profile one because it needs no use case:
// the answers are a read-through of embedded reference data through the same
// Divisions port the use cases depend on, so it holds no business rule and never
// touches an account (research D1).
type DivisionsHandler struct {
	divisions appinterface.Divisions
	logger    *slog.Logger
}

// NewDivisionsHandler creates the reference-data handler.
func NewDivisionsHandler(divisions appinterface.Divisions, logger *slog.Logger) *DivisionsHandler {
	return &DivisionsHandler{divisions: divisions, logger: logger}
}

// Router builds the `/divisions` group. Mount it under `/api/v1`.
//
// Both listings are unpaginated: they are bounded by the embedded dataset
// (roughly 35 provinces and a few hundred wards), and a page of a result smaller
// than any page size a client would ask for adds nothing (plan.md, Complexity
// Tracking).
func (h *DivisionsHandler) Router(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuthentication(hooks))
	r.Get("/provinces", h.ListProvinces)
	r.Get("/provinces/{provinceCode}/wards", h.ListWards)
	return r
}

// ListProvinces returns every province, ordered by name.
func (h *DivisionsHandler) ListProvinces(w http.ResponseWriter, r *http.Request) {
	provinces, err := h.divisions.Provinces(r.Context())
	if err != nil {
		httpx.WriteError(w, r, mapError(err), h.logger)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toProvinceResponses(provinces))
}

// ListWards returns the wards of one province. A ward list is always scoped to a
// single province, so a client never receives the whole set at once (FR-007c).
func (h *DivisionsHandler) ListWards(w http.ResponseWriter, r *http.Request) {
	provinceCode := chi.URLParam(r, "provinceCode")
	wards, err := h.divisions.Wards(r.Context(), provinceCode)
	if err != nil {
		httpx.WriteError(w, r, mapError(err), h.logger)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toWardResponses(wards))
}

func toProvinceResponses(provinces []appinterface.Province) []httpdto.ProvinceResponse {
	if len(provinces) == 0 {
		return nil
	}
	out := make([]httpdto.ProvinceResponse, 0, len(provinces))
	for _, province := range provinces {
		out = append(out, httpdto.ProvinceResponse{Code: province.Code, Name: province.Name})
	}
	return out
}

func toWardResponses(wards []appinterface.Ward) []httpdto.WardResponse {
	if len(wards) == 0 {
		return nil
	}
	out := make([]httpdto.WardResponse, 0, len(wards))
	for _, ward := range wards {
		out = append(out, httpdto.WardResponse{Code: ward.Code, Name: ward.Name, ProvinceCode: ward.ProvinceCode})
	}
	return out
}
