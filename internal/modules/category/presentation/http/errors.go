package httpapi

import (
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// coded builds a module error response with an explicit status, so a code that
// is not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// fieldError reports a request-shape problem the client can fix itself, such as
// an out-of-range page window. Pagination and request-shape validation stay on
// the shared VALIDATION_ERROR code: they are a foundation concern, not a
// category-module rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// mapError translates the module's sentinel errors into the codes and statuses
// of specs/005-category-catalog/contracts/error-codes.md.
//
// The public surface can only answer the one module code below: a withheld, a
// removed and an unknown category are the same not-found, so the response never
// confirms that a withheld category exists (FR-005, SC-004).
//
// US2 extends this switch with the collision codes (CATEGORY_NAME_TAKEN,
// CATEGORY_SLUG_TAKEN -> 409) and the invalid-value carrier
// (VALIDATION_ERROR). They are deliberately not added here: no public route can
// produce them.
func mapError(err error) *httpx.AppError {
	switch {
	case errors.Is(err, domainerr.ErrCategoryNotFound):
		return coded(constant.CodeCategoryNotFound, http.StatusNotFound, "Category not found")
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
