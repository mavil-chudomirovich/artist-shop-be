package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file covers the mapping of every code the category module can answer with
// (specs/005-category-catalog/contracts/error-codes.md). The public not-found is
// the only module code the read path produces; the collision and invalid-value
// cases are added by the administrator surface.

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
			name:   "unknown, withheld and removed share one not-found",
			err:    domainerr.ErrCategoryNotFound,
			code:   constant.CodeCategoryNotFound,
			status: http.StatusNotFound,
		},
		{
			name:    "a duplicate name names the field and is a conflict",
			err:     domainerr.ErrCategoryNameTaken,
			code:    constant.CodeCategoryNameTaken,
			status:  http.StatusConflict,
			field:   model.FieldName,
			details: true,
		},
		{
			name:    "a duplicate slug names the field and is a conflict",
			err:     domainerr.ErrCategorySlugTaken,
			code:    constant.CodeCategorySlugTaken,
			status:  http.StatusConflict,
			field:   model.FieldSlug,
			details: true,
		},
		{
			name:    "an invalid slug is a validation error naming the field",
			err:     domainerr.InvalidCategoryField(model.FieldSlug, "must be lowercase letters, digits and single hyphens"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldSlug,
			details: true,
		},
		{
			name:    "a blank name is a validation error naming the field",
			err:     domainerr.InvalidCategoryField(model.FieldName, "is required"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldName,
			details: true,
		},
		{
			name:    "an over-long description is a validation error naming the field",
			err:     domainerr.InvalidCategoryField(model.FieldDescription, "must be at most 2000 characters"),
			code:    string(httpx.CodeValidation),
			status:  http.StatusBadRequest,
			field:   model.FieldDescription,
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

// The two collision codes are deliberately distinct: a client that shows "that
// name is taken" learns which of the two it was from the code, not from a
// secondary detail field (contracts/error-codes.md).
func TestTheTwoCollisionCodesAreDistinct(t *testing.T) {
	if constant.CodeCategoryNameTaken == constant.CodeCategorySlugTaken {
		t.Fatal("the two collision codes must be different")
	}
	nameErr := mapError(domainerr.ErrCategoryNameTaken)
	slugErr := mapError(domainerr.ErrCategorySlugTaken)
	if nameErr.Code == slugErr.Code {
		t.Fatalf("the two collisions must map to different codes, both got %s", nameErr.Code)
	}
	if nameErr.Details[0].Field == slugErr.Details[0].Field {
		t.Fatalf("the two collisions must name different fields, both got %q", nameErr.Details[0].Field)
	}
}

// A collision is 409, not 400: the value is well formed and simply not available,
// which is a different fix for the operator than a malformed value.
func TestACollisionMapsTo409AndAnInvalidValueTo400(t *testing.T) {
	collision := mapError(domainerr.ErrCategoryNameTaken)
	if collision.Status != http.StatusConflict {
		t.Fatalf("a collision must be 409, got %d", collision.Status)
	}
	invalid := mapError(domainerr.InvalidCategoryField(model.FieldSlug, "is not URL-safe"))
	if invalid.Status != http.StatusBadRequest {
		t.Fatalf("an invalid value must be 400, got %d", invalid.Status)
	}
}

func TestMapErrorFindsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), domainerr.ErrCategorySlugTaken)

	appErr := mapError(wrapped)

	if string(appErr.Code) != constant.CodeCategorySlugTaken {
		t.Fatalf("expected the sentinel to be found through a wrapper, got %s", appErr.Code)
	}
}

func TestMapErrorHidesUnmappedFailures(t *testing.T) {
	appErr := mapError(errors.New("storage said: relation categories does not exist"))

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

// The field names the domain reports are the contract's member names, and
// presentation must not rename them.
func TestTheCategoryFieldDetailsAreTheContractMemberNames(t *testing.T) {
	cases := map[string]string{
		model.FieldName:        "name",
		model.FieldSlug:        "slug",
		model.FieldDescription: "description",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("field detail %q does not match the contract member name %q", got, want)
		}
	}
}
