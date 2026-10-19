package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
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
	// ListMine returns one page of the caller's own orders, newest first
	// (FR-018).
	ListMine(ctx context.Context, in appdto.ListInput) (appdto.OrderPage, error)
	// GetMine reads one of the caller's own orders in full; another customer's
	// identifier answers not-found (FR-018, FR-020).
	GetMine(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error)
	// CancelMine cancels one of the caller's own orders before it is paid —
	// while it awaits the artist's confirmation or payment — and returns it
	// (FR-019, FR-020).
	CancelMine(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error)
	// EditMine replaces one of the caller's own orders' lines and, when named,
	// its delivery address while it awaits the artist or payment (FR-012 to
	// FR-017).
	EditMine(ctx context.Context, in appdto.EditInput) (appdto.OrderView, error)
	// ConfirmReceived completes one of the caller's own delivered orders by
	// confirming receipt; another customer's order answers not-found (FR-011,
	// FR-014, FR-018).
	ConfirmReceived(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error)
	// SubmitFeedback records the caller's one order-level feedback for a
	// delivered or completed order; another customer's order answers not-found
	// (FR-015, FR-016, FR-018).
	SubmitFeedback(ctx context.Context, in appdto.FeedbackInput) (appdto.OrderView, error)

	// ListAll returns one page of every order, newest first, with its owner
	// (FR-021).
	ListAll(ctx context.Context, in appdto.ListInput) (appdto.AdminOrderPage, error)
	// GetByIDAdmin reads any order in full (FR-021).
	GetByIDAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error)
	// ConfirmByAdmin accepts an order awaiting the artist, holds the whole order
	// and opens the payment window (FR-004, FR-005).
	ConfirmByAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error)
	// RejectByAdmin declines an order awaiting the artist, moving it to
	// cancelled; no goods were held, so nothing is returned (FR-018).
	RejectByAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error)
	// StartShippingByAdmin moves a paid order in transit (dispatch), attaching
	// zero to five optional evidence photos, and records the act (FR-005,
	// FR-006, FR-019).
	StartShippingByAdmin(ctx context.Context, in appdto.EvidenceInput) (appdto.AdminOrderView, error)
	// DeliverByAdmin marks an in-transit order delivered, attaching zero to five
	// optional evidence photos, stamps the delivery instant and records the act
	// (FR-007, FR-008, FR-019).
	DeliverByAdmin(ctx context.Context, in appdto.EvidenceInput) (appdto.AdminOrderView, error)
	// Transfer hands a paid order to another existing account, named by email,
	// changing only the owner, and records the act (FR-024).
	Transfer(ctx context.Context, in appdto.TransferInput) (appdto.AdminOrderView, error)
}

// Pagination bounds of the customer's and the operator's order lists
// (contracts/openapi.yaml, Page/PageSize). They match the project's existing
// convention (research D14).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the order use cases to HTTP. It carries no business rule: it
// reads the session, parses the request, calls the use case and maps the answer
// onto the shared envelope (Constitution I). No route takes an owner identifier;
// the acting customer always comes from the authenticated session (FR-020).
type Handler struct {
	svc    Service
	logger *slog.Logger
	// streams is the shared SSE hub the stream handlers subscribe to. It is
	// attached by SetStreams at the composition root and stays out of New so
	// existing constructions keep working; a nil hub answers streams with 503.
	streams streamsHub
}

