package httpapi

import (
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// coded builds a module error response with an explicit status, so a code that
// is not in the shared catalogue still carries its documented status.
func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

// withField attaches a field-level detail so the client can highlight the exact
// input (FR-020). The detail never repeats internal state.
func withField(err *httpx.AppError, field, issue string) *httpx.AppError {
	err.Details = append(err.Details, httpx.Detail{Field: field, Issue: issue})
	return err
}

// fieldError reports a request-shape problem the client can fix itself, such as
// a malformed path identifier or an out-of-range page window. Pagination and
// request-shape validation stay on the shared VALIDATION_ERROR code: they are a
// foundation concern, not a category-module rule.
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// withPendingOrders attaches the blocking order identifiers the hide guard
// refused with, one detail per order. The detail field is the contract's member
// name for an order identifier, so an administrator client can map each detail
// straight onto the order it names. An untyped sentinel — one that carries no
// identifiers — answers without details rather than failing.
func withPendingOrders(err *httpx.AppError, cause error) *httpx.AppError {
	var pending *domainerr.CategoryPendingOrdersError
	if !errors.As(cause, &pending) {
		return err
	}
	for _, id := range pending.OrderIDs {
		err.Details = append(err.Details, httpx.Detail{Field: fieldOrderID, Issue: id.String()})
	}
	return err
}

// mapError translates the module's sentinel errors into the codes and statuses
// of specs/005-category-catalog/contracts/error-codes.md.
//
// A collision is 409, not 400 (research D4): the value is well formed and simply
// not available, and the two module codes name which field collided, so the
// operator knows whether to change the name or the slug. An invalid value stays
// on the shared VALIDATION_ERROR and names the field, because it is invalid on
// its own and the operator fixes that value rather than picking another.
//
// The public surface produces only the not-found; the collision and invalid
// cases are reachable through the administrator routes but share this one
// mapping, so a code cannot answer differently depending on which route reached
// it.
func mapError(err error) *httpx.AppError {
	var invalid *domainerr.CategoryFieldError
	switch {
	case errors.Is(err, domainerr.ErrCategoryNotFound):
		return coded(constant.CodeCategoryNotFound, http.StatusNotFound, "Category not found")
	case errors.Is(err, domainerr.ErrCategoryNameTaken):
		return withField(
			coded(constant.CodeCategoryNameTaken, http.StatusConflict, "Another category already uses that name"),
			model.FieldName, "choose a different name",
		)
	case errors.Is(err, domainerr.ErrCategorySlugTaken):
		return withField(
			coded(constant.CodeCategorySlugTaken, http.StatusConflict, "Another category already uses that slug"),
			model.FieldSlug, "choose a different slug",
		)
	case errors.Is(err, domainerr.ErrCategoryInUse):
		// The category is well formed and the request is understandable; what
		// stands in the way is that products still belong to it. That is a
		// conflict, not a validation failure, and the message names the operator's
		// own fact rather than the database's wording (FR-036, FR-037).
		return coded(constant.CodeCategoryInUse, http.StatusConflict, "Category still has products and cannot be removed")
	case errors.Is(err, domainerr.ErrCategoryReorderDuplicateID):
		// The reorder body names the same category twice. It is a request-shape
		// problem the client fixes, and it carries its own code so a client can
		// tell a duplicate from an incomplete array without parsing the message
		// (specs/018-catalog-reorder research D8). The detail names the
		// offending member.
		return withField(
			coded(constant.CodeCategoryReorderDuplicateID, http.StatusBadRequest, "The same category is named more than once"),
			fieldCategoryIDs, "send each id once",
		)
	case errors.Is(err, domainerr.ErrCategoryHasPendingOrders):
		// Hiding the category is refused because at least one PENDING or
		// PAYMENT_PENDING order holds one of its variants. Like the removal
		// above it is a conflict: the request is well formed and the current
		// data forbids it. The refusal names the blocking orders — one detail
		// per order — so the administrator can act on them; the hide actions
		// sit behind the ADMIN guard, so no customer information leaks
		// (specs/016-hide-guard research D7).
		return withPendingOrders(
			coded(constant.CodeCategoryHasPendingOrders, http.StatusConflict, "Category has pending orders and cannot be hidden"),
			err,
		)
	case errors.As(err, &invalid):
		// A value that is invalid on its own is a request-shape problem the
		// client fixes, so it stays on the shared VALIDATION_ERROR and adds no
		// module code (contracts/error-codes.md, "Codes deliberately reused").
		// The domain already names the member, which is what FR-018 to FR-020
		// need.
		return fieldError(invalid.Field, invalid.Issue)
	default:
		// Never leak storage or provider detail to a client; the cause is logged
		// with the correlation id by httpx.WriteError.
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its query.
const (
	fieldPage        = "page"
	fieldPageSize    = "pageSize"
	fieldAll         = "all"
	fieldCategoryID  = "categoryId"
	fieldCategoryIDs = "categoryIds"
	fieldOrderID     = "orderId"
)
