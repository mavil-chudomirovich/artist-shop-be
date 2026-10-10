package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file covers the mapping of every code the order module can answer with
// (specs/009-order/contracts/error-codes.md): the six checkout refusal codes, the
// state and transfer codes, the field-level `addressId` validation, and the shared
// codes that pass through unchanged. Every refusal that has a field populates
// `details[].field` so a client can point at the exact input, and the quantity
// code's issue states the available amount.

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
			name:   "an unknown or foreign order is the module not-found",
			err:    domainerr.ErrNotFound,
			code:   constant.CodeOrderNotFound,
			status: http.StatusNotFound,
		},
		{
			name:   "an empty cart is a conflict",
			err:    domainerr.ErrCartEmpty,
			code:   constant.CodeCartEmpty,
			status: http.StatusConflict,
		},
		{
			name:    "an off-sale product is a conflict naming the product",
			err:     domainerr.ItemNotPurchasable(uuid.New()),
			code:    constant.CodeItemNotPurchasable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "the bare not-purchasable sentinel is still a conflict",
			err:     domainerr.ErrItemNotPurchasable,
			code:    constant.CodeItemNotPurchasable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "a changed price is a conflict naming the product",
			err:     domainerr.ItemPriceChanged(uuid.New()),
			code:    constant.CodeItemPriceChanged,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "the bare price-changed sentinel is still a conflict",
			err:     domainerr.ErrItemPriceChanged,
			code:    constant.CodeItemPriceChanged,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "a quantity above available is a conflict naming the product and the amount",
			err:     domainerr.QuantityExceedsAvailable(uuid.New(), 3, 5),
			code:    constant.CodeQuantityExceedsAvailable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:    "the bare exceeds-available sentinel is still a conflict",
			err:     domainerr.ErrQuantityExceedsAvailable,
			code:    constant.CodeQuantityExceedsAvailable,
			status:  http.StatusConflict,
			field:   model.FieldProductID,
			details: true,
		},
		{
			name:   "a customer with no address is a conflict",
			err:    domainerr.ErrNoAddress,
			code:   constant.CodeNoAddress,
			status: http.StatusConflict,
		},
		{
			name:   "an illegal state move is a conflict naming the current state",
			err:    domainerr.StateTransitionInvalid(constant.StatusPaid, constant.StatusCancelled),
			code:   constant.CodeStateTransitionInvalid,
			status: http.StatusConflict,
		},
		{
			name:   "the bare transition sentinel is still a conflict",
			err:    domainerr.ErrStateTransitionInvalid,
			code:   constant.CodeStateTransitionInvalid,
			status: http.StatusConflict,
		},
		{
			name:   "an order that is not paid is not transferable",
			err:    domainerr.ErrNotTransferable,
			code:   constant.CodeNotTransferable,
			status: http.StatusConflict,
		},
		{
			name:   "an email no account carries is a not-found",
			err:    domainerr.ErrTransferTargetNotFound,
			code:   constant.CodeTransferTargetNotFound,
			status: http.StatusNotFound,
		},
		{
			name:   "an order that may not be changed is a not-editable conflict",
			err:    domainerr.ErrNotEditable,
			code:   constant.CodeNotEditable,
			status: http.StatusConflict,
		},
		{
			name:   "an edit that would empty the order is a conflict",
			err:    domainerr.ErrEmptyOrder,
			code:   constant.CodeEmptyOrder,
			status: http.StatusConflict,
		},
		{
			name:    "a foreign addressId is a validation error naming the field",
			err:     domainerr.InvalidValue(model.FieldAddressID, "is not one of the customer's addresses"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldAddressID,
			details: true,
		},
		{
			name:    "the bare invalid-value sentinel is still a validation error",
			err:     domainerr.ErrInvalidValue,
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldAddressID,
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

// The quantity refusal's issue states the available amount, so a client knows how
// far to reduce the line (contracts/error-codes.md).
func TestQuantityRefusalNamesTheAvailableAmount(t *testing.T) {
	appErr := mapError(domainerr.QuantityExceedsAvailable(uuid.New(), 3, 5))
	if len(appErr.Details) != 1 || !strings.Contains(appErr.Details[0].Issue, "3") {
		t.Fatalf("the issue must state the available amount, got %+v", appErr.Details)
	}
}

// A refused state move names the current state in its message (FR-011).
func TestTransitionRefusalNamesTheCurrentState(t *testing.T) {
	appErr := mapError(domainerr.StateTransitionInvalid(constant.StatusShipped, constant.StatusCompleted))
	if !strings.Contains(appErr.Message, string(constant.StatusShipped)) {
		t.Fatalf("the message must name the current state, got %q", appErr.Message)
	}
}

// A field-level rejection is 400 with `details[].field` populated.
func TestFieldErrorNamesTheField(t *testing.T) {
	appErr := fieldError(model.FieldAddressID, "must be a UUID")
	if string(appErr.Code) != string(httpx.CodeValidation) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected VALIDATION_ERROR/400, got %s/%d", appErr.Code, appErr.Status)
	}
	if len(appErr.Details) != 1 || appErr.Details[0].Field != model.FieldAddressID {
		t.Fatalf("expected the detail to name addressId, got %+v", appErr.Details)
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
	appErr := coded(constant.CodeCartEmpty, http.StatusConflict, "Cart is empty")
	if string(appErr.Code) != constant.CodeCartEmpty || appErr.Status != http.StatusConflict {
		t.Fatalf("unexpected coded error: %s/%d", appErr.Code, appErr.Status)
	}
}

// withField attaches the field detail to the coded error.
func TestWithFieldAttachesTheDetail(t *testing.T) {
	appErr := withField(coded(constant.CodeQuantityExceedsAvailable, http.StatusConflict, "Quantity exceeds available"),
		model.FieldProductID, "only 3 available")
	if len(appErr.Details) != 1 || appErr.Details[0].Field != model.FieldProductID {
		t.Fatalf("expected a detail naming productId, got %+v", appErr.Details)
	}
	if !strings.Contains(appErr.Details[0].Issue, "3") {
		t.Fatalf("the issue must state the available amount, got %q", appErr.Details[0].Issue)
	}
}

// The module's sentinels are found through a wrapper.
func TestMapErrorFindsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrCartEmpty)
	if appErr := mapError(wrapped); string(appErr.Code) != constant.CodeCartEmpty {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

// An unmapped failure is hidden behind INTERNAL_ERROR so storage detail is never
// leaked.
func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("storage said: relation orders does not exist"))
	if string(appErr.Code) != string(httpx.CodeInternal) || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected INTERNAL_ERROR/500, got %s/%d", appErr.Code, appErr.Status)
	}
	if strings.Contains(appErr.Message, "orders") {
		t.Fatalf("the message must not leak storage detail, got %q", appErr.Message)
	}
	if appErr.Unwrap() == nil {
		t.Fatal("expected the cause to be kept for logging")
	}
}
