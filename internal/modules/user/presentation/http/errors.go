package httpapi

import (
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
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
// a missing required member, an unknown path parameter or an out-of-range page.
// Pagination and request-shape validation stay on the shared VALIDATION_ERROR
// code: they are a foundation concern, not a user-module rule
// (contracts/error-codes.md, "Codes deliberately not added").
func fieldError(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// mapError translates the module's sentinel errors and the shared division errors
// into the codes and statuses of specs/003-user-profile/contracts/error-codes.md.
//
// The division errors belong to internal/share/administrative because that
// package is shared with the order, shipping and commission modules and must not
// import a module's domain errors (Constitution I); mapping them here is what
// keeps `application` free of that dependency (research D1).
func mapError(err error) *httpx.AppError {
	var invalidAddress *domainerr.AddressFieldError
	switch {
	// The two sentinels are the same condition seen from two sides: the module's own
	// domain error, and the cross-module contract error the operator lookup reports
	// so a consumer can branch on it without importing this module. Both answer the
	// documented 404 rather than adding a code a client would have to learn twice.
	case errors.Is(err, domainerr.ErrUserNotFound), errors.Is(err, contracts.ErrCustomerNotFound):
		return coded(constant.CodeUserNotFound, http.StatusNotFound, "No account carries that identifier")
	case errors.Is(err, domainerr.ErrAddressNotFound):
		return coded(constant.CodeAddressNotFound, http.StatusNotFound, "Address not found")
	case errors.As(err, &invalidAddress):
		// A structurally invalid address member is a request-shape problem the
		// client fixes on the form, so it stays on the shared VALIDATION_ERROR and
		// adds no module code (contracts/error-codes.md, "Codes deliberately not
		// added"). The entity already names the member, which is what FR-020 needs.
		return fieldError(invalidAddress.Field, invalidAddress.Issue)
	case errors.Is(err, domainerr.ErrInvalidPhone):
		return withField(
			coded(constant.CodeInvalidPhone, http.StatusBadRequest, "Not a valid Vietnamese mobile number"),
			fieldPhone, "must be ten digits starting with 0",
		)
	case errors.Is(err, administrative.ErrUnknownProvince):
		return withField(
			coded(constant.CodeUnknownProvince, http.StatusBadRequest, "Province code is not in the official dataset"),
			fieldProvinceCode, "reload the province list",
		)
	case errors.Is(err, administrative.ErrUnknownWard):
		return withField(
			coded(constant.CodeUnknownWard, http.StatusBadRequest, "Ward code is not in the official dataset"),
			fieldWardCode, "reload the ward list of the selected province",
		)
	case errors.Is(err, administrative.ErrWardProvinceMismatch):
		return withField(
			coded(constant.CodeWardProvinceMismatch, http.StatusBadRequest, "Ward does not belong to the chosen province"),
			fieldWardCode, "choose a ward of the selected province",
		)
	case errors.Is(err, domainerr.ErrAvatarTypeUnsupported):
		return withField(
			coded(constant.CodeAvatarTypeUnsupported, http.StatusBadRequest, "Only JPEG, PNG and WebP images are accepted"),
			fieldFile, "unsupported image content",
		)
	case errors.Is(err, domainerr.ErrAvatarTooLarge):
		return withField(
			coded(constant.CodeAvatarTooLarge, http.StatusRequestEntityTooLarge, "Image exceeds the size limit"),
			fieldFile, "compress the image before uploading",
		)
	case errors.Is(err, domainerr.ErrMediaUnavailable):
		// Retryable: the previous avatar was left untouched, so the customer can
		// try again unchanged (contracts/error-codes.md).
		return coded(constant.CodeMediaUnavailable, http.StatusServiceUnavailable,
			"The media service is unavailable; the profile was not changed")
	case errors.Is(err, domainerr.ErrIncompleteAvatar):
		// An incomplete reference is our own metadata problem, not a client one, so
		// it stays on the shared INTERNAL_ERROR rather than adding a module code no
		// client could act on (contracts/error-codes.md, "Codes deliberately not
		// added"). The cause is logged with the correlation id.
		return httpx.Wrap(err, httpx.CodeInternal)
	default:
		// Never leak provider or storage detail to a client; the cause is logged
		// with the correlation id by httpx.WriteError.
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}

// mapAddressError maps an error raised by an address use case. It reuses the
// module mapping and renames the phone detail, because one domain rule guards two
// different members: phone on the profile and recipientPhone on an address.
func mapAddressError(err error) *httpx.AppError {
	appErr := mapError(err)
	if errors.Is(err, domainerr.ErrInvalidPhone) {
		for i := range appErr.Details {
			appErr.Details[i].Field = fieldRecipient
		}
	}
	return appErr
}

// Field names used in error details. They are the JSON member names of the
// contract, so a client can map a detail straight onto its form.
//
// The domain entity members are taken from the domain constants rather than
// repeated as literals, because the entities already report these exact names in
// their rejections and two lists could drift apart.
const (
	fieldPhone         = "phone"
	fieldDisplayName   = model.FieldDisplayName
	fieldRecipientName = model.FieldRecipientName
	fieldRecipient     = model.FieldRecipientPhone
	fieldProvinceCode  = model.FieldProvinceCode
	fieldWardCode      = model.FieldWardCode
	fieldStreet        = model.FieldStreetAddress
	fieldFile          = "file"
	fieldPage          = "page"
	fieldPageSize      = "pageSize"
	fieldAddressID     = "addressId"
	fieldUserID        = "userId"
)
