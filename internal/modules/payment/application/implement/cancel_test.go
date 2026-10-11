package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	paymentmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
)

// This file is the use-case contract of US4 (FR-015, FR-016, research D9): a
// cancelled order's live payment is cancelled at the provider and marked
// CANCELLED with the PAYMENT_CANCELLED event; a provider that refuses the cancel
// is only logged, leaving the payment live for reconciliation and never failing;
// and an order with no live payment is a no-op.

// cancelRepo is the in-memory repository the cancellation use case runs against.
// It answers the live-by-order lookup and the locked reload from its fixture, and
// records the writes the use case made. Embedding the interface makes any method
// the use case drifts into panic loudly rather than silently pass.
type cancelRepo struct {
	appinterface.PaymentRepository
	live      *model.Payment
	liveErr   error
	locked    *model.Payment
	lockedErr error

	statuses  []constant.Status
	events    []*model.PaymentEvent
	lockCalls int
}

func (r *cancelRepo) FindLiveByOrder(_ context.Context, _ uuid.UUID) (*model.Payment, error) {
	if r.liveErr != nil {
		return nil, r.liveErr
	}
	if r.live == nil {
		return nil, domainerr.ErrNotFound
	}
	return r.live, nil
}

func (r *cancelRepo) LockByID(_ context.Context, _ uuid.UUID) (*model.Payment, error) {
	r.lockCalls++
	if r.lockedErr != nil {
		return nil, r.lockedErr
	}
	return r.locked, nil
}

func (r *cancelRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status constant.Status, _ time.Time) error {
	r.statuses = append(r.statuses, status)
	return nil
}

func (r *cancelRepo) CreateEvent(_ context.Context, event *model.PaymentEvent) error {
	r.events = append(r.events, event)
	return nil
}

// cancelProvider is the in-memory provider the cancellation tests run against. It
// records the cancel input so a test can prove which link the module asked to
// cancel, can refuse the cancel, and records whether it was called while a
// transaction was open, so a test can prove the provider call sits outside the
// UnitOfWork (research D9).
type cancelProvider struct {
	uow        *fakeUoW
	cancelErr  error
	cancels    int
	calledInTx bool
	lastCancel dto.CancelLinkInput
}

func (p *cancelProvider) CreateLink(context.Context, dto.CreateLinkInput) (dto.PaymentLink, error) {
	return dto.PaymentLink{}, nil
}

func (p *cancelProvider) CancelLink(_ context.Context, in dto.CancelLinkInput) error {
	p.cancels++
	p.lastCancel = in
	if p.uow != nil && p.uow.active {
		p.calledInTx = true
	}
	return p.cancelErr
}

func (p *cancelProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	return dto.WebhookInput{}, nil
}

// cancelHarness bundles the fakes the cancellation service is built over.
type cancelHarness struct {
	repo     *cancelRepo
	provider *cancelProvider
	audit    *recordingAuditor
	uow      *fakeUoW
	service  *Service
}

func newCancelHarness() *cancelHarness {
	uow := &fakeUoW{}
	h := &cancelHarness{
		repo:     &cancelRepo{},
		provider: &cancelProvider{uow: uow},
		audit:    &recordingAuditor{},
		uow:      uow,
	}
	h.service = New(Service{
		Payments: h.repo,
		Provider: h.provider,
		Tx:       uow,
		Clock:    fixedClock{now: testInstant},
		Audit:    h.audit,
		Mapper:   paymentmapper.New(),
	})
	return h
}

// livePayment is the fixture: a live payment for the given order, carrying a
// known order code and link handle.
func livePayment(orderID uuid.UUID) *model.Payment {
	return &model.Payment{
		ID:            uuid.New(),
		OrderID:       orderID,
		UserID:        uuid.New(),
		OrderCode:     555,
		Amount:        120000,
		Currency:      "VND",
		OrderVersion:  2,
		Status:        constant.StatusPending,
		Provider:      constant.ProviderPayOS,
		PaymentLinkID: "pl-555",
		CheckoutURL:   "https://pay.example/555",
	}
}

