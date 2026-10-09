package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Pagination bounds of the movement history (contracts/openapi.yaml,
// Page/PageSize). They match the project's existing list convention (research
// D14).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// stockService is the slice of the module's use-case surface the administrator
// routes consume: the manual stock operations and the read US1 delivers, plus the
// history read US6 adds. The hold and event methods stay off it — they have no
// HTTP surface (research D7).
type stockService interface {
	Restock(ctx context.Context, in appdto.RestockInput) (appdto.StockOutput, error)
	Damage(ctx context.Context, in appdto.DamageInput) (appdto.StockOutput, error)
	Adjust(ctx context.Context, in appdto.AdjustmentInput) (appdto.StockOutput, error)
	Stock(ctx context.Context, in appdto.StockRefInput) (appdto.StockOutput, error)
	History(ctx context.Context, in appdto.HistoryInput) (appdto.MovementPage, error)
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

// GetMovements returns one page of a product's movement history, oldest first
// (FR-008). An unknown product is 404 PRODUCT_NOT_FOUND, because the history of a
// product that does not exist is not an empty history (research D12). The window
// is validated here — a page or size outside its range is 400 VALIDATION_ERROR
// naming the field — and the paginated envelope carries the page metadata.
func (h *Handler) GetMovements(w http.ResponseWriter, r *http.Request) {
	id, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.History(r.Context(), appdto.HistoryInput{ProductID: id, Page: page, PageSize: pageSize})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toMovementResponses(out.Movements), out.Page, out.PageSize, out.Total)
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

// pageParams reads the movement-history window. Pagination validation is a
// foundation concern and stays on the shared VALIDATION_ERROR code, matching the
// catalogue and address lists (research D14).
func pageParams(r *http.Request) (int, int, *httpx.AppError) {
	query := r.URL.Query()
	page, err := positiveQuery(query.Get("page"), 1)
	if err != nil {
		return 0, 0, fieldError(fieldPage, "must be an integer of at least 1")
	}
	pageSize, err := positiveQuery(query.Get("pageSize"), defaultPageSize)
	if err != nil {
		return 0, 0, fieldError(fieldPageSize, "must be an integer between 1 and 100")
	}
	if pageSize > maxPageSize {
		return 0, 0, fieldError(fieldPageSize, "must be an integer between 1 and 100")
	}
	return page, pageSize, nil
}

// positiveQuery parses a whole-number query member, falling back to the default
// when it is absent and reporting a value below 1 as unacceptable.
func positiveQuery(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("not an integer")
	}
	if parsed < 1 {
		return 0, errors.New("below the lower bound")
	}
	return parsed, nil
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

// toMovementResponse is the single conversion point from the application movement
// DTO to the HTTP shape, so the nine-member shape cannot drift per route. A nil
// source reference and a nil actor are carried through as null, which is what
// keeps a manual change distinguishable from a system-caused sale (FR-008).
func toMovementResponse(out appdto.MovementOutput) httpdto.MovementResponse {
	return httpdto.MovementResponse{
		ID:                out.ID,
		ProductID:         out.ProductID,
		Kind:              string(out.Kind),
		Delta:             out.Delta,
		ResultingQuantity: out.ResultingQuantity,
		SourceReference:   out.SourceReference,
		ActorID:           out.ActorID,
		Note:              out.Note,
		CreatedAt:         out.CreatedAt,
	}
}

// toMovementResponses maps a page of movements. It always returns a non-nil slice
// — an empty page serialises as `[]`, which is what a client expects of a history
// that has no changes (quickstart 9d), rather than as `null`.
func toMovementResponses(list []appdto.MovementOutput) []httpdto.MovementResponse {
	out := make([]httpdto.MovementResponse, 0, len(list))
	for _, movement := range list {
		out = append(out, toMovementResponse(movement))
	}
	return out
}
