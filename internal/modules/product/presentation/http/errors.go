package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// coded builds a module error response with an explicit status, so a code that
// is not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// withField attaches a field-level detail so the client can highlight the exact
// input. The detail never repeats internal state.
func withField(err *httpx.AppError, field, issue string) *httpx.AppError {
	err.Details = append(err.Details, httpx.Detail{Field: field, Issue: issue})
	return err
}

// fieldError reports a request-shape problem the client can fix itself, such as
// a malformed path identifier or an out-of-range page window. Pagination and
// request-shape validation stay on the shared VALIDATION_ERROR code: they are a
// foundation concern, not a product-module rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// imageTooLarge is the one refusal for an upload over the ceiling, wherever the
// ceiling was reached: while reading the part, or by the route's own body limit.
// Naming the image in the detail is what lets a client point at the right input
// (FR-020).
func imageTooLarge() *httpx.AppError {
	return withField(
		coded(constant.CodeProductImageTooLarge, http.StatusRequestEntityTooLarge, "Image exceeds the size limit"),
		fieldImage, "compress the image before uploading",
	)
}

// mapError translates the module's sentinel errors into the codes and statuses
// of specs/006-product-catalog/contracts/error-codes.md.
//
// A slug collision is 409, not 400: the value is well formed and simply not
// available, so the operator's fix is to pick another value rather than to repair
// the one sent. A picture-format or size refusal names the image, because the
// client acts on that input. A value invalid on its own stays on the shared
// VALIDATION_ERROR and names the field the domain reports. The public surface
// produces only the not-found: an unknown slug, a product hidden by its own state,
// a retired product, a removed product and a product in a hidden category are all
// the same answer (FR-003, FR-008).
func mapError(err error) *httpx.AppError {
	var invalid *domainerr.ProductFieldError
	var transition *domainerr.StateTransitionError
	switch {
	case errors.Is(err, domainerr.ErrProductNotFound):
		return coded(constant.CodeProductNotFound, http.StatusNotFound, "Product not found")
	case errors.Is(err, domainerr.ErrProductSlugTaken):
		return withField(
			coded(constant.CodeProductSlugTaken, http.StatusConflict, "Another product already uses that slug"),
			model.FieldSlug, "choose a different slug",
		)
	case errors.As(err, &transition):
		// The refusal names the state the product is in, which is what makes an
		// invalid transition distinguishable from a typo (FR-024).
		return coded(constant.CodeProductStateTransitionInvalid, http.StatusConflict,
			fmt.Sprintf("Cannot move from %s to %s", transition.Current, transition.Requested))
	case errors.Is(err, domainerr.ErrProductImageLimitReached):
		return coded(constant.CodeProductImageLimitReached, http.StatusConflict,
			"The product already carries the maximum number of pictures")
	case errors.Is(err, domainerr.ErrProductImageTypeUnsupported):
		return withField(
			coded(constant.CodeProductImageTypeUnsupported, http.StatusBadRequest, "The uploaded file is not a supported image"),
			fieldImage, "choose a JPEG, PNG or WebP file",
		)
	case errors.Is(err, domainerr.ErrProductImageTooLarge):
		return imageTooLarge()
	case errors.Is(err, domainerr.ErrProductMediaUnavailable):
		return coded(constant.CodeProductMediaUnavailable, http.StatusServiceUnavailable, "The media service is unavailable")
	case errors.As(err, &invalid):
		return fieldError(invalid.Field, invalid.Issue)
	default:
		// Never leak storage or provider detail to a client; the cause is logged
		// with the correlation id by httpx.WriteError.
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its request.
const (
	fieldPage     = "page"
	fieldPageSize = "pageSize"
	fieldID       = "id"
	fieldImageID  = "imageId"
	fieldImage    = "image"
)
