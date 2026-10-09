package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// codeProductNotFound is the shared PRODUCT_NOT_FOUND (404). It is declared here
// rather than imported from module 04, because presentation of one module must
// not import another module (Constitution I); its value mirrors module 04's and
// contracts/error-codes.md records it as deliberately reused rather than
// duplicated in meaning.
const codeProductNotFound = "PRODUCT_NOT_FOUND"

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its request.
const (
	fieldProductID = "productId"
	fieldQuantity  = "quantity"
)

// coded builds a module error response with an explicit status, so a code that is
// not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// withField attaches a field-level detail so the client can point at the exact
// input. The detail never repeats internal state.
func withField(err *httpx.AppError, field, issue string) *httpx.AppError {
	err.Details = append(err.Details, httpx.Detail{Field: field, Issue: issue})
	return err
}

// fieldError reports a request-shape problem the client can fix itself, such as a
// malformed identifier or a fractional quantity. It stays on the shared
// VALIDATION_ERROR code: a malformed request is a foundation concern, not a cart
// rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// mapError translates the module's sentinel errors into the codes and statuses of
// specs/008-cart/contracts/error-codes.md.
//
// A product that does not exist, was removed, or is not a line of this cart is the
// reused PRODUCT_NOT_FOUND (404), because the two are deliberately one answer so a
// route never confirms what is in another customer's cart. A product that is not
// on sale and a quantity above what is available are conflicts (409) that name the
// field the customer must change. A value invalid on its own stays on the shared
// VALIDATION_ERROR and names the field the domain reports (FR-006). A shared
// AppError the transport produced (a malformed request, an authentication failure)
// passes through unchanged; anything else is hidden behind INTERNAL_ERROR so
// storage detail is never leaked.
func mapError(err error) *httpx.AppError {
	var notPurchasable *domainerr.NotPurchasableError
	var exceeds *domainerr.QuantityExceedsAvailableError
	var invalid *domainerr.InvalidValueError

	switch {
	case errors.Is(err, domainerr.ErrProductNotFound):
		return coded(codeProductNotFound, http.StatusNotFound, "Product not found")
	case errors.As(err, &notPurchasable):
		return withField(
			coded(constant.CodeProductNotPurchasable, http.StatusConflict, "Product not purchasable"),
			fieldProductID, "is not on sale",
		)
	case errors.Is(err, domainerr.ErrProductNotPurchasable):
		return withField(
			coded(constant.CodeProductNotPurchasable, http.StatusConflict, "Product not purchasable"),
			fieldProductID, "is not on sale",
		)
	case errors.As(err, &exceeds):
		return withField(
			coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
			fieldQuantity, fmt.Sprintf("only %d available", exceeds.Available),
		)
	case errors.Is(err, domainerr.ErrQuantityExceedsAvailable):
		return withField(
			coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
			fieldQuantity, "exceeds the available quantity",
		)
	case errors.As(err, &invalid):
		return fieldError(invalid.Field, invalid.Issue)
	case errors.Is(err, domainerr.ErrInvalidValue):
		return fieldError(fieldQuantity, "is not a valid cart value")
	default:
		var appErr *httpx.AppError
		if errors.As(err, &appErr) {
			return appErr
		}
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}