// New creates the order HTTP handler.
func New(svc Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// Checkout turns the signed-in customer's cart into an order awaiting the
// artist's confirmation: a snapshot of every line and the delivery address, no
// goods held, and the cart emptied, all in one operation. The order becomes
// payable only once the artist confirms it.
//
//	@Summary		Check out the cart into an order awaiting the artist's confirmation
//	@Description	Turns the caller's cart into an order awaiting the artist's confirmation, holding no goods. Every line is re-checked against the product's current sale state and price and against what is available; the whole checkout is refused if any line fails. On success the cart is emptied and the order is returned in PENDING; the goods are held only when the artist confirms it.
//	@Tags			Orders
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		httpdto.CheckoutRequest	false	"Delivery address to use (optional addressId; omit for the default)"
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

// ListMine returns a page of the signed-in customer's own orders, newest first.
// The owner is the session's, never the request's, so the list is always the
// caller's (FR-018, FR-020).
//
//	@Summary		List the signed-in customer's orders
//	@Description	Returns the caller's own orders, newest first, paginated. No request can name an owner, so the list is always the session's.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query	int	false	"Page number (default 1)"
//	@Param			pageSize	query	int	false	"Page size (default 20)"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.OrderSummaryResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		429			{object}	httpx.SwaggerError
//	@Failure		500			{object}	httpx.SwaggerError
//	@Router			/orders [get]
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.ListMine(ctx, appdto.ListInput{Page: page, PageSize: pageSize})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toOrderSummaryResponses(out.Orders), out.Page, out.PageSize, out.Total)
}

// GetMine returns one of the signed-in customer's own orders in full. An order
// that does not exist, or belongs to another customer, answers the same
// not-found, so the route never confirms another customer's order (FR-018,
// FR-020).
//
//	@Summary		Read one of the signed-in customer's orders
//	@Description	Returns the caller's own order in full. An unknown order, or one that belongs to another customer, answers the same not-found.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId} [get]
func (h *Handler) GetMine(w http.ResponseWriter, r *http.Request) {
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
	view, err := h.svc.GetMine(ctx, appdto.OrderRefInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOrderResponse(view))
}

// CancelMine cancels one of the signed-in customer's own orders before it is
// paid — while it awaits the artist's confirmation or payment — and returns its
// goods. A paid order is refused by the state machine, because a paid order is
// transferred instead (FR-019, FR-020).
//
//	@Summary		Cancel the signed-in customer's unpaid order
//	@Description	Cancels an order before it is paid — while it awaits the artist's confirmation or payment — and returns its goods. A paid order cannot be cancelled; the refusal names the current state.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId}/cancel [post]
func (h *Handler) CancelMine(w http.ResponseWriter, r *http.Request) {
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
	view, err := h.svc.CancelMine(ctx, appdto.OrderRefInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOrderResponse(view))
}

// EditMine changes one of the signed-in customer's own orders while it awaits the
// artist or payment: it replaces the order's lines and, when named, its delivery
// address, re-snapshots each line's current price, recomputes the total and bumps
// the content version. An order awaiting payment is returned to awaiting
// confirmation and its goods are released, so the artist must confirm the new
// content afresh; an order awaiting the artist stays awaiting the artist.
//
//	@Summary		Change the signed-in customer's order before paying
//	@Description	Replaces the order's lines and, when named, its delivery address while the order awaits the artist or payment. Each line is re-checked and re-priced at its current value, the total is recomputed and the content version is bumped. An awaiting-payment order is returned to PENDING and its goods are released; a paid order is refused 409 ORDER_NOT_EDITABLE and an edit that would leave no lines is refused 409 ORDER_EMPTY.
//	@Tags			Orders
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path		string				true	"Order identifier"
//	@Param			request	body		httpdto.EditOrderRequest	true	"New lines and optional addressId"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId} [put]
func (h *Handler) EditMine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	var req httpdto.EditOrderRequest
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

	lines := make([]appdto.EditLineInput, 0, len(req.Lines))
	for _, line := range req.Lines {
		variantID, appErr := parseUUID(line.VariantID, fieldVariantID)
		if appErr != nil {
			httpx.WriteError(w, r, appErr, h.logger)
			return
		}
		if line.Quantity < 1 {
			httpx.WriteError(w, r, fieldError(fieldQuantity, "must be at least 1"), h.logger)
			return
		}
		lines = append(lines, appdto.EditLineInput{VariantID: variantID, Quantity: line.Quantity})
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.EditMine(ctx, appdto.EditInput{OrderID: orderID, AddressID: addressID, Lines: lines})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOrderResponse(view))
}

