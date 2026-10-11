package implement

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	paymentmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
)

// This file is the use-case contract of US1 (FR-001 to FR-006): a start creates
// exactly one payment for the order's total and its link; a second start reuses
// the live payment and returns the same link; an order that is not awaiting
// payment is refused; another customer's order answers not-found; a live payment
// whose link can no longer be returned is refused; and a provider failure leaves
// the order — and the store — untouched.

// testInstant is the deterministic instant the fixture stamps payments with.
var testInstant = time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)

// fixedClock is the injected clock, so the assertions do not depend on the wall
// clock.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// fakeUoW runs the function directly, so the use case under test exercises the
// same code path it would inside a real transaction. It tracks whether a
// transaction is currently open, so a test can prove the provider is called
// outside it (research D7).
type fakeUoW struct {
	calls  int
	active bool
}

func (f *fakeUoW) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	f.active = true
	defer func() { f.active = false }()
	return fn(ctx)
}

// fakeProvider is the in-memory provider the use-case tests run against. It
// records the create-link input so a test can prove the amount and description
// the module asked for, and answers a fixed link or failure. It also records
// whether it was called while a transaction was open, so a test can prove the
// provider call sits outside the UnitOfWork (research D7).
type fakeProvider struct {
	uow        *fakeUoW
	link       dto.PaymentLink
	err        error
	creates    int
	calledInTx bool
	lastCreate dto.CreateLinkInput
}

func (f *fakeProvider) CreateLink(_ context.Context, in dto.CreateLinkInput) (dto.PaymentLink, error) {
	f.creates++
	f.lastCreate = in
	if f.uow != nil && f.uow.active {
		f.calledInTx = true
	}
	if f.err != nil {
		return dto.PaymentLink{}, f.err
	}
	if f.link.CheckoutURL == "" {
		f.link = dto.PaymentLink{
			PaymentLinkID: "pl-" + strconv.FormatInt(in.OrderCode, 10),
			CheckoutURL:   "https://pay.example/" + strconv.FormatInt(in.OrderCode, 10),
			QRCode:        "qr-" + strconv.FormatInt(in.OrderCode, 10),
		}
	}
	return f.link, nil
}

func (f *fakeProvider) CancelLink(context.Context, dto.CancelLinkInput) error { return nil }

func (f *fakeProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	return dto.WebhookInput{}, nil
}

// fakeRepo is the in-memory repository. It records the payments and events the
// use case wrote, answers the live-by-order lookup from its fixture, and answers
// the order's reads from its fixture.
type fakeRepo struct {
	appinterface.PaymentRepository
	created    []*model.Payment
	events     []*model.PaymentEvent
	live       *model.Payment
	liveErr    error
	payments   []model.Payment
	byOrderErr error
	createErr  error
}

func (f *fakeRepo) Create(_ context.Context, payment *model.Payment) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, payment)
	return nil
}

func (f *fakeRepo) CreateEvent(_ context.Context, event *model.PaymentEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeRepo) FindLiveByOrder(_ context.Context, _ uuid.UUID) (*model.Payment, error) {
	if f.liveErr != nil {
		return nil, f.liveErr
	}
	if f.live == nil {
		return nil, domainerr.ErrNotFound
	}
	return f.live, nil
}

func (f *fakeRepo) FindByOrder(_ context.Context, _ uuid.UUID) ([]model.Payment, error) {
	if f.byOrderErr != nil {
		return nil, f.byOrderErr
	}
	return f.payments, nil
}

// fakeOrderPayment is the in-memory OrderPayment contract. It answers a fixed
// payment target and records whether MarkPaid was driven (US1 never drives it).
type fakeOrderPayment struct {
	target    contracts.OrderPaymentTarget
	targetErr error
	targets   int
	markPaid  bool
}

func (f *fakeOrderPayment) PaymentTarget(context.Context, uuid.UUID) (contracts.OrderPaymentTarget, error) {
	f.targets++
	return f.target, f.targetErr
}

func (f *fakeOrderPayment) MarkPaid(context.Context, uuid.UUID, string, int64) error {
	f.markPaid = true
	return nil
}

// recordingAuditor records the audit actions the use case emitted.
type recordingAuditor struct{ actions []string }

func (a *recordingAuditor) Record(_ context.Context, action, _ string, _ *uuid.UUID, _, _, _ string, _ map[string]any) {
	a.actions = append(a.actions, action)
}

// harness bundles the fakes the service is built over, so each test sets only what
// it asserts.
type harness struct {
	repo     *fakeRepo
	orders   *fakeOrderPayment
	provider *fakeProvider
	audit    *recordingAuditor
	uow      *fakeUoW
	service  *Service
}

