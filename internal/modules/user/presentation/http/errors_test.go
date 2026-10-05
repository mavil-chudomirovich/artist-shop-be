package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

func TestMapErrorCoversEveryDocumentedCode(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    string
		status  int
		field   string
		details bool
	}{
		{
			name:   "unknown account",
			err:    domainerr.ErrUserNotFound,
			code:   constant.CodeUserNotFound,
			status: http.StatusNotFound,
		},
		{
			name:   "address is unknown, hidden or not owned alike",
			err:    domainerr.ErrAddressNotFound,
			code:   constant.CodeAddressNotFound,
			status: http.StatusNotFound,
		},
		{
			name:    "invalid phone names the field",
			err:     domainerr.ErrInvalidPhone,
			code:    constant.CodeInvalidPhone,
			status:  http.StatusBadRequest,
			field:   fieldPhone,
			details: true,
		},
		{
			name: "an invalid address member names itself",
			err: &domainerr.AddressFieldError{
				Field: fieldStreet, Issue: "is required",
			},
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   fieldStreet,
			details: true,
		},
		{
			// A member over the maxLength the contract declares is the same kind of
			// client-fixable problem as a blank one, so it must not grow a module
			// code of its own and must reach the client as 400 VALIDATION_ERROR
			// naming the member (contracts/error-codes.md, "Codes deliberately not
			// added").
			name: "an address member over the contract length ceiling names itself",
			err: domainerr.InvalidAddressField(
				model.FieldRecipientName, "must be at most 120 characters",
			),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldRecipientName,
			details: true,
		},
		{
			name:    "unknown province names the field",
			err:     administrative.ErrUnknownProvince,
			code:    constant.CodeUnknownProvince,
			status:  http.StatusBadRequest,
			field:   fieldProvinceCode,
			details: true,
		},
		{
			name:    "unknown ward names the field",
			err:     administrative.ErrUnknownWard,
			code:    constant.CodeUnknownWard,
			status:  http.StatusBadRequest,
			field:   fieldWardCode,
			details: true,
		},
		{
			name:    "ward of another province names the field",
			err:     administrative.ErrWardProvinceMismatch,
			code:    constant.CodeWardProvinceMismatch,
			status:  http.StatusBadRequest,
			field:   fieldWardCode,
			details: true,
		},
		{
			name:    "unsupported image content names the file",
			err:     domainerr.ErrAvatarTypeUnsupported,
			code:    constant.CodeAvatarTypeUnsupported,
			status:  http.StatusBadRequest,
			field:   fieldFile,
			details: true,
		},
		{
			name:    "oversized image is 413",
			err:     domainerr.ErrAvatarTooLarge,
			code:    constant.CodeAvatarTooLarge,
			status:  http.StatusRequestEntityTooLarge,
			field:   fieldFile,
			details: true,
		},
		{
			name:   "media outage is retryable and says so with 503",
			err:    domainerr.ErrMediaUnavailable,
			code:   constant.CodeMediaUnavailable,
			status: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appErr := mapError(tc.err)
			if string(appErr.Code) != tc.code {
				t.Fatalf("expected code %s, got %s", tc.code, appErr.Code)
			}
			if appErr.Status != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, appErr.Status)
			}
			if appErr.Message == "" {
				t.Fatal("expected a safe client message")
			}
			if tc.details {
				if len(appErr.Details) != 1 || appErr.Details[0].Field != tc.field {
					t.Fatalf("expected one detail naming %q, got %+v", tc.field, appErr.Details)
				}
				if appErr.Details[0].Issue == "" {
					t.Fatal("expected the detail to explain the problem")
				}
				return
			}
			if len(appErr.Details) != 0 {
				t.Fatalf("expected no details, got %+v", appErr.Details)
			}
		})
	}
}

func TestMapErrorUnwrapsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrAddressNotFound)

	appErr := mapError(wrapped)

	if string(appErr.Code) != constant.CodeAddressNotFound {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("provider said: key sk_live_123"))

	if string(appErr.Code) != string(httpx.CodeInternal) {
		t.Fatalf("expected INTERNAL_ERROR, got %s", appErr.Code)
	}
	if appErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", appErr.Status)
	}
	if appErr.Message != httpx.New(httpx.CodeInternal).Message {
		t.Fatalf("expected the generic safe message, got %q", appErr.Message)
	}
	if errors.Unwrap(appErr) == nil {
		t.Fatal("expected the cause to be kept for logging")
	}
}

func TestMapAddressErrorNamesTheRecipientField(t *testing.T) {
	appErr := mapAddressError(domainerr.ErrInvalidPhone)

	if len(appErr.Details) != 1 || appErr.Details[0].Field != fieldRecipient {
		t.Fatalf("expected the detail to name %q, got %+v", fieldRecipient, appErr.Details)
	}
}

func TestFieldErrorNamesTheOffendingMember(t *testing.T) {
	appErr := fieldError(fieldPageSize, "must be an integer between 1 and 100")

	if string(appErr.Code) != string(httpx.CodeValidation) {
		t.Fatalf("expected VALIDATION_ERROR, got %s", appErr.Code)
	}
	if appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", appErr.Status)
	}
	if len(appErr.Details) != 1 || appErr.Details[0].Field != fieldPageSize {
		t.Fatalf("expected the detail to name %q, got %+v", fieldPageSize, appErr.Details)
	}
}

// The entity reports the contract's member names, and presentation puts them into
// the detail untouched. This test is what stops the two lists from drifting apart.
func TestTheAddressFieldDetailsAreTheContractMemberNames(t *testing.T) {
	cases := map[string]string{
		fieldRecipientName: "recipientName",
		fieldRecipient:     "recipientPhone",
		fieldProvinceCode:  "provinceCode",
		fieldWardCode:      "wardCode",
		fieldStreet:        "streetAddress",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("field detail %q does not match the contract member name %q", got, want)
		}
	}
}

// The address error keeps ONE sentinel, so a caller can branch on it without
// knowing the carrier, and the field detail never leaks internal state.
func TestTheAddressFieldErrorIsFoundThroughItsSentinel(t *testing.T) {
	err := domainerr.InvalidAddressField(fieldWardCode, "is required")

	if !errors.Is(err, domainerr.ErrAddressInvalid) {
		t.Fatalf("expected the sentinel to match, got %v", err)
	}
	var carrier *domainerr.AddressFieldError
	if !errors.As(err, &carrier) {
		t.Fatalf("expected the typed carrier, got %v", err)
	}
	if carrier.Field != fieldWardCode || carrier.Issue == "" {
		t.Fatalf("unexpected carrier: %+v", carrier)
	}
	if errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatal("an invalid member must not be reported as a missing address")
	}
}