// ConfirmReceived completes one of the signed-in customer's own delivered orders:
// the owner confirms receipt and the order moves to COMPLETED, recording when and
// who confirmed. An unknown order, or one that belongs to another customer,
// answers the same 404 ORDER_NOT_FOUND, so the route never confirms another
// customer's order exists; an order that is not delivered answers 409
// ORDER_STATE_TRANSITION_INVALID naming its current state (FR-011, FR-014,
// FR-018, contracts/error-codes.md).
//
//	@Summary		Confirm receipt of a delivered order and complete it
//	@Description	Completes one of the caller's own delivered orders by confirming receipt. The order must be delivered; an order that is not delivered answers 409 ORDER_STATE_TRANSITION_INVALID naming its current state, and an unknown order, or one belonging to another customer, answers 404 ORDER_NOT_FOUND.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId}/complete [post]
func (h *Handler) ConfirmReceived(w http.ResponseWriter, r *http.Request) {
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
	view, err := h.svc.ConfirmReceived(ctx, appdto.OrderRefInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOrderResponse(view))
}

// SubmitFeedback sends the signed-in customer's one order-level feedback to the
// artist about a delivered or completed order, optionally attaching up to five
// photos. The author is the session's, never the request's. The content is
// required and must carry 1 to 2000 characters after trimming; a blank or
// over-long value is 400 VALIDATION_ERROR naming `content`. Each photo is
// validated by signature (JPEG, PNG or WebP) and size before it is stored, and
// more than five photos answer 409 ORDER_IMAGE_LIMIT_REACHED; an unsupported one
// answers 400 ORDER_IMAGE_TYPE_UNSUPPORTED and an oversized one answers 413
// ORDER_IMAGE_TOO_LARGE. An order that is not delivered or completed answers 409
// ORDER_FEEDBACK_NOT_ALLOWED naming its state, a second feedback answers 409
// ORDER_FEEDBACK_ALREADY_SENT, and an unknown order, or one belonging to another
// customer, answers 404 ORDER_NOT_FOUND (FR-015 to FR-018, FR-025,
// contracts/error-codes.md).
//
//	@Summary		Send order-level feedback to the artist, with up to five photos
//	@Description	Records the caller's one order-level feedback for a delivered or completed order, optionally attaching up to five photos, and returns the order with it attached. The content must carry 1 to 2000 characters after trimming. Each photo is validated by signature (JPEG, PNG or WebP) and size before it is stored. The order must be delivered or completed; a wrong state answers 409 ORDER_FEEDBACK_NOT_ALLOWED naming its state, a second feedback answers 409 ORDER_FEEDBACK_ALREADY_SENT, more than five photos answer 409 ORDER_IMAGE_LIMIT_REACHED, an unsupported photo answers 400 ORDER_IMAGE_TYPE_UNSUPPORTED, an oversized one answers 413 ORDER_IMAGE_TOO_LARGE and an unknown order, or one belonging to another customer, answers 404 ORDER_NOT_FOUND.
//	@Tags			Orders
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path		string	true	"Order identifier"
//	@Param			content	formData	string	true	"Feedback text, 1 to 2000 characters"
//	@Param			photos	formData	file	false	"Feedback photo (JPEG, PNG or WebP); send the part zero to five times"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.OrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		413		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/orders/{orderId}/feedback [post]
func (h *Handler) SubmitFeedback(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	content, photos, appErr := h.readFeedbackMultipart(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	// The same rule the use case applies, checked here so a malformed message is
	// refused before any order is read and the detail names `content`
	// (FR-015, contracts/error-codes.md).
	if err := model.ValidateFeedbackContent(content); err != nil {
		h.fail(w, r, err)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.SubmitFeedback(ctx, appdto.FeedbackInput{OrderID: orderID, Content: content, Photos: photos})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOrderResponse(view))
}

// readFeedbackMultipart streams the `content` field and the optional `photos`
// parts of a feedback multipart body, refusing to buffer more than the image
// ceiling per photo. Reading with a ceiling is what makes the size rule
// enforceable: a post-hoc check on an already buffered body cannot stop a
// memory-exhaustion upload. The client-declared file name and media type are
// deliberately not trusted — the use case decides the image type from the leading
// signature (FR-025, FR-009).
//
// An absent body is an empty content and zero photos; the empty content is then
// refused by the content rule, so a feedback must always carry text (FR-015). The
// route's own body ceiling refuses a body whose size is already over the limit
// before this runs, and the reader maps a stream cut by that ceiling to the same
// ORDER_IMAGE_TOO_LARGE refusal.
func (h *Handler) readFeedbackMultipart(r *http.Request) (string, []appdto.UploadedPhoto, *httpx.AppError) {
	if r.Body == nil || r.ContentLength == 0 {
		return "", nil, nil
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, fieldError(fieldContent, "a multipart/form-data body with a content field is required")
	}
	ceiling := int64(model.MaxDeliveryPhotoBytes)
	var content string
	photos := make([]appdto.UploadedPhoto, 0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return "", nil, photosTooLarge()
			}
			return "", nil, httpx.New(httpx.CodeMalformedRequest)
		}
		switch part.FormName() {
		case fieldContent:
			raw, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					return "", nil, photosTooLarge()
				}
				return "", nil, httpx.New(httpx.CodeMalformedRequest)
			}
			content = string(raw)
		case fieldPhotos:
			// One extra byte distinguishes "at the ceiling" from "over it".
			raw, err := io.ReadAll(io.LimitReader(part, ceiling+1))
			_ = part.Close()
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					return "", nil, photosTooLarge()
				}
				return "", nil, httpx.New(httpx.CodeMalformedRequest)
			}
			if int64(len(raw)) > ceiling {
				return "", nil, photosTooLarge()
			}
			photos = append(photos, appdto.UploadedPhoto{Content: raw})
		default:
			_ = part.Close()
		}
	}
	return content, photos, nil
}

