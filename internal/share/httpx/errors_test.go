package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorCatalogueStatus(t *testing.T) {
	cases := map[ErrorCode]int{
		CodeValidation:       http.StatusBadRequest,
		CodeUnauthenticated:  http.StatusUnauthorized,
		CodeForbidden:        http.StatusForbidden,
		CodeNotFound:         http.StatusNotFound,
		CodeMethodNotAllowed: http.StatusMethodNotAllowed,
		CodeConflict:         http.StatusConflict,
		CodePayloadTooLarge:  http.StatusRequestEntityTooLarge,
		CodeRateLimited:      http.StatusTooManyRequests,
		CodeInternal:         http.StatusInternalServerError,
		CodeUnavailable:      http.StatusServiceUnavailable,
	}
	for code, want := range cases {
		if got := New(code).Status; got != want {
			t.Errorf("code %s: expected status %d, got %d", code, want, got)
		}
	}
}

func TestFromErrorMapsUnknownToInternal(t *testing.T) {
	appErr := FromError(errors.New("db exploded"))
	if appErr.Code != CodeInternal {
		t.Fatalf("expected INTERNAL_ERROR, got %s", appErr.Code)
	}
	if appErr.Message != catalogue[CodeInternal].message {
		t.Fatalf("internal message must not leak the cause: %q", appErr.Message)
	}
}

func TestFromErrorPreservesAppError(t *testing.T) {
	orig := New(CodeConflict)
	if got := FromError(orig); got != orig {
		t.Fatal("expected the original AppError to be returned")
	}
}

func TestCatalogueRegistersAllCommonCodes(t *testing.T) {
	codes := []ErrorCode{
		CodeValidation,
		CodeMalformedRequest,
		CodeUnauthenticated,
		CodeForbidden,
		CodeNotFound,
		CodeMethodNotAllowed,
		CodeConflict,
		CodePayloadTooLarge,
		CodeUnsupportedMedia,
		CodeRateLimited,
		CodeInternal,
		CodeUnavailable,
	}
	for _, code := range codes {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("error code %q is not registered in the catalogue", code)
			continue
		}
		if appErr := New(code); appErr.Status == 0 || appErr.Message == "" {
			t.Errorf("error code %q produced an incomplete AppError", code)
		}
	}
}

func TestNewDoesNotFallBackForRegisteredCodes(t *testing.T) {
	for code := range catalogue {
		if code == CodeInternal {
			continue
		}
		if New(code).Message == catalogue[CodeInternal].message {
			t.Errorf("error code %q falls back to the INTERNAL_ERROR message", code)
		}
	}
}

func TestWrappedCauseNotExposed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/", nil), "corr-3")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	WriteError(rec, req, Wrap(fmt.Errorf("secret sql statement"), CodeInternal), logger)

	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error.Message != catalogue[CodeInternal].message {
		t.Fatalf("cause leaked to client: %q", body.Error.Message)
	}
}
