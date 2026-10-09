package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of US5: an administrator hands a paid order
// to another existing account, named by email. The owner changes and nothing
// else does — the lines, the state and the total are untouched and no stock
// moves — the act is audited, an order that is not paid is refused, and an email
// no account carries is refused (FR-024, research D8, D12). It runs the real
// transfer over in-memory fakes of the repository, the account lookup and the
// inventory reservation contract, so the answers asserted are the service's own.

// ownerWrite records one owner change the transfer made.
type ownerWrite struct {
	id      uuid.UUID
	ownerID uuid.UUID
}

// transferOrders is the in-memory OrderRepository the transfer uses. It embeds
// the admin store so the row lock and the reads behave as they do for the desk,
// and records the owner write a transfer makes (FR-024). Embedding the interface
// through the admin store makes any other method the use case drifts into panic
// loudly rather than silently pass.
type transferOrders struct {
	*adminOrders
	ownerWrites []ownerWrite
}

func (r *transferOrders) UpdateOwner(_ context.Context, id, ownerID uuid.UUID, _ time.Time) error {
	r.ownerWrites = append(r.ownerWrites, ownerWrite{id: id, ownerID: ownerID})
	if order, ok := r.byID[id]; ok {
		order.UserID = ownerID
	}
	return nil
}

// fakeAccountLookup answers the AccountLookup contract from a fixed set of
// accounts. An email it does not carry answers the contract's sentinel, exactly
// as the auth module's adapter does (research D8).
type fakeAccountLookup struct {
	byEmail map[string]uuid.UUID
	err     error
}

func (f *fakeAccountLookup) UserIDByEmail(_ context.Context, email string) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	if id, ok := f.byEmail[email]; ok {
		return id, nil
	}
	return uuid.Nil, contracts.ErrAccountNotFound
}

// transferFixture is the real transfer use case over in-memory fakes.
type transferFixture struct {
	svc          *Service
	orders       *transferOrders
	accounts     *fakeAccountLookup
	audit        *recordingAuditor
	reservations *fakeReservation
}

// newTransferFixture seeds the repository with the orders in the order supplied
// and builds the use cases over the shared in-memory UnitOfWork, clock, account
// lookup, reservation contract and a recording auditor.
func newTransferFixture(orders ...*model.Order) *transferFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	store := &transferOrders{adminOrders: &adminOrders{byID: byID}}
	accounts := &fakeAccountLookup{byEmail: map[string]uuid.UUID{}}
	recorder := &recordingAuditor{}
	reservations := &fakeReservation{}
	svc := New(Service{
		Orders:       store,
		Accounts:     accounts,
		Reservations: reservations,
		Tx:           passthroughTx{},
		Clock:        memoryClock{},
		Mapper:       mapper.New(),
		Audit:        recorder,
	})
	return &transferFixture{svc: svc, orders: store, accounts: accounts, audit: recorder, reservations: reservations}
}

// paidOrder builds a paid order over the given lines, using the domain
// constructor so its identifier, line links and total are the ones a checkout
// would produce.
func paidOrder(owner uuid.UUID, lines ...model.OrderLine) *model.Order {
	order := orderAt(owner, fixedNow, lines...)
	order.Status = constant.StatusPaid
	return order
}

