package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file covers the mapping of every code the cart module can answer with
// (specs/008-cart/contracts/error-codes.md): the two conflict codes, the shared
// PRODUCT_NOT_FOUND, the field-level validations, and the shared codes that pass
// through unchanged. Each rejection that has a field must populate
// `details[].field` so a client can point at the exact input.

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
			name:   "an unknown or removed product is the reused not-found",
			err:    domainerr.ErrProductNotFound,
			code:   "PRODUCT_NOT_FOUND",
			status: http.StatusNotFound,
		},
		{
			name:    "a not-on-sale product is a conflict naming the product",
			err:     domainerr.ProductNotPurchasable(uuid.New()),
			code:    constant.CodeProductNotPurchasable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "the bare not-purchasable sentinel is still a conflict",
			err:     domainerr.ErrProductNotPurchasable,
			code:    constant.CodeProductNotPurchasable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "a quantity above what is available is a conflict naming the quantity",
			err:     domainerr.QuantityExceedsAvailable(uuid.New(), 3, 5),
			code:    constant.CodeQuantityExceedsAvailable,
			status:  http.StatusConflict,
			field:   model.FieldQuantity,
			details: true,
		},
		{
			name:    "the bare exceeds-available sentinel is still a conflict",
			err:     domainerr.ErrQuantityExceedsAvailable,
			code:    constant.CodeQuantityExceedsAvailable,
			status:  http.StatusConflict,
			field:   model.FieldQuantity,
			details: true,
		},
		{
			name:    "an invalid value is a validation error naming the field",
			err:     domainerr.InvalidValue(model.FieldQuantity, "must be a positive whole number"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldQuantity,
			details: true,
		},
		{
			name:    "the bare invalid-value sentinel is still a validation error",
			err:     domainerr.ErrInvalidValue,
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldQuantity,
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

// A field-level rejection is 400 with `details[].field` populated.
func TestFieldErrorNamesTheField(t *testing.T) {
	appErr := fieldError(model.FieldProductID, "must be a UUID")
	if string(appErr.Code) != string(httpx.CodeValidation) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected VALIDATION_ERROR/400, got %s/%d", appErr.Code, appErr.Status)
	}
	if len(appErr.Details) != 1 || appErr.Details[0].Field != model.FieldProductID {
		t.Fatalf("expected the detail to name productId, got %+v", appErr.Details)
	}
}

// The shared codes the transport produces pass through unchanged.
func TestSharedCodesPassThroughUnchanged(t *testing.T) {
	shared := httpx.New(httpx.CodeUnauthenticated)
	if got := mapError(shared); got != shared {
		t.Fatal("the shared error envelope must pass an AppError through unchanged")
	}
	if got := httpx.FromError(shared); got != shared {
		t.Fatal("the shared error envelope must pass an AppError through unchanged")
	}
}

// A module code outside the shared catalogue still carries its documented status.
func TestCodedCarriesTheDocumentedStatus(t *testing.T) {
	appErr := coded(constant.CodeProductNotPurchasable, http.StatusConflict, "Product not purchasable")
	if string(appErr.Code) != constant.CodeProductNotPurchasable || appErr.Status != http.StatusConflict {
		t.Fatalf("unexpected coded error: %s/%d", appErr.Code, appErr.Status)
	}
}

// withField attaches the field detail to the coded error.
func TestWithFieldAttachesTheDetail(t *testing.T) {
	appErr := withField(coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
		model.FieldQuantity, "only 3 available")
	if len(appErr.Details) != 1 || appErr.Details[0].Field != model.FieldQuantity {
		t.Fatalf("expected a detail naming quantity, got %+v", appErr.Details)
	}
	if !strings.Contains(appErr.Details[0].Issue, "3") {
		t.Fatalf("the issue must state the available amount, got %q", appErr.Details[0].Issue)
	}
}

// The module's sentinels are found through a wrapper.
func TestMapErrorFindsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrProductNotFound)
	if appErr := mapError(wrapped); string(appErr.Code) != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

// An unmapped failure is hidden behind INTERNAL_ERROR so storage detail is never
// leaked.
func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("storage said: relation cart_items does not exist"))
	if string(appErr.Code) != string(httpx.CodeInternal) || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected INTERNAL_ERROR/500, got %s/%d", appErr.Code, appErr.Status)
	}
	if strings.Contains(appErr.Message, "cart_items") {
		t.Fatalf("the message must not leak storage detail, got %q", appErr.Message)
	}
	if appErr.Unwrap() == nil {
		t.Fatal("expected the cause to be kept for logging")
	}
}
