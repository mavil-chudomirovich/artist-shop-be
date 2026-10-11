//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the payment adapter against a real PostgreSQL container. Its
// centre of gravity is what the migration and the storage enforce: a payment and
// its events round-trip, the live-by-order lookup finds only the PENDING one, a
// duplicate order code is a retryable collision, the status filter is the
// reconciliation queue, the provider reference is unique only when present, the
// UNMATCHED ⇔ reason check holds, and deleting a payment takes its events with it
// (quickstart scenario 6, FR-004, FR-009, FR-011, FR-012, FR-018, FR-022).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// paymentFixture is one migrated PostgreSQL container plus the adapter under test.
type paymentFixture struct {
	pool *pgxpool.Pool
	repo *PaymentRepository
}

func newPaymentFixture(t *testing.T) *paymentFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return &paymentFixture{pool: pool, repo: NewPaymentRepository(pool)}
}

// buildPayment builds a fresh PENDING payment for one order.
func buildPayment(orderCode int64) *model.Payment {
	return model.NewPayment(uuid.New(), uuid.New(), orderCode, 120000, "VND", "VQRIO123", 2, time.Now().UTC())
}

// TestPaymentRoundTrips pins that a payment and its nullable columns survive a
// write and a read, through every lookup the module uses.
func TestPaymentRoundTrips(t *testing.T) {
	f := newPaymentFixture(t)
	payment := buildPayment(123)
	if err := f.repo.Create(context.Background(), payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	byID, err := f.repo.FindByID(context.Background(), payment.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.OrderCode != 123 || byID.Amount != 120000 || byID.Currency != "VND" || byID.OrderVersion != 2 {
		t.Fatalf("round trip lost a field: %+v", byID)
	}
	if byID.Status != constant.StatusPending || byID.Provider != constant.ProviderPayOS {
		t.Fatalf("round trip lost the status or provider: %s / %s", byID.Status, byID.Provider)
	}
	if byID.ProviderReference != "" || byID.UnmatchedReason != "" {
		t.Fatalf("a fresh payment must carry no provider reference or reason, got %q / %q",
			byID.ProviderReference, byID.UnmatchedReason)
	}

	byCode, err := f.repo.FindByOrderCode(context.Background(), 123)
	if err != nil || byCode.ID != payment.ID {
		t.Fatalf("FindByOrderCode = %+v / %v, want the payment", byCode, err)
	}

	byOrder, err := f.repo.FindByOrder(context.Background(), payment.OrderID)
	if err != nil || len(byOrder) != 1 || byOrder[0].ID != payment.ID {
		t.Fatalf("FindByOrder = %+v / %v, want the one payment", byOrder, err)
	}
}

// TestFindLiveByOrderReturnsOnlyThePendingPayment pins that the reuse/cancel path
// finds the live payment and answers not-found once it is terminal.
func TestFindLiveByOrderReturnsOnlyThePendingPayment(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	payment := buildPayment(200)
	if err := f.repo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	live, err := f.repo.FindLiveByOrder(ctx, payment.OrderID)
	if err != nil || live.ID != payment.ID {
		t.Fatalf("FindLiveByOrder = %+v / %v, want the pending payment", live, err)
	}

	if err := f.repo.UpdateStatus(ctx, payment.ID, constant.StatusCancelled, time.Now().UTC()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := f.repo.FindLiveByOrder(ctx, payment.OrderID); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("a cancelled order must have no live payment, got %v", err)
	}
}

// TestCreateRefusesADuplicateOrderCode pins research D4: a collision is a
// retryable refusal, never a silently stored second row.
func TestCreateRefusesADuplicateOrderCode(t *testing.T) {
	f := newPaymentFixture(t)
	if err := f.repo.Create(context.Background(), buildPayment(777)); err != nil {
		t.Fatalf("first create: %v", err)
	}

	err := f.repo.Create(context.Background(), buildPayment(777))
	if !errors.Is(err, domainerr.ErrOrderCodeCollision) {
		t.Fatalf("a duplicate order code must be a collision, got %v", err)
	}
}

// TestListAllFiltersByStatus pins FR-018: the operator's list is every payment,
// and status=UNMATCHED is the reconciliation queue.
func TestListAllFiltersByStatus(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()

	pending := buildPayment(1)
	unmatched := buildPayment(2)
	for _, payment := range []*model.Payment{pending, unmatched} {
		if err := f.repo.Create(ctx, payment); err != nil {
			t.Fatalf("create payment: %v", err)
		}
	}
	if err := f.repo.RecordUnmatched(ctx, unmatched.ID, "amount mismatch", time.Now().UTC()); err != nil {
		t.Fatalf("record unmatched: %v", err)
	}

	all, total, err := f.repo.ListAll(ctx, 1, 20, nil)
	if err != nil || total != 2 || len(all) != 2 {
		t.Fatalf("ListAll = %d rows / total %d / err %v, want 2/2", len(all), total, err)
	}
	for _, payment := range all {
		if payment.UnmatchedReason != "" && payment.ID != unmatched.ID {
			t.Fatalf("only the unmatched payment may carry a reason, got %+v", payment)
		}
	}

	status := constant.StatusUnmatched
	queue, total, err := f.repo.ListAll(ctx, 1, 20, &status)
	if err != nil || total != 1 || len(queue) != 1 || queue[0].ID != unmatched.ID {
		t.Fatalf("the queue = %+v / total %d / err %v, want the one unmatched payment", queue, total, err)
	}
}

// TestProviderReferenceIsUniqueOnlyWhenPresent pins research D7: several rows may
// hold no reference, but two rows must never claim the same confirmed one.
func TestProviderReferenceIsUniqueOnlyWhenPresent(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()

	first := buildPayment(10)
	second := buildPayment(11)
	for _, payment := range []*model.Payment{first, second} {
		if err := f.repo.Create(ctx, payment); err != nil {
			t.Fatalf("create payment: %v", err)
		}
	}

	if err := f.repo.ApplyPaid(ctx, first.ID, "provider-ref-1", time.Now().UTC()); err != nil {
		t.Fatalf("apply the first payment: %v", err)
	}
	if err := f.repo.ApplyPaid(ctx, second.ID, "provider-ref-1", time.Now().UTC()); err == nil {
		t.Fatal("a duplicate provider reference must be refused")
	}

	byID, err := f.repo.FindByID(ctx, first.ID)
	if err != nil || byID.Status != constant.StatusPaid || byID.ProviderReference != "provider-ref-1" {
		t.Fatalf("the first payment = %+v / %v, want PAID with the reference", byID, err)
	}
}

// TestRecordUnmatchedRequiresAReasonAtStorage pins FR-012 and FR-013: the storage
// refuses UNMATCHED without a reason, and a reason on another state.
func TestRecordUnmatchedRequiresAReasonAtStorage(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	payment := buildPayment(20)
	if err := f.repo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`UPDATE payments SET status = 'UNMATCHED' WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("the storage must refuse UNMATCHED without a reason")
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE payments SET status = 'PENDING', unmatched_reason = 'x' WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("the storage must refuse a reason on a non-UNMATCHED state")
	}

	if err := f.repo.RecordUnmatched(ctx, payment.ID, "order version changed", time.Now().UTC()); err != nil {
		t.Fatalf("record unmatched: %v", err)
	}
	byID, err := f.repo.FindByID(ctx, payment.ID)
	if err != nil || byID.Status != constant.StatusUnmatched || byID.UnmatchedReason != "order version changed" {
		t.Fatalf("the payment = %+v / %v, want UNMATCHED with the reason", byID, err)
	}
}

// TestStorageRefusesAnUnlistedStatusAndBadAmount pins the check constraints: a
// status outside the four and a non-positive amount are refused by the database.
func TestStorageRefusesAnUnlistedStatusAndBadAmount(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	payment := buildPayment(30)
	if err := f.repo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE payments SET status = 'BOGUS' WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("the storage must refuse a status outside the four")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE payments SET amount = 0 WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("the storage must refuse a non-positive amount")
	}
}

// TestDeletingAPaymentCascadesItsEvents pins FR-022: a removed payment takes its
// events with it.
func TestDeletingAPaymentCascadesItsEvents(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	payment := buildPayment(40)
	if err := f.repo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO payment_events (payment_id, action, detail) VALUES ($1, 'PAYMENT_CREATED', 'created')`,
		payment.ID); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM payments WHERE id = $1`, payment.ID); err != nil {
		t.Fatalf("delete payment: %v", err)
	}

	var count int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM payment_events WHERE payment_id = $1`, payment.ID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Fatalf("a deleted payment must take its events with it, found %d", count)
	}
}
