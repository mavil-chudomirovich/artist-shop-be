package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// stockService is the slice of the module's use-case surface the administrator
// routes consume. It is the US1 half of appinterface.InventoryService; the hold,
// event and history methods are added to it by the stories that implement them,
// so a route cannot reach a use case that does not exist yet.
type stockService interface {
	Restock(ctx context.Context, in appdto.RestockInput) (appdto.StockOutput, error)
	Damage(ctx context.Context, in appdto.DamageInput) (appdto.StockOutput, error)
	Adjust(ctx context.Context, in appdto.AdjustmentInput) (appdto.StockOutput, error)
	Stock(ctx context.Context, in appdto.StockRefInput) (appdto.StockOutput, error)
}

// Handler adapts the inventory administrator use cases to HTTP. It carries no
// business rule: it parses the request, calls the use case and maps the answer
// onto the shared envelope (Constitution I).
type Handler struct {
	svc    stockService
	logger *slog.Logger
}

// New creates the inventory HTTP handler.
func New(svc stockService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// GetStock returns one product's physical, held and available quantities
// (FR-007). An unknown product is 404 PRODUCT_NOT_FOUND; one that exists but has
// never been stocked answers zero.
func (h *Handler) GetStock(w http.ResponseWriter, r *http.Request) {
	id, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.Stock(r.Context(), appdto.StockRefInput{ProductID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toStockResponse(out))
}

// Restock records goods arriving (FR-001). The acting administrator comes from
// the session the middleware resolved, never from the body: the input carries no
// account member and the decoder refuses an unknown one.
func (h *Handler) Restock(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, quantity, note, ok := h.quantityBody(w, r)
	if !ok {
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.Restock(ctx, appdto.RestockInput{ProductID: id, Quantity: quantity, Note: note})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toStockResponse(out))
}

// Damage records goods lost (FR-002). A decrease over the shelf is refused by the
// use case with the insufficient-stock sentinel, which maps to 409.
func (h *Handler) Damage(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, quantity, note, ok := h.quantityBody(w, r)
	if !ok {
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.Damage(ctx, appdto.DamageInput{ProductID: id, Quantity: quantity, Note: note})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toStockResponse(out))
}

// Adjustment corrects a product's physical stock to a recounted value (FR-003).
// Zero is a valid counted value; correcting to what is stored changes nothing and
// writes no movement.
func (h *Handler) Adjustment(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.AdjustmentRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	quantity, appErr := quantityValue(req.Quantity)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.Adjust(ctx, appdto.AdjustmentInput{ProductID: id, Quantity: quantity, Note: req.Note})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toStockResponse(out))
}

// quantityBody parses the path identifier and the shared restock/damage body,
// reporting false after writing the response when either is unacceptable.
func (h *Handler) quantityBody(w http.ResponseWriter, r *http.Request) (uuid.UUID, int64, *string, bool) {
	id, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return uuid.Nil, 0, nil, false
	}
	var req httpdto.QuantityRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return uuid.Nil, 0, nil, false
	}
	quantity, appErr := quantityValue(req.Quantity)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return uuid.Nil, 0, nil, false
	}
	return id, quantity, req.Note, true
}

// decode reads a JSON body and rejects unknown members, matching the
// additionalProperties: false of the request schemas. A body that names the
// acting account is therefore refused rather than decoded (FR-005).
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// quantityValue turns the raw JSON number into an integer, refusing a missing,
// fractional or non-numeric value as a VALIDATION_ERROR naming `quantity`. The
// positive-whole-number rule on the value itself is the use case's.
func quantityValue(raw json.Number) (int64, *httpx.AppError) {
	text := raw.String()
	if text == "" {
		return 0, fieldError(fieldQuantity, "is required and must be a whole number")
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fieldError(fieldQuantity, "must be a whole number")
	}
	return value, nil
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No inventory route accepts an owner identifier: the session is the only source
// of the acting account, which is what makes an actor chosen by a client
// impossible by construction (FR-005). It reports false after writing the
// response, so the caller returns without doing any work.
func (h *Handler) sessionActor(w http.ResponseWriter, r *http.Request) (appinterface.Actor, bool) {
	identity, ok := middleware.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	accountID, err := uuid.Parse(identity.Subject)
	if err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	return appinterface.Actor{ID: accountID, Role: access.Role(identity.Role)}, true
}

// pathUUID reads a UUID path parameter. A value that is not a UUID is a request
// error, never a lookup that returns nothing: it answers 400 VALIDATION_ERROR
// with the offending member named (contracts/error-codes.md).
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	raw := chi.URLParam(r, name)
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "must be a UUID")
	}
	return parsed, nil
}

// toStockResponse is the single conversion point from the application DTO to the
// HTTP shape, so the three-member shape cannot drift per route.
func toStockResponse(out appdto.StockOutput) httpdto.StockResponse {
	return httpdto.StockResponse{
		PhysicalQuantity:  out.PhysicalQuantity,
		HeldQuantity:      out.HeldQuantity,
		AvailableQuantity: out.AvailableQuantity,
	}
}
