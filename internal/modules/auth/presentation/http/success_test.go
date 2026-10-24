package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// TestSuccessAcknowledgementsReturnCodeAndMessage pins the success contract of
// every MessageResponse endpoint: HTTP status, stable success code and default
// message. Clients branch on data.code, never on data.message. Any wording or
// code change must update docs/api-reference.md in the same change.
func TestSuccessAcknowledgementsReturnCodeAndMessage(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		role    access.Role
		token   string
		status  int
		code    string
		message string
	}{
		{
			name: "verify registration", method: http.MethodPost, path: "/register/verify-otp",
			body: `{"registrationId":"reg-1","otp":"123456"}`,
			role: access.RoleCustomer, status: http.StatusOK,
			code: constant.CodeRegistrationConfirmed, message: constant.MsgRegistrationConfirmed,
		},
		{
			name: "resend confirmation code", method: http.MethodPost, path: "/register/resend-otp",
			body: `{"registrationId":"reg-1"}`,
			role: access.RoleCustomer, status: http.StatusAccepted,
			code: constant.CodeCodeSent, message: constant.MsgCodeSent,
		},
		{
			name: "cancel registration", method: http.MethodPost, path: "/register/cancel",
			body: `{"registrationId":"reg-1"}`,
			role: access.RoleCustomer, status: http.StatusOK,
			code: constant.CodeRegistrationCancelled, message: constant.MsgRegistrationCancelled,
		},
		{
			name: "forgot password", method: http.MethodPost, path: "/password/forgot",
			body: `{"email":"user@example.com"}`,
			role: access.RoleCustomer, status: http.StatusAccepted,
			code: constant.CodeCodeSent, message: constant.MsgCodeSent,
		},
		{
			name: "reset password", method: http.MethodPost, path: "/password/reset",
			body: `{"token":"tok","newPassword":"N3w!Passw0rd"}`,
			role: access.RoleCustomer, status: http.StatusOK,
			code: constant.CodePasswordReset, message: constant.MsgPasswordReset,
		},
		{
			name: "admin probe", method: http.MethodGet, path: "/admin/probe",
			role: access.RoleAdmin, token: "token", status: http.StatusOK,
			code: constant.CodeAdminProbe, message: constant.MsgAdminProbe,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.method == http.MethodGet {
				rec = do(testRouter(tc.role), tc.method, tc.path, tc.token)
			} else {
				rec = doJSON(testRouter(tc.role), tc.method, tc.path, tc.body, tc.token)
			}
			if rec.Code != tc.status {
				t.Fatalf("expected status %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			var body struct {
				Data struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("expected a JSON success body, got %v", err)
			}
			if body.Data.Code != tc.code {
				t.Fatalf("expected code %q, got %q", tc.code, body.Data.Code)
			}
			if body.Data.Message != tc.message {
				t.Fatalf("expected message %q, got %q", tc.message, body.Data.Message)
			}
		})
	}
}