// ListAll returns a page of every order, newest first by default, with its owner,
// state and total. An optional `status` narrows it to one state and an optional
// `sort` chooses the ordering, so `status=PENDING&sort=oldest` is the artist's
// FIFO confirmation queue. The route is behind the administrator role guard, so
// the caller is always an administrator; no owner filter is applied (FR-021,
// FR-026).
//
//	@Summary		List every order (administrator)
//	@Description	Returns every order, newest first by default, with its owner, state and total, paginated. An optional `status` filters to one order state and an optional `sort` chooses the ordering; `status=PENDING&sort=oldest` is the confirmation queue oldest-first. Administrator role required.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query	int		false	"Page number (default 1)"
//	@Param			pageSize	query	int		false	"Page size (default 20)"
//	@Param			status		query	string	false	"Only orders in this state (PENDING, PAYMENT_PENDING, PAID, SHIPPING, DELIVERED, COMPLETED or CANCELLED)"
//	@Param			sort		query	string	false	"Ordering: newest (default) or oldest; oldest with status=PENDING is the FIFO confirmation queue"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.AdminOrderSummaryResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		429			{object}	httpx.SwaggerError
//	@Failure		500			{object}	httpx.SwaggerError
//	@Router			/admin/orders [get]
func (h *Handler) ListAll(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	status, sort, appErr := adminListFilter(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.ListAll(ctx, appdto.ListInput{Page: page, PageSize: pageSize, Status: status, Sort: sort})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toAdminOrderSummaryResponses(out.Orders), out.Page, out.PageSize, out.Total)
}

// GetByIDAdmin reads any order in full, including its owner, its address and its
// lines. An unknown identifier answers 404 ORDER_NOT_FOUND (FR-021).
//
//	@Summary		Read one order in full (administrator)
//	@Description	Returns any order in full, including its owner. An unknown order answers 404 ORDER_NOT_FOUND. Administrator role required.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId} [get]
func (h *Handler) GetByIDAdmin(w http.ResponseWriter, r *http.Request) {
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
	view, err := h.svc.GetByIDAdmin(ctx, appdto.OrderRefInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminOrderResponse(view))
}

