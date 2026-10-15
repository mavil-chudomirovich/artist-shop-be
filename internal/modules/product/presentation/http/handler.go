package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// Pagination bounds of the public catalogue (contracts/openapi.yaml,
// Page/PageSize). They match the project's existing convention (research D12).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the product use cases to HTTP.
//
// It consumes application/interface.ProductService directly — the one declared
// use-case surface — rather than declaring its own subset of it: the real
// *implement.Service satisfies it, and a second declaration for the same service
// is the drift the project refuses elsewhere (reconciliation with module 03's
// handler, which takes appinterface.CategoryService).
type Handler struct {
	svc    appinterface.ProductService
	cfg    appinterface.Config
	logger *slog.Logger
}

// New creates the product HTTP handler.
//
// The picture configuration is carried here as well as in the use cases because
// the upload route sizes its body limit from the very ceiling the use case
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
// The page is the merged catalogue: a standalone product and a variant group
// both appear as one entry, discriminated by `type`, so a grouped product is
// shown once, not once per option (FR-001). The `category` filter is passed down
// as the slug a customer-facing link carries; the use case resolves it, and a
// hidden or unknown slug answers an empty page rather than a not-found (FR-006).
//
//	@Summary		List the catalogue a customer can see (products and variant groups)
//	@Description	Returns a page of the visible catalogue — standalone products and variant groups — ordered by the operator's position. A group is visible only while at least one of its options is; its price is the lowest among its visible options. The optional category filter narrows the list by category slug; a hidden or unknown slug answers an empty list. No session is required.
//	@Tags			Product
//	@Produce		json
//	@Param			page		query	int		false	"Page number (default 1)"
//	@Param			pageSize	query	int		false	"Page size (default 20)"
//	@Param			category	query	string	false	"Category slug to narrow the list to"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.PublicCatalogueEntryResponse}
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
	httpx.WriteSuccessList(w, r, toPublicCatalogueEntryResponses(out.Entries), out.Page, out.PageSize, out.Total)
}

// GetPublicBySlug returns one visible product with its description and every
// picture, addressed by the slug a customer-facing link is built from
// (FR-005). A hidden, retired, removed or unknown slug answers the same
// not-found (FR-003). When the product is an option of a variant group, the
// detail carries the `variantGroup` selector (FR-007).
//
//	@Summary		Read one product by slug
//	@Description	Returns one visible product with its description and every picture, plus the variantGroup selector when the product is an option. A hidden, retired, removed or unknown slug answers the same not-found. No session is required.
//	@Tags			Product
//	@Produce		json
//	@Param			slug	path	string	true	"Product slug"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.PublicProductDetailResponse}
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/products/{slug} [get]
func (h *Handler) GetPublicBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	out, err := h.svc.GetPublicBySlug(r.Context(), appdto.PublicProductRefInput{Slug: slug})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPublicProductDetailResponse(out))
}

// GetPublicGroupBySlug returns one visible variant group, addressed by the slug a
// customer-facing link is built from: its shared name, description and gallery
// together with its visible options (FR-002). A group hidden by its category,
// with no visible option, removed or unknown answers the same not-found (FR-008).
//
//	@Summary		Read a variant group by slug
//	@Description	Returns a visible variant group's shared name, description, choice-dimension name and its own picture gallery, together with its visible options (each with label, price, picture and preorder flag). A group hidden by its category, with no visible option, removed or unknown answers the same not-found. No session is required.
//	@Tags			Product
//	@Produce		json
//	@Param			slug	path	string	true	"Variant group slug"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.PublicVariantGroupDetailResponse}
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/product-groups/{slug} [get]
func (h *Handler) GetPublicGroupBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	out, err := h.svc.GetPublicGroupBySlug(r.Context(), appdto.PublicVariantGroupRefInput{Slug: slug})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPublicVariantGroupDetailResponse(out))
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

