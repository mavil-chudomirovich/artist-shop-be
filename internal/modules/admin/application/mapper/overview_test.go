package mapper

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/model"
)

// FR-023: a share whose category could not be resolved (nil id) is labelled
// "Khác" by the mapper, regardless of the stored name.
func TestCategoryBreakdownNamesNullIdKhac(t *testing.T) {
	id := uuid.New()
	shares := []model.CategoryShare{
		{CategoryID: nil, CategoryName: "", Amount: model.Money{Amount: 100, Currency: "VND"}, Percentage: 0},
		{CategoryID: &id, CategoryName: "Acrylic", Amount: model.Money{Amount: 200, Currency: "VND"}, Percentage: 0},
	}

	out := New().categoryBreakdown(shares)

	if len(out) != 2 {
		t.Fatalf("expected 2 shares, got %d", len(out))
	}
	if out[0].CategoryName != constant.UnknownCategoryName {
		t.Fatalf("null id must be labelled %q, got %q", constant.UnknownCategoryName, out[0].CategoryName)
	}
	if out[0].CategoryID != nil {
		t.Fatalf("null id must stay nil, got %v", out[0].CategoryID)
	}
	if out[1].CategoryName != "Acrylic" {
		t.Fatalf("resolved name must pass through, got %q", out[1].CategoryName)
	}
}

// FR-024: the percentage is carried through with one decimal precision.
func TestCategoryBreakdownCarriesPercentageOneDecimal(t *testing.T) {
	id := uuid.New()
	shares := []model.CategoryShare{
		{CategoryID: &id, CategoryName: "A", Amount: model.Money{Amount: 1, Currency: "VND"}, Percentage: 33.3},
		{CategoryID: &id, CategoryName: "B", Amount: model.Money{Amount: 2, Currency: "VND"}, Percentage: 66.7},
	}

	out := New().categoryBreakdown(shares)

	if out[0].Percentage != 33.3 {
		t.Fatalf("percentage must be carried through, got %v", out[0].Percentage)
	}
	if out[1].Percentage != 66.7 {
		t.Fatalf("percentage must be carried through, got %v", out[1].Percentage)
	}
}

// FR-024: the mapper preserves the caller's amount-desc / name-asc order.
func TestCategoryBreakdownPreservesOrder(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	shares := []model.CategoryShare{
		{CategoryID: &id1, CategoryName: "B", Amount: model.Money{Amount: 200, Currency: "VND"}, Percentage: 66.7},
		{CategoryID: &id2, CategoryName: "A", Amount: model.Money{Amount: 100, Currency: "VND"}, Percentage: 33.3},
	}

	out := New().categoryBreakdown(shares)

	if len(out) != 2 {
		t.Fatalf("expected 2 shares, got %d", len(out))
	}
	if out[0].Amount.Amount != 200 || out[1].Amount.Amount != 100 {
		t.Fatalf("mapper must preserve the caller's order: got %d, %d", out[0].Amount.Amount, out[1].Amount.Amount)
	}
}
