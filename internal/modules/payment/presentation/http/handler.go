package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Service is the slice of the payment use-case surface this package's customer
// routes need. It is declared here, narrow, so the transport names only the
// capability it uses and each later user story can grow without the transport
// depending on the module's whole use-case interface. The concrete payment use
// case satisfies it.
type Service interface {
	// Start starts (or reuses) a payment for the caller's awaiting-payment
	// order and returns it with its link (FR-001 to FR-003).
	Start(ctx context.Context, in appdto.StartInput) (appdto.PaymentView, error)
	// Read returns the caller's live payment for their order (FR-017).
	Read(ctx context.Context, in appdto.ReadInput) (appdto.PaymentView, error)
	// HandleWebhook verifies a raw provider callback and applies it exactly once,
	// or records it UNMATCHED when it cannot be applied (FR-007 to FR-011,
	// FR-019).
	HandleWebhook(ctx context.Context, body []byte) error
	// ListAll returns one page of every payment, newest first, optionally
	// narrowed to one state; status=UNMATCHED is the reconciliation queue
	// (FR-018).
	ListAll(ctx context.Context, in appdto.ListInput) (appdto.PaymentPage, error)
}

// fieldOrderID is the path parameter naming the order, used in the VALIDATION_ERROR
// detail so a client can point at the exact member (contracts/error-codes.md).
const fieldOrderID = "orderId"

// Query members the operator list names in a VALIDATION_ERROR, and the pagination
// bounds it accepts. Pagination and filter validation are a foundation concern and
// stay on the shared VALIDATION_ERROR code (contracts/error-codes.md).
const (
	fieldPage     = "page"
	fieldPageSize = "pageSize"
	fieldStatus   = "status"

	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the payment use cases to HTTP. It carries no business rule: it
// reads the session, parses the request, calls the use case and maps the answer
// onto the shared envelope (Constitution I). No route takes an owner identifier;
// the acting customer always comes from the authenticated session (FR-003,
// FR-017).
type Handler struct {
	svc    Service
	logger *slog.Logger
}

// New creates the payment HTTP handler.
func New(svc Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// StartPayment starts (or reuses) an online payment for the caller's
// awaiting-payment order and returns it with the link the customer pays through.
// A repeat start reuses the live payment and returns the same link. An order that
// is not awaiting payment answers 409 PAYMENT_ORDER_NOT_PAYABLE; another
// customer's order answers 404 PAYMENT_ORDER_NOT_FOUND; a provider refusal answers
// 422 PAYMENT_PROVIDER_ERROR with the order unchanged (FR-001 to FR-006,
// contracts/error-codes.md).
//
//	@Summary		Start (or reuse) an online payment for the caller's order
//	@Description	Creates a provider payment request for the order's exact total and returns the link the customer pays through. When a live payment already exists for the order it is reused and the same link is returned. The order must belong to the caller and must be awaiting payment; a provider refusal leaves the order unchanged.
//	@Tags			payments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.PaymentResponse}
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		422		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId}/payment [post]
func (h *Handler) StartPayment(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Start(ctx, appdto.StartInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPaymentResponse(view))
}

// ReadPayment returns the caller's most recent payment for their order, in any
// state, so a payment that has reached PAID, CANCELLED or UNMATCHED is still
// readable. An order that does not exist, or belongs to another customer, answers
// 404 PAYMENT_ORDER_NOT_FOUND; an order of the caller's with no payment at all
// answers 404 PAYMENT_NOT_FOUND (FR-017, contracts/error-codes.md).
//
//	@Summary		Read the payment state of the caller's order
//	@Description	Returns the caller's most recent payment for the order, in any state. Another customer's order answers the same not-found an unknown order does; an order with no payment at all answers PAYMENT_NOT_FOUND.
//	@Tags			payments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.PaymentResponse}
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId}/payment [get]
func (h *Handler) ReadPayment(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Read(ctx, appdto.ReadInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPaymentResponse(view))
}

