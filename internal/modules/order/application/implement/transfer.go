package implement

import (
	"context"
	"errors"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file is US5: handing a paid order to another account. A transfer is not a
// state change — it changes only who owns the order, resolved through module 01
// by email — and it touches no stock: the lines, the state and the totals are
// exactly as they were (FR-024, research D8, D12).

// Transfer hands a paid order to another existing account, named by email. It
// locks the order, refuses one that is not paid, resolves the recipient through
// AccountLookup, changes only the owner and records the act — all in one
// UnitOfWork, so the owner change and its audit entry commit together or not at
// all (FR-024, research D12).
//
// The recipient is resolved at the source of truth (module 01) rather than by a
// foreign key, which is what makes "transfer to a non-existent account" refusable
// without an owner reference to the account table (research D12). The acting
// administrator is the session's, never an input; a call with no session behind
// it is a programming error rather than a client-facing refusal, because the
// transport's role guard answers FORBIDDEN first (FR-023).
func (s *Service) Transfer(ctx context.Context, in dto.TransferInput) (dto.AdminOrderView, error) {
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
		// A transfer replaces cancelling a paid order, so it applies only to a
		// paid order; every other state is refused (FR-024, error-codes.md).
		if order.Status != constant.StatusPaid {
			return domainerr.ErrNotTransferable
		}

		recipient, err := s.Accounts.UserIDByEmail(txCtx, in.Email)
		if err != nil {
			if errors.Is(err, contracts.ErrAccountNotFound) {
				return domainerr.ErrTransferTargetNotFound
			}
			return err
		}

		// Only the owner changes; the lines, the state and the total are not
		// written (FR-024, research D12).
		if err := s.Orders.UpdateOwner(txCtx, order.ID, recipient, s.now()); err != nil {
			return err
		}
		order.UserID = recipient

		// The audit entry is emitted from inside the transaction, so a successful
		// transfer and its record are produced by the same unit of work; the
		// shared writer queues it and never fails the business operation (FR-023,
		// Constitution VI).
		s.record(txCtx, constant.AuditOrderTransferred, actor, order.ID)
		view = s.Mapper.AdminOrder(*order)
		return nil
	}); err != nil {
		return dto.AdminOrderView{}, err
	}
	return view, nil
}
