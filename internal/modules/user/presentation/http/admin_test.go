package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file exercises the US4 route end to end: the real handler, the real role
// middleware and the real lookup use case over in-memory storage. router_test.go
// proves the route is wired with a stub; the point here is the authorization
// boundary an operator capability depends on — who may read, what is refused, and
// what the audit trail has to hold afterwards (FR-022, FR-022a, Constitution V, VI).

// Tokens the fixture's hooks understand. They stand in for what the auth module
// issues, so no test needs a real token issuer.
const (
	adminToken    = "administrator-session"
	customerToken = "customer-session"
)

// adminAuditEvent is one recorded audit event. The whole event is kept rather than
// just the action, because FR-022a is about *who* looked at *which* customer: an
// action name alone would not prove the trail carries the actor.
type adminAuditEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

// adminAudit collects the audit events the use cases emit.
type adminAudit struct {
	mu     sync.Mutex
	events []adminAuditEvent
}

func (a *adminAudit) Record(_ context.Context, action, outcome string, actorID *uuid.UUID,
	actorRole, targetType, targetID string, metadata map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, adminAuditEvent{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
		metadata:   metadata,
	})
}

func (a *adminAudit) all() []adminAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]adminAuditEvent(nil), a.events...)
}

// countOf counts the events of one action.
func (a *adminAudit) countOf(action string) int {
	total := 0
	for _, event := range a.all() {
		if event.action == action {
			total++
		}
	}
	return total
}

// sole returns the only event of an action, failing when there is not exactly one.
func (a *adminAudit) sole(t *testing.T, action string) adminAuditEvent {
	t.Helper()
	events := a.all()
	var matched []adminAuditEvent
	for _, event := range events {
		if event.action == action {
			matched = append(matched, event)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("expected exactly one %s event, got %d in %+v", action, len(matched), events)
	}
	return matched[0]
}

var _ appinterface.Auditor = (*adminAudit)(nil)

// privilegeDenial records what the composition root's OnDenied hook receives.
//
// The hook belongs to the auth module, which records the denial there with
// AUTH_PRIVILEGE_DENIED for its own guarded routes; this module only supplies the
// role requirement, so what it can assert is that a refusal reaches the hook with
// the refusing identity and the path that was probed. The router comment in
// router.go says the same.
type privilegeDenial struct {
	mu       sync.Mutex
	denials  []deniedRequest
	recorded int
}

type deniedRequest struct {
	subject string
	role    string
	path    string
}

func (d *privilegeDenial) record(_ context.Context, id middleware.Identity, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recorded++
	d.denials = append(d.denials, deniedRequest{subject: id.Subject, role: id.Role, path: r.URL.Path})
}

func (d *privilegeDenial) all() []deniedRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]deniedRequest(nil), d.denials...)
}

// lookupService binds the real lookup use case to the module's full use-case
// surface. The remaining methods come from the stub in router_test.go, whose routes
// these tests never reach; this adapter disappears once T023 wires
// *implement.Service directly.
type lookupService struct {
	*stubService
	users *implement.Service
}

func (s lookupService) LookupCustomer(ctx context.Context, userID uuid.UUID) (contracts.Customer, error) {
	return s.users.LookupCustomer(ctx, userID)
}

var _ appinterface.UserService = lookupService{}