// toPublicProductResponse is the single conversion point from the application
// DTO to the public HTTP shape, so the six-member shape cannot drift per route.
func toPublicProductResponse(product appdto.PublicProductOutput) httpdto.PublicProductResponse {
	return httpdto.PublicProductResponse{
		ID:         product.ID,
		Name:       product.Name,
		Slug:       product.Slug,
		Price:      httpdto.PriceResponse{Amount: product.Price.Amount, Currency: product.Price.Currency},
		ImageURL:   product.ImageURL,
		IsPreorder: product.IsPreorder,
	}
}

// toPublicCatalogueEntryResponse is the single conversion point from the merged
// application entry to the public HTTP shape, so the `type` discriminator and
// the seven members cannot drift per route.
func toPublicCatalogueEntryResponse(entry appdto.PublicCatalogueEntry) httpdto.PublicCatalogueEntryResponse {
	return httpdto.PublicCatalogueEntryResponse{
		Type:       entry.Type,
		ID:         entry.ID,
		Name:       entry.Name,
		Slug:       entry.Slug,
		Price:      httpdto.PriceResponse{Amount: entry.Price.Amount, Currency: entry.Price.Currency},
		ImageURL:   entry.ImageURL,
		IsPreorder: entry.IsPreorder,
	}
}

// toPublicCatalogueEntryResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null: the contract declares the
// list as an array, and FR-007 requires an empty result, not an error.
func toPublicCatalogueEntryResponses(list []appdto.PublicCatalogueEntry) []httpdto.PublicCatalogueEntryResponse {
	out := make([]httpdto.PublicCatalogueEntryResponse, 0, len(list))
	for _, entry := range list {
		out = append(out, toPublicCatalogueEntryResponse(entry))
	}
	return out
}

// toPublicProductDetailResponse maps one product's detail. The pictures are
// mapped through their own conversion so the public picture shape cannot gain a
// member the contract does not declare; the optional variantGroup selector is
// mapped through its own conversion too.
func toPublicProductDetailResponse(detail appdto.PublicProductDetailOutput) httpdto.PublicProductDetailResponse {
	return httpdto.PublicProductDetailResponse{
		PublicProductResponse: toPublicProductResponse(detail.PublicProductOutput),
		Description:           detail.Description,
		Images:                toPublicImageResponses(detail.Images),
		VariantGroup:          toPublicProductVariantGroupResponse(detail.VariantGroup),
	}
}

// toPublicProductVariantGroupResponse maps the selector when the product is an
// option. A nil selector stays nil, so a standalone product omits the member.
func toPublicProductVariantGroupResponse(selector *appdto.PublicProductVariantGroupOutput) *httpdto.PublicProductVariantGroupResponse {
	if selector == nil {
		return nil
	}
	return &httpdto.PublicProductVariantGroupResponse{
		ID:         selector.ID,
		Slug:       selector.Slug,
		Name:       selector.Name,
		OptionName: selector.OptionName,
		Images:     toPublicImageResponses(selector.Images),
		Options:    toPublicVariantOptionResponses(selector.Options),
	}
}

// toPublicVariantGroupDetailResponse maps one group's detail. It always
// allocates the lists, so a group with no picture or no option answers `[]`
// rather than null.
func toPublicVariantGroupDetailResponse(detail appdto.PublicVariantGroupDetailOutput) httpdto.PublicVariantGroupDetailResponse {
	return httpdto.PublicVariantGroupDetailResponse{
		ID:          detail.ID,
		Name:        detail.Name,
		Slug:        detail.Slug,
		Description: detail.Description,
		OptionName:  detail.OptionName,
		Images:      toPublicImageResponses(detail.Images),
		Options:     toPublicVariantOptionResponses(detail.Options),
	}
}