// FR-024, research D12: a transfer changes only the owner. The lines, the state
// and the total are unchanged, no stock moves, and the act is audited naming the
// order and the administrator.
func TestTransferChangesOnlyTheOwner(t *testing.T) {
	admin, owner, recipient := uuid.New(), uuid.New(), uuid.New()
	product := uuid.New()
	order := paidOrder(owner, line(product, 120000, 2))
	f := newTransferFixture(order)
	f.accounts.byEmail["recipient@example.com"] = recipient

	beforeTotal := order.Total
	beforeLines := len(order.Lines)

	view, err := f.svc.Transfer(adminContext(admin), appdto.TransferInput{OrderID: order.ID, Email: "recipient@example.com"})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if view.UserID != recipient || view.ID != order.ID {
		t.Fatalf("the order must answer with the recipient as owner, got %+v", view)
	}
	if order.UserID != recipient {
		t.Fatalf("the persisted owner must be the recipient, got %s", order.UserID)
	}

	// The state, the lines and the total are untouched (FR-024).
	if order.Status != constant.StatusPaid {
		t.Fatalf("a transfer must not change the state, got %s", order.Status)
	}
	if len(order.Lines) != beforeLines || order.Total != beforeTotal {
		t.Fatalf("a transfer must not change the lines or the total, got %d lines / %+v", len(order.Lines), order.Total)
	}

	if len(f.orders.ownerWrites) != 1 || f.orders.ownerWrites[0].id != order.ID || f.orders.ownerWrites[0].ownerID != recipient {
		t.Fatalf("the transfer must write exactly the new owner, got %+v", f.orders.ownerWrites)
	}

	// No stock movement: the transfer never reaches module 05 (FR-024).
	if len(f.reservations.calls) != 0 || len(f.reservations.sales) != 0 || len(f.reservations.releases) != 0 {
		t.Fatalf("a transfer must not move stock, got %+v / %+v / %+v", f.reservations.calls, f.reservations.sales, f.reservations.releases)
	}

	if len(f.audit.events) != 1 {
		t.Fatalf("the transfer must record one audit entry, got %+v", f.audit.events)
	}
	assertAudit(t, f.audit.events[0], constant.AuditOrderTransferred, admin, order.ID)
}

// FR-024: an order that is not paid is not transferable; nothing changes.
func TestTransferRefusesAnUnpaidOrder(t *testing.T) {
	admin, owner, recipient := uuid.New(), uuid.New(), uuid.New()
	order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
	f := newTransferFixture(order)
	f.accounts.byEmail["recipient@example.com"] = recipient

	_, err := f.svc.Transfer(adminContext(admin), appdto.TransferInput{OrderID: order.ID, Email: "recipient@example.com"})
	if !errors.Is(err, domainerr.ErrNotTransferable) {
		t.Fatalf("expected ErrNotTransferable, got %v", err)
	}
	if order.UserID != owner {
		t.Fatalf("a refused transfer must leave the owner untouched, got %s", order.UserID)
	}
	if len(f.orders.ownerWrites) != 0 {
		t.Fatalf("a refused transfer must write nothing, got %+v", f.orders.ownerWrites)
	}
	if len(f.audit.events) != 0 {
		t.Fatalf("a refused transfer must audit nothing, got %+v", f.audit.events)
	}
}

// FR-024, research D8: an email no account carries is refused; nothing changes.
func TestTransferRefusesAnUnknownEmail(t *testing.T) {
	admin, owner := uuid.New(), uuid.New()
	order := paidOrder(owner, line(uuid.New(), 1000, 1))
	f := newTransferFixture(order)

	_, err := f.svc.Transfer(adminContext(admin), appdto.TransferInput{OrderID: order.ID, Email: "nobody@example.com"})
	if !errors.Is(err, domainerr.ErrTransferTargetNotFound) {
		t.Fatalf("expected ErrTransferTargetNotFound, got %v", err)
	}
	if order.UserID != owner {
		t.Fatalf("a refused transfer must leave the owner untouched, got %s", order.UserID)
	}
	if len(f.orders.ownerWrites) != 0 || len(f.audit.events) != 0 {
		t.Fatalf("a refused transfer must write nothing and audit nothing, got %+v / %+v", f.orders.ownerWrites, f.audit.events)
	}
}

// FR-021, FR-024: an unknown order answers the module's not-found.
func TestTransferAnswersNotFoundForAnUnknownOrder(t *testing.T) {
	admin := uuid.New()
	f := newTransferFixture()
	f.accounts.byEmail["recipient@example.com"] = uuid.New()

	_, err := f.svc.Transfer(adminContext(admin), appdto.TransferInput{OrderID: uuid.New(), Email: "recipient@example.com"})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// FR-023: a transfer reached without a session is a programming error, not a
// client-facing refusal — the role guard at the transport answers FORBIDDEN.
func TestTransferRequiresASession(t *testing.T) {
	order := paidOrder(uuid.New(), line(uuid.New(), 1000, 1))
	f := newTransferFixture(order)
	f.accounts.byEmail["recipient@example.com"] = uuid.New()

	if _, err := f.svc.Transfer(context.Background(), appdto.TransferInput{OrderID: order.ID, Email: "recipient@example.com"}); !errors.Is(err, errNoActor) {
		t.Fatalf("Transfer without a session must fail with errNoActor, got %v", err)
	}
}
