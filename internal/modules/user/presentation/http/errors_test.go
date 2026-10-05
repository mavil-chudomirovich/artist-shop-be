package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
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
