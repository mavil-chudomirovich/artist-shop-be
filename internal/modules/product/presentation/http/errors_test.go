package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file covers the mapping of every code the product module can answer with
// (specs/006-product-catalog/contracts/error-codes.md). The public not-found is
// the only module code the read path produces; the collision, the picture
// failures and the invalid-field cases are reachable through the administrator
// routes.

func TestMapErrorCoversEveryModuleCode(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    string
		status  int
		field   string
		details bool
	}{
		{
			name:   "unknown, hidden and removed share one not-found",
			err:    domainerr.ErrProductNotFound,
			code:   constant.CodeProductNotFound,
			status: http.StatusNotFound,
		},
		{
			name:    "a duplicate slug names the field and is a conflict",
			err:     domainerr.ErrProductSlugTaken,
			code:    constant.CodeProductSlugTaken,
			status:  http.StatusConflict,
			field:   model.FieldSlug,
			details: true,
		},
		{
			name:   "an invalid state change is a conflict",
			err:    domainerr.InvalidStateTransition(constant.SellStateOutOfStock, constant.SellStateComingSoon),
			code:   constant.CodeProductStateTransitionInvalid,
			status: http.StatusConflict,
		},
		{
			name:   "a full product is a conflict",
			err:    domainerr.ErrProductImageLimitReached,
			code:   constant.CodeProductImageLimitReached,
			status: http.StatusConflict,
		},
		{
			name:    "an unsupported image type names the field",
			err:     domainerr.ErrProductImageTypeUnsupported,
			code:    constant.CodeProductImageTypeUnsupported,
			status:  http.StatusBadRequest,
			field:   "image",
			details: true,
		},
		{
			name:    "an oversized upload is 413 and names the field",
			err:     domainerr.ErrProductImageTooLarge,
			code:    constant.CodeProductImageTooLarge,
			status:  http.StatusRequestEntityTooLarge,
			field:   "image",
			details: true,
		},
		{
			name:   "a media outage is a retryable 503",
			err:    domainerr.ErrProductMediaUnavailable,
			code:   constant.CodeProductMediaUnavailable,
			status: http.StatusServiceUnavailable,
		},
		{
			name:    "a field-level rejection is a validation error naming the field",
			err:     domainerr.InvalidProductField(model.FieldCategoryID, "does not exist"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldCategoryID,
			details: true,
		},
		{
			name:    "a non-positive price names the price field",
			err:     domainerr.InvalidProductField(model.FieldPrice, "must be a positive amount"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldPrice,
			details: true,
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

// A collision is 409, not 400: the value is well formed and simply not available,
// which is a different fix for the operator than a malformed value.
func TestACollisionMapsTo409AndAnInvalidValueTo400(t *testing.T) {
	collision := mapError(domainerr.ErrProductSlugTaken)
	if collision.Status != http.StatusConflict {
		t.Fatalf("a collision must be 409, got %d", collision.Status)
	}
	invalid := mapError(domainerr.InvalidProductField(model.FieldSlug, "is not URL-safe"))
	if invalid.Status != http.StatusBadRequest {
		t.Fatalf("an invalid value must be 400, got %d", invalid.Status)
	}
}

// The refused state change must name the current state (FR-024).
func TestAnInvalidStateChangeNamesTheCurrentState(t *testing.T) {
	appErr := mapError(domainerr.InvalidStateTransition(constant.SellStateDiscontinued, constant.SellStateActive))
	if !stringContains(appErr.Message, string(constant.SellStateDiscontinued)) {
		t.Fatalf("expected the message to name the current state, got %q", appErr.Message)
	}
}

func TestMapErrorFindsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrProductSlugTaken)

	appErr := mapError(wrapped)

	if string(appErr.Code) != constant.CodeProductSlugTaken {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("storage said: relation products does not exist"))

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

// stringContains is a tiny helper so this file does not import strings only for
// one check.
func stringContains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
