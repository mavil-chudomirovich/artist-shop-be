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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the transport contract of US3: `PUT /orders/{id}` is session-guarded,
// returns 200 with the edited order, passes the named address and the new lines
// through to the use case, and maps every refusal to its documented code — 400 for
// a bad body or a malformed `addressId`/line, 404 for another customer's order, 409
// for not-editable, empty or an invalid line. It mounts the route the way the
// composition root mounts it, over a fake of the use case, so the answers asserted
// are the handler's own (contracts/openapi.yaml, contracts/error-codes.md).

// fakeEditOrders is a fake of the handler's service covering the edit route. It
// records the input and the actor the handler handed it and answers the fixed
// result each test configures. It embeds the customer-order fake so a single value
// satisfies the whole service surface; its own EditMine overrides the stub the
// embedded fakes carry.
type fakeEditOrders struct {
	fakeOrders

	editView appdto.OrderView
	editErr  error

	gotEditInput appdto.EditInput
	gotEditActor appinterface.Actor
	editCalls    int
}

func (f *fakeEditOrders) EditMine(ctx context.Context, in appdto.EditInput) (appdto.OrderView, error) {
	f.gotEditInput = in
	f.gotEditActor, _ = appinterface.ActorFromContext(ctx)
	f.editCalls++
	return f.editView, f.editErr
}

// The US1/US3 fakes predate the edit route; this stub keeps them satisfying the
// handler's service interface. The edit tests use fakeEditOrders.
func (f *fakeCheckout) EditMine(context.Context, appdto.EditInput) (appdto.OrderView, error) {
	return appdto.OrderView{}, nil
}

// FR-012: a successful edit answers 200 with the edited order, and the handler
// hands the session's account and the addressed identifier to the use case.
func TestEditReturns200WithTheEditedOrder(t *testing.T) {
	edited := sampleView()
	edited.Status = constant.StatusPending
	fake := &fakeEditOrders{editView: edited}
	router := newOrderRouter(fake)

	body := `{"lines":[{"productId":"` + uuid.New().String() + `","quantity":2}]}`
	rec := perform(router, http.MethodPut, ordersPath+"/"+edited.ID.String(), body, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var decoded orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if decoded.Data.ID != edited.ID || decoded.Data.Status != "PENDING" {
		t.Fatalf("the edited order must answer PENDING, got %+v", decoded.Data)
	}
	if fake.gotEditActor.ID != testCustomerID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotEditActor.ID)
	}
	if fake.gotEditInput.OrderID != edited.ID || len(fake.gotEditInput.Lines) != 1 {
		t.Fatalf("the addressed id and line set must be passed through, got %+v", fake.gotEditInput)
	}
}