// ConfirmByAdmin accepts an order awaiting the artist: it holds the whole order
// all-or-nothing and opens the payment window, moving it to PAYMENT_PENDING. A
// move the current state does not allow answers 409
// ORDER_STATE_TRANSITION_INVALID naming the current state; a line that cannot be
// held answers 409 ORDER_QUANTITY_EXCEEDS_AVAILABLE naming the item (FR-004,
// FR-005, contracts/error-codes.md).
//
//	@Summary		Confirm an order awaiting the artist (administrator)
//	@Description	Holds the whole order all-or-nothing and opens the payment window, moving it to PAYMENT_PENDING. A wrong state answers 409 ORDER_STATE_TRANSITION_INVALID; a line that cannot be held answers 409 ORDER_QUANTITY_EXCEEDS_AVAILABLE. Administrator role required.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId}/confirm [post]
func (h *Handler) ConfirmByAdmin(w http.ResponseWriter, r *http.Request) {
	h.adminMove(w, r, h.svc.ConfirmByAdmin)
}

// RejectByAdmin declines an order awaiting the artist: it moves the order to
// CANCELLED. No goods were held, so nothing is returned. A move the current
// state does not allow answers 409 ORDER_STATE_TRANSITION_INVALID naming the
// current state (FR-018, contracts/error-codes.md).
//
//	@Summary		Decline an order awaiting the artist (administrator)
//	@Description	Declines an order awaiting the artist's confirmation, moving it to CANCELLED. No goods were held, so nothing is returned. A wrong state answers 409 ORDER_STATE_TRANSITION_INVALID naming the current state. Administrator role required.
//	@Tags			Orders
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId}/reject [post]
func (h *Handler) RejectByAdmin(w http.ResponseWriter, r *http.Request) {
	h.adminMove(w, r, h.svc.RejectByAdmin)
}

// StartShippingByAdmin dispatches a paid order, attaching zero to five optional
// dispatch photos, and records the act. A move the current state does not allow
// answers 409 ORDER_STATE_TRANSITION_INVALID naming the current state; more than
// five photos answer 409 ORDER_IMAGE_LIMIT_REACHED; an unsupported photo answers
// 400 ORDER_IMAGE_TYPE_UNSUPPORTED and one over the ceiling answers 413
// ORDER_IMAGE_TOO_LARGE, both before anything is stored (FR-005, FR-006, FR-009,
// contracts/error-codes.md).
//
//	@Summary		Mark a paid order in transit, attaching up to five photos (administrator)
//	@Description	Moves a paid order in transit and records the act naming the order and the administrator, optionally attaching up to five dispatch photos. Each photo is validated by signature (JPEG, PNG or WebP) and size before it is stored. The order must be paid; an illegal move answers 409 ORDER_STATE_TRANSITION_INVALID naming the current state, more than five photos answer 409 ORDER_IMAGE_LIMIT_REACHED, an unsupported photo answers 400 ORDER_IMAGE_TYPE_UNSUPPORTED and an oversized one answers 413 ORDER_IMAGE_TOO_LARGE. Administrator role required.
//	@Tags			Orders
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path		string	true	"Order identifier"
//	@Param			photos	formData	file	false	"Dispatch evidence photo (JPEG, PNG or WebP); send the part zero to five times"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		413		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId}/ship [post]
func (h *Handler) StartShippingByAdmin(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	photos, appErr := h.readEvidencePhotos(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.StartShippingByAdmin(ctx, appdto.EvidenceInput{OrderID: orderID, Photos: photos})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminOrderResponse(view))
}