// ListPayments returns one page of every payment, newest first by default, each
// row carrying its owner, state, amount and provider reference. An optional
// `status` narrows it to one payment state, so `status=UNMATCHED` is the queue of
// payments whose confirmation could not be applied and need the operator's
// review. The route is behind the administrator role guard, so the caller is
// always an administrator; no owner filter is applied (FR-018, research D15,
// contracts/openapi.yaml).
//
//	@Summary		List payments, optionally the reconciliation queue
//	@Description	Returns every payment, newest first by default, with its owner and the provider's transaction reference, paginated. An optional `status` filters to one payment state; `status=UNMATCHED` is the queue of payments whose confirmation could not be applied and need the operator's review. Administrator role required.
//	@Tags			admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query	int		false	"Page number (default 1)"
//	@Param			pageSize	query	int		false	"Page size (default 20)"
//	@Param			status		query	string	false	"Only payments in this state (PENDING, PAID, CANCELLED or UNMATCHED)"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.AdminPaymentResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		429			{object}	httpx.SwaggerError
//	@Failure		500			{object}	httpx.SwaggerError
//	@Router			/admin/payments [get]
func (h *Handler) ListPayments(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	status, appErr := paymentStatusFilter(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.ListAll(ctx, appdto.ListInput{Page: page, PageSize: pageSize, Status: status})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toAdminPaymentResponses(out.Payments), out.Page, out.PageSize, out.Total)
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No payment route accepts an owner identifier: the session is the only source of
// the acting account, which is what makes a client-chosen owner impossible by
// construction (FR-003, FR-017). It reports false after writing the response, so
// the caller returns without doing any work.
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

// coded builds a module error response with an explicit status, so a code that is
// not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// fieldError reports a request-shape problem the client can fix itself. It stays
// on the shared VALIDATION_ERROR code: a malformed request is a foundation
// concern, not a payment rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// pageParams reads the operator list's window. Pagination validation is a
// foundation concern and stays on the shared VALIDATION_ERROR code
// (contracts/error-codes.md).
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

// positiveQuery parses a query value that must be a positive integer, falling
// back when it is absent.
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

// paymentStatusFilter reads the operator list's optional state filter, so
// status=UNMATCHED is the reconciliation queue. An absent status applies no
// filter; a value outside the four payment states is the shared VALIDATION_ERROR
// naming the member, exactly as the contract requires (FR-018, research D15,
// contracts/error-codes.md).
func paymentStatusFilter(r *http.Request) (*constant.Status, *httpx.AppError) {
	raw := strings.TrimSpace(r.URL.Query().Get("status"))
	if raw == "" {
		return nil, nil
	}
	candidate := constant.Status(raw)
	if !constant.IsValidStatus(candidate) {
		return nil, fieldError(fieldStatus, "must be one of the four payment states")
	}
	return &candidate, nil
}

// mapError translates the module's sentinel errors into the codes and statuses of
// specs/011-order-payment/contracts/error-codes.md. A shared AppError the
// transport produced (a malformed request, an authentication failure) passes
// through unchanged; anything else is hidden behind INTERNAL_ERROR so storage
// detail is never leaked.
func mapError(err error) *httpx.AppError {
	switch {
	case errors.Is(err, domainerr.ErrOrderNotFound):
		return coded(constant.CodeOrderNotFound, http.StatusNotFound, "Payment order not found")
	case errors.Is(err, domainerr.ErrOrderNotPayable):
		return coded(constant.CodeOrderNotPayable, http.StatusConflict, "Order is not awaiting payment")
	case errors.Is(err, domainerr.ErrAlreadyLive):
		return coded(constant.CodeAlreadyLive, http.StatusConflict, "A live payment already exists")
	case errors.Is(err, domainerr.ErrProviderFailure):
		return coded(constant.CodeProviderError, http.StatusUnprocessableEntity, "Payment provider error")
	case errors.Is(err, domainerr.ErrNotFound):
		return coded(constant.CodeNotFound, http.StatusNotFound, "Payment not found")
	case errors.Is(err, domainerr.ErrWebhookInvalidSignature):
		return coded(constant.CodeWebhookInvalidSignature, http.StatusUnauthorized, "Payment webhook signature invalid")
	case errors.Is(err, domainerr.ErrMalformedWebhook):
		return httpx.New(httpx.CodeMalformedRequest)
	case errors.Is(err, domainerr.ErrInvalidValue):
		return fieldError(fieldOrderID, "is not a valid payment value")
	default:
		var appErr *httpx.AppError
		if errors.As(err, &appErr) {
			return appErr
		}
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}

// toPaymentResponse maps one payment view to its wire shape. The amount and
// currency are carried through unchanged, so the two cannot drift per route.
func toPaymentResponse(view appdto.PaymentView) httpdto.PaymentResponse {
	return httpdto.PaymentResponse{
		ID:      view.ID,
		OrderID: view.OrderID,
		Amount: httpdto.MoneyResponse{
			Amount:   view.Amount.Amount,
			Currency: view.Amount.Currency,
		},
		Status:          string(view.Status),
		Provider:        view.Provider,
		OrderCode:       view.OrderCode,
		CheckoutURL:     view.CheckoutURL,
		QRCode:          view.QRCode,
		UnmatchedReason: view.UnmatchedReason,
		CreatedAt:       view.CreatedAt,
		UpdatedAt:       view.UpdatedAt,
	}
}

// toAdminPaymentResponse maps one operator view to its wire shape: the customer
// payment plus the owner and the provider reference (FR-018).
func toAdminPaymentResponse(view appdto.AdminPaymentView) httpdto.AdminPaymentResponse {
	return httpdto.AdminPaymentResponse{
		PaymentResponse:   toPaymentResponse(view.PaymentView),
		UserID:            view.UserID,
		ProviderReference: view.ProviderReference,
	}
}

// toAdminPaymentResponses maps a page of the operator's rows. It always
// allocates, so an empty list serialises `data: []` rather than null.
func toAdminPaymentResponses(views []appdto.AdminPaymentView) []httpdto.AdminPaymentResponse {
	out := make([]httpdto.AdminPaymentResponse, 0, len(views))
	for _, view := range views {
		out = append(out, toAdminPaymentResponse(view))
	}
	return out
}
