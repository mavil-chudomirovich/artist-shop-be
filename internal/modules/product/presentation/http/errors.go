package httpapi

import (
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// coded builds a module error response with an explicit status, so a code that
// is not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// fieldError reports a request-shape problem the client can fix itself, such as
// an out-of-range page window. It stays on the shared VALIDATION_ERROR code:
// pagination is a foundation concern, not a product-module rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// mapError translates the module's sentinel errors into the codes and statuses
// of specs/006-product-catalog/contracts/error-codes.md.
//
// The public surface produces only the not-found: an unknown slug, a product
// hidden by its own state, a retired product, a removed product and a product in
// a hidden category are all the same answer, so nothing confirms that a hidden
// product exists (FR-003, FR-008). The administrator mappings (slug collision,
// invalid transition, picture failures) land with the administrator routes.
func mapError(err error) *httpx.AppError {
	switch {
	case errors.Is(err, domainerr.ErrProductNotFound):
		return coded(constant.CodeProductNotFound, http.StatusNotFound, "Product not found")
	default:
		// Never leak storage or provider detail to a client; the cause is logged
		// with the correlation id by httpx.WriteError.
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its query.
const (
	fieldPage     = "page"
	fieldPageSize = "pageSize"
)