// toPublicVariantOptionResponse is the single conversion point for an option as
// a customer reads it, so its label and its own price cannot drift per route.
func toPublicVariantOptionResponse(option appdto.PublicVariantOptionOutput) httpdto.PublicVariantOptionResponse {
	return httpdto.PublicVariantOptionResponse{
		ID:         option.ID,
		Slug:       option.Slug,
		Label:      option.Label,
		Price:      httpdto.PriceResponse{Amount: option.Price.Amount, Currency: option.Price.Currency},
		ImageURL:   option.ImageURL,
		IsPreorder: option.IsPreorder,
	}
}

// toPublicVariantOptionResponses maps the options in the order they were given —
// the display order the use case and the repository already applied. It always
// allocates, so a group with no visible option answers `[]` rather than null.
func toPublicVariantOptionResponses(list []appdto.PublicVariantOptionOutput) []httpdto.PublicVariantOptionResponse {
	out := make([]httpdto.PublicVariantOptionResponse, 0, len(list))
	for _, option := range list {
		out = append(out, toPublicVariantOptionResponse(option))
	}
	return out
}

// toPublicImageResponses maps the pictures in the order they were given — the
// display order the use case and the repository already applied. It always
// allocates, so a product with no picture answers `[]` rather than null.
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

// --- Administrator surface (US2) ---

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
// with the offending member named (contracts/error-codes.md).
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	raw := chi.URLParam(r, name)
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "must be a UUID")
	}
	return parsed, nil
}

// ListAdmin returns a page of every product, including the ones withheld from
// customers (FR-011). It is reached only behind the administrator role guard.
//
//	@Summary		List every product (ADMIN)
//	@Description	Returns a page of every product, including the ones withheld from customers, with the sell state, the position, the category and the picture summary a customer never sees.
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
// customers (FR-011). A malformed identifier is answered with the field named; an
// unknown one is the module not-found.
//
//	@Summary		Read one product by identifier (ADMIN)
//	@Description	Returns one product, including one not visible to customers, with its pictures and, for a set, its members.
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

// CreateProduct creates a product in COMING_SOON (FR-010). The acting
// administrator comes from the session the middleware resolved, never from the
// body: the input carries no account member and the decoder refuses an unknown
// one.
//
//	@Summary		Create a product (ADMIN)
//	@Description	Creates a product in COMING_SOON. The acting administrator comes from the session, never the body.
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
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.CreateProduct(ctx, appdto.CreateProductInput{
		Name:               req.Name,
		Slug:               req.Slug,
		Description:        req.Description,
		Price:              model.Price{Amount: req.Price.Amount, Currency: req.Price.Currency},
		CategoryID:         req.CategoryID,
		Position:           req.Position,
		IsSet:              req.IsSet,
		MemberProductIDs:   req.MemberProductIDs,
		IsPreorder:         req.IsPreorder,
		PreorderExpectedAt: toPreorderDate(req.PreorderExpectedAt),
		VariantGroupID:     req.VariantGroupID,
		VariantLabel:       req.VariantLabel,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminProductDetailResponse(out))
}

// UpdateProduct applies a partial administrator edit (FR-012). An omitted member
// keeps its current value; the sell state is never changed here (research D11).
//
//	@Summary		Update a product (ADMIN)
//	@Description	Applies a partial edit. An omitted member keeps its value; the sell state is never changed here (it has its own transition endpoint).
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
		ID:                 id,
		Name:               req.Name,
		Slug:               req.Slug,
		Description:        req.Description,
		Price:              toPricePointer(req.Price),
		CategoryID:         req.CategoryID,
		Position:           req.Position,
		IsSet:              req.IsSet,
		MemberProductIDs:   req.MemberProductIDs,
		IsPreorder:         req.IsPreorder,
		PreorderExpectedAt: toPreorderDate(req.PreorderExpectedAt),
		VariantGroupID:     req.VariantGroupID.Value,
		VariantGroupSet:    req.VariantGroupID.Set,
		VariantLabel:       req.VariantLabel.Value,
		VariantLabelSet:    req.VariantLabel.Set,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// DeleteProduct removes a product (FR-013). Removal is a hard delete; the audit
