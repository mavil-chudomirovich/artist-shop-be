package implement

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
)

// This file is US5's read surface: the operator's list. It returns one page of
// every payment, newest first, optionally narrowed to one state, so
// status=UNMATCHED is the reconciliation queue the operator reviews (FR-018,
// research D15). It is review-only: no manual apply lives here, and the list is a
// read, so it neither opens a transaction nor records an audit entry.

// defaultListPageSize is the window the operator list falls back to when a size
// is absent or out of range; maxListPageSize is the largest it accepts. The
// values mirror the repository's convention, so the page echoed in the response
// agrees with the page the repository returned (research D15).
const (
	defaultListPageSize = 20
	maxListPageSize     = 100
)

// ListAll returns one page of every payment, newest first, with its owner, state,
// amount and provider reference, optionally narrowed to one state (FR-018).
// Unlike the customer's read, it applies no owner filter: the caller reached it
// behind the administrator role guard, so the acting administrator is not read
// and no session identity is part of the query. status=UNMATCHED is the
// reconciliation queue (research D15), and the state is passed through to the
// repository, which owns the ordering and the filter.
func (s *Service) ListAll(ctx context.Context, in dto.ListInput) (dto.PaymentPage, error) {
	page, size := paymentListWindow(in.Page, in.PageSize)
	payments, total, err := s.Payments.ListAll(ctx, page, size, in.Status)
	if err != nil {
		return dto.PaymentPage{}, err
	}
	return dto.PaymentPage{
		Payments: s.Mapper.AdminPayments(payments),
		Page:     page,
		PageSize: size,
		Total:    total,
	}, nil
}

// paymentListWindow normalizes a requested window the same way the repository
// does, so the page echoed to the operator agrees with the page returned. It is
// the use case's own guard; the transport already rejects an out-of-range window
// with VALIDATION_ERROR (contracts/error-codes.md).
func paymentListWindow(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case size < 1:
		size = defaultListPageSize
	case size > maxListPageSize:
		size = maxListPageSize
	}
	return page, size
}
