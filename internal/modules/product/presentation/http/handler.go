package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// Pagination bounds of the public catalogue (contracts/openapi.yaml,
// Page/PageSize). They match the project's existing convention (research D12).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// CatalogueService is the part of the product use-case surface the public browse
// routes consume.
//
// It is declared here rather than taking application/interface.ProductService
// whole, because the public group is delivered before the administrator use
// cases and must not depend on them. It is a consumer-side subset of the
// declared surface, not a second declaration of it: the real
// *implement.Service satisfies it, and the administrator group that lands with
// US2 widens this to the full interface.
type CatalogueService interface {
	// ListPublic returns one page of the products a customer may see, in the
	// configured order (FR-001 to FR-009).
	ListPublic(ctx context.Context, in appdto.ListPublicInput) (appdto.PublicProductPage, error)
	// GetPublicBySlug returns one visible product with its description and every
	// picture. A hidden, removed or unknown slug answers the same not-found
	// (FR-005).
	GetPublicBySlug(ctx context.Context, in appdto.PublicProductRefInput) (appdto.PublicProductDetailOutput, error)
}

// Handler adapts the product use cases to HTTP.
type Handler struct {
	svc    CatalogueService
	logger *slog.Logger
}

// New creates the product HTTP handler.
func New(svc CatalogueService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

// ListPublic returns a page of the catalogue a customer may see. It is reachable
// with no session (FR-001): the route installs no authentication middleware.
//
// The `category` filter is passed down as the slug a customer-facing link
// carries; the use case resolves it, and a hidden or unknown slug answers an
// empty page rather than a not-found (FR-006).
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

// GetPublicBySlug returns one visible product with its description and every
// picture, addressed by the slug a customer-facing link is built from
// (FR-005). A hidden, retired, removed or unknown slug answers the same
// not-found (FR-003).
func (h *Handler) GetPublicBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	out, err := h.svc.GetPublicBySlug(r.Context(), appdto.PublicProductRefInput{Slug: slug})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toPublicProductDetailResponse(out))
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

// toPublicProductResponses maps a page. It always allocates, so an empty
// catalogue answers an empty array rather than null: the contract declares the
// list as an array, and FR-007 requires an empty result, not an error.
func toPublicProductResponses(list []appdto.PublicProductOutput) []httpdto.PublicProductResponse {
	out := make([]httpdto.PublicProductResponse, 0, len(list))
	for _, product := range list {
		out = append(out, toPublicProductResponse(product))
	}
	return out
}

// toPublicProductDetailResponse maps one product's detail. The pictures are
// mapped through their own conversion so the public picture shape cannot gain a
// member the contract does not declare.
func toPublicProductDetailResponse(detail appdto.PublicProductDetailOutput) httpdto.PublicProductDetailResponse {
	return httpdto.PublicProductDetailResponse{
		PublicProductResponse: toPublicProductResponse(detail.PublicProductOutput),
		Description:           detail.Description,
		Images:                toPublicImageResponses(detail.Images),
	}
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