// FR-015, research D9: cancelling an order cancels its live payment — the provider
// is asked to cancel the order's link, the payment is marked CANCELLED and the
// PAYMENT_CANCELLED event is recorded, in one transaction; the provider call sits
// outside it.
func TestCancelForOrderCancelsALivePayment(t *testing.T) {
	h := newCancelHarness()
	orderID := uuid.New()
	live := livePayment(orderID)
	h.repo.live = live
	h.repo.locked = live

	h.service.CancelForOrder(context.Background(), orderID)

	if h.provider.cancels != 1 {
		t.Fatalf("the provider cancel was called %d times, want exactly 1", h.provider.cancels)
	}
	if h.provider.lastCancel.OrderCode != 555 || h.provider.lastCancel.PaymentLinkID != "pl-555" {
		t.Errorf("provider cancel = %+v, want the stored order code and link handle", h.provider.lastCancel)
	}
	// research D9: the provider call must not sit inside a database transaction.
	if h.provider.calledInTx {
		t.Error("the provider cancel must be called outside the UnitOfWork (research D9)")
	}
	if len(h.repo.statuses) != 1 || h.repo.statuses[0] != constant.StatusCancelled {
		t.Fatalf("status writes = %v, want exactly one CANCELLED", h.repo.statuses)
	}
	if live.Status != constant.StatusCancelled {
		t.Fatalf("payment status = %s, want CANCELLED", live.Status)
	}
	if len(h.repo.events) != 1 || h.repo.events[0].Action != constant.AuditPaymentCancelled {
		t.Fatalf("events = %+v, want one PAYMENT_CANCELLED", h.repo.events)
	}
	if h.repo.events[0].PaymentID != live.ID {
		t.Errorf("event names payment %s, want %s", h.repo.events[0].PaymentID, live.ID)
	}
	// The port carries no acting account, so the event cannot name a person.
	if h.repo.events[0].ActorID != nil {
		t.Errorf("event actor = %v, want nil (no account crosses the port)", h.repo.events[0].ActorID)
	}
	if len(h.audit.actions) != 1 || h.audit.actions[0] != constant.AuditPaymentCancelled {
		t.Errorf("audit actions = %v, want one PAYMENT_CANCELLED", h.audit.actions)
	}
	if h.uow.calls != 1 {
		t.Errorf("the cancel opened %d transactions, want exactly 1", h.uow.calls)
	}
}

// FR-016, research D9: a provider that refuses the cancel is logged and the
// payment is left live for reconciliation; the call never fails and writes
// nothing.
func TestCancelForOrderLogsAProviderFailureAndLeavesThePaymentLive(t *testing.T) {
	h := newCancelHarness()
	orderID := uuid.New()
	live := livePayment(orderID)
	h.repo.live = live
	h.repo.locked = live
	h.provider.cancelErr = errors.New("provider refused the cancel")

	h.service.CancelForOrder(context.Background(), orderID)

	if h.provider.cancels != 1 {
		t.Fatalf("the provider cancel was called %d times, want exactly 1", h.provider.cancels)
	}
	if live.Status != constant.StatusPending {
		t.Fatalf("payment status = %s, want PENDING — left live for reconciliation", live.Status)
	}
	if len(h.repo.statuses) != 0 || len(h.repo.events) != 0 {
		t.Fatalf("a provider failure must write nothing, statuses=%v events=%v", h.repo.statuses, h.repo.events)
	}
	if h.uow.calls != 0 {
		t.Errorf("a provider failure opened %d transactions, want none", h.uow.calls)
	}
	if h.repo.lockCalls != 0 {
		t.Errorf("a provider failure locked the payment %d times, want none", h.repo.lockCalls)
	}
}

// FR-015: an order with no live payment — never started, or already terminal — is
// a no-op: the provider is not called and nothing is written.
func TestCancelForOrderIsANoOpWhenTheOrderHasNoLivePayment(t *testing.T) {
	h := newCancelHarness()
	h.repo.liveErr = domainerr.ErrNotFound

	h.service.CancelForOrder(context.Background(), uuid.New())

	if h.provider.cancels != 0 {
		t.Fatalf("the provider cancel was called %d times for an order with no live payment, want none", h.provider.cancels)
	}
	if h.uow.calls != 0 || len(h.repo.statuses) != 0 || len(h.repo.events) != 0 {
		t.Fatalf("an order with no live payment must write nothing, calls=%d", h.uow.calls)
	}
}

// FR-015: a live row the lock finds already terminal (a concurrent webhook
// applied it, or a concurrent cancel moved it) is a no-op — the payment is not
// moved twice.
func TestCancelForOrderDoesNotMoveAnAlreadyTerminalPayment(t *testing.T) {
	h := newCancelHarness()
	orderID := uuid.New()
	live := livePayment(orderID)
	h.repo.live = live
	// Between the live read and the locked reload the payment was applied.
	paid := livePayment(orderID)
	paid.Status = constant.StatusPaid
	h.repo.locked = paid

	h.service.CancelForOrder(context.Background(), orderID)

	if h.provider.cancels != 1 {
		t.Fatalf("the provider cancel was called %d times, want exactly 1", h.provider.cancels)
	}
	if len(h.repo.statuses) != 0 || len(h.repo.events) != 0 {
		t.Fatalf("an already-terminal payment must not be moved, statuses=%v events=%v", h.repo.statuses, h.repo.events)
	}
}
