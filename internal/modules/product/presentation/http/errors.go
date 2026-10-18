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
// request-shape validation stay on the shared VALIDATION_ERROR code.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// imageTooLarge is the one refusal for an upload over the ceiling, wherever the
// ceiling was reached: while reading the part, or by the route's own body limit.
// Naming the image in the detail is what lets a client point at the right input.
func imageTooLarge() *httpx.AppError {
	return withField(
		coded(constant.CodeProductImageTooLarge, http.StatusRequestEntityTooLarge, "Image exceeds the size limit"),
		fieldImage, "compress the image before uploading",
	)
}

// withProductPendingOrders attaches the blocking order identifiers the product
// flag hide guard refused with, one detail per order. It mirrors
// withPendingOrders for the variant guard: the detail field is the contract's
// member name for an order identifier, so an administrator client can map each
// detail straight onto the order it names. An untyped sentinel — one that
// carries no identifiers — answers without details rather than failing.
func withProductPendingOrders(err *httpx.AppError, cause error) *httpx.AppError {
	var pending *domainerr.ProductPendingOrdersError
	if !errors.As(cause, &pending) {
		return err
	}
	for _, id := range pending.OrderIDs {
		err.Details = append(err.Details, httpx.Detail{Field: fieldOrderID, Issue: id.String()})
	}
	return err
}

// withPendingOrders attaches the blocking order identifiers the variant hide
// guard refused with, one detail per order. The detail field is the contract's
// member name for an order identifier, so an administrator client can map each
// detail straight onto the order it names. An untyped sentinel — one that
// carries no identifiers — answers without details rather than failing.
func withPendingOrders(err *httpx.AppError, cause error) *httpx.AppError {
	var pending *domainerr.VariantPendingOrdersError
	if !errors.As(cause, &pending) {
		return err
	}
	for _, id := range pending.OrderIDs {
		err.Details = append(err.Details, httpx.Detail{Field: fieldOrderID, Issue: id.String()})
	}
	return err
}

// mapError translates the module's sentinel errors into the codes and statuses of
// specs/015-product-sku-model/contracts/error-codes.md.
//
// A code or SKU collision is 409, not 400: the value is well formed and
// simply not available, so the operator's fix is to pick another value rather than
// to repair the one sent. A picture-format or size refusal names the image, because
// the client acts on that input. A value invalid on its own stays on the shared
// VALIDATION_ERROR and names the field the domain reports. The public surface
// produces only the not-found: an unknown code, a product with no on-sale variant,
// a product whose own flag is off, a removed product and a product in a hidden
// category are all the same answer
// (FR-008).
func mapError(err error) *httpx.AppError {
	var invalid *domainerr.ProductFieldError
	var transition *domainerr.VariantStateTransitionError
	switch {
	case errors.Is(err, domainerr.ErrProductNotFound):
		return coded(constant.CodeProductNotFound, http.StatusNotFound, "Product not found")
	case errors.Is(err, domainerr.ErrVariantNotFound):
		return coded(constant.CodeVariantNotFound, http.StatusNotFound, "Variant not found")
	case errors.Is(err, domainerr.ErrProductCodeTaken):
		return withField(
			coded(constant.CodeProductCodeTaken, http.StatusConflict, "Another product already uses that code"),
			model.FieldCode, "choose a different code",
		)
	case errors.Is(err, domainerr.ErrVariantSKUTaken):
		return withField(
			coded(constant.CodeVariantSKUTaken, http.StatusConflict, "Another variant already uses that SKU"),
			model.FieldSKU, "choose a different SKU",
		)
	case errors.Is(err, domainerr.ErrVariantLastRemoval):
		return coded(constant.CodeVariantLastRemoval, http.StatusConflict,
			"A product must keep at least one variant")
	case errors.Is(err, domainerr.ErrProductHasPendingOrders):
		// Turning the product's visibility flag off is refused because at least
		// one PENDING or PAYMENT_PENDING order holds one of its variants. Like
		// the variant move above it is a conflict: the request is well formed
		// and the current data forbids it. The refusal names the blocking
		// orders — one detail per order — so the administrator can act on them;
		// the hide actions sit behind the ADMIN guard, so no customer
		// information leaks (specs/016-hide-guard research D7).
		return withProductPendingOrders(
			coded(constant.CodeProductHasPendingOrders, http.StatusConflict, "Product has pending orders and cannot be hidden"),
			err,
		)
	case errors.Is(err, domainerr.ErrVariantHasPendingOrders):
		// Moving the variant away from on-sale is refused because at least one
		// PENDING or PAYMENT_PENDING order holds it. Like the removal above it
		// is a conflict: the request is well formed and the current data
		// forbids it. The refusal names the blocking orders — one detail per
		// order — so the administrator can act on them; the hide actions sit
		// behind the ADMIN guard, so no customer information leaks
		// (specs/016-hide-guard research D7).
		return withPendingOrders(
			coded(constant.CodeVariantHasPendingOrders, http.StatusConflict, "Variant has pending orders and cannot be hidden"),
			err,
		)
	case errors.Is(err, domainerr.ErrProductReorderDuplicateID):
		// The reorder body names the same product twice: a request-shape
		// problem the client fixes, carrying its own code so a client can tell a
		// duplicate from an incomplete array (specs/018-catalog-reorder research
		// D8). The detail names the offending member.
		return withField(
			coded(constant.CodeProductReorderDuplicateID, http.StatusBadRequest, "The same product is named more than once"),
			fieldProductIDs, "send each id once",
		)
	case errors.Is(err, domainerr.ErrVariantReorderDuplicateID):
		// The variant reorder body names the same variant twice.
		return withField(
			coded(constant.CodeVariantReorderDuplicateID, http.StatusBadRequest, "The same variant is named more than once"),
			fieldVariantIDs, "send each id once",
		)
	case errors.Is(err, domainerr.ErrVariantBelongsToOtherProduct):
		// The variant reorder names a variant that is not one of the addressed
		// product's variants, so ordering it would be a cross-product write. It
		// is a request-shape problem the client fixes, distinct from a duplicate
		// (specs/018-catalog-reorder FR-008, research D4, D8).
		return withField(
			coded(constant.CodeVariantBelongsToOtherProduct, http.StatusBadRequest, "A variant does not belong to the addressed product"),
			fieldVariantIDs, "order only that product's variants",
		)
	case errors.As(err, &transition):
		// The refusal names the state the variant is in, which is what makes an
		// invalid transition distinguishable from a typo (FR-015).
		return coded(constant.CodeVariantStateTransitionInvalid, http.StatusConflict,
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
	fieldPage       = "page"
	fieldPageSize   = "pageSize"
	fieldAll        = "all"
	fieldID         = "id"
	fieldVariantID  = "variantId"
	fieldImageID    = "imageId"
	fieldImage      = "image"
	fieldOrderID    = "orderId"
	fieldProductIDs = "productIds"
	fieldVariantIDs = "variantIds"
)
