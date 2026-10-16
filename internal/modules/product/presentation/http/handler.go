package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Pagination bounds of the public catalogue. They match the project's existing
// convention.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the product use cases to HTTP.
//
// It consumes application/interface.ProductService directly — the one declared
// use-case surface — rather than declaring its own subset of it: the real
// *implement.Service satisfies it, and a second declaration for the same service
// is the drift the project refuses elsewhere.
type Handler struct {
	svc    appinterface.ProductService
	cfg    appinterface.Config
	logger *slog.Logger
}

// New creates the product HTTP handler.
//
// The picture configuration is carried here as well as in the use cases because
// the upload routes size their body limit from the very ceiling the use case
// enforces, so a declared length and a chunked body answer the same rule.
func New(svc appinterface.ProductService, cfg appinterface.Config, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, cfg: cfg, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// ListPublic returns a page of the catalogue a customer may see. It is reachable
// with no session (FR-001): the route installs no authentication middleware.
//
// The page is one entry per product, with the lowest on-sale variant price and the
// default variant's image. The `category` filter is passed down as the slug a
// customer-facing link carries; the use case resolves it, and a hidden or unknown
// slug answers an empty page rather than a not-found (FR-006).
//
//	@Summary		List the catalogue a customer can see
//	@Description	Returns a page of the visible catalogue — one entry per product, ordered by the operator's position. A product appears only when at least one of its variants is on sale; the card price is the lowest on-sale variant price and the image is the default variant's image. The optional category filter narrows the list by category slug; a hidden or unknown slug answers an empty list. No session is required.
//	@Tags			Product
//	@Produce		json
//	@Param			page		query	int		false	"Page number (default 1)"
//	@Param			pageSize	query	int		false	"Page size (default 20)"
//	@Param			category	query	string	false	"Category slug to narrow the list to"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.PublicProductResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Router			/products [get]
func (h *Handler) ListPublic(w http.ResponseWriter, r *http.Request) {
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.ListPublic(r.Context(), appdto.ListPublicInput{
		Page:         page,
		PageSize:     pageSize,
		CategorySlug: strings.TrimSpace(r.URL.Query().Get("category")),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toPublicProductResponses(out.Products), out.Page, out.PageSize, out.Total)
}

// GetPublicByCode returns one visible product with its shared content, gallery and
// its visible variants, addressed by the code a customer-facing link is built from
// (FR-003). A hidden, removed or unknown code answers the same not-found (FR-008).
//
//	@Summary		Read one product by code (with its variants)
//	@Description	Returns one visible product with its description, gallery and its visible variants (on sale or out of stock), each with name, effective price and stock status. A hidden, removed or unknown code answers the same not-found. No session is required.
//	@Tags			Product
//	@Produce		json
//	@Param			code	path	string	true	"Product code"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.PublicProductDetailResponse}
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/products/{code} [get]
func (h *Handler) GetPublicByCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	out, err := h.svc.GetPublicByCode(r.Context(), appdto.PublicProductRefInput{Code: code})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPublicProductDetailResponse(out))
}

// ListAdmin returns a page of every product, including the ones withheld from
// customers (FR-010). It is reached only behind the administrator role guard.
//
//	@Summary		List every product (ADMIN)
//	@Description	Returns a page of every product, including the ones withheld from customers, with the code, the default price, the position, the variant count and the picture summary a customer never sees.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query	int	false	"Page number (default 1)"
//	@Param			pageSize	query	int	false	"Page size (default 20)"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.AdminProductResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Router			/admin/products [get]
func (h *Handler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.ListAdmin(r.Context(), appdto.ListAdminInput{Page: page, PageSize: pageSize})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toAdminProductResponses(out.Products), out.Page, out.PageSize, out.Total)
}