// entry is what survives it.
//
//	@Summary		Delete a product (ADMIN)
//	@Description	Removes a product. Removal is a hard delete; the audit entry is what survives it.
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

// ChangeSellState moves a product through its selling life (FR-022 to FR-026).
//
// The handler only decodes the requested target and reports: whether the move is
// one the current state allows is the domain transition table's decision, and the
// refusal names the current state (FR-023, FR-024). A target that is not one of the
// four states is reported against `to` by the use case. The acting administrator
// comes from the session, never from the body (FR-015).
//
//	@Summary		Move a product through its selling life (ADMIN)
//	@Description	Requests a sell-state transition. The transition table decides whether the move is allowed; an invalid one is refused naming the current state. The acting administrator comes from the session, never the body.
//	@Tags			Product
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string					true	"Product identifier"
//	@Param			request	body	httpdto.ChangeStateRequest	true	"Target sell state"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminProductDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/products/{id}/state [post]
func (h *Handler) ChangeSellState(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.ChangeStateRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.ChangeSellState(ctx, appdto.ChangeSellStateInput{
		ID: id,
		To: constant.SellState(req.To),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminProductDetailResponse(out))
}

// AddPicture stores an uploaded picture. The bytes are read with a hard ceiling
// and handed to the use case, which validates them by content and by the
// product's count; neither the file name nor the client-declared media type is
// trusted (FR-019, FR-020).
//
//	@Summary		Upload a product picture (ADMIN)
//	@Description	Stores a picture for the product through the media provider. The content is validated by signature, not by name or declared media type. The first picture becomes the main one.
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

// RemovePicture detaches one picture from a product and releases its stored asset
// (FR-021).
//
//	@Summary		Remove a product picture (ADMIN)
//	@Description	Detaches one picture from the product and releases its stored asset. Removing the main picture promotes the next one by position.
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

// SetPrimaryPicture makes one picture the product's main one (FR-017).
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

// readPictureFile streams the "image" part of the multipart body, refusing to
// buffer more than the configured ceiling. Reading with a ceiling is what makes
// the size rule enforceable: a post-hoc check on an already buffered body cannot
// stop a memory-exhaustion upload.
//
// The client-declared file name and media type are deliberately not trusted: the
// use case decides the image type from the leading signature (FR-019).
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

// toPricePointer converts an optional request price to the domain value. A nil
// request price keeps the stored one, which is what makes a partial edit partial.
func toPricePointer(price *httpdto.PriceRequest) *model.Price {
	if price == nil {
		return nil
	}
	return &model.Price{Amount: price.Amount, Currency: price.Currency}
}

// toPreorderDate converts the request's date-only value to the domain's optional
// time. A nil request date keeps the stored one, which is what makes a partial
// edit partial; clearing the label clears the date explicitly (FR-040).
func toPreorderDate(date *httpdto.PreorderDate) *time.Time {
	if date == nil {
		return nil
	}
	at := date.Time
	return &at
}

// toPreorderDateResponse converts the stored optional time to the response's
// contract form. The administrator response writes the calendar date
// (format: date), so this is where the stored value becomes the date the
// contract declares; a nil date stays nil and marshals as null.
func toPreorderDateResponse(date *time.Time) *httpdto.PreorderDate {
	if date == nil {
		return nil
	}
	return &httpdto.PreorderDate{Time: *date}
}

// toAdminProductResponse is the single conversion point from the application
// administrator DTO to the administrator HTTP shape, so the members a customer
// must not see cannot drift per route.
func toAdminProductResponse(product appdto.AdminProductOutput) httpdto.AdminProductResponse {
	return httpdto.AdminProductResponse{
		ID:                 product.ID,
		Name:               product.Name,
		Slug:               product.Slug,
		Description:        product.Description,
		Price:              httpdto.PriceResponse{Amount: product.Price.Amount, Currency: product.Price.Currency},
		CategoryID:         product.CategoryID,
		Position:           product.Position,
		SellState:          string(product.SellState),
		IsSet:              product.IsSet,
		IsPreorder:         product.IsPreorder,
		PreorderExpectedAt: toPreorderDateResponse(product.PreorderExpectedAt),
		ImageCount:         product.ImageCount,
		ImageURL:           product.ImageURL,
		CreatedAt:          product.CreatedAt,
		UpdatedAt:          product.UpdatedAt,
	}
}

// toAdminProductResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null.
func toAdminProductResponses(list []appdto.AdminProductOutput) []httpdto.AdminProductResponse {
	out := make([]httpdto.AdminProductResponse, 0, len(list))
	for _, product := range list {
		out = append(out, toAdminProductResponse(product))
	}
	return out
}

// toAdminProductDetailResponse maps one product's administrator detail, including
// its pictures and, for a set, its members.
func toAdminProductDetailResponse(detail appdto.AdminProductDetailOutput) httpdto.AdminProductDetailResponse {
	return httpdto.AdminProductDetailResponse{
		AdminProductResponse: toAdminProductResponse(detail.AdminProductOutput),
		Images:               toAdminImageResponses(detail.Images),
		Members:              toSetMemberResponses(detail.Members),
	}
}

// toAdminImageResponses maps the pictures in the order they were given. It always
// allocates, so a product with no picture answers `[]` rather than null.
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

// toSetMemberResponses maps a set's members in the order they were given. An
// empty list maps to a nil slice, which the contract omits.
func toSetMemberResponses(list []appdto.SetMemberOutput) []httpdto.SetMemberResponse {
	if len(list) == 0 {
		return nil
	}
	out := make([]httpdto.SetMemberResponse, 0, len(list))
	for _, member := range list {
		out = append(out, httpdto.SetMemberResponse{ID: member.ID, Name: member.Name, Slug: member.Slug})
	}
	return out
}

// --- Variant-group administrator surface (US2) ---

// ListAdminGroups returns a page of every variant group, including the ones
// withheld from customers (FR-013). It is reached only behind the administrator
// role guard.
//
//	@Summary		List every variant group (ADMIN)
//	@Description	Returns a page of every variant group, including the ones not visible to customers, with the category, the position and the picture summary a customer never sees.
//	@Tags			VariantGroup
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query	int	false	"Page number (default 1)"
//	@Param			pageSize	query	int	false	"Page size (default 20)"
//	@Success		200			{object}	httpx.SwaggerSuccess{data=[]httpdto.AdminVariantGroupResponse}
//	@Failure		400			{object}	httpx.SwaggerError
//	@Failure		401			{object}	httpx.SwaggerError
//	@Failure		403			{object}	httpx.SwaggerError
//	@Router			/admin/product-groups [get]
func (h *Handler) ListAdminGroups(w http.ResponseWriter, r *http.Request) {
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.ListAdminGroups(r.Context(), appdto.ListAdminGroupsInput{Page: page, PageSize: pageSize})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toAdminVariantGroupResponses(out.Groups), out.Page, out.PageSize, out.Total)
}

