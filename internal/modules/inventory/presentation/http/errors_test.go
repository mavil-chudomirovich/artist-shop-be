package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file covers the mapping of every code the inventory module can answer
// with (specs/007-inventory-tracking/contracts/error-codes.md): the module's
// insufficient-stock conflict, the invalid-value and malformed-identifier
// validations, the reused PRODUCT_NOT_FOUND, and the shared codes that pass
// through unchanged.

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
			name:   "a missing product is the reused not-found",
			err:    domainerr.ErrProductNotFound,
			code:   "PRODUCT_NOT_FOUND",
			status: http.StatusNotFound,
		},
		{
			name:    "insufficient stock is a conflict naming the field",
			err:     domainerr.InsufficientStock(model.FieldQuantity, 2, 5),
			code:    constant.CodeInsufficientStock,
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
			name:    "the bare insufficient-stock sentinel is still a conflict",
			err:     domainerr.ErrInsufficientStock,
			code:    constant.CodeInsufficientStock,
			status:  http.StatusConflict,
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

// The insufficiency is 409, not 400: the quantity is well formed and it is the
// current state of the shelf that makes the change impossible.
func TestInsufficientStockMapsTo409(t *testing.T) {
	appErr := mapError(domainerr.InsufficientStock(model.FieldQuantity, 0, 1))
	if appErr.Status != http.StatusConflict {
		t.Fatalf("insufficient stock must be 409, got %d", appErr.Status)
	}
	if string(appErr.Code) != "INVENTORY_INSUFFICIENT_STOCK" {
		t.Fatalf("unexpected code %s", appErr.Code)
	}
}

// The shared codes the transport produces pass through unchanged: a
// malformed-identifier detail keeps VALIDATION_ERROR and names the field.
func TestSharedCodesPassThroughUnchanged(t *testing.T) {
	appErr := mapError(httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: fieldProductID, Issue: "must be a UUID"}))
	if string(appErr.Code) != string(httpx.CodeValidation) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected VALIDATION_ERROR/400, got %s/%d", appErr.Code, appErr.Status)
	}

	field := fieldError(fieldProductID, "must be a UUID")
	if string(field.Code) != string(httpx.CodeValidation) || field.Status != http.StatusBadRequest {
		t.Fatalf("fieldError must answer VALIDATION_ERROR/400, got %s/%d", field.Code, field.Status)
	}
	if len(field.Details) != 1 || field.Details[0].Field != fieldProductID {
		t.Fatalf("fieldError must name the field, got %+v", field.Details)
	}

	shared := httpx.New(httpx.CodeUnauthenticated)
	if got := httpx.FromError(shared); got != shared {
		t.Fatal("the shared error envelope must pass an AppError through unchanged")
	}
}

// A module code outside the shared catalogue still carries its documented status.
func TestCodedCarriesTheDocumentedStatus(t *testing.T) {
	appErr := coded(constant.CodeInsufficientStock, http.StatusConflict, "Insufficient stock")
	if string(appErr.Code) != "INVENTORY_INSUFFICIENT_STOCK" || appErr.Status != http.StatusConflict {
		t.Fatalf("unexpected coded error: %s/%d", appErr.Code, appErr.Status)
	}
}

func TestMapErrorFindsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrProductNotFound)
	if appErr := mapError(wrapped); string(appErr.Code) != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("storage said: relation stock_levels does not exist"))
	if string(appErr.Code) != string(httpx.CodeInternal) || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected INTERNAL_ERROR/500, got %s/%d", appErr.Code, appErr.Status)
	}
	if appErr.Message != httpx.New(httpx.CodeInternal).Message {
		t.Fatalf("expected the generic safe message, got %q", appErr.Message)
	}
	if appErr.Unwrap() == nil {
		t.Fatal("expected the cause to be kept for logging")
	}
	if strings.Contains(appErr.Message, "stock_levels") {
		t.Fatalf("the message must not leak storage detail, got %q", appErr.Message)
	}
}