// GetAdmin returns one product by identifier, including one not visible to
// customers, with its gallery and every variant (FR-010).
//
//	@Summary		Read one product by identifier (ADMIN)
//	@Description	Returns one product, including one not visible to customers, with its gallery and every variant.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Product identifier"
//	@Success		200	{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400	{object}	httpx.SwaggerError
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Failure		404	{object}	httpx.SwaggerError
//	@Router			/admin/products/{id} [get]
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.GetAdmin(r.Context(), appdto.AdminProductRefInput{ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// CreateProduct creates a product with at least one variant (FR-012). The acting
// administrator comes from the session the middleware resolved, never from the
// body: the input carries no account member and the decoder refuses an unknown one.
//
//	@Summary		Create a product with at least one variant (ADMIN)
//	@Description	Creates a product together with at least one variant. The acting administrator comes from the session, never the body.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body	httpdto.CreateProductRequest	true	"Product payload"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/products [post]
func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	var req httpdto.CreateProductRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	variants := make([]appdto.CreateVariantInput, 0, len(req.Variants))
	for _, v := range req.Variants {
		variants = append(variants, appdto.CreateVariantInput{
			Name:        v.Name,
			SKU:         v.SKU,
			PriceAmount: variantPriceAmount(v.Price),
			Position:    v.Position,
		})
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.CreateProduct(ctx, appdto.CreateProductInput{
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		Price:       model.Price{Amount: req.DefaultPrice.Amount, Currency: req.DefaultPrice.Currency},
		CategoryID:  req.CategoryID,
		Position:    req.Position,
		Variants:    variants,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminProductDetailResponse(out))
}

// UpdateProduct applies a partial administrator edit (FR-010). An omitted member
// keeps its current value; the variants are edited through their own routes.
//
//	@Summary		Update a product (ADMIN)
//	@Description	Applies a partial edit. An omitted member keeps its value; variants are edited through their own routes.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string						true	"Product identifier"
//	@Param			request	body	httpdto.UpdateProductRequest	true	"Product update"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id} [patch]
func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.UpdateProductRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.UpdateProduct(ctx, appdto.UpdateProductInput{
		ID:          id,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		Price:       toPrice(req.DefaultPrice),
		CategoryID:  req.CategoryID,
		Position:    req.Position,
		IsVisible:   req.IsVisible,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// DeleteProduct removes a product and its variants (FR-010). Removal is a hard
// delete; the audit entry is what survives it.
//
//	@Summary		Delete a product (ADMIN)
//	@Description	Removes a product and its variants. Removal is a hard delete; the audit entry is what survives it.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Product identifier"
//	@Success		204	"No Content"
//	@Failure		400	{object}	httpx.SwaggerError
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Failure		404	{object}	httpx.SwaggerError
//	@Router			/admin/products/{id} [delete]
func (h *Handler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	if err := h.svc.DeleteProduct(ctx, appdto.AdminProductRefInput{ID: id}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddVariant adds a variant to a product (FR-014). A created variant starts in
// COMING_SOON; its sell state moves through the variant state endpoint.
//
//	@Summary		Add a variant to a product (ADMIN)
//	@Description	Adds a variant. A new variant starts in COMING_SOON. A duplicate SKU is a conflict.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string						true	"Product identifier"
//	@Param			request	body	httpdto.CreateVariantRequest	true	"Variant payload"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants [post]
func (h *Handler) AddVariant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.CreateVariantRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.AddVariant(ctx, appdto.AddVariantInput{
		ProductID:   id,
		Name:        req.Name,
		SKU:         req.SKU,
		PriceAmount: variantPriceAmount(req.Price),
		Position:    req.Position,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminProductDetailResponse(out))
}

// UpdateVariant edits a variant (FR-010). An omitted member keeps its current
// value; an explicit null price clears the variant's own amount (FR-017).
//
//	@Summary		Update a variant (ADMIN)
//	@Description	Applies a partial variant edit. An omitted member keeps its value; an explicit null price clears the variant's own amount back to the product's default.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string						true	"Product identifier"
//	@Param			variantId	path	string						true	"Variant identifier"
//	@Param			request		body	httpdto.UpdateVariantRequest	true	"Variant update"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		409			{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants/{variantId} [patch]
func (h *Handler) UpdateVariant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	variantID, appErr := pathUUID(r, "variantId", fieldVariantID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.UpdateVariantRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	in := appdto.UpdateVariantInput{
		ProductID:      id,
		VariantID:      variantID,
		Name:           req.Name,
		SKU:            req.SKU,
		PriceAmountSet: req.Price.Set,
		Position:       req.Position,
	}
	if req.Price.Set && req.Price.Value != nil {
		amount := req.Price.Value.Amount
		in.PriceAmount = &amount
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.UpdateVariant(ctx, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// RemoveVariant removes a variant (FR-012). Refused when it is the product's last.
//
//	@Summary		Remove a variant (ADMIN)
//	@Description	Removes a variant. Refused with VARIANT_LAST_REMOVAL when it is the product's last variant.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string	true	"Product identifier"
//	@Param			variantId	path	string	true	"Variant identifier"
//	@Success		204			"No Content"
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		409			{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants/{variantId} [delete]
func (h *Handler) RemoveVariant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	variantID, appErr := pathUUID(r, "variantId", fieldVariantID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	if err := h.svc.RemoveVariant(ctx, appdto.VariantRefInput{ProductID: id, VariantID: variantID}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddVariantImage stores an uploaded variant image. The bytes are read with a hard
// ceiling and handed to the use case, which validates them by content; uploading
// again replaces the variant's single image (FR-016).
//
//	@Summary		Upload the variant image (ADMIN)
//	@Description	Stores a picture for the variant through the media provider. Uploading again replaces the variant's single image. The content is validated by signature, not by name or declared media type.
//	@Tags			Product
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string	true	"Product identifier"
//	@Param			variantId	path		string	true	"Variant identifier"
//	@Param			image		formData	file	true	"Variant image (JPEG, PNG or WebP)"
//	@Success		201			{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		413			{object}	httpx.SwaggerError
//	@Failure		503			{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants/{variantId}/image [post]
func (h *Handler) AddVariantImage(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	variantID, appErr := pathUUID(r, "variantId", fieldVariantID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	content, appErr := h.readPictureFile(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.AddVariantImage(ctx, appdto.AddVariantImageInput{
		ProductID: id,
		VariantID: variantID,
		Content:   content,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminProductDetailResponse(out))
}

// RemoveVariantImage clears the variant's image and releases its stored asset
// (FR-016).
//
//	@Summary		Remove the variant image (ADMIN)
//	@Description	Clears the variant's image and releases its stored asset.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string	true	"Product identifier"
//	@Param			variantId	path	string	true	"Variant identifier"
//	@Success		204			"No Content"
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants/{variantId}/image [delete]
func (h *Handler) RemoveVariantImage(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	variantID, appErr := pathUUID(r, "variantId", fieldVariantID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	if _, err := h.svc.RemoveVariantImage(ctx, appdto.VariantRefInput{ProductID: id, VariantID: variantID}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ChangeVariantState moves a variant through its selling life (FR-015).
//
// The handler only decodes the requested target and reports: whether the move is
// one the current state allows is the domain transition table's decision, and the
// refusal names the current state. A target that is not one of the four states is
// reported against `to` by the use case. The acting administrator comes from the
// session, never from the body.
//
//	@Summary		Move a variant through its selling life (ADMIN)
//	@Description	Requests a variant sell-state transition. The transition table decides whether the move is allowed; an invalid one is refused naming the current state. The acting administrator comes from the session, never the body.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string							true	"Product identifier"
//	@Param			variantId	path	string							true	"Variant identifier"
//	@Param			request		body	httpdto.ChangeVariantStateRequest	true	"Target sell state"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Failure		404			{object}	httpx.SwaggerError
//	@Failure		409			{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/variants/{variantId}/state [post]
func (h *Handler) ChangeVariantState(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	variantID, appErr := pathUUID(r, "variantId", fieldVariantID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.ChangeVariantStateRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.ChangeVariantState(ctx, appdto.ChangeVariantStateInput{
		ProductID: id,
		VariantID: variantID,
		To:        constant.SellState(req.To),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// AddPicture stores an uploaded gallery picture. The bytes are read with a hard
// ceiling and handed to the use case, which validates them by content and by the
// gallery's count; neither the file name nor the client-declared media type is
// trusted (FR-019).
//
//	@Summary		Upload a product picture (ADMIN)
//	@Description	Stores a picture for the product's gallery through the media provider. The content is validated by signature, not by name or declared media type. The first picture becomes the main one.
//	@Tags			Product
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string	true	"Product identifier"
//	@Param			image	formData	file	true	"Product image (JPEG, PNG or WebP)"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		413		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/images [post]
func (h *Handler) AddPicture(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	content, appErr := h.readPictureFile(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.AddPicture(ctx, appdto.AddPictureInput{ProductID: id, Content: content})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminProductDetailResponse(out))
}

// RemovePicture detaches one gallery picture from a product and releases its
// stored asset (FR-021).
//
//	@Summary		Remove a product picture (ADMIN)
//	@Description	Detaches one picture from the product's gallery and releases its stored asset. Removing the main picture promotes the next one by position.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Product identifier"
//	@Param			imageId	path	string	true	"Picture identifier"
//	@Success		204		"No Content"
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/images/{imageId} [delete]
func (h *Handler) RemovePicture(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	imageID, appErr := pathUUID(r, "imageId", fieldImageID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	if err := h.svc.RemovePicture(ctx, appdto.RemovePictureInput{ProductID: id, PictureID: imageID}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPrimaryPicture makes one gallery picture the product's main one (FR-021).
//
//	@Summary		Set the main product picture (ADMIN)
//	@Description	Makes one picture the product's main one, clearing any other in the same operation.
//	@Tags			Product
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Product identifier"
//	@Param			imageId	path	string	true	"Picture identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/images/{imageId}/primary [post]
func (h *Handler) SetPrimaryPicture(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	imageID, appErr := pathUUID(r, "imageId", fieldImageID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.SetPrimaryPicture(ctx, appdto.SetPrimaryPictureInput{ProductID: id, PictureID: imageID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// pageParams reads the catalogue window. Pagination validation is a foundation
// concern and stays on the shared VALIDATION_ERROR code.
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

// decode reads a JSON body and rejects unknown members, matching the
// additionalProperties: false of the administrator request schemas. A body that
// tries to name the acting account is therefore refused rather than decoded.
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No maintenance route accepts an owner identifier: the session is the only
// source of the acting account, which is what makes an actor chosen by a client
// impossible by construction (FR-015). It reports false after writing the
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
// with the offending member named.
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	raw := chi.URLParam(r, name)
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "must be a UUID")
	}
	return parsed, nil
}

// readPictureFile streams the "image" part of the multipart body, refusing to
// buffer more than the configured ceiling. Reading with a ceiling is what makes
// the size rule enforceable: a post-hoc check on an already buffered body cannot
// stop a memory-exhaustion upload.
//
// The client-declared file name and media type are deliberately not trusted: the
// use case decides the image type from the leading signature.
func (h *Handler) readPictureFile(r *http.Request) ([]byte, *httpx.AppError) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fieldError(fieldImage, "a multipart/form-data body with an image part is required")
	}
	ceiling := h.cfg.PictureMaxBytesOrDefault()
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, imageTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if part.FormName() != fieldImage {
			_ = part.Close()
			continue
		}
		// One extra byte distinguishes "at the ceiling" from "over it".
		content, err := io.ReadAll(io.LimitReader(part, ceiling+1))
		_ = part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, imageTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if int64(len(content)) > ceiling {
			return nil, imageTooLarge()
		}
		return content, nil
	}
	return nil, fieldError(fieldImage, "an image part is required")
}

// variantPriceAmount converts an optional request price to the domain's optional
// amount. A nil price means the variant uses the product's default (FR-017).
func variantPriceAmount(price *httpdto.VariantPriceRequest) *int64 {
	if price == nil {
		return nil
	}
	amount := price.Amount
	return &amount
}

// toPrice converts an optional request price to the domain value. A nil request
// price keeps the stored one, which is what makes a partial edit partial.
func toPrice(price *httpdto.PriceRequest) *model.Price {
	if price == nil {
		return nil
	}
	return &model.Price{Amount: price.Amount, Currency: price.Currency}
}

// toPriceResponse is the single conversion from a domain price to its HTTP shape.
func toPriceResponse(price model.Price) httpdto.PriceResponse {
	return httpdto.PriceResponse{Amount: price.Amount, Currency: price.Currency}
}

// toPublicProductResponse is the single conversion point from the application DTO
// to the public HTTP shape, so the shape cannot drift per route.
func toPublicProductResponse(product appdto.PublicProductOutput) httpdto.PublicProductResponse {
	return httpdto.PublicProductResponse{
		ID:       product.ID,
		Name:     product.Name,
		Code:     product.Code,
		Price:    toPriceResponse(product.Price),
		ImageURL: product.ImageURL,
	}
}

// toPublicProductResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null.
func toPublicProductResponses(list []appdto.PublicProductOutput) []httpdto.PublicProductResponse {
	out := make([]httpdto.PublicProductResponse, 0, len(list))
	for _, product := range list {
		out = append(out, toPublicProductResponse(product))
	}
	return out
}

// toPublicProductDetailResponse maps one product's detail, including its gallery
// and its variants.
func toPublicProductDetailResponse(detail appdto.PublicProductDetailOutput) httpdto.PublicProductDetailResponse {
	return httpdto.PublicProductDetailResponse{
		PublicProductResponse: toPublicProductResponse(detail.PublicProductOutput),
		Description:           detail.Description,
		Images:                toPublicImageResponses(detail.Images),
		Variants:              toPublicVariantResponses(detail.Variants),
	}
}

// toPublicVariantResponse is the single conversion point for a variant a customer
// reads, so its effective price and stock flag cannot drift per route.
func toPublicVariantResponse(variant appdto.PublicVariantOutput) httpdto.PublicVariantResponse {
	return httpdto.PublicVariantResponse{
		ID:        variant.ID,
		Name:      variant.Name,
		Price:     toPriceResponse(variant.Price),
		ImageURL:  variant.ImageURL,
		SellState: string(variant.SellState),
		InStock:   variant.InStock,
	}
}

// toPublicVariantResponses maps the visible variants in the order they were given.
// It always allocates, so a product with no visible variant answers an empty array.
func toPublicVariantResponses(list []appdto.PublicVariantOutput) []httpdto.PublicVariantResponse {
	out := make([]httpdto.PublicVariantResponse, 0, len(list))
	for _, variant := range list {
		out = append(out, toPublicVariantResponse(variant))
	}
	return out
}

// toPublicImageResponses maps the gallery pictures in the order they were given.
// It always allocates, so a product with no picture answers an empty array.
func toPublicImageResponses(list []appdto.PublicImageOutput) []httpdto.PublicImageResponse {
	out := make([]httpdto.PublicImageResponse, 0, len(list))
	for _, image := range list {
		out = append(out, httpdto.PublicImageResponse{
			ID:     image.ID,
			URL:    image.URL,
			Width:  image.Width,
			Height: image.Height,
		})
	}
	return out
}

// toAdminProductResponse is the single conversion point from the application
// administrator DTO to the administrator HTTP shape, so the members a customer
// must not see cannot drift per route.
func toAdminProductResponse(product appdto.AdminProductOutput) httpdto.AdminProductResponse {
	return httpdto.AdminProductResponse{
		ID:           product.ID,
		Name:         product.Name,
		Code:         product.Code,
		Description:  product.Description,
		DefaultPrice: toPriceResponse(product.Price),
		CategoryID:   product.CategoryID,
		Position:     product.Position,
		IsVisible:    product.IsVisible,
		VariantCount: product.VariantCount,
		ImageURL:     product.ImageURL,
		CreatedAt:    product.CreatedAt,
		UpdatedAt:    product.UpdatedAt,
	}
}

// toAdminProductResponses maps a page. It always allocates, so an empty catalogue
// answers an empty array rather than null.
func toAdminProductResponses(list []appdto.AdminProductOutput) []httpdto.AdminProductResponse {
	out := make([]httpdto.AdminProductResponse, 0, len(list))
	for _, product := range list {
		out = append(out, toAdminProductResponse(product))
	}
	return out
}

// toAdminProductDetailResponse maps one product's administrator detail, including
// its gallery and every variant.
func toAdminProductDetailResponse(detail appdto.AdminProductDetailOutput) httpdto.AdminProductDetailResponse {
	return httpdto.AdminProductDetailResponse{
		AdminProductResponse: toAdminProductResponse(detail.AdminProductOutput),
		Images:               toAdminImageResponses(detail.Images),
		Variants:             toAdminVariantResponses(detail.Variants),
	}
}

// toAdminVariantResponse is the single conversion point for a variant as an
// administrator reads it.
func toAdminVariantResponse(variant appdto.AdminVariantOutput) httpdto.AdminVariantResponse {
	return httpdto.AdminVariantResponse{
		ID:             variant.ID,
		Name:           variant.Name,
		SKU:            variant.SKU,
		Price:          toAdminVariantPrice(variant.PriceAmount),
		EffectivePrice: toPriceResponse(variant.EffectivePrice),
		ImageURL:       variant.ImageURL,
		Position:       variant.Position,
		SellState:      string(variant.SellState),
	}
}

// toAdminVariantPrice turns the optional own amount into the nullable price object
// the contract exposes: a nil amount is the variant falling back to the product's
// default, never a zero price.
func toAdminVariantPrice(amount *int64) *httpdto.AdminVariantPriceResponse {
	if amount == nil {
		return nil
	}
	return &httpdto.AdminVariantPriceResponse{Amount: *amount}
}

// toAdminVariantResponses maps the variants in the order they were given. It
// always allocates, so a product with no variant answers an empty array.
func toAdminVariantResponses(list []appdto.AdminVariantOutput) []httpdto.AdminVariantResponse {
	out := make([]httpdto.AdminVariantResponse, 0, len(list))
	for _, variant := range list {
		out = append(out, toAdminVariantResponse(variant))
	}
	return out
}

// toAdminImageResponses maps the gallery pictures in the order they were given. It
// always allocates, so a product with no picture answers an empty array.
func toAdminImageResponses(list []appdto.AdminImageOutput) []httpdto.AdminImageResponse {
	out := make([]httpdto.AdminImageResponse, 0, len(list))
	for _, image := range list {
		out = append(out, httpdto.AdminImageResponse{
			ID:        image.ID,
			PublicID:  image.PublicID,
			URL:       image.URL,
			Width:     image.Width,
			Height:    image.Height,
			Position:  image.Position,
			IsPrimary: image.IsPrimary,
		})
	}
	return out
}
