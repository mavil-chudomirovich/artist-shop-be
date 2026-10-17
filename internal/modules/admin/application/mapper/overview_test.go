package mapper

import (
	"testing"
	"time"

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

// FR-027, T054: itemSummary formats the first line as
// "<name> <variantName> (x<quantity>)" and appends " +N" when there are more
// lines; the joined parts are trimmed and a missing variant name does not leave
// a double space (legacy tombstone).
func TestItemSummaryFormatsFirstLineAndRemainder(t *testing.T) {
	cases := []struct {
		name  string
		order model.RecentOrder
		want  string
	}{
		{
			name:  "name and variant",
			order: model.RecentOrder{FirstLineName: "Stand", FirstLineVariantName: "Elf", FirstLineQuantity: 2, LineCount: 3},
			want:  "Stand Elf (x2) +2",
		},
		{
			name:  "single line, no remainder",
			order: model.RecentOrder{FirstLineName: "Stand", FirstLineVariantName: "Elf", FirstLineQuantity: 2, LineCount: 1},
			want:  "Stand Elf (x2)",
		},
		{
			name:  "empty variant name avoids double space",
			order: model.RecentOrder{FirstLineName: "Stand", FirstLineVariantName: "", FirstLineQuantity: 2, LineCount: 2},
			want:  "Stand (x2) +1",
		},
		{
			name:  "trims surrounding whitespace",
			order: model.RecentOrder{FirstLineName: "  Stand  ", FirstLineVariantName: "  Elf  ", FirstLineQuantity: 1, LineCount: 1},
			want:  "Stand Elf (x1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := itemSummary(tc.order)
			if got != tc.want {
				t.Fatalf("itemSummary = %q, want %q", got, tc.want)
			}
		})
	}
}

// FR-026, T054: a missing customer maps to nil in the application DTO.
func TestRecentOrdersMapsMissingCustomerToNil(t *testing.T) {
	orders := []model.RecentOrder{
		{ID: uuid.New(), Customer: nil, Total: model.Money{Amount: 100, Currency: "VND"}, Status: "PAID", CreatedAt: time.Now(), LineCount: 1, TotalQuantity: 1},
	}
	out := New().recentOrders(orders)
	if len(out) != 1 {
		t.Fatalf("expected 1 order, got %d", len(out))
	}
	if out[0].Customer != nil {
		t.Fatalf("missing customer must map to nil, got %+v", out[0].Customer)
	}
}

// FR-027, T054: itemCount is the line count and itemQuantity is the total quantity.
func TestRecentOrdersMapsItemCountAndQuantity(t *testing.T) {
	orders := []model.RecentOrder{
		{ID: uuid.New(), Total: model.Money{Amount: 100, Currency: "VND"}, Status: "PAID", CreatedAt: time.Now(), LineCount: 2, TotalQuantity: 5},
	}
	out := New().recentOrders(orders)
	if out[0].ItemCount != 2 {
		t.Fatalf("itemCount = %d, want 2", out[0].ItemCount)
	}
	if out[0].ItemQuantity != 5 {
		t.Fatalf("itemQuantity = %d, want 5", out[0].ItemQuantity)
	}
}
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