// adminFixture is the whole operator-lookup stack over in-memory storage.
type adminFixture struct {
	router    http.Handler
	profiles  *memoryProfiles
	addresses *memoryAddressRepo
	audit     *adminAudit
	denials   *privilegeDenial
	// admin is the account an administrator session identifies.
	admin uuid.UUID
	// customer is the account an administrator may read.
	customer uuid.UUID
	// customerToken is the account a customer session identifies.
	customerToken uuid.UUID
	// defaultAddress and otherAddress are the customer's two addresses.
	defaultAddress uuid.UUID
	otherAddress   uuid.UUID
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	admin := uuid.New()
	customer := uuid.New()
	customerSession := uuid.New()
	addresses := newMemoryAddressRepo()
	divisions := cascadeDivisions{}
	profiles := newMemoryProfiles(
		model.Profile{ID: admin, Email: "admin@example.com", Role: access.RoleAdmin},
		model.Profile{
			ID:          customer,
			Email:       "customer@example.com",
			Role:        access.RoleCustomer,
			DisplayName: "Nguyen Van A",
		},
		model.Profile{ID: customerSession, Email: "session@example.com", Role: access.RoleCustomer},
	)
	audit := &adminAudit{}
	denials := &privilegeDenial{}
	users := implement.New(implement.Service{
		Profiles:  profiles,
		Addresses: addresses,
		Divisions: divisions,
		Tx:        &immediateUnitOfWork{repo: addresses},
		Audit:     audit,
		Mapper:    mapper.New(divisions),
	})
	svc := lookupService{stubService: newStub(access.RoleCustomer), users: users}
	handler := New(svc, appinterface.Config{}, testLogger)
	return &adminFixture{
		router:         handler.Router(openLimits, operatorHooks(admin, customerSession, denials)),
		profiles:       profiles,
		addresses:      addresses,
		audit:          audit,
		denials:        denials,
		admin:          admin,
		customer:       customer,
		customerToken:  customerSession,
		defaultAddress: uuid.New(),
		otherAddress:   uuid.New(),
	}
}

// operatorHooks resolves two sessions, one per role, and records every privilege
// denial the way the composition root does for the auth module's own routes.
func operatorHooks(admin, customer uuid.UUID, denials *privilegeDenial) middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			header := r.Header.Get("Authorization")
			switch {
			case header == "":
				return nil, nil
			case strings.Contains(header, adminToken):
				return &middleware.Identity{Subject: admin.String(), Role: string(access.RoleAdmin), TokenID: "jti"}, nil
			case strings.Contains(header, customerToken):
				return &middleware.Identity{Subject: customer.String(), Role: string(access.RoleCustomer), TokenID: "jti"}, nil
			default:
				return nil, httpx.New(httpx.CodeUnauthenticated)
			}
		},
		OnDenied: denials.record,
	}
}

// seedAddresses stores two addresses of the customer.
//
// The rows are stored in the wrong order on purpose: the non-default address is the
// more recently updated one and is inserted first, so a lookup that answered with
// the default first can only have got that order from the repository rather than
// from the insertion order (FR-007d).
func (f *adminFixture) seedAddresses(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	older := now.Add(-time.Hour)
	f.addresses.mu.Lock()
	defer f.addresses.mu.Unlock()
	f.addresses.rows[f.otherAddress] = model.Address{
		ID: f.otherAddress, UserID: f.customer,
		RecipientName: "Nguyen Van A", RecipientPhone: "0912345678",
		ProvinceCode: "79", ProvinceName: "Ho Chi Minh",
		WardCode: "26735", WardName: "Phuong Ben Thanh",
		StreetAddress: "1 Le Loi",
		CreatedAt:     now, UpdatedAt: now,
	}
	f.addresses.seq = append(f.addresses.seq, f.otherAddress)
	f.addresses.rows[f.defaultAddress] = model.Address{
		ID: f.defaultAddress, UserID: f.customer,
		RecipientName: "Nguyen Van A", RecipientPhone: "0912345678",
		ProvinceCode: "79", ProvinceName: "Ho Chi Minh",
		WardCode: "26734", WardName: "Phuong Ben Nghe",
		StreetAddress: "12 Nguyen Hue", IsDefault: true,
		CreatedAt: older, UpdatedAt: older,
	}
	f.addresses.seq = append(f.addresses.seq, f.defaultAddress)
}

func (f *adminFixture) lookup(token string) *httptest.ResponseRecorder {
	return do(f.router, http.MethodGet, "/"+f.customer.String(), token)
}

// lookupBody mirrors the contract's CustomerLookup shape.
type lookupBody struct {
	Data struct {
		ID          uuid.UUID        `json:"id"`
		Email       string           `json:"email"`
		Role        access.Role      `json:"role"`
		DisplayName string           `json:"displayName"`
		Phone       *string          `json:"phone"`
		Addresses   []httpdtoAddress `json:"addresses"`
	} `json:"data"`
}

func decodeLookup(t *testing.T, rec *httptest.ResponseRecorder) lookupBody {
	t.Helper()
	var body lookupBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode lookup body %q: %v", rec.Body.String(), err)
	}
	return body
}