// FR-012: an omitted `addressId` keeps the current address (nil), and a named one
// is passed through to the use case.
func TestEditPassesTheAddressChoice(t *testing.T) {
	fake := &fakeEditOrders{editView: sampleView()}
	router := newOrderRouter(fake)
	path := ordersPath + "/" + fake.editView.ID.String()

	if rec := perform(router, http.MethodPut, path, `{"lines":[]}`, "customer-token"); rec.Code != http.StatusOK {
		t.Fatalf("empty address: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotEditInput.AddressID != nil {
		t.Fatalf("an omitted addressId must keep the current address, got %v", *fake.gotEditInput.AddressID)
	}

	named := uuid.New()
	rec := perform(router, http.MethodPut, path, `{"addressId":"`+named.String()+`","lines":[]}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("named address: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotEditInput.AddressID == nil || *fake.gotEditInput.AddressID != named {
		t.Fatalf("the named address must be passed through, got %v", fake.gotEditInput.AddressID)
	}
}

// contracts/error-codes.md: every edit refusal answers its documented code and
// status, and the offending item is named where the contract says.
func TestEditMapsEveryRefusal(t *testing.T) {
	product := uuid.New()
	cases := []struct {
		name   string
		err    error
		code   string
		status int
		field  string
	}{
		{"not editable", domainerr.ErrNotEditable, constant.CodeNotEditable, http.StatusConflict, ""},
		{"empty order", domainerr.ErrEmptyOrder, constant.CodeEmptyOrder, http.StatusConflict, ""},
		{"off-sale line", domainerr.ItemNotPurchasable(product), constant.CodeItemNotPurchasable, http.StatusConflict, "productId"},
		{"over available", domainerr.QuantityExceedsAvailable(product, 3, 5), constant.CodeQuantityExceedsAvailable, http.StatusConflict, "productId"},
		{"unknown or foreign order", domainerr.ErrNotFound, constant.CodeOrderNotFound, http.StatusNotFound, ""},
		{"foreign addressId", domainerr.InvalidValue(model.FieldAddressID, "is not one of the customer's addresses"), "VALIDATION_ERROR", http.StatusBadRequest, "addressId"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newOrderRouter(&fakeEditOrders{editErr: tc.err})
			body := `{"lines":[{"productId":"` + product.String() + `","quantity":1}]}`
			rec := perform(router, http.MethodPut, ordersPath+"/"+uuid.New().String(), body, "customer-token")
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			decoded := decodeError(t, rec)
			if decoded.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, decoded.Error.Code)
			}
			if tc.field != "" {
				if len(decoded.Error.Details) != 1 || decoded.Error.Details[0].Field != tc.field {
					t.Fatalf("expected a detail naming %s, got %+v", tc.field, decoded.Error.Details)
				}
			}
		})
	}
}

// contracts/openapi.yaml: a malformed `addressId` or line member is 400
// VALIDATION_ERROR naming it, and an unknown member or malformed JSON is 400
// MALFORMED_REQUEST (a body cannot name an owner).
func TestEditRejectsBadRequestShapes(t *testing.T) {
	product := uuid.New()
	cases := []struct {
		name  string
		body  string
		code  string
		field string
	}{
		{
			name:  "addressId is not a UUID",
			body:  `{"addressId":"not-a-uuid","lines":[]}`,
			code:  "VALIDATION_ERROR",
			field: "addressId",
		},
		{
			name:  "productId is not a UUID",
			body:  `{"lines":[{"productId":"not-a-uuid","quantity":1}]}`,
			code:  "VALIDATION_ERROR",
			field: "productId",
		},
		{
			name:  "quantity is not positive",
			body:  `{"lines":[{"productId":"` + product.String() + `","quantity":0}]}`,
			code:  "VALIDATION_ERROR",
			field: "quantity",
		},
		{
			name: "unknown member names an owner",
			body: `{"ownerId":"` + uuid.New().String() + `","lines":[]}`,
			code: "MALFORMED_REQUEST",
		},
		{
			name: "malformed JSON",
			body: `{"lines":`,
			code: "MALFORMED_REQUEST",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newOrderRouter(&fakeEditOrders{editView: sampleView()})
			rec := perform(router, http.MethodPut, ordersPath+"/"+uuid.New().String(), tc.body, "customer-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			decoded := decodeError(t, rec)
			if decoded.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, decoded.Error.Code)
			}
			if tc.field != "" && (len(decoded.Error.Details) != 1 || decoded.Error.Details[0].Field != tc.field) {
				t.Fatalf("expected a detail naming %s, got %+v", tc.field, decoded.Error.Details)
			}
		})
	}
}

// contracts/openapi.yaml: an edit with no session answers 401 UNAUTHENTICATED,
// before reaching the use case.
func TestEditRefusesWithoutASession(t *testing.T) {
	fake := &fakeEditOrders{editView: sampleView()}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPut, ordersPath+"/"+uuid.New().String(), `{"lines":[]}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
		t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
	}
	if fake.editCalls != 0 {
		t.Fatalf("an unauthenticated request must not reach the use case, got %d calls", fake.editCalls)
	}
}

// contracts/openapi.yaml: a malformed order identifier is 400 VALIDATION_ERROR
// naming `orderId`, not a lookup that returns nothing.
func TestEditRejectsAMalformedOrderIdentifier(t *testing.T) {
	fake := &fakeEditOrders{editView: sampleView()}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPut, ordersPath+"/not-a-uuid", `{"lines":[]}`, "customer-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "orderId" {
		t.Fatalf("expected a detail naming orderId, got %+v", body.Error.Details)
	}
}
