// Package httpapi exposes the category module over HTTP. It owns the
// /categories public routes and carries no business rules: handlers translate
// HTTP to application DTOs and map sentinel errors to the shared error envelope.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Pagination bounds of the public catalogue (contracts/openapi.yaml,
// Page/PageSize). They match the project's existing convention (research D8).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the category use cases to HTTP.
type Handler struct {
	svc    appinterface.CategoryService
	logger *slog.Logger
}

// New creates the category HTTP handler.
func New(svc appinterface.CategoryService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// ListPublic returns a page of the catalogue on display. It is reachable with no
// session (FR-001, FR-014): the route installs no authentication middleware.
func (h *Handler) ListPublic(w http.ResponseWriter, r *http.Request) {
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.ListPublic(r.Context(), appdto.ListPublicInput{Page: page, PageSize: pageSize})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toPublicCategoryResponses(out.Categories), out.Page, out.PageSize, out.Total)
}

// GetPublicBySlug returns one category on display, addressed by the slug a
// customer-facing link is built from (research D7). A withheld, removed or
// unknown slug answers the same not-found (FR-005).
func (h *Handler) GetPublicBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	out, err := h.svc.GetPublicBySlug(r.Context(), appdto.PublicCategoryRefInput{Slug: slug})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPublicCategoryResponse(out))
}

// ListAdmin returns a page of every category, including the ones not on display
// (FR-009). It is reached only behind the administrator role guard, which is what
// keeps the display state and the position from a customer (FR-007, FR-014).
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
	httpx.WriteSuccessList(w, r, toAdminCategoryResponses(out.Categories), out.Page, out.PageSize, out.Total)
}

// GetAdmin returns one category by identifier, including one not on display
// (FR-009). A malformed identifier is a request-shape problem answered with the
// field named; an unknown one is the module not-found.
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	id, appErr := pathUUID(r, "categoryId", fieldCategoryID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.GetAdmin(r.Context(), appdto.AdminCategoryRefInput{ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminCategoryResponse(out))
}

// CreateCategory creates a category on display (FR-008). The acting
// administrator comes from the session the middleware resolved, never from the
// body: the input carries no account member and the decoder refuses an unknown
// one.
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	var req httpdto.CreateCategoryRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.CreateCategory(ctx, appdto.CreateCategoryInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Position:    req.Position,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAdminCategoryResponse(out))
}

// UpdateCategory applies a partial administrator edit, including a display
// change (FR-010, FR-011). An omitted member keeps its current value; the acting
// administrator comes from the session.
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "categoryId", fieldCategoryID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.UpdateCategoryRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.UpdateCategory(ctx, appdto.UpdateCategoryInput{
		ID:          id,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Position:    req.Position,
		IsVisible:   req.IsVisible,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAdminCategoryResponse(out))
}

// DeleteCategory removes a category (FR-012). Removal is a hard delete; the
// audit entry is what survives it. The acting administrator comes from the
// session.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	id, appErr := pathUUID(r, "categoryId", fieldCategoryID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	if err := h.svc.DeleteCategory(ctx, appdto.AdminCategoryRefInput{ID: id}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
// No route accepts an owner identifier: the session is the only source of the
// acting account, which is what makes an actor chosen by a client impossible by
// construction (FR-014). A missing or unparseable identity answers
// UNAUTHENTICATED, which behind the administrator guard is an invalid session
// this service cannot produce.
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

// toPublicCategoryResponse is the single conversion point from the application
// DTO to the public HTTP shape, so the four-member shape cannot drift per route.
func toPublicCategoryResponse(category appdto.PublicCategoryOutput) httpdto.PublicCategoryResponse {
	return httpdto.PublicCategoryResponse{
		ID:          category.ID,
		Name:        category.Name,
		Slug:        category.Slug,
		Description: category.Description,
	}
}

// toPublicCategoryResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null: the contract declares the
// list as an array, and FR-006 requires an empty result, not an error (FR-006).
func toPublicCategoryResponses(list []appdto.PublicCategoryOutput) []httpdto.PublicCategoryResponse {
	out := make([]httpdto.PublicCategoryResponse, 0, len(list))
	for _, category := range list {
		out = append(out, toPublicCategoryResponse(category))
	}
	return out
}

// toAdminCategoryResponse is the single conversion point from the application
// administrator DTO to the administrator HTTP shape, so the members a customer
// must not see cannot drift per route. It always allocates on a list, so an empty
// catalogue answers an empty array rather than null.
func toAdminCategoryResponse(category appdto.AdminCategoryOutput) httpdto.AdminCategoryResponse {
	return httpdto.AdminCategoryResponse{
		ID:          category.ID,
		Name:        category.Name,
		Slug:        category.Slug,
		Description: category.Description,
		Position:    category.Position,
		IsVisible:   category.IsVisible,
		CreatedAt:   category.CreatedAt,
		UpdatedAt:   category.UpdatedAt,
	}
}

func toAdminCategoryResponses(list []appdto.AdminCategoryOutput) []httpdto.AdminCategoryResponse {
	out := make([]httpdto.AdminCategoryResponse, 0, len(list))
	for _, category := range list {
		out = append(out, toAdminCategoryResponse(category))
	}
	return out
}
