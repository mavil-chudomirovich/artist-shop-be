package implement

import (
	"context"
	"errors"
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

// This file is the use-case contract of US2 (FR-007 to FR-011): a verified
// confirmation that matches the stored amount is applied exactly once — the
// order's version-aware paid transition is driven with the stored version, the
// payment becomes PAID with the provider reference and the PAYMENT_APPLIED event
// is recorded, all in one transaction. A replay is a no-op success; an unverified
// body is refused; a wrong amount records UNMATCHED and does not apply; and an
// unknown order code is refused with nothing recorded (only a warning).

// webhookRepo is the in-memory repository the webhook use case runs against. It
// answers the order-code lookup and the locked reload from its fixture, and
// records the writes the use case made.
type webhookRepo struct {
	appinterface.PaymentRepository
	byCode    *model.Payment
	byCodeErr error
	locked    *model.Payment
	lockedErr error

	applyPaid      []string
	unmatched      []string
	events         []*model.PaymentEvent
	lockCalls      int
	findCodeCalls  int
	applyPaidCalls int
}

func (r *webhookRepo) FindByOrderCode(_ context.Context, _ int64) (*model.Payment, error) {
	r.findCodeCalls++
	if r.byCodeErr != nil {
		return nil, r.byCodeErr
	}
	return r.byCode, nil
}

func (r *webhookRepo) LockByID(_ context.Context, _ uuid.UUID) (*model.Payment, error) {
	r.lockCalls++
	if r.lockedErr != nil {
		return nil, r.lockedErr
	}
	return r.locked, nil
}

func (r *webhookRepo) ApplyPaid(_ context.Context, _ uuid.UUID, reference string, _ time.Time) error {
	r.applyPaidCalls++
	r.applyPaid = append(r.applyPaid, reference)
	return nil
}

func (r *webhookRepo) RecordUnmatched(_ context.Context, _ uuid.UUID, reason string, _ time.Time) error {
	r.unmatched = append(r.unmatched, reason)
	return nil
}

func (r *webhookRepo) CreateEvent(_ context.Context, event *model.PaymentEvent) error {
	r.events = append(r.events, event)
	return nil
}

// webhookProvider is the fake provider: it answers the verification with a fixed
// verified input or error, so the use case is exercised without a real signature.
type webhookProvider struct {
	input dto.WebhookInput
	err   error
	calls int
}

func (p *webhookProvider) CreateLink(context.Context, dto.CreateLinkInput) (dto.PaymentLink, error) {
	return dto.PaymentLink{}, nil
}

func (p *webhookProvider) CancelLink(context.Context, dto.CancelLinkInput) error { return nil }

func (p *webhookProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	p.calls++
	return p.input, p.err
}

// webhookOrders is the fake OrderPayment contract: it records the paid transition
// it was asked to drive and can refuse it.
type webhookOrders struct {
	markErr     error
	markCalls   int
	lastSource  string
	lastVersion int64
	lastOrderID uuid.UUID
}

func (o *webhookOrders) PaymentTarget(context.Context, uuid.UUID) (contracts.OrderPaymentTarget, error) {
	return contracts.OrderPaymentTarget{}, nil
}

func (o *webhookOrders) MarkPaid(_ context.Context, orderID uuid.UUID, source string, version int64) error {
	o.markCalls++
	o.lastSource = source
	o.lastVersion = version
	o.lastOrderID = orderID
	return o.markErr
}

// webhookHarness bundles the fakes the service is built over for US2.
type webhookHarness struct {
	repo     *webhookRepo
	orders   *webhookOrders
	provider *webhookProvider
	audit    *recordingAuditor
	uow      *fakeUoW
	service  *Service
}

func newWebhookHarness() *webhookHarness {
	uow := &fakeUoW{}
	h := &webhookHarness{
		repo:     &webhookRepo{},
		orders:   &webhookOrders{},
		provider: &webhookProvider{},
		audit:    &recordingAuditor{},
		uow:      uow,
	}
	h.service = New(Service{
		Payments: h.repo,
		Orders:   h.orders,
		Provider: h.provider,
		Tx:       uow,
		Clock:    fixedClock{now: testInstant},
		Audit:    h.audit,
		Mapper:   paymentmapper.New(),
	})
	return h
}

// pendingPayment is the fixture: a live payment for the given order/version/amount
// carrying a known order code.
func pendingPayment(orderID uuid.UUID, orderCode, amount, version int64) *model.Payment {
	return &model.Payment{
		ID:           uuid.New(),
		OrderID:      orderID,
		UserID:       uuid.New(),
		OrderCode:    orderCode,
		Amount:       amount,
		Currency:     "VND",
		OrderVersion: version,
		Status:       constant.StatusPending,
		Provider:     constant.ProviderPayOS,
	}
}

// FR-007, FR-009, FR-010: a verified confirmation whose amount matches applies
// once — the order's version-aware MarkPaid is driven with the stored version and
// the payment's own reference, the payment becomes PAID and the PAYMENT_APPLIED
// event is recorded, all in one transaction.
func TestHandleWebhookAppliesAVerifiedConfirmationOnce(t *testing.T) {
	h := newWebhookHarness()
	orderID := uuid.New()
	const orderCode = 424242
	h.repo.byCode = pendingPayment(orderID, orderCode, 120000, 2)
	h.repo.locked = h.repo.byCode
	h.provider.input = dto.WebhookInput{OrderCode: orderCode, Amount: 120000, Reference: "TF-2302", Currency: "VND"}

	if err := h.service.HandleWebhook(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	if h.orders.markCalls != 1 {
		t.Fatalf("MarkPaid called %d times, want exactly 1", h.orders.markCalls)
	}
	if h.orders.lastOrderID != orderID {
		t.Errorf("MarkPaid order = %s, want %s", h.orders.lastOrderID, orderID)
	}
	if want := "payos:424242"; h.orders.lastSource != want {
		t.Errorf("source reference = %q, want the payment's own stable reference %q", h.orders.lastSource, want)
	}
	if h.orders.lastVersion != 2 {
		t.Errorf("expected version = %d, want the stored 2", h.orders.lastVersion)
	}
	if h.repo.applyPaidCalls != 1 || h.repo.applyPaid[0] != "TF-2302" {
		t.Errorf("ApplyPaid = %v, want one write with the provider reference TF-2302", h.repo.applyPaid)
	}
	if len(h.repo.events) != 1 || h.repo.events[0].Action != constant.AuditPaymentApplied {
		t.Fatalf("events = %+v, want one PAYMENT_APPLIED", h.repo.events)
	}
	if h.repo.events[0].ActorID != nil {
		t.Errorf("event actor = %v, want nil (the provider is not a person)", h.repo.events[0].ActorID)
	}
	if len(h.audit.actions) != 1 || h.audit.actions[0] != constant.AuditPaymentApplied {
		t.Errorf("audit actions = %v, want one PAYMENT_APPLIED", h.audit.actions)
	}
	if h.uow.calls != 1 {
		t.Errorf("the apply opened %d transactions, want exactly 1", h.uow.calls)
	}
	if h.provider.calls != 1 {
		t.Errorf("the signature was verified %d times, want exactly 1", h.provider.calls)
	}
	if h.repo.lockCalls != 1 {
		t.Errorf("the payment was locked %d times, want exactly 1", h.repo.lockCalls)
	}
}

// FR-009: a replay of an already-applied confirmation is a no-op success — nothing
// is applied a second time (the order's MarkPaid is never re-driven).
func TestHandleWebhookReplayIsANoOpSuccess(t *testing.T) {
	h := newWebhookHarness()
	paid := pendingPayment(uuid.New(), 77, 120000, 2)
	paid.Status = constant.StatusPaid
	h.repo.byCode = paid
	h.repo.locked = paid
	h.provider.input = dto.WebhookInput{OrderCode: 77, Amount: 120000, Reference: "TF-2302"}

	if err := h.service.HandleWebhook(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("a replay must be a no-op success, got %v", err)
	}
	if h.orders.markCalls != 0 || h.repo.applyPaidCalls != 0 || len(h.repo.events) != 0 {
		t.Fatal("a replay must apply nothing")
	}
	if h.uow.calls != 0 {
		t.Errorf("a replay opened %d transactions, want none", h.uow.calls)
	}
}

// FR-019: a body whose signature does not verify is refused and nothing is
// applied.
func TestHandleWebhookRefusesAnUnverifiedBody(t *testing.T) {
	h := newWebhookHarness()
	h.provider.err = domainerr.ErrWebhookInvalidSignature

	err := h.service.HandleWebhook(context.Background(), []byte(`{}`))
	if !errors.Is(err, domainerr.ErrWebhookInvalidSignature) {
		t.Fatalf("HandleWebhook error = %v, want ErrWebhookInvalidSignature", err)
	}
	if h.repo.findCodeCalls != 0 {
		t.Error("an unverified body must not be looked up")
	}
	if h.orders.markCalls != 0 || h.repo.applyPaidCalls != 0 || len(h.repo.events) != 0 {
		t.Fatal("an unverified body must apply nothing")
	}
}

// A body the provider could not read at all is MALFORMED_REQUEST at the transport,
// carried here as the module's own sentinel (contracts/error-codes.md).
func TestHandleWebhookTranslatesAMalformedBody(t *testing.T) {
	h := newWebhookHarness()
	h.provider.err = errors.New("invalid character 'o' looking for beginning of value")

	err := h.service.HandleWebhook(context.Background(), []byte(`not-json`))
	if !errors.Is(err, domainerr.ErrMalformedWebhook) {
		t.Fatalf("HandleWebhook error = %v, want ErrMalformedWebhook", err)
	}
}

// FR-008: a verified confirmation whose amount differs from the stored amount is
// recorded UNMATCHED and never applied; the order is untouched.
func TestHandleWebhookRecordsAMismatchedAmount(t *testing.T) {
	h := newWebhookHarness()
	orderID := uuid.New()
	h.repo.byCode = pendingPayment(orderID, 900, 120000, 2)
	h.repo.locked = h.repo.byCode
	h.provider.input = dto.WebhookInput{OrderCode: 900, Amount: 99000, Reference: "TF-999"}

	if err := h.service.HandleWebhook(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("an amount mismatch is handled, not an error: %v", err)
	}
	if h.orders.markCalls != 0 {
		t.Fatal("a mismatched amount must not be applied")
	}
	if h.repo.applyPaidCalls != 0 {
		t.Fatal("a mismatched amount must not mark the payment PAID")
	}
	if len(h.repo.unmatched) != 1 || h.repo.unmatched[0] == "" {
		t.Fatalf("unmatched reason = %v, want one non-empty reason", h.repo.unmatched)
	}
	if len(h.repo.events) != 1 || h.repo.events[0].Action != constant.AuditPaymentUnmatched {
		t.Fatalf("events = %+v, want one PAYMENT_UNMATCHED", h.repo.events)
	}
}

// FR-022, research D7: a webhook for an order code no payment carries is refused
// and nothing is recorded — there is no payment to attach a record to (only a
// warning is logged).
func TestHandleWebhookRefusesAnUnknownOrderCode(t *testing.T) {
	h := newWebhookHarness()
	h.repo.byCodeErr = domainerr.ErrNotFound
	h.provider.input = dto.WebhookInput{OrderCode: 123456, Amount: 120000}

	err := h.service.HandleWebhook(context.Background(), []byte(`{}`))
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("HandleWebhook error = %v, want ErrNotFound", err)
	}
	if h.repo.applyPaidCalls != 0 || len(h.repo.unmatched) != 0 || len(h.repo.events) != 0 {
		t.Fatal("an unknown order code must record nothing")
	}
	if h.uow.calls != 0 {
		t.Errorf("an unknown order code opened %d transactions, want none", h.uow.calls)
	}
}
