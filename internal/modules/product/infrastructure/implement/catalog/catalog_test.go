package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// fakeRepository implements only the single bulk read the adapter uses. The
// embedded interface is nil on purpose: any other method called on it panics, so
// a test that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.VariantRepository
	facts []domainrepo.VariantFacts
	err   error
}

func (f *fakeRepository) FindFactsByIDs(context.Context, []uuid.UUID) ([]domainrepo.VariantFacts, error) {
	return f.facts, f.err
}

// research D6: the adapter maps each variant fact to the contract DTO, carrying
// the product codes and the effective price through unchanged.
func TestVariantsMapsTheDomainFactsToTheContract(t *testing.T) {
	onSaleID := uuid.New()
	draftID := uuid.New()
	productID := uuid.New()
	repo := &fakeRepository{facts: []domainrepo.VariantFacts{
		{
			VariantID:   onSaleID,
			ProductID:   productID,
			ProductName: "Tranh sơn dầu",
			ProductCode: "tranh-son-dau",
			VariantName: "Khổ lớn",
			SKU:         "SKU-1",
			OnSale:      true,
			Price:       model.Price{Amount: 120000, Currency: "VND"},
		},
		{
			VariantID:   draftID,
			ProductID:   productID,
			ProductName: "Tranh sắp ra mắt",
			ProductCode: "tranh-sap-ra-mat",
			VariantName: "Khổ nhỏ",
			SKU:         "SKU-2",
			OnSale:      false,
			Price:       model.Price{Amount: 90000, Currency: "VND"},
		},
	}}

	got, err := New(repo).Variants(context.Background(), []uuid.UUID{onSaleID, draftID})
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(got))
	}

	if got[0].VariantID != onSaleID || got[0].ProductName != "Tranh sơn dầu" || got[0].ProductCode != "tranh-son-dau" {
		t.Errorf("first summary = %+v", got[0])
	}
	if !got[0].OnSale {
		t.Errorf("an ACTIVE variant must report OnSale=true")
	}
	if got[0].Price.Amount != 120000 || got[0].Price.Currency != "VND" {
		t.Errorf("first price = %+v, want 120000 VND", got[0].Price)
	}

	if got[1].VariantID != draftID {
		t.Errorf("second summary id = %s, want %s", got[1].VariantID, draftID)
	}
	if got[1].OnSale {
		t.Errorf("a COMING_SOON variant must report OnSale=false")
	}
}

// research D6: an unknown identifier is simply absent from the result, not an
// error, so the consumer can tell a removed variant from an available one.
func TestVariantsReturnsOnlyTheVariantsThatExist(t *testing.T) {
	known := uuid.New()
	repo := &fakeRepository{facts: []domainrepo.VariantFacts{
		{VariantID: known, ProductName: "Có", ProductCode: "co", OnSale: true},
	}}

	got, err := New(repo).Variants(context.Background(), []uuid.UUID{known, uuid.New()})
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(got) != 1 || got[0].VariantID != known {
		t.Fatalf("expected only the existing variant, got %+v", got)
	}
}

// A repository failure is reported as itself, never swallowed or replaced: the
// cart decides what an unavailable variant-facts read means for its own answer.
func TestVariantsReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	_, err := New(&fakeRepository{err: want}).Variants(context.Background(), []uuid.UUID{uuid.New()})
	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
