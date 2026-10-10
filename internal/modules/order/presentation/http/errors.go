package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its request
// (contracts/error-codes.md).
const (
	fieldProductID = "productId"
	fieldAddressID = "addressId"
	fieldOrderID   = "orderId"
	fieldQuantity  = "quantity"
	fieldPage      = "page"
	fieldPageSize  = "pageSize"
	fieldStatus    = "status"
	fieldSort      = "sort"
	fieldEmail     = "email"
)

// coded builds a module error response with an explicit status, so a code that is
// not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// withField attaches a field-level detail so the client can point at the exact
// input.
func withField(err *httpx.AppError, field, issue string) *httpx.AppError {
	err.Details = append(err.Details, httpx.Detail{Field: field, Issue: issue})
	return err
}

// fieldError reports a request-shape problem the client can fix itself, such as a
// malformed or foreign `addressId`. It stays on the shared VALIDATION_ERROR code:
// a malformed request is a foundation concern, not an order rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// mapError translates the module's sentinel errors into the codes and statuses of
// specs/010-order-confirmation/contracts/error-codes.md.
//
// The three item refusals are conflicts (409) that name `productId` in their
// detail — the item the customer must change — and the quantity code's issue also
// states what remained available. A state move the order's current state forbids
// answers ORDER_STATE_TRANSITION_INVALID naming the current state. An order that
// may not be edited or an edit that would empty it answers ORDER_NOT_EDITABLE or
// ORDER_EMPTY (both 409). A malformed or foreign `addressId` stays on the shared
// VALIDATION_ERROR and names the field. A shared AppError the transport produced
// (a malformed request, an authentication failure) passes through unchanged;
// anything else is hidden behind INTERNAL_ERROR so storage detail is never leaked.
func mapError(err error) *httpx.AppError {
	var notPurchasable *domainerr.ItemNotPurchasableError
	var priceChanged *domainerr.ItemPriceChangedError
	var exceeds *domainerr.QuantityExceedsAvailableError
	var transition *domainerr.StateTransitionError
	var invalid *domainerr.InvalidValueError

	switch {
	case errors.Is(err, domainerr.ErrNotFound):
		return coded(constant.CodeOrderNotFound, http.StatusNotFound, "Order not found")
	case errors.Is(err, domainerr.ErrCartEmpty):
		return coded(constant.CodeCartEmpty, http.StatusConflict, "Cart is empty")
	case errors.As(err, &notPurchasable):
		return withField(
			coded(constant.CodeItemNotPurchasable, http.StatusConflict, "Order item is not purchasable"),
			fieldProductID, notPurchasable.ProductID.String(),
		)
	case errors.Is(err, domainerr.ErrItemNotPurchasable):
		return withField(
			coded(constant.CodeItemNotPurchasable, http.StatusConflict, "Order item is not purchasable"),
			fieldProductID, "is not on sale or was removed",
		)
	case errors.As(err, &priceChanged):
		return withField(
			coded(constant.CodeItemPriceChanged, http.StatusConflict, "Order item price changed"),
			fieldProductID, priceChanged.ProductID.String(),
		)
	case errors.Is(err, domainerr.ErrItemPriceChanged):
		return withField(
			coded(constant.CodeItemPriceChanged, http.StatusConflict, "Order item price changed"),
			fieldProductID, "price changed since it was added",
		)
	case errors.As(err, &exceeds):
		return withField(
			coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
			fieldProductID, fmt.Sprintf("%s: requested %d, only %d available", exceeds.ProductID, exceeds.Requested, exceeds.Available),
		)
	case errors.Is(err, domainerr.ErrQuantityExceedsAvailable):
		return withField(
			coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
			fieldProductID, "exceeds the available quantity",
		)
	case errors.Is(err, domainerr.ErrNoAddress):
		return coded(constant.CodeNoAddress, http.StatusConflict, "Customer has no address")
	case errors.As(err, &transition):
		return coded(constant.CodeStateTransitionInvalid, http.StatusConflict,
			fmt.Sprintf("Order is %s and cannot move to %s", transition.From, transition.To))
	case errors.Is(err, domainerr.ErrStateTransitionInvalid):
		return coded(constant.CodeStateTransitionInvalid, http.StatusConflict, "Order state transition invalid")
	case errors.Is(err, domainerr.ErrNotTransferable):
		return coded(constant.CodeNotTransferable, http.StatusConflict, "Order is not transferable")
	case errors.Is(err, domainerr.ErrTransferTargetNotFound):
		return coded(constant.CodeTransferTargetNotFound, http.StatusNotFound, "Order transfer target not found")
	case errors.Is(err, domainerr.ErrNotEditable):
		return coded(constant.CodeNotEditable, http.StatusConflict, "Order is not editable")
	case errors.Is(err, domainerr.ErrEmptyOrder):
		return coded(constant.CodeEmptyOrder, http.StatusConflict, "Order would be left empty")
	case errors.As(err, &invalid):
		return fieldError(invalid.Field, invalid.Issue)
	case errors.Is(err, domainerr.ErrInvalidValue):
		return fieldError(fieldAddressID, "is not a valid order value")
	default:
		var appErr *httpx.AppError
		if errors.As(err, &appErr) {
			return appErr
		}
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}