// newHarness builds the use-case service over the fakes, with a fixed clock.
func newHarness() *harness {
	uow := &fakeUoW{}
	h := &harness{
		repo:     &fakeRepo{},
		orders:   &fakeOrderPayment{},
		provider: &fakeProvider{uow: uow},
		audit:    &recordingAuditor{},
		uow:      uow,
	}
	h.service = New(Service{
		Payments:  h.repo,
		Orders:    h.orders,
		Provider:  h.provider,
		Tx:        uow,
		Clock:     fixedClock{now: testInstant},
		Audit:     h.audit,
		Mapper:    paymentmapper.New(),
		ReturnURL: "https://shop.example/payment/return",
		CancelURL: "https://shop.example/payment/cancel",
	})
	return h
}

// awaitingTarget is an awaiting-payment order of the given owner.
func awaitingTarget(owner uuid.UUID) contracts.OrderPaymentTarget {
	return contracts.OrderPaymentTarget{
		UserID:           owner,
		Amount:           120000,
		Currency:         "VND",
		Version:          2,
		AwaitingPayment:  true,
		PaymentExpiresAt: testInstant.Add(time.Hour),
	}
}

// withActor puts the acting account in the context the way presentation does.
func withActor(owner uuid.UUID) context.Context {
	return appinterface.WithActor(context.Background(), appinterface.Actor{ID: owner})
}

