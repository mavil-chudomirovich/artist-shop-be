package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file is the transport contract of US5: `POST /admin/orders/{id}/transfer`
// is guarded by the administrator role, hands a paid order to the account named
// by `email`, answers `200` with the order now owned by the recipient, refuses a
// missing or malformed `email` with `400 VALIDATION_ERROR` naming the field, maps
// an unpaid order to `409 ORDER_NOT_TRANSFERABLE` and an unknown recipient to
// `404 ORDER_TRANSFER_TARGET_NOT_FOUND`, and refuses a customer's session with
// `403 FORBIDDEN`. It mounts the route the way the composition root mounts it,
// over a fake of the use case, so the answers asserted are the handler's own
// (contracts/openapi.yaml, contracts/error-codes.md).

// fakeTransfer is a fake of the handler's service covering the transfer route. It
// embeds the administrator fake so a single value satisfies the whole service
// surface, and records the actor, the input and the result of the transfer call.
type fakeTransfer struct {
	fakeAdmin

	transferView     appdto.AdminOrderView
	transferErr      error
	gotTransferInput appdto.TransferInput
	gotTransferActor appinterface.Actor
	transferCalls    int
}

func (f *fakeTransfer) Transfer(ctx context.Context, in appdto.TransferInput) (appdto.AdminOrderView, error) {
	f.gotTransferInput = in
	f.gotTransferActor, _ = appinterface.ActorFromContext(ctx)
	f.transferCalls++
	return f.transferView, f.transferErr
}

// FR-024, quickstart 6a: a transfer answers 200 with the order now owned by the
// recipient, and the session's actor, the addressed identifier and the email are
// passed through.
func TestTransferReturnsTheOrderWithTheNewOwner(t *testing.T) {
	recipient := uuid.New()
	transferred := sampleAdminView()
	transferred.Status = constant.StatusPaid
	transferred.UserID = recipient
	fake := &fakeTransfer{transferView: transferred}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodPost,
		adminOrdersPath+"/"+transferred.ID.String()+"/transfer",
		`{"email":"recipient@example.com"}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the transferred order %q: %v", rec.Body.String(), err)
	}
	if body.Data.ID != transferred.ID || body.Data.UserID != recipient || body.Data.Status != "PAID" {
		t.Fatalf("the order must answer with the recipient as owner, got %+v", body.Data)
	}
	if fake.gotTransferInput.OrderID != transferred.ID || fake.gotTransferInput.Email != "recipient@example.com" {
		t.Fatalf("the addressed id and the email must be passed through, got %+v", fake.gotTransferInput)
	}
	if fake.gotTransferActor.ID != testAdminID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotTransferActor.ID)
	}
}

// contracts/error-codes.md, quickstart 6c/6d: every transfer refusal answers its
// documented code and status.
func TestTransferMapsEveryRefusal(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"an unpaid order is not transferable", domainerr.ErrNotTransferable, constant.CodeNotTransferable, http.StatusConflict},
		{"an email no account carries is a not-found", domainerr.ErrTransferTargetNotFound, constant.CodeTransferTargetNotFound, http.StatusNotFound},
		{"an unknown order is a not-found", domainerr.ErrNotFound, constant.CodeOrderNotFound, http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newOrderAdminRouter(&fakeTransfer{transferErr: tc.err})
			rec := perform(router, http.MethodPost,
				adminOrdersPath+"/"+uuid.New().String()+"/transfer",
				`{"email":"recipient@example.com"}`, "admin-token")
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, body.Error.Code)
			}
		})
	}
}

// contracts/error-codes.md: a missing or malformed `email` is 400
// VALIDATION_ERROR naming the field, before the use case is reached.
func TestTransferRejectsAMalformedEmail(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"an omitted email", `{}`},
		{"an empty email", `{"email":""}`},
		{"a blank email", `{"email":"   "}`},
		{"a value that is not an email", `{"email":"not-an-email"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransfer{}
			router := newOrderAdminRouter(fake)
			rec := perform(router, http.MethodPost,
				adminOrdersPath+"/"+uuid.New().String()+"/transfer", tc.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
			if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "email" {
				t.Fatalf("expected a detail naming email, got %+v", body.Error.Details)
			}
			if fake.transferCalls != 0 {
				t.Fatal("a malformed email must not reach the use case")
			}
		})
	}
}

// contracts/error-codes.md: a malformed order identifier is 400 VALIDATION_ERROR
// naming `orderId`, and an unknown member is 400 MALFORMED_REQUEST.
func TestTransferRejectsBadRequestShapes(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		body  string
		code  string
		field string
	}{
		{
			name:  "orderId is not a UUID",
			path:  adminOrdersPath + "/not-a-uuid/transfer",
			body:  `{"email":"recipient@example.com"}`,
			code:  "VALIDATION_ERROR",
			field: "orderId",
		},
		{
			name: "an unknown member names an owner",
			path: adminOrdersPath + "/" + uuid.New().String() + "/transfer",
			body: `{"email":"recipient@example.com","userId":"` + uuid.New().String() + `"}`,
			code: "MALFORMED_REQUEST",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransfer{}
			router := newOrderAdminRouter(fake)
			rec := perform(router, http.MethodPost, tc.path, tc.body, "admin-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, body.Error.Code)
			}
			if tc.field != "" && (len(body.Error.Details) != 1 || body.Error.Details[0].Field != tc.field) {
				t.Fatalf("expected a detail naming %s, got %+v", tc.field, body.Error.Details)
			}
			if fake.transferCalls != 0 {
				t.Fatal("a malformed request must not reach the use case")
			}
		})
	}
}

// FR-023, contracts/error-codes.md, quickstart 6e: a customer's session is refused
// 403 FORBIDDEN on the transfer route, before reaching the use case.
func TestTransferRefusesACustomerSession(t *testing.T) {
	fake := &fakeTransfer{}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodPost,
		adminOrdersPath+"/"+uuid.New().String()+"/transfer",
		`{"email":"recipient@example.com"}`, "customer-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", body.Error.Code)
	}
	if fake.transferCalls != 0 {
		t.Fatal("a forbidden request must not reach the use case")
	}
}

// contracts/error-codes.md: the transfer route with no session answers 401
// UNAUTHENTICATED, before reaching the use case.
func TestTransferRefusesWithoutASession(t *testing.T) {
	fake := &fakeTransfer{}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodPost,
		adminOrdersPath+"/"+uuid.New().String()+"/transfer",
		`{"email":"recipient@example.com"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
		t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
	}
	if fake.transferCalls != 0 {
		t.Fatal("an unauthenticated request must not reach the use case")
	}
}

// The US1 fake predates the transfer route; this stub keeps it satisfying the
// handler's service interface so its checkout tests are unaffected. The transfer
// tests use fakeTransfer.
func (f *fakeCheckout) Transfer(context.Context, appdto.TransferInput) (appdto.AdminOrderView, error) {
	return appdto.AdminOrderView{}, nil
}
