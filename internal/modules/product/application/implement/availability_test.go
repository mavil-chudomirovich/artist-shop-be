package implement

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// This file exercises the system-facing availability use case the inventory module
// drives. Each signal must move only the edge it owns (ACTIVE ↔ OUT_OF_STOCK) and
// must leave an announced or retired product exactly where it was, so a stock
// change can never resurrect a retired product or launch an announced one
// (FR-024 to FR-027, research D4).

// requireSystemAudited asserts one availability audit event was recorded for the
// product with no human actor, which is truthful for a change caused by stock
// rather than by an administrator (FR-020, Constitution VI).
func requireSystemAudited(t *testing.T, recorder *recordingAuditor, action string, targetID uuid.UUID) {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action != action {
			continue
		}
		if event.outcome != "SUCCESS" {
			t.Fatalf("%s: expected outcome SUCCESS, got %q", action, event.outcome)
		}
		if event.actorID != nil {
			t.Fatalf("%s: an availability change must carry no human actor, got %s", action, *event.actorID)
		}
		if event.actorRole != "" {
			t.Fatalf("%s: an availability change must carry no actor role, got %q", action, event.actorRole)
		}
		if event.targetType != "product" || event.targetID != targetID.String() {
			t.Fatalf("%s: expected the product %s as the target, got %q/%q", action, targetID, event.targetType, event.targetID)
		}
		return
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
}

// FR-024: MarkOutOfStock moves an on-sale product out of stock and audits the
// change with no actor.
func TestMarkOutOfStockMovesAnActiveProductOutOfStock(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateActive, false)

	if err := maintenanceService(repo, recorder).MarkOutOfStock(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOutOfStock: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateOutOfStock {
		t.Fatalf("stored state = %q, want %q", got, constant.SellStateOutOfStock)
	}
	requireSystemAudited(t, recorder, constant.AuditProductStateChanged, seeded.ID)
}

// FR-024: an announced product is not moved by the signal.
func TestMarkOutOfStockLeavesAnAnnouncedProductUntouched(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateComingSoon, false)

	if err := maintenanceService(repo, recorder).MarkOutOfStock(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOutOfStock on an announced product: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateComingSoon {
		t.Fatalf("an announced product must not move, got %q", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-024, FR-027: a retired product is never moved by the signal.
func TestMarkOutOfStockLeavesARetiredProductUntouched(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateDiscontinued, false)

	if err := maintenanceService(repo, recorder).MarkOutOfStock(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOutOfStock on a retired product: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateDiscontinued {
		t.Fatalf("a retired product must not move, got %q", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-028: a signal for the state the product is already in is a no-op, not an
// error, and records nothing because nothing changed.
func TestMarkOutOfStockIsANoOpWhenAlreadyOutOfStock(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateOutOfStock, false)

	if err := maintenanceService(repo, recorder).MarkOutOfStock(context.Background(), seeded.ID); err != nil {
		t.Fatalf("an already-out-of-stock product must accept the signal: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateOutOfStock {
		t.Fatalf("stored state = %q, want %q", got, constant.SellStateOutOfStock)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-025: MarkOnSale moves an out-of-stock product back on sale and audits the
// change with no actor.
func TestMarkOnSaleMovesAnOutOfStockProductBackOnSale(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateOutOfStock, false)

	if err := maintenanceService(repo, recorder).MarkOnSale(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOnSale: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateActive {
		t.Fatalf("stored state = %q, want %q", got, constant.SellStateActive)
	}
	requireSystemAudited(t, recorder, constant.AuditProductStateChanged, seeded.ID)
}

// FR-025: an announced product is not moved by the on-sale signal.
func TestMarkOnSaleLeavesAnAnnouncedProductUntouched(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateComingSoon, false)

	if err := maintenanceService(repo, recorder).MarkOnSale(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOnSale on an announced product: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateComingSoon {
		t.Fatalf("an announced product must not move, got %q", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-027: a retired product never returns to sale through stock.
func TestMarkOnSaleLeavesARetiredProductUntouched(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateDiscontinued, false)

	if err := maintenanceService(repo, recorder).MarkOnSale(context.Background(), seeded.ID); err != nil {
		t.Fatalf("MarkOnSale on a retired product: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateDiscontinued {
		t.Fatalf("a retired product must not move, got %q", got)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-028: a signal for the state the product is already in is a no-op.
func TestMarkOnSaleIsANoOpWhenAlreadyOnSale(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	seeded := seedInSellState(t, repo, constant.SellStateActive, false)

	if err := maintenanceService(repo, recorder).MarkOnSale(context.Background(), seeded.ID); err != nil {
		t.Fatalf("an already-on-sale product must accept the signal: %v", err)
	}
	if got := storedProduct(t, repo, seeded.ID).SellState; got != constant.SellStateActive {
		t.Fatalf("stored state = %q, want %q", got, constant.SellStateActive)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a no-op signal must record nothing, got %v", recorder.actions())
	}
}

// FR-024: an unknown product is the module not-found, not a silent no-op.
func TestAvailabilitySignalOnAnUnknownProductAnswersNotFound(t *testing.T) {
	svc := maintenanceService(newFauxProducts(), &recordingAuditor{})
	if err := svc.MarkOutOfStock(context.Background(), uuid.New()); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("MarkOutOfStock: expected ErrProductNotFound, got %v", err)
	}
	if err := svc.MarkOnSale(context.Background(), uuid.New()); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("MarkOnSale: expected ErrProductNotFound, got %v", err)
	}
}
