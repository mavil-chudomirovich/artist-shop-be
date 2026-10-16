package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file pins the module's error mapping to
// specs/015-product-sku-model/contracts/error-codes.md. A code is part of the
// contract, so each sentinel must keep the status and code the reference names;
// the field-level collisions must also name the member the client has to fix.

// The variant and code collisions introduced by this feature map to the status
// and code of the reference, naming the offending field where the reference does.
func TestMapErrorMapsTheVariantAndCodeCollisions(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantField string
	}{
		{"duplicate product code", domainerr.ErrProductCodeTaken, constant.CodeProductCodeTaken, model.FieldCode},
		{"duplicate SKU", domainerr.ErrVariantSKUTaken, constant.CodeVariantSKUTaken, model.FieldSKU},
		{"unknown variant", domainerr.ErrVariantNotFound, constant.CodeVariantNotFound, ""},
		{"last variant removal", domainerr.ErrVariantLastRemoval, constant.CodeVariantLastRemoval, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appErr := mapError(tc.err)
			if want := http.StatusConflict; tc.wantCode == constant.CodeVariantNotFound {
				if appErr.Status != http.StatusNotFound {
					t.Fatalf("expected 404, got %d", appErr.Status)
				}
			} else if appErr.Status != want {
				t.Fatalf("expected %d, got %d", want, appErr.Status)
			}
			if string(appErr.Code) != tc.wantCode {
				t.Fatalf("expected %s, got %s", tc.wantCode, appErr.Code)
			}
			if tc.wantField == "" {
				return
			}
			if len(appErr.Details) != 1 || appErr.Details[0].Field != tc.wantField {
				t.Fatalf("expected the field %q to be named, got %+v", tc.wantField, appErr.Details)
			}
		})
	}
}

// A refused sell-state transition is a conflict whose message names the state the
// variant was in, which is what makes it distinguishable from a typo (FR-015).
func TestMapErrorNamesTheCurrentStateInARefusedTransition(t *testing.T) {
	err := domainerr.InvalidVariantStateTransition(constant.SellStateActive, constant.SellStateComingSoon)

	appErr := mapError(err)
	if appErr.Status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", appErr.Status)
	}
	if string(appErr.Code) != constant.CodeVariantStateTransitionInvalid {
		t.Fatalf("expected %s, got %s", constant.CodeVariantStateTransitionInvalid, appErr.Code)
	}
	if !strings.Contains(appErr.Message, string(constant.SellStateActive)) {
		t.Fatalf("the refusal must name the current state, got %q", appErr.Message)
	}
}
