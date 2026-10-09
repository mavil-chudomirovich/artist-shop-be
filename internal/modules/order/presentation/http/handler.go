package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Service is the slice of the order use-case surface this package's routes need.
// It is declared here, narrow, so the transport names only the capability it
// uses and each user story's routes can grow without the transport depending on
// the module's whole use-case interface. The concrete order use case satisfies it.
type Service interface {
	// Checkout turns the caller's cart into an order (FR-001).
	Checkout(ctx context.Context, in appdto.CheckoutInput) (appdto.OrderView, error)
}

// Handler adapts the order use cases to HTTP. It carries no business rule: it
// reads the session, parses the request, calls the use case and maps the answer
// onto the shared envelope (Constitution I). No route takes an owner identifier;
// the acting customer always comes from the authenticated session (FR-020).
type Handler struct {
	svc    Service
	logger *slog.Logger
}

// New creates the order HTTP handler.
func New(svc Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// Checkout turns the signed-in customer's cart into an order awaiting payment: a
// snapshot of every line and the delivery address, the goods held for the
// customer, and the cart emptied, all in one operation.
//
//	@Summary		Check out the cart into an order
//	@Description	Turns the caller's cart into an order. Every line is re-checked against the product's current sale state and price and against what is available; the whole checkout is refused if any line fails. On success the goods are held, the cart is emptied, and the order is returned awaiting payment.
//	@Tags			Orders
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		httpdto.CheckoutRequest	true	"Delivery address to use (optional addressId; omit for the default)"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders [post]
func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}

	var req httpdto.CheckoutRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}

	var addressID *uuid.UUID
	if req.AddressID != nil {
		parsed, appErr := parseUUID(*req.AddressID, fieldAddressID)
		if appErr != nil {
			httpx.WriteError(w, r, appErr, h.logger)
			return
		}
		addressID = &parsed
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Checkout(ctx, appdto.CheckoutInput{AddressID: addressID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toOrderResponse(view))
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No order route accepts an owner identifier: the session is the only source of
// the acting account, which is what makes a client-chosen owner impossible by
// construction (FR-020). It reports false after writing the response, so the
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
// additionalProperties: false of the request schema. An absent body is an empty
// request, which means "use the customer's default address" (FR-003).
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
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

// toMoneyResponse is the single conversion from a money DTO to its wire shape, so
// the amount and currency cannot drift per route.
func toMoneyResponse(money appdto.MoneyView) httpdto.MoneyResponse {
	return httpdto.MoneyResponse{Amount: money.Amount, Currency: money.Currency}
}

// toOrderResponse maps one order view to its wire shape. It always returns a
// non-nil slice of lines, so an order serialises `lines: []` rather than
// `lines: null`.
func toOrderResponse(view appdto.OrderView) httpdto.OrderResponse {
	lines := make([]httpdto.OrderLineResponse, 0, len(view.Lines))
	for _, line := range view.Lines {
		lines = append(lines, httpdto.OrderLineResponse{
			ProductID: line.ProductID,
			Name:      line.Name,
			Slug:      line.Slug,
			Quantity:  line.Quantity,
			UnitPrice: toMoneyResponse(line.UnitPrice),
			LineTotal: toMoneyResponse(line.LineTotal),
		})
	}
	return httpdto.OrderResponse{
		ID:        view.ID,
		Status:    string(view.Status),
		Total:     toMoneyResponse(view.Total),
		ItemCount: view.ItemCount,
		CreatedAt: view.CreatedAt,
		Address: httpdto.OrderAddressResponse{
			RecipientName:  view.Address.RecipientName,
			RecipientPhone: view.Address.RecipientPhone,
			ProvinceCode:   view.Address.ProvinceCode,
			ProvinceName:   view.Address.ProvinceName,
			WardCode:       view.Address.WardCode,
			WardName:       view.Address.WardName,
			StreetAddress:  view.Address.StreetAddress,
		},
		Lines: lines,
	}
}
