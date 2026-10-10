package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// This file is US4: the operator's order desk. An administrator lists every order
// and reads one in full; the ship and complete moves drive the US2 transitions
// (the domain's allowed edges) and record each act in the audit trail, naming the
// order and the administrator (FR-021 to FR-023). The role guard is presentation's
// — every route requires the administrator role — while this layer names the actor
// the session filled, never an input.

// targetTypeOrder is the audit_logs target type of the order events. The target of
// an administrator mutation is the order, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// order without scanning the actor's other events (FR-023, Constitution VI).
const targetTypeOrder = "order"

// ListAll returns one page of every order, newest first, with its owner, state and
// total (FR-021). Unlike the customer's list it applies no owner filter, because
// the caller reached it behind the administrator role guard.
func (s *Service) ListAll(ctx context.Context, in dto.ListInput) (dto.AdminOrderPage, error) {
	page, size := orderListWindow(in.Page, in.PageSize)
	summaries, total, err := s.Orders.ListAll(ctx, page, size, in.Status, in.Sort)
	if err != nil {
		return dto.AdminOrderPage{}, err
	}
	return dto.AdminOrderPage{
		Orders:   s.Mapper.AdminSummaries(summaries),
		Page:     page,
		PageSize: size,
		Total:    total,
	}, nil
}

// GetByIDAdmin reads any order in full, including its owner and its lines; an
// unknown identifier answers the module's not-found (FR-021).
func (s *Service) GetByIDAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error) {
	order, err := s.Orders.FindByID(ctx, in.OrderID)
	if err != nil {
		return dto.AdminOrderView{}, err
	}
	return s.Mapper.AdminOrder(*order), nil
}

// ShipByAdmin moves a paid order to shipped and records the act (FR-022, FR-023).
func (s *Service) ShipByAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error) {
	return s.adminMove(ctx, in, constant.AuditOrderShipped, (*model.Order).Ship)
}

// CompleteByAdmin moves a shipped order to completed and records the act (FR-022,
// FR-023).
func (s *Service) CompleteByAdmin(ctx context.Context, in dto.OrderRefInput) (dto.AdminOrderView, error) {
	return s.adminMove(ctx, in, constant.AuditOrderCompleted, (*model.Order).Complete)
}

// adminMove locks one order, applies the administrator's allowed edge — the same
// domain transition US2 exposes as Ship and Complete — persists the new state and
// records the act, all in one UnitOfWork. A move the current state does not allow
// is refused by the domain with the current state named and leaves the order
// exactly as it was, writing no state and no audit entry (FR-010, FR-011, FR-022,
// FR-023).
//
// The acting administrator is the session's, never an input; a call with no
// session behind it is a programming error rather than a client-facing refusal,
// because the transport's role guard answers FORBIDDEN first (FR-020, FR-023).
func (s *Service) adminMove(ctx context.Context, in dto.OrderRefInput, action string, move func(*model.Order) error) (dto.AdminOrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.AdminOrderView{}, err
	}

	var view dto.AdminOrderView
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.Orders.LockByID(txCtx, in.OrderID)
		if err != nil {
			return err
		}
		if err := move(order); err != nil {
			return err
		}
		if err := s.Orders.UpdateStatus(txCtx, order.ID, order.Status, s.now()); err != nil {
			return err
		}
		// The audit entry is emitted from inside the transaction, so a successful
		// move and its record are produced by the same unit of work; the shared
		// writer queues it and never fails the business operation (FR-023,
		// Constitution VI).
		s.record(txCtx, action, actor, order.ID)
		view = s.Mapper.AdminOrder(*order)
		return nil
	}); err != nil {
		return dto.AdminOrderView{}, err
	}
	return view, nil
}

// record emits one administrative audit event naming the acting administrator and
// the order (FR-023, Constitution VI). Emission never fails the business
// operation: the shared writer queues, retries and reports a dropped event through
// the logger. An absent auditor is tolerated so a read-only construction of the
// service cannot panic on a mutation it never serves.
func (s *Service) record(ctx context.Context, action string, actor appinterface.Actor, orderID uuid.UUID) {
	if s.Audit == nil {
		return
	}
	actorID := actor.ID
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		&actorID, string(actor.Role), targetTypeOrder, orderID.String(), nil)
}