// FR-001, FR-002, FR-004: a start creates one payment for the order's exact total
// (no shipping fee), at the order's content version, and returns the provider's
// link; it writes the PAYMENT_CREATED event beside the payment.
func TestStartCreatesOnePaymentForTheOrdersTotal(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	orderID := uuid.New()
	h.orders.target = awaitingTarget(owner)

	view, err := h.service.Start(withActor(owner), dto.StartInput{OrderID: orderID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if len(h.repo.created) != 1 {
		t.Fatalf("Start created %d payments, want exactly 1", len(h.repo.created))
	}
	payment := h.repo.created[0]
	if payment.OrderID != orderID || payment.UserID != owner {
		t.Errorf("payment names order %s / user %s, want %s / %s", payment.OrderID, payment.UserID, orderID, owner)
	}
	if payment.Amount != 120000 || payment.Currency != "VND" {
		t.Errorf("amount = %d %s, want the order total 120000 VND", payment.Amount, payment.Currency)
	}
	if payment.OrderVersion != 2 {
		t.Errorf("order_version = %d, want the order's 2", payment.OrderVersion)
	}
	if payment.Status != constant.StatusPending {
		t.Errorf("status = %s, want PENDING", payment.Status)
	}
	if view.Status != constant.StatusPending || view.CheckoutURL == "" {
		t.Errorf("view = %+v, want PENDING with a checkout URL", view)
	}
	if h.provider.lastCreate.Amount != 120000 || h.provider.lastCreate.OrderCode != payment.OrderCode {
		t.Errorf("provider got %+v, want amount 120000 and the stored order code", h.provider.lastCreate)
	}

	if len(h.repo.events) != 1 || h.repo.events[0].Action != constant.AuditPaymentCreated {
		t.Fatalf("events = %+v, want one PAYMENT_CREATED", h.repo.events)
	}
	if h.repo.events[0].PaymentID != payment.ID {
		t.Errorf("event names payment %s, want %s", h.repo.events[0].PaymentID, payment.ID)
	}

	// research D7: the provider call sits outside the transaction that writes the
	// row, so it never holds a database transaction open across the HTTP call.
	if h.provider.calledInTx {
		t.Error("the provider must be called outside the UnitOfWork (research D7)")
	}
	if h.uow.calls != 1 {
		t.Errorf("the write opened %d transactions, want exactly 1", h.uow.calls)
	}
}

// FR-005, the 2026-10-11 clarification: a second start reuses the live payment and
// returns the same link; no second payment is created.
func TestStartReusesTheLivePayment(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	orderID := uuid.New()
	h.orders.target = awaitingTarget(owner)
	h.repo.live = &model.Payment{
		ID:          uuid.New(),
		OrderID:     orderID,
		UserID:      owner,
		Status:      constant.StatusPending,
		CheckoutURL: "https://pay.example/existing",
	}

	view, err := h.service.Start(withActor(owner), dto.StartInput{OrderID: orderID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if view.CheckoutURL != "https://pay.example/existing" {
		t.Errorf("checkoutUrl = %q, want the live link reused", view.CheckoutURL)
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("a repeat start must create nothing, created %d", len(h.repo.created))
	}
	if h.provider.creates != 0 {
		t.Fatalf("a repeat start must not call the provider, called %d times", h.provider.creates)
	}
}

// FR-003: an order that is not awaiting payment is refused, and nothing is written.
func TestStartRefusesAnOrderNotAwaitingPayment(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	target := awaitingTarget(owner)
	target.AwaitingPayment = false
	h.orders.target = target

	_, err := h.service.Start(withActor(owner), dto.StartInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrOrderNotPayable) {
		t.Fatalf("Start error = %v, want ErrOrderNotPayable", err)
	}
	if len(h.repo.created) != 0 || h.provider.creates != 0 {
		t.Fatal("a refused start must create nothing and call no provider")
	}
}

// FR-003: another customer's order answers the same not-found an unknown order
// does, so the route never confirms it exists.
func TestStartAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	h := newHarness()
	h.orders.target = awaitingTarget(uuid.New()) // owned by someone else

	_, err := h.service.Start(withActor(uuid.New()), dto.StartInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrOrderNotFound) {
		t.Fatalf("Start error = %v, want ErrOrderNotFound", err)
	}
	if len(h.repo.created) != 0 || h.provider.creates != 0 {
		t.Fatal("a foreign order must create nothing and call no provider")
	}
}

// A missing order (the contract's not-found) is translated to the module's own
// not-found, so presentation answers PAYMENT_ORDER_NOT_FOUND.
func TestStartTranslatesAMissingOrder(t *testing.T) {
	h := newHarness()
	h.orders.targetErr = contracts.ErrOrderNotFound

	_, err := h.service.Start(withActor(uuid.New()), dto.StartInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrOrderNotFound) {
		t.Fatalf("Start error = %v, want ErrOrderNotFound", err)
	}
}

// FR-005 edge case: a live payment whose link can no longer be returned is
// refused (PAYMENT_ALREADY_LIVE) rather than a second link being made.
func TestStartRefusesWhenTheLiveLinkCannotBeReturned(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	h.orders.target = awaitingTarget(owner)
	h.repo.live = &model.Payment{
		ID:      uuid.New(),
		OrderID: uuid.New(),
		UserID:  owner,
		Status:  constant.StatusPending,
		// No checkout URL: the provider's link is gone.
	}

	_, err := h.service.Start(withActor(owner), dto.StartInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrAlreadyLive) {
		t.Fatalf("Start error = %v, want ErrAlreadyLive", err)
	}
	if len(h.repo.created) != 0 || h.provider.creates != 0 {
		t.Fatal("a refused start must create nothing and call no provider")
	}
}

// FR-006: a provider failure refuses the start and leaves the order — and the
// store — untouched.
func TestStartRefusesWhenTheProviderFails(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	h.orders.target = awaitingTarget(owner)
	h.provider.err = errors.New("provider unreachable")

	_, err := h.service.Start(withActor(owner), dto.StartInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrProviderFailure) {
		t.Fatalf("Start error = %v, want ErrProviderFailure", err)
	}
	if len(h.repo.created) != 0 || len(h.repo.events) != 0 {
		t.Fatal("a provider failure must write nothing")
	}
}

// A use case reached with no session behind the call is a programming error, not a
// client-facing refusal.
func TestStartWithoutASessionFails(t *testing.T) {
	h := newHarness()
	if _, err := h.service.Start(context.Background(), dto.StartInput{OrderID: uuid.New()}); !errors.Is(err, errNoActor) {
		t.Fatalf("Start without a session = %v, want errNoActor", err)
	}
}

// FR-017: Read returns the caller's most recent payment for the order, in any
// state — a terminal payment is still the order's payment state and must remain
// readable.
func TestReadReturnsTheLatestPaymentEvenWhenTerminal(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	orderID := uuid.New()
	h.orders.target = awaitingTarget(owner)
	latest := model.Payment{
		ID:          uuid.New(),
		OrderID:     orderID,
		UserID:      owner,
		Status:      constant.StatusPaid,
		CheckoutURL: "https://pay.example/paid",
	}
	older := model.Payment{ID: uuid.New(), OrderID: orderID, UserID: owner, Status: constant.StatusCancelled}
	// FindByOrder answers newest first; the first is the most recent.
	h.repo.payments = []model.Payment{latest, older}

	view, err := h.service.Read(withActor(owner), dto.ReadInput{OrderID: orderID})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.ID != latest.ID || view.Status != constant.StatusPaid {
		t.Fatalf("Read = %+v, want the latest payment %s (PAID)", view, latest.ID)
	}
}

// FR-017: an order of the caller's that has no payment at all answers
// PAYMENT_NOT_FOUND.
func TestReadAnswersNotFoundWhenTheOrderHasNoPayment(t *testing.T) {
	h := newHarness()
	owner := uuid.New()
	h.orders.target = awaitingTarget(owner)

	_, err := h.service.Read(withActor(owner), dto.ReadInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("Read error = %v, want ErrNotFound", err)
	}
}

// FR-017: another customer cannot read the caller's payment.
func TestReadAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	h := newHarness()
	h.orders.target = awaitingTarget(uuid.New())
	h.repo.payments = []model.Payment{{ID: uuid.New(), Status: constant.StatusPaid}}

	_, err := h.service.Read(withActor(uuid.New()), dto.ReadInput{OrderID: uuid.New()})
	if !errors.Is(err, domainerr.ErrOrderNotFound) {
		t.Fatalf("Read error = %v, want ErrOrderNotFound", err)
	}
}
