package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file owns the US6 transport boundary: who may reach the overview and how a
// malformed query is refused. The route is guarded by the foundation's
// RequireRole, so a guest answers 401 and a customer answers 403 with the
// foundation's OnDenied hook recording the denial, and the query parser answers a
// field-named VALIDATION_ERROR rather than silently defaulting (FR-001, FR-003,
// FR-004). The 429 RATE_LIMITED path is not owned here; see
// TestOverviewRouterInstallsNoSecondRateLimiter.

// testCustomerID is the account a customer session identifies.
var testCustomerID = uuid.MustParse("22222222-2222-2222-2222-222222222222")

// denials records the privilege denials the foundation's OnDenied hook reports,
// the way the composition root records them for the auth module.
type denials struct {
	count   int
	subject string
}

func (d *denials) record(_ context.Context, id middleware.Identity, _ *http.Request) {
	d.count++
	d.subject = id.Subject
}

// boundaryHooks resolves an administrator or a customer from the token and records
// every role denial through OnDenied, exactly as the composition root does.
func boundaryHooks(denied *denials) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer admin-token":
				return &middleware.Identity{Subject: testAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			default:
				return nil, nil
			}
		},
		OnDenied: denied.record,
	}
}

// performOverviewQuery issues the overview request with an optional raw query and
// token.
func performOverviewQuery(handler http.Handler, rawQuery, token string) *httptest.ResponseRecorder {
	target := "/api/v1/admin/dashboard/overview"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// overviewError is the error envelope the handler answers with. The detail must
// name the offending member only; it must not leak storage or parse internals
// (FR-031, contracts/error-codes.md).
type overviewError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func decodeOverviewError(t *testing.T, rec *httptest.ResponseRecorder) overviewError {
	t.Helper()
	var body overviewError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-001: no token answers 401 UNAUTHENTICATED and the use case is never reached.
func TestOverviewRefusesAGuest(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}

	rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(&denials{})), "", "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeOverviewError(t, rec); body.Error.Code != string(httpx.CodeUnauthenticated) {
		t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
	}
	if svc.called {
		t.Fatal("a guest must not reach the use case")
	}
}

// FR-001: a customer token answers 403 FORBIDDEN and the denial is recorded through
// the foundation's OnDenied hook, so the privilege denial is audited exactly as
// module 01 audits its own. This is what verifies the router wires the shared
// RequireRole rather than a second denial mechanism (T046).
func TestOverviewRefusesACustomerAndRecordsTheDenial(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}
	denied := &denials{}

	rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(denied)), "", "customer-token")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeOverviewError(t, rec); body.Error.Code != string(httpx.CodeForbidden) {
		t.Fatalf("expected FORBIDDEN, got %s", body.Error.Code)
	}
	if denied.count != 1 {
		t.Fatalf("the role denial must be recorded once through OnDenied, got %d", denied.count)
	}
	if denied.subject != testCustomerID.String() {
		t.Fatalf("the denial must name the refusing session, got %q", denied.subject)
	}
	if svc.called {
		t.Fatal("a customer must not reach the use case")
	}
}

// FR-001, FR-002: an administrator token reaches the use case and answers 200 with
// no denial recorded.
func TestOverviewAdmitsAnAdministrator(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}
	denied := &denials{}

	rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(denied)), "", "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !svc.called {
		t.Fatal("an administrator must reach the use case")
	}
	if denied.count != 0 {
		t.Fatalf("an admitted administrator must not be denied, got %d", denied.count)
	}
}

// FR-003, FR-004: an invalid days or lowStockThreshold answers 400
// VALIDATION_ERROR naming the offending member in details[].field, and the use case
// is not reached.
func TestOverviewRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name  string
		query string
		field string
	}{
		{"days outside the set", "days=14", "days"},
		{"days not a number", "days=abc", "days"},
		{"threshold below the range", "lowStockThreshold=-1", "lowStockThreshold"},
		{"threshold above the range", "lowStockThreshold=10001", "lowStockThreshold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubOverviewService{out: sampleOverview()}

			rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(&denials{})), tc.query, "admin-token")

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeOverviewError(t, rec)
			if body.Error.Code != string(httpx.CodeValidation) {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != tc.field {
				t.Fatalf("expected one detail naming %q, got %+v", tc.field, body.Error.Details)
			}
			if body.Error.Details[0].Issue == "" {
				t.Fatal("the detail must describe the accepted range")
			}
			if svc.called {
				t.Fatal("an invalid request must not reach the use case")
			}
		})
	}
}

// FR-003, FR-004: the accepted boundaries are inclusive. days=30 and
// lowStockThreshold=0 are valid, and the parsed values reach the use case.
func TestOverviewAcceptsTheDocumentedBoundaries(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}

	rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(&denials{})), "days=30&lowStockThreshold=0", "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Days != 30 || svc.lastInput.LowStockThreshold != 0 {
		t.Fatalf("the parsed query must reach the use case, got %+v", svc.lastInput)
	}
}

// FR-003, FR-004: a request that names neither value is served with the defaults.
func TestOverviewAppliesDefaultsWhenTheQueryIsEmpty(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}

	rec := performOverviewQuery(newOverviewRouter(svc, boundaryHooks(&denials{})), "", "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Days != constant.DefaultDays || svc.lastInput.LowStockThreshold != constant.DefaultLowStockThreshold {
		t.Fatalf("expected the defaults %d/%d, got %+v",
			constant.DefaultDays, constant.DefaultLowStockThreshold, svc.lastInput)
	}
}

// The 429 RATE_LIMITED behaviour is inherited, not owned here. The shared
// /api/v1 limiter (internal/share/httpserver/routes.go) is the only rate-limit
// middleware in the pipeline, and this module's router installs no second one, so a
// rapid burst through the admin route alone never produces a 429. The inherited
// contract is asserted by the httpserver routes test; this test records that the
// admin router adds no limiter of its own.
func TestOverviewRouterInstallsNoSecondRateLimiter(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}
	router := newOverviewRouter(svc, boundaryHooks(&denials{}))

	for i := 0; i < 50; i++ {
		rec := performOverviewQuery(router, "", "admin-token")
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("the admin router must not install its own limiter, got 429 on request %d", i+1)
		}
	}
}
