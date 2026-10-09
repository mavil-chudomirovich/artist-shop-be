package implement

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// This file is US6's use-case contract: the history read over the adapter's paged
// read. It asserts the order the ledger is read in, that a product with no
// movements answers an empty page rather than an error, that the page metadata
// echoes the effective window, and that an unknown product answers not-found
// before any row is read (FR-008, research D12, D14).

// Movements implements the paged ledger read for the in-memory repository. It
// mirrors the storage contract the PostgreSQL adapter provides — oldest first,
// ordered by created_at then id — so the use case's preservation of that order
// and the page metadata can be asserted without a database (research D14).
func (r *fakeRepository) Movements(_ context.Context, productID uuid.UUID, page, pageSize int) ([]model.Movement, int64, error) {
	product := make([]model.Movement, 0, len(r.movements))
	for _, movement := range r.movements {
		if movement.ProductID == productID {
			product = append(product, movement)
		}
	}
	sort.SliceStable(product, func(i, j int) bool {
		if !product[i].CreatedAt.Equal(product[j].CreatedAt) {
			return product[i].CreatedAt.Before(product[j].CreatedAt)
		}
		return product[i].ID.String() < product[j].ID.String()
	})
	total := int64(len(product))
	start := (page - 1) * pageSize
	if start >= len(product) {
		return nil, total, nil
	}
	end := start + pageSize
	if end > len(product) {
		end = len(product)
	}
	return product[start:end], total, nil
}

// FR-008, research D14: the history reads oldest first, and two reads sharing a
// timestamp return the same order because the identifier breaks the tie.
func TestHistoryReadsOldestFirstAndIsStable(t *testing.T) {
	productID := uuid.New()
	earlier := fixedNow
	later := fixedNow.Add(time.Minute)
	tiedLow := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tiedHigh := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	latest := uuid.New()
	repo := &fakeRepository{movements: []model.Movement{
		{ID: latest, ProductID: productID, Kind: constant.MovementDamage, Delta: -3, ResultingQuantity: 7, CreatedAt: later},
		{ID: tiedHigh, ProductID: productID, Kind: constant.MovementAdjustment, Delta: 2, ResultingQuantity: 12, CreatedAt: earlier},
		{ID: tiedLow, ProductID: productID, Kind: constant.MovementRestock, Delta: 10, ResultingQuantity: 10, CreatedAt: earlier},
	}}
	svc := newTestService(repo, &fakeLookup{exists: true}, nil)

	first, err := svc.History(adminContext(), dto.HistoryInput{ProductID: productID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if first.Total != 3 || first.Page != 1 || first.PageSize != 20 {
		t.Fatalf("unexpected page metadata: %+v", first)
	}
	if len(first.Movements) != 3 {
		t.Fatalf("expected three movements, got %d", len(first.Movements))
	}
	want := []uuid.UUID{tiedLow, tiedHigh, latest}
	for i, id := range want {
		if first.Movements[i].ID != id {
			t.Fatalf("movement %d: expected %s, got %s", i, id, first.Movements[i].ID)
		}
	}

	second, err := svc.History(adminContext(), dto.HistoryInput{ProductID: productID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("History (second read): %v", err)
	}
	for i := range first.Movements {
		if first.Movements[i].ID != second.Movements[i].ID {
			t.Fatalf("the order changed between two reads at index %d", i)
		}
	}
}

// FR-008, quickstart 9d: a product with no movements answers an empty page, not
// an error, with the project's default window.
func TestHistoryOfAProductWithNoMovementsIsAnEmptyPage(t *testing.T) {
	repo := &fakeRepository{}
	svc := newTestService(repo, &fakeLookup{exists: true}, nil)

	out, err := svc.History(adminContext(), dto.HistoryInput{ProductID: uuid.New()})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(out.Movements) != 0 || out.Total != 0 {
		t.Fatalf("expected an empty page, got %+v", out)
	}
	if out.Page != 1 || out.PageSize != 20 {
		t.Fatalf("an omitted window must fall back to page 1 size 20, got %+v", out)
	}
}

// FR-007, research D12: an unknown product answers not-found before the ledger is
// read, so a fabricated empty history is never shown.
func TestHistoryOfAnUnknownProductAnswersNotFound(t *testing.T) {
	repo := &fakeRepository{}
	svc := newTestService(repo, &fakeLookup{exists: false}, nil)

	_, err := svc.History(adminContext(), dto.HistoryInput{ProductID: uuid.New()})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// FR-008, quickstart 9b: the requested window is applied and the metadata echoes
// it, so an operator can page through a long history.
func TestHistoryPagesThroughTheMovements(t *testing.T) {
	productID := uuid.New()
	seeded := make([]model.Movement, 0, 5)
	for i := 0; i < 5; i++ {
		seeded = append(seeded, model.Movement{
			ID:                uuid.New(),
			ProductID:         productID,
			Kind:              constant.MovementRestock,
			Delta:             1,
			ResultingQuantity: int64(i + 1),
			CreatedAt:         fixedNow.Add(time.Duration(i) * time.Minute),
		})
	}
	repo := &fakeRepository{movements: seeded}
	svc := newTestService(repo, &fakeLookup{exists: true}, nil)

	out, err := svc.History(adminContext(), dto.HistoryInput{ProductID: productID, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if out.Total != 5 || out.Page != 2 || out.PageSize != 2 {
		t.Fatalf("unexpected page metadata: %+v", out)
	}
	if len(out.Movements) != 2 {
		t.Fatalf("expected two movements on page 2, got %d", len(out.Movements))
	}
	if out.Movements[0].ID != seeded[2].ID || out.Movements[1].ID != seeded[3].ID {
		t.Fatalf("page 2 of size 2 must hold the third and fourth movements, got %+v", out.Movements)
	}
}
