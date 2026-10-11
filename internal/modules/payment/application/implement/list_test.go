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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
)

// This file is the use-case contract of US5 (FR-018): the operator's list returns
// one page of every payment, newest first, optionally narrowed to one state —
// status=UNMATCHED is the reconciliation queue (research D15). The adapter's
// ordering and status filter run against real PostgreSQL in the integration test;
// here the real use case runs over an in-memory repository, so the answers
// asserted are the service's own.

// listRepo is the in-memory PaymentRepository the operator-list use case runs
// over. It answers a configured page and records the window and the status filter
// it was handed, so a test can prove the request reached the repository unchanged.
// Embedding the interface makes any method the use case drifts into panic loudly
// rather than silently pass.
type listRepo struct {
	appinterface.PaymentRepository
	payments  []model.Payment
	total     int64
	err       error
	gotPage   int
	gotSize   int
	gotStatus *constant.Status
}

func (r *listRepo) ListAll(_ context.Context, page, size int, status *constant.Status) ([]model.Payment, int64, error) {
	r.gotPage, r.gotSize, r.gotStatus = page, size, status
	if r.err != nil {
		return nil, 0, r.err
	}
	return r.payments, r.total, nil
}

// newListService builds the use case over the in-memory repository. The list path
// needs no order contract, provider, transaction or actor: it is a review surface
// behind the administrator role guard (FR-018).
func newListService(repo appinterface.PaymentRepository) *Service {
	return New(Service{Payments: repo, Mapper: paymentmapper.New()})
}

// listPayment is one payment fixture as the repository returns it (newest first
// is the repository's ordering, which the use case must preserve).
func listPayment(status constant.Status, reason, reference string, createdAt time.Time) model.Payment {
	payment := model.Payment{
		ID:          uuid.New(),
		OrderID:     uuid.New(),
		UserID:      uuid.New(),
		OrderCode:   int64(createdAt.Unix()),
		Amount:      120000,
		Currency:    "VND",
		Description: "DH0001",
		Status:      status,
		Provider:    constant.ProviderPayOS,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	payment.UnmatchedReason = reason
	payment.ProviderReference = reference
	return payment
}

// FR-018: the operator's list is one page of every payment, newest first, with its
// owner and the provider reference, and echoes the requested window and the total.
func TestListAllReturnsEveryPaymentNewestFirstWithItsOwner(t *testing.T) {
	newest := listPayment(constant.StatusUnmatched, "amount mismatch", "", testInstant)
	older := listPayment(constant.StatusPaid, "", "TF-1", testInstant.Add(-time.Hour))
	repo := &listRepo{payments: []model.Payment{newest, older}, total: 3}
	svc := newListService(repo)

	page, err := svc.ListAll(context.Background(), dto.ListInput{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if page.Page != 1 || page.PageSize != 2 {
		t.Fatalf("the page must echo the requested window, got %d/%d", page.Page, page.PageSize)
	}
	if page.Total != 3 {
		t.Fatalf("the total must count every payment, got %d", page.Total)
	}
	if len(page.Payments) != 2 || page.Payments[0].ID != newest.ID {
		t.Fatalf("the first row must be the newest payment, got %+v", page.Payments)
	}
	if page.Payments[0].UserID != newest.UserID {
		t.Fatalf("each row must carry its owner, got %+v", page.Payments[0])
	}
	if page.Payments[0].Status != constant.StatusUnmatched || page.Payments[0].UnmatchedReason != "amount mismatch" {
		t.Fatalf("the reconciliation row must carry its state and reason, got %+v", page.Payments[0])
	}
	if page.Payments[1].ProviderReference != "TF-1" {
		t.Fatalf("the operator view must carry the provider reference, got %+v", page.Payments[1])
	}
	if repo.gotStatus != nil {
		t.Fatalf("an absent status filter must stay absent, got %v", *repo.gotStatus)
	}
}

// FR-018, research D15: the operator's list accepts an optional state filter;
// status=UNMATCHED is the reconciliation queue. The window and the filter reach
// the repository unchanged.
func TestListAllPassesTheStatusFilter(t *testing.T) {
	repo := &listRepo{}
	svc := newListService(repo)
	status := constant.StatusUnmatched

	page, err := svc.ListAll(context.Background(), dto.ListInput{Page: 2, PageSize: 5, Status: &status})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if repo.gotStatus == nil || *repo.gotStatus != constant.StatusUnmatched {
		t.Fatalf("the status filter must reach the repository, got %v", repo.gotStatus)
	}
	if repo.gotPage != 2 || repo.gotSize != 5 {
		t.Fatalf("the window must reach the repository, got %d/%d", repo.gotPage, repo.gotSize)
	}
	if page.Page != 2 || page.PageSize != 5 {
		t.Fatalf("the page must echo the requested window, got %d/%d", page.Page, page.PageSize)
	}
}

// FR-018: a repository failure is surfaced, never swallowed into an empty page.
func TestListAllPropagatesAStorageFailure(t *testing.T) {
	boom := errors.New("storage down")
	repo := &listRepo{err: boom}
	svc := newListService(repo)

	if _, err := svc.ListAll(context.Background(), dto.ListInput{Page: 1, PageSize: 20}); !errors.Is(err, boom) {
		t.Fatalf("ListAll error = %v, want the storage failure", err)
	}
}