// DeliverByAdmin marks an in-transit order delivered, attaching zero to five
// optional delivery photos, and records the act and the delivery instant. A move
// the current state does not allow answers 409 ORDER_STATE_TRANSITION_INVALID
// naming the current state; more than five photos answer 409
// ORDER_IMAGE_LIMIT_REACHED; an unsupported photo answers 400
// ORDER_IMAGE_TYPE_UNSUPPORTED and one over the ceiling answers 413
// ORDER_IMAGE_TOO_LARGE, both before anything is stored; a media provider refusal
// answers 503 ORDER_MEDIA_UNAVAILABLE (FR-007, FR-008, FR-009,
// contracts/error-codes.md).
//
//	@Summary		Mark an in-transit order delivered, attaching up to five photos (administrator)
//	@Description	Moves an in-transit order to delivered, records the delivery instant and the act naming the order and the administrator, optionally attaching up to five delivery photos. Each photo is validated by signature (JPEG, PNG or WebP) and size before it is stored. The order must be in transit; an illegal move answers 409 ORDER_STATE_TRANSITION_INVALID naming the current state, more than five photos answer 409 ORDER_IMAGE_LIMIT_REACHED, an unsupported photo answers 400 ORDER_IMAGE_TYPE_UNSUPPORTED and an oversized one answers 413 ORDER_IMAGE_TOO_LARGE. Administrator role required.
//	@Tags			Orders
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path		string	true	"Order identifier"
//	@Param			photos	formData	file	false	"Delivery evidence photo (JPEG, PNG or WebP); send the part zero to five times"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		413		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId}/deliver [post]
func (h *Handler) DeliverByAdmin(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	photos, appErr := h.readEvidencePhotos(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.DeliverByAdmin(ctx, appdto.EvidenceInput{OrderID: orderID, Photos: photos})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminOrderResponse(view))
}

// readEvidencePhotos streams the "photos" parts of a multipart body, refusing to
// buffer more than the image ceiling per part. Reading with a ceiling is what
// makes the size rule enforceable: a post-hoc check on an already buffered body
// cannot stop a memory-exhaustion upload. The client-declared file name and media
// type are deliberately not trusted — the use case decides the image type from the
// leading signature (FR-009).
//
// An absent body is zero photos, so a dispatch with no evidence is the empty
// request it is (FR-006). The route's own body ceiling refuses a body whose size
// is already over the limit before this runs, and the reader maps a stream cut by
// that ceiling to the same ORDER_IMAGE_TOO_LARGE refusal.
func (h *Handler) readEvidencePhotos(r *http.Request) ([]appdto.UploadedPhoto, *httpx.AppError) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fieldError(fieldPhotos, "a multipart/form-data body with photo parts is required")
	}
	ceiling := int64(model.MaxDeliveryPhotoBytes)
	photos := make([]appdto.UploadedPhoto, 0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, photosTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if part.FormName() != fieldPhotos {
			_ = part.Close()
			continue
		}
		// One extra byte distinguishes "at the ceiling" from "over it".
		content, err := io.ReadAll(io.LimitReader(part, ceiling+1))
		_ = part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, photosTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if int64(len(content)) > ceiling {
			return nil, photosTooLarge()
		}
		photos = append(photos, appdto.UploadedPhoto{Content: content})
	}
	return photos, nil
}

// Transfer hands a paid order to the account whose email is given, changing only
// the owner. A missing or malformed `email` is a request-shape refusal naming the
// member; an unpaid order answers 409 ORDER_NOT_TRANSFERABLE and an email no
// account carries answers 404 ORDER_TRANSFER_TARGET_NOT_FOUND (FR-024,
// contracts/error-codes.md).
//
//	@Summary		Transfer a paid order to another account (administrator)
//	@Description	Hands a paid order to the account whose email is given, changing only the owner. The order's lines, state and total are unchanged and no stock moves. An unpaid order cannot be transferred. Administrator role required.
//	@Tags			Orders
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			orderId	path	string	true	"Order identifier"
//	@Param			request	body	httpdto.TransferRequest	true	"Recipient account email"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminOrderResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/admin/orders/{orderId}/transfer [post]
func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	orderID, appErr := pathUUID(r, "orderId", fieldOrderID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}

	var req httpdto.TransferRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	email := strings.TrimSpace(req.Email)
	if !validEmail(email) {
		httpx.WriteError(w, r, fieldError(fieldEmail, "must be a valid email"), h.logger)
		return
	}

	ctx := appinterface.WithActor(r.Context(), actor)
	view, err := h.svc.Transfer(ctx, appdto.TransferInput{OrderID: orderID, Email: email})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminOrderResponse(view))
}

