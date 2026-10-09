package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Handler adapts the cart use cases to HTTP. It carries no business rule: it reads
// the session, parses the request, calls the use case and maps the answer onto the
// shared envelope (Constitution I). No route takes a cart or owner identifier; the
// acting customer always comes from the authenticated session (FR-010).
type Handler struct {
	svc    appinterface.CartService
	logger *slog.Logger
}

// New creates the cart HTTP handler.
func New(svc appinterface.CartService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// GetCart returns the signed-in customer's cart, re-checking each line's live
// product facts. An empty cart answers an empty list with no subtotal, not an
// error.
//
//	@Summary		Read the signed-in customer's cart
//	@Description	Returns every line with its product, quantity, the price captured when it was added and the line total, plus the cart subtotal. An empty cart answers an empty list with a null subtotal.
//	@Tags			Cart
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.SwaggerSuccess{data=httpdto.CartResponse}
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		429	{object}	httpx.SwaggerError
//	@Failure		500	{object}	httpx.SwaggerError
//	@Router			/cart [get]
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Get(ctx, appdto.GetCartInput{})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toCartResponse(view))
}

// AddItem adds a product with a positive whole quantity to the customer's cart.
// Adding a product already held raises its line rather than creating a second one;
// the price captured is the product's current price.
//
//	@Summary		Add a product to the cart
//	@Description	Adds a product with a positive whole quantity. Adding a product already in the cart raises its quantity; the captured price is the product's current price. An unknown product answers not-found.
//	@Tags			Cart
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		httpdto.AddItemRequest	true	"Product and quantity to add"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.CartResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/cart/items [post]
func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	var req httpdto.AddItemRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	productID, appErr := parseUUID(req.ProductID, fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	quantity, appErr := quantityValue(req.Quantity)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Add(ctx, appdto.AddItemInput{ProductID: productID, Quantity: quantity})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toCartResponse(view))
}

// ChangeQuantity sets the quantity of the line for one product.
//
//	@Summary		Change a line's quantity
//	@Description	Sets the quantity of the line for that product. A product that is not a line of this cart answers not-found.
//	@Tags			Cart
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			productId	path		string					true	"Product identifier"
//	@Param			request		body		httpdto.QuantityRequest	true	"New quantity"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=httpdto.CartResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		409			{object}	httpx.SwaggerError
//	@Failure		429			{object}	httpx.SwaggerError
//	@Failure		500			{object}	httpx.SwaggerError
//	@Router			/cart/items/{productId} [patch]
func (h *Handler) ChangeQuantity(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	productID, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.QuantityRequest
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
	view, err := h.svc.ChangeQuantity(ctx, appdto.ChangeQuantityInput{ProductID: productID, Quantity: quantity})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toCartResponse(view))
}

// RemoveItem removes the line for one product. An identifier that is not a line of
// this cart answers not-found, so the route never confirms what is in another
// customer's cart.
//
//	@Summary		Remove a line from the cart
//	@Description	Removes the line for that product. An identifier that is not a line of this cart answers not-found.
//	@Tags			Cart
//	@Produce		json
//	@Security		BearerAuth
//	@Param			productId	path	string	true	"Product identifier"
//	@Success		204			"The line is removed."
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		429			{object}	httpx.SwaggerError
//	@Failure		500			{object}	httpx.SwaggerError
//	@Router			/cart/items/{productId} [delete]
func (h *Handler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	productID, appErr := pathUUID(r, "productId", fieldProductID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	if err := h.svc.Remove(ctx, appdto.RemoveItemInput{ProductID: productID}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No cart route accepts an owner identifier: the session is the only source of the
// acting account, which is what makes a client-chosen owner impossible by
// construction (FR-010). It reports false after writing the response, so the
// caller returns without doing any work.
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

// decode reads a JSON body and rejects unknown members, matching the
// additionalProperties: false of the request schemas. A body that names an owner
// is therefore refused rather than decoded (FR-010).
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

// parseUUID reads a UUID from a request value. A value that is not a UUID is a
// request error, never a lookup that returns nothing: it answers 400
// VALIDATION_ERROR with the offending member named (contracts/error-codes.md).
func parseUUID(raw, field string) (uuid.UUID, *httpx.AppError) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "must be a UUID")
	}
	return parsed, nil
}

// pathUUID reads a UUID path parameter.
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	return parseUUID(chi.URLParam(r, name), field)
}

// toMoneyResponse is the single conversion from a money DTO to its wire shape, so
// the amount and currency cannot drift per route.
func toMoneyResponse(money appdto.MoneyView) httpdto.MoneyResponse {
	return httpdto.MoneyResponse{Amount: money.Amount, Currency: money.Currency}
}

// toLineResponse maps one cart line. The name and slug are carried through as
// null when the product is gone.
func toLineResponse(line appdto.LineView) httpdto.CartLineResponse {
	return httpdto.CartLineResponse{
		ProductID: line.ProductID,
		Name:      line.Name,
		Slug:      line.Slug,
		Quantity:  line.Quantity,
		UnitPrice: toMoneyResponse(line.UnitPrice),
		LineTotal: toMoneyResponse(line.LineTotal),
	}
}

// toCartResponse maps a whole cart. It always returns a non-nil slice — an empty
// cart serialises as `lines: []` rather than `lines: null` — and a nil subtotal
// survives as `null` (quickstart scenario 1).
func toCartResponse(view appdto.CartView) httpdto.CartResponse {
	lines := make([]httpdto.CartLineResponse, 0, len(view.Lines))
	for _, line := range view.Lines {
		lines = append(lines, toLineResponse(line))
	}
	var subtotal *httpdto.MoneyResponse
	if view.Subtotal != nil {
		money := toMoneyResponse(*view.Subtotal)
		subtotal = &money
	}
	return httpdto.CartResponse{Lines: lines, Subtotal: subtotal}
}