// FR-022, FR-022a and SC-010: an administrator reads one customer's contact details
// and address list, the default address comes first, and the read leaves a trail
// naming the administrator and the customer.
func TestAnAdministratorReadsTheCustomerAndTheReadIsAudited(t *testing.T) {
	fixture := newAdminFixture(t)
	fixture.seedAddresses(t)

	rec := fixture.lookup(adminToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeLookup(t, rec)
	if body.Data.ID != fixture.customer || body.Data.Email != "customer@example.com" {
		t.Fatalf("expected the customer's contact details, got %+v", body.Data)
	}
	if body.Data.DisplayName != "Nguyen Van A" || body.Data.Role != access.RoleCustomer {
		t.Fatalf("expected the stored profile, got %+v", body.Data)
	}
	if body.Data.Phone != nil {
		t.Fatalf("expected no phone for a customer who never set one, got %v", *body.Data.Phone)
	}
	if len(body.Data.Addresses) != 2 {
		t.Fatalf("expected both addresses, got %+v", body.Data.Addresses)
	}
	// The default comes first even though the other address is more recent and was
	// stored first: the order is the repository's, not the storage order's (FR-007d).
	if body.Data.Addresses[0].ID != fixture.defaultAddress.String() || !body.Data.Addresses[0].IsDefault {
		t.Fatalf("expected the default address first, got %+v", body.Data.Addresses)
	}
	if body.Data.Addresses[1].ID != fixture.otherAddress.String() {
		t.Fatalf("expected the other address second, got %+v", body.Data.Addresses)
	}
	// The operator view is contact details and addresses: the avatar is not part of
	// it, and neither is the customer-facing remapping hint.
	if strings.Contains(rec.Body.String(), "avatar") || strings.Contains(rec.Body.String(), "divisionNeedsReview") {
		t.Fatalf("the operator view must not carry customer-only members: %s", rec.Body.String())
	}

	event := fixture.audit.sole(t, constant.AuditProfileViewedByAdmin)
	if event.outcome != "SUCCESS" {
		t.Fatalf("expected a SUCCESS outcome, got %q", event.outcome)
	}
	if event.actorID == nil || *event.actorID != fixture.admin {
		t.Fatalf("expected the administrator %s as the actor, got %v", fixture.admin, event.actorID)
	}
	if event.actorRole != string(access.RoleAdmin) {
		t.Fatalf("expected the administrator role, got %q", event.actorRole)
	}
	if event.targetType != "user" || event.targetID != fixture.customer.String() {
		t.Fatalf("expected the customer as the target, got %q/%q", event.targetType, event.targetID)
	}
}

// research D10: the operator view has no divisionNeedsReview member at all, while
// the customer's own list states it even when false. The two answers are opposite
// on purpose — nothing on this path checks a stored code against the official
// dataset, so the honest answer is "this view cannot say" rather than a `false`
// that would claim every code is current.
//
// Each address is decoded into a map so the member's absence is asserted per row;
// a typed decode reports the same zero value whether the member is there or not.
func TestTheOperatorViewCarriesNoDivisionNeedsReviewMember(t *testing.T) {
	fixture := newAdminFixture(t)
	fixture.seedAddresses(t)

	rec := fixture.lookup(adminToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Addresses []map[string]any `json:"addresses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode lookup body %q: %v", rec.Body.String(), err)
	}
	if len(envelope.Data.Addresses) != 2 {
		t.Fatalf("expected both addresses, got %d", len(envelope.Data.Addresses))
	}
	for i, address := range envelope.Data.Addresses {
		if _, present := address["divisionNeedsReview"]; present {
			t.Fatalf("address %d must not carry the member: %v", i, address)
		}
		// The members both views do share must survive the split.
		if _, present := address["isDefault"]; !present {
			t.Fatalf("address %d lost isDefault: %v", i, address)
		}
	}
}

// FR-022a: every successful read leaves a trace, not only the first one.
func TestEverySuccessfulOperatorReadIsAudited(t *testing.T) {
	fixture := newAdminFixture(t)

	for i := 1; i <= 3; i++ {
		if rec := fixture.lookup(adminToken); rec.Code != http.StatusOK {
			t.Fatalf("read %d: expected 200, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}

	if got := fixture.audit.countOf(constant.AuditProfileViewedByAdmin); got != 3 {
		t.Fatalf("expected three audit events, got %d", got)
	}
	for _, event := range fixture.audit.all() {
		if event.actorID == nil || *event.actorID != fixture.admin {
			t.Fatalf("expected the administrator as the actor of every read, got %v", event.actorID)
		}
	}
}

// A customer probing the operator route is refused before the use case runs, and
// the refusal reaches the composition's OnDenied hook with the refusing identity,
// which is where the auth module records AUTH_PRIVILEGE_DENIED.
func TestTheOperatorLookupRefusesACustomerAndRecordsTheDenial(t *testing.T) {
	fixture := newAdminFixture(t)

	rec := fixture.lookup(customerToken)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", got)
	}
	if got := fixture.audit.countOf(constant.AuditProfileViewedByAdmin); got != 0 {
		t.Fatalf("a refused read must not be audited as a successful one, got %d events", got)
	}
	denied := fixture.denials.all()
	if len(denied) != 1 {
		t.Fatalf("expected exactly one recorded denial, got %+v", denied)
	}
	if denied[0].role != string(access.RoleCustomer) {
		t.Fatalf("expected the caller's role on the denial, got %q", denied[0].role)
	}
	if denied[0].subject != fixture.customerToken.String() {
		t.Fatalf("expected the caller's subject on the denial, got %q", denied[0].subject)
	}
	if denied[0].path != "/"+fixture.customer.String() {
		t.Fatalf("expected the probed path on the denial, got %q", denied[0].path)
	}
}

// FR-022 and the OpenAPI 404: an unknown account is the module's own
// USER_NOT_FOUND, and it opens nothing, so it is not audited.
func TestTheOperatorLookupAnswersAnUnknownAccountAsNotFound(t *testing.T) {
	fixture := newAdminFixture(t)

	rec := do(fixture.router, http.MethodGet, "/"+uuid.New().String(), adminToken)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeUserNotFound {
		t.Fatalf("expected %s, got %s", constant.CodeUserNotFound, got)
	}
	if got := fixture.audit.countOf(constant.AuditProfileViewedByAdmin); got != 0 {
		t.Fatalf("a read that opened nothing must not be audited, got %d events", got)
	}
}

// A path parameter that is not a UUID is a request error, not a lookup that comes
// back empty, and it never reaches the use case (FR-020).
func TestTheOperatorLookupRefusesANonUUIDIdentifier(t *testing.T) {
	fixture := newAdminFixture(t)

	rec := do(fixture.router, http.MethodGet, "/not-a-uuid", adminToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != string(httpx.CodeValidation) {
		t.Fatalf("expected VALIDATION_ERROR, got %s", got)
	}
	if got := detailField(t, rec); got != fieldUserID {
		t.Fatalf("expected the detail to name %q, got %q", fieldUserID, got)
	}
	if got := fixture.audit.countOf(constant.AuditProfileViewedByAdmin); got != 0 {
		t.Fatalf("a refused request must not be audited, got %d events", got)
	}
}

// SC-010 and FR-022: "cannot modify them". The route is registered for GET only, so
// every write verb on the same path is refused by the router, and nothing about the
// customer's stored profile or addresses moves.
func TestTheOperatorRouteOffersNoWritePath(t *testing.T) {
	fixture := newAdminFixture(t)
	fixture.seedAddresses(t)
	savesBefore := fixture.profiles.saveCount()

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			rec := doJSON(fixture.router, method, "/"+fixture.customer.String(),
				`{"displayName":"Somebody Else","phone":"0900000000"}`, adminToken)

			if rec.Code == http.StatusOK || rec.Code == http.StatusCreated || rec.Code == http.StatusNoContent {
				t.Fatalf("an administrator must not be able to change customer data, got %d (%s)",
					rec.Code, rec.Body.String())
			}
		})
	}

	if got := fixture.profiles.saveCount(); got != savesBefore {
		t.Fatalf("a refused write reached storage, %d -> %d saves", savesBefore, got)
	}
	if got := fixture.addresses.writeCount(); got != 0 {
		t.Fatalf("a refused write reached the addresses, got %d writes", got)
	}
	stored, ok := fixture.profiles.stored(fixture.customer)
	if !ok || stored.DisplayName != "Nguyen Van A" {
		t.Fatalf("the customer's profile changed: %+v", stored)
	}
}
