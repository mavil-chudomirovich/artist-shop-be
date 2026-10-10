package reservation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
)

// fakeService records the calls the adapter makes, so each test observes the
// mapping from the contract's positional arguments onto module 05's DTO inputs.
type fakeService struct {
	reserved []dto.ReserveInput
	released []dto.ReleaseInput
	sold     []dto.SaleInput

	reserveErr error
	releaseErr error
	saleErr    error
}

func (f *fakeService) Reserve(_ context.Context, in dto.ReserveInput) error {
	f.reserved = append(f.reserved, in)
	return f.reserveErr
}

func (f *fakeService) Release(_ context.Context, in dto.ReleaseInput) error {
	f.released = append(f.released, in)
	return f.releaseErr
}

func (f *fakeService) ApplySale(_ context.Context, in dto.SaleInput) error {
	f.sold = append(f.sold, in)
	return f.saleErr
}

// research D4: Reserve holds the ordered quantity for the order and product.
func TestReserveMapsToTheInventoryUseCase(t *testing.T) {
	order, product := uuid.New(), uuid.New()
	svc := &fakeService{}

	if err := New(svc).Reserve(context.Background(), order, product, 3); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if len(svc.reserved) != 1 {
		t.Fatalf("expected one reserve call, got %d", len(svc.reserved))
	}
	got := svc.reserved[0]
	if got.OrderID != order || got.ProductID != product || got.Quantity != 3 {
		t.Fatalf("reserve = %+v, want order %s product %s quantity 3", got, order, product)
	}
}

// research D4: Release names the order and product, with no quantity.
func TestReleaseMapsToTheInventoryUseCase(t *testing.T) {
	order, product := uuid.New(), uuid.New()
	svc := &fakeService{}

	if err := New(svc).Release(context.Background(), order, product); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(svc.released) != 1 {
		t.Fatalf("expected one release call, got %d", len(svc.released))
	}
	got := svc.released[0]
	if got.OrderID != order || got.ProductID != product {
		t.Fatalf("release = %+v, want order %s product %s", got, order, product)
	}
}

// research D5: ApplySale carries the per-line source reference that makes the
// sale idempotent.
func TestApplySaleMapsToTheInventoryUseCase(t *testing.T) {
	order, product := uuid.New(), uuid.New()
	svc := &fakeService{}

	if err := New(svc).ApplySale(context.Background(), order, product, "evt-1:line-1"); err != nil {
		t.Fatalf("ApplySale: %v", err)
	}
	if len(svc.sold) != 1 {
		t.Fatalf("expected one sale call, got %d", len(svc.sold))
	}
	got := svc.sold[0]
	if got.OrderID != order || got.ProductID != product || got.SourceReference != "evt-1:line-1" {
		t.Fatalf("sale = %+v, want order %s product %s reference evt-1:line-1", got, order, product)
	}
}

// FR-016, research D4, D6: the window comes from module 05's single constant, so
// the order never repeats the sixty minutes.
func TestHoldWindowIsModule05sWindow(t *testing.T) {
	if got := New(&fakeService{}).HoldWindow(); got != constant.HoldTTL {
		t.Fatalf("HoldWindow = %v, want %v", got, constant.HoldTTL)
	}
}

// A use-case failure is reported as itself, never swallowed or replaced.
func TestReserveReportsTheServiceError(t *testing.T) {
	want := errors.New("insufficient stock")

	if err := New(&fakeService{reserveErr: want}).Reserve(context.Background(), uuid.New(), uuid.New(), 1); !errors.Is(err, want) {
		t.Fatalf("expected the service error, got %v", err)
	}
}