// validEmail reports whether the trimmed value is a single, bare email address.
// A missing or malformed recipient stays on the shared VALIDATION_ERROR naming
// `email`, exactly as the contract requires, rather than reaching the use case as
// an unknown account (contracts/error-codes.md). It requires the parsed address
// to equal the input, so a display name is refused.
func validEmail(raw string) bool {
	address, err := mail.ParseAddress(raw)
	if err != nil {
		return false
	}
	return address.Address == raw
}

// adminMove is the shared body of the two administrator transitions: it resolves
// the session, reads the addressed identifier, calls the move and answers the
// advanced order. The two moves differ only in the use case they invoke, so the
// guard, the identifier parsing and the response are written once.
func (h *Handler) adminMove(w http.ResponseWriter, r *http.Request,
	move func(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error)) {
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
	view, err := move(ctx, appdto.OrderRefInput{OrderID: orderID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminOrderResponse(view))
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

// pathUUID reads a UUID path parameter.
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	return parseUUID(chi.URLParam(r, name), field)
}

// pageParams reads the order-list window. Pagination validation is a foundation
// concern and stays on the shared VALIDATION_ERROR code (contracts/error-codes.md,
// research D14).
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

// adminListFilter reads the operator list's optional state filter and ordering.
// Both are optional: an absent status applies no filter, and an absent sort keeps
// the default newest-first. A value outside the permitted set stays on the shared
// VALIDATION_ERROR naming the member, exactly as the contract requires (FR-026,
// research D11, contracts/error-codes.md).
func adminListFilter(r *http.Request) (*constant.Status, constant.OrderListSort, *httpx.AppError) {
	query := r.URL.Query()

	var status *constant.Status
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		candidate := constant.Status(raw)
		if !constant.IsValidStatus(candidate) {
			return nil, "", fieldError(fieldStatus, "must be one of the seven order states")
		}
		status = &candidate
	}

	sort := constant.SortNewest
	if raw := strings.TrimSpace(query.Get("sort")); raw != "" {
		candidate := constant.OrderListSort(raw)
		switch candidate {
		case constant.SortNewest, constant.SortOldest:
			sort = candidate
		default:
			return nil, "", fieldError(fieldSort, "must be newest or oldest")
		}
	}
	return status, sort, nil
}

// toMoneyResponse is the single conversion from a money DTO to its wire shape, so
// the amount and currency cannot drift per route.
func toMoneyResponse(money appdto.MoneyView) httpdto.MoneyResponse {
	return httpdto.MoneyResponse{Amount: money.Amount, Currency: money.Currency}
}

// toOrderSummaryResponses maps a page of list rows. It always allocates, so an
// empty list serialises `data: []` rather than null.
func toOrderSummaryResponses(summaries []appdto.OrderSummaryView) []httpdto.OrderSummaryResponse {
	out := make([]httpdto.OrderSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, toOrderSummaryResponse(summary))
	}
	return out
}

// toOrderSummaryResponse maps one customer list row to its wire shape.
func toOrderSummaryResponse(summary appdto.OrderSummaryView) httpdto.OrderSummaryResponse {
	return httpdto.OrderSummaryResponse{
		ID:        summary.ID,
		Status:    string(summary.Status),
		Total:     toMoneyResponse(summary.Total),
		ItemCount: summary.ItemCount,
		CreatedAt: summary.CreatedAt,
	}
}

// toAdminOrderSummaryResponses maps a page of the administrator's list rows. It
// always allocates, so an empty list serialises `data: []` rather than null, and
// each row carries its owner.
func toAdminOrderSummaryResponses(summaries []appdto.AdminOrderSummaryView) []httpdto.AdminOrderSummaryResponse {
	out := make([]httpdto.AdminOrderSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, httpdto.AdminOrderSummaryResponse{
			OrderSummaryResponse: toOrderSummaryResponse(summary.OrderSummaryView),
			UserID:               summary.UserID,
		})
	}
	return out
}