// CreateVariantGroup creates a variant group (FR-009). The acting administrator
// comes from the session, never from the body: the input carries no account member
// and the decoder refuses an unknown one.
//
//	@Summary		Create a variant group (ADMIN)
//	@Description	Creates a variant group. A slug another group holds is a conflict; a category that does not exist is refused naming the field. The acting administrator comes from the session, never the body.
//	@Tags			VariantGroup
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body	httpdto.CreateVariantGroupRequest	true	"Variant-group payload"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.AdminVariantGroupDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/product-groups [post]
func (h *Handler) CreateVariantGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	var req httpdto.CreateVariantGroupRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.CreateVariantGroup(ctx, appdto.CreateVariantGroupInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		Position:    req.Position,
		OptionName:  req.OptionName,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminVariantGroupDetailResponse(out))
}

// GetAdminGroup returns one variant group by identifier, including one not visible
// to customers, with its pictures and every option (FR-013).
//
//	@Summary		Read one variant group by identifier (ADMIN)
//	@Description	Returns one variant group, including one not visible to customers, with its own pictures and every option, visible or not.
//	@Tags			VariantGroup
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Variant-group identifier"
//	@Success		200	{object}	httpx.SwaggerSuccess{data=httpdto.AdminVariantGroupDetailResponse}
//	@Failure		400	{object}	httpx.SwaggerError
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Failure		404	{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id} [get]
func (h *Handler) GetAdminGroup(w http.ResponseWriter, r *http.Request) {
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.GetAdminGroup(r.Context(), appdto.AdminVariantGroupRefInput{ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminVariantGroupDetailResponse(out))
}

// UpdateVariantGroup applies a partial administrator edit (FR-011). An omitted
// member keeps its current value; the pictures are edited through their own
// endpoints.
//
//	@Summary		Update a variant group (ADMIN)
//	@Description	Applies a partial edit. An omitted member keeps its value; the pictures are edited through their own endpoints. A slug another group holds is a conflict.
//	@Tags			VariantGroup
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string							true	"Variant-group identifier"
//	@Param			request	body	httpdto.UpdateVariantGroupRequest	true	"Variant-group update"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminVariantGroupDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id} [patch]
func (h *Handler) UpdateVariantGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "id", fieldID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.UpdateVariantGroupRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.UpdateVariantGroup(ctx, appdto.UpdateVariantGroupInput{
		ID:          id,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		Position:    req.Position,
		OptionName:  req.OptionName,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminVariantGroupDetailResponse(out))
}

// DeleteVariantGroup removes a variant group (FR-012). Removal is refused while
// options remain; the products are never removed with the group.
//
//	@Summary		Remove a variant group (ADMIN)
//	@Description	Removes a variant group. Refused with VARIANT_GROUP_HAS_OPTIONS while the group still has options; the options are never removed with it.
//	@Tags			VariantGroup
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Variant-group identifier"
//	@Success		204	"No Content"
//	@Failure		400	{object}	httpx.SwaggerError
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Failure		404	{object}	httpx.SwaggerError
//	@Failure		409	{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id} [delete]
func (h *Handler) DeleteVariantGroup(w http.ResponseWriter, r *http.Request) {
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
	if err := h.svc.DeleteVariantGroup(ctx, appdto.AdminVariantGroupRefInput{ID: id}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddGroupImage stores an uploaded group picture. The bytes are read with a hard
// ceiling and handed to the use case, which validates them by content and by the
// gallery's count; neither the file name nor the client-declared media type is
// trusted (FR-018).
//
//	@Summary		Upload a group picture (ADMIN)
//	@Description	Stores a picture for the variant group through the media provider. The content is validated by signature, not by name or declared media type. The first picture becomes the main one.
//	@Tags			VariantGroup
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string	true	"Variant-group identifier"
//	@Param			image	formData	file	true	"Group image (JPEG, PNG or WebP)"
//	@Success		201		{object}	httpx.SwaggerSuccess{data=httpdto.AdminVariantGroupDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		413		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id}/images [post]
func (h *Handler) AddGroupImage(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.svc.AddGroupImage(ctx, appdto.AddGroupImageInput{GroupID: id, Content: content})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminVariantGroupDetailResponse(out))
}

// RemoveGroupImage detaches one picture from a variant group and releases its
// stored asset (FR-018).
//
//	@Summary		Remove a group picture (ADMIN)
//	@Description	Detaches one picture from the variant group and releases its stored asset. Removing the main picture promotes the next one by position.
//	@Tags			VariantGroup
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Variant-group identifier"
//	@Param			imageId	path	string	true	"Picture identifier"
//	@Success		204		"No Content"
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id}/images/{imageId} [delete]
func (h *Handler) RemoveGroupImage(w http.ResponseWriter, r *http.Request) {
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
	if err := h.svc.RemoveGroupImage(ctx, appdto.RemoveGroupImageInput{GroupID: id, ImageID: imageID}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPrimaryGroupImage makes one picture the group's main one (FR-018).
//
//	@Summary		Set the main group picture (ADMIN)
//	@Description	Makes one picture the variant group's main one, clearing any other in the same operation.
//	@Tags			VariantGroup
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Variant-group identifier"
//	@Param			imageId	path	string	true	"Picture identifier"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.AdminVariantGroupDetailResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		404		{object}	httpx.SwaggerError
//	@Router			/admin/product-groups/{id}/images/{imageId}/primary [post]
func (h *Handler) SetPrimaryGroupImage(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.svc.SetPrimaryGroupImage(ctx, appdto.SetPrimaryGroupImageInput{GroupID: id, ImageID: imageID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminVariantGroupDetailResponse(out))
}

// toAdminVariantGroupResponse is the single conversion point from the application
// group DTO to the administrator HTTP shape, so the members a customer must not
// see cannot drift per route.
func toAdminVariantGroupResponse(group appdto.AdminVariantGroupOutput) httpdto.AdminVariantGroupResponse {
	return httpdto.AdminVariantGroupResponse{
		ID:          group.ID,
		Name:        group.Name,
		Slug:        group.Slug,
		Description: group.Description,
		CategoryID:  group.CategoryID,
		Position:    group.Position,
		OptionName:  group.OptionName,
		ImageCount:  group.ImageCount,
		ImageURL:    group.ImageURL,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
}

// toAdminVariantGroupResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null.
func toAdminVariantGroupResponses(list []appdto.AdminVariantGroupOutput) []httpdto.AdminVariantGroupResponse {
	out := make([]httpdto.AdminVariantGroupResponse, 0, len(list))
	for _, group := range list {
		out = append(out, toAdminVariantGroupResponse(group))
	}
	return out
}

// toAdminVariantGroupDetailResponse maps one group's administrator detail,
// including its pictures and every option.
func toAdminVariantGroupDetailResponse(detail appdto.AdminVariantGroupDetailOutput) httpdto.AdminVariantGroupDetailResponse {
	return httpdto.AdminVariantGroupDetailResponse{
		AdminVariantGroupResponse: toAdminVariantGroupResponse(detail.AdminVariantGroupOutput),
		Images:                    toAdminGroupImageResponses(detail.Images),
		Options:                   toAdminGroupOptionResponses(detail.Options),
	}
}

// toAdminGroupImageResponses maps the group pictures in the order they were given.
// It always allocates, so a group with no picture answers `[]` rather than null.
func toAdminGroupImageResponses(list []appdto.AdminGroupImageOutput) []httpdto.AdminGroupImageResponse {
	out := make([]httpdto.AdminGroupImageResponse, 0, len(list))
	for _, image := range list {
		out = append(out, httpdto.AdminGroupImageResponse{
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

// toAdminGroupOptionResponses maps a group's options in the order they were given.
// It always allocates, so an empty group answers `[]` rather than null.
func toAdminGroupOptionResponses(list []appdto.AdminGroupOptionOutput) []httpdto.AdminGroupOptionResponse {
	out := make([]httpdto.AdminGroupOptionResponse, 0, len(list))
	for _, option := range list {
		out = append(out, httpdto.AdminGroupOptionResponse{
			ID:         option.ID,
			Name:       option.Name,
			Slug:       option.Slug,
			Label:      option.Label,
			Price:      httpdto.PriceResponse{Amount: option.Price.Amount, Currency: option.Price.Currency},
			ImageURL:   option.ImageURL,
			Position:   option.Position,
			SellState:  string(option.SellState),
			IsPreorder: option.IsPreorder,
		})
	}
	return out
}
