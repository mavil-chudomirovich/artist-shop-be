// Package httpapi exposes the category module over HTTP. It owns the
// /categories public routes and carries no business rules: handlers translate
// HTTP to application DTOs and map sentinel errors to the shared error envelope.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
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