// toAdminOrderResponse maps one administrator order view to its wire shape: the
// customer detail plus the owner, so the two shapes cannot drift apart. The
// delivery facts travel with the embedded customer detail, because the
// administrator reads the same evidence the customer does (FR-010, FR-017).
func toAdminOrderResponse(view appdto.AdminOrderView) httpdto.AdminOrderResponse {
	return httpdto.AdminOrderResponse{
		OrderResponse: toOrderResponse(appdto.OrderView{
			OrderSummaryView: view.OrderSummaryView,
			Address:          view.Address,
			Lines:            view.Lines,
			DeliveredAt:      view.DeliveredAt,
			CompletedAt:      view.CompletedAt,
			DeliveryPhotos:   view.DeliveryPhotos,
			Feedback:         view.Feedback,
		}),
		UserID: view.UserID,
	}
}

// toDeliveryPhotoResponses maps an order's evidence, preserving the
// stage-then-position order the use case applied. It always allocates, so an
// order with no evidence serialises `deliveryPhotos: []` rather than null.
func toDeliveryPhotoResponses(photos []appdto.DeliveryPhotoView) []httpdto.DeliveryPhotoResponse {
	out := make([]httpdto.DeliveryPhotoResponse, 0, len(photos))
	for _, photo := range photos {
		out = append(out, httpdto.DeliveryPhotoResponse{
			ID:       photo.ID,
			Stage:    photo.Stage,
			URL:      photo.URL,
			Width:    photo.Width,
			Height:   photo.Height,
			Position: photo.Position,
		})
	}
	return out
}

// toFeedbackPhotoResponses maps a feedback's images, preserving the position
// order the use case applied. It always allocates, so a feedback with no photos
// serialises `photos: []` rather than null.
func toFeedbackPhotoResponses(photos []appdto.FeedbackPhotoView) []httpdto.FeedbackPhotoResponse {
	out := make([]httpdto.FeedbackPhotoResponse, 0, len(photos))
	for _, photo := range photos {
		out = append(out, httpdto.FeedbackPhotoResponse{
			ID:       photo.ID,
			URL:      photo.URL,
			Width:    photo.Width,
			Height:   photo.Height,
			Position: photo.Position,
		})
	}
	return out
}

// toFeedbackResponse maps the order's feedback, or returns nil when it has none,
// so a detail serialises `feedback: null` (FR-015, FR-017, FR-025).
func toFeedbackResponse(feedback *appdto.FeedbackView) *httpdto.FeedbackResponse {
	if feedback == nil {
		return nil
	}
	return &httpdto.FeedbackResponse{
		Content:   feedback.Content,
		CreatedAt: feedback.CreatedAt,
		Photos:    toFeedbackPhotoResponses(feedback.Photos),
	}
}

// toOrderResponse maps one order view to its wire shape. It always returns a
// non-nil slice of lines and delivery photos, so an order serialises `lines: []`
// and `deliveryPhotos: []` rather than null.
func toOrderResponse(view appdto.OrderView) httpdto.OrderResponse {
	lines := make([]httpdto.OrderLineResponse, 0, len(view.Lines))
	for _, line := range view.Lines {
		lines = append(lines, httpdto.OrderLineResponse{
			ProductID:   line.ProductID,
			ProductName: line.ProductName,
			VariantID:   line.VariantID,
			VariantName: line.VariantName,
			Quantity:    line.Quantity,
			UnitPrice:   toMoneyResponse(line.UnitPrice),
			LineTotal:   toMoneyResponse(line.LineTotal),
			Position:    line.Position,
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
			FacebookURL:    view.Address.FacebookURL,
		},
		Lines: lines,
		// The delivery facts are detail-only; a list row never carries them
		// (research D11).
		DeliveredAt:    view.DeliveredAt,
		CompletedAt:    view.CompletedAt,
		DeliveryPhotos: toDeliveryPhotoResponses(view.DeliveryPhotos),
		Feedback:       toFeedbackResponse(view.Feedback),
	}
}
