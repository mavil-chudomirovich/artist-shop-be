package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// fakeRepository implements only the single bulk read the adapter uses. The
// embedded interface is nil on purpose: any other method called on it panics, so
// a test that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.ProductRepository
	products []model.Product
	err      error
}

func (f *fakeRepository) FindByIDs(context.Context, []uuid.UUID) ([]model.Product, error) {
	return f.products, f.err
}

// research D1: the adapter maps each product to the contract DTO, taking OnSale
// from the domain's own buyable decision and carrying the price through unchanged.
func TestProductsMapsTheDomainFactsToTheContract(t *testing.T) {
	onSaleID := uuid.New()
	draftID := uuid.New()
	repo := &fakeRepository{products: []model.Product{
		{
			ID:        onSaleID,
			Name:      "Tranh sơn dầu",
			Slug:      "tranh-son-dau",
			SellState: constant.SellStateActive,
			Price:     model.Price{Amount: 120000, Currency: "VND"},
		},
		{
			ID:        draftID,
			Name:      "Tranh sắp ra mắt",
			Slug:      "tranh-sap-ra-mat",
			SellState: constant.SellStateComingSoon,
			Price:     model.Price{Amount: 90000, Currency: "VND"},
		},
	}}

	got, err := New(repo).Products(context.Background(), []uuid.UUID{onSaleID, draftID})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(got))
	}

	if got[0].ID != onSaleID || got[0].Name != "Tranh sơn dầu" || got[0].Slug != "tranh-son-dau" {
		t.Errorf("first summary = %+v", got[0])
	}
	if !got[0].OnSale {
		t.Errorf("an ACTIVE product must report OnSale=true")
	}
	if got[0].Price.Amount != 120000 || got[0].Price.Currency != "VND" {
		t.Errorf("first price = %+v, want 120000 VND", got[0].Price)
	}

	if got[1].ID != draftID {
		t.Errorf("second summary id = %s, want %s", got[1].ID, draftID)
	}
	if got[1].OnSale {
		t.Errorf("a COMING_SOON product must report OnSale=false")
	}
}

// research D1: an unknown identifier is simply absent from the result, not an
// error, so the consumer can tell a removed product from an available one.
func TestProductsReturnsOnlyTheProductsThatExist(t *testing.T) {
	known := uuid.New()
	repo := &fakeRepository{products: []model.Product{
		{ID: known, Name: "Có", Slug: "co", SellState: constant.SellStateActive},
	}}

	got, err := New(repo).Products(context.Background(), []uuid.UUID{known, uuid.New()})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(got) != 1 || got[0].ID != known {
		t.Fatalf("expected only the existing product, got %+v", got)
	}
}

// A repository failure is reported as itself, never swallowed or replaced: the
// cart decides what an unavailable catalogue read means for its own answer.
func TestProductsReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	_, err := New(&fakeRepository{err: want}).Products(context.Background(), []uuid.UUID{uuid.New()})
	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
