package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
)

// fakeRepository implements only the three methods the adapter uses. The embedded
// interface is nil on purpose: any other method called on it panics, so a test
// that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.CartRepository

	cart     *model.Cart
	found    bool
	findErr  error
	lines    []model.CartLine
	linesErr error

	clearCalled  bool
	clearErr     error
	gotOwnerID   uuid.UUID
	gotCartID    uuid.UUID
	gotClearCart uuid.UUID
}

func (f *fakeRepository) FindByOwner(_ context.Context, userID uuid.UUID) (*model.Cart, bool, error) {
	f.gotOwnerID = userID
	return f.cart, f.found, f.findErr
}

func (f *fakeRepository) Lines(_ context.Context, cartID uuid.UUID) ([]model.CartLine, error) {
	f.gotCartID = cartID
	return f.lines, f.linesErr
}

func (f *fakeRepository) ClearLines(_ context.Context, cartID uuid.UUID) error {
	f.clearCalled = true
	f.gotClearCart = cartID
	return f.clearErr
}

// research D1: the adapter maps the cart's lines to the order's contract shape,
// preserving the cart's order and carrying the captured price as integer minor
// units with its currency.
func TestCartLinesMapsTheCartLinesInOrder(t *testing.T) {
	owner := uuid.New()
	cartID := uuid.New()
	first, second := uuid.New(), uuid.New()

	repo := &fakeRepository{
		cart:  &model.Cart{ID: cartID, UserID: owner},
		found: true,
		lines: []model.CartLine{
			{ProductID: first, Quantity: 2, UnitPrice: model.Price{Amount: 120000, Currency: "VND"}},
			{ProductID: second, Quantity: 1, UnitPrice: model.Price{Amount: 50000, Currency: "VND"}},
		},
	}

	got, err := New(repo).CartLines(context.Background(), owner)
	if err != nil {
		t.Fatalf("CartLines: %v", err)
	}
	if repo.gotOwnerID != owner {
		t.Errorf("the adapter looked up owner %s, want %s", repo.gotOwnerID, owner)
	}
	if repo.gotCartID != cartID {
		t.Errorf("the adapter read cart %s, want %s", repo.gotCartID, cartID)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	if got[0].ProductID != first || got[0].Quantity != 2 || got[0].UnitPriceAmount != 120000 || got[0].Currency != "VND" {
		t.Errorf("first = %+v, want %s x2 at 120000 VND", got[0], first)
	}
	if got[1].ProductID != second || got[1].Quantity != 1 || got[1].UnitPriceAmount != 50000 {
		t.Errorf("second = %+v, want %s x1 at 50000", got[1], second)
	}
}

// FR-006: a customer with no cart answers an empty slice, not an error and not a
// not-found, so the order module is the one that decides an empty cart is an
// empty checkout.
func TestCartLinesAnswersEmptyForACustomerWithNoCart(t *testing.T) {
	got, err := New(&fakeRepository{found: false}).CartLines(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("CartLines: %v", err)
	}
	if got == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected no lines, got %+v", got)
	}
}

// FR-007: clearing an existing cart empties its lines.
func TestClearCartEmptiesAnExistingCart(t *testing.T) {
	owner := uuid.New()
	cartID := uuid.New()

	repo := &fakeRepository{cart: &model.Cart{ID: cartID, UserID: owner}, found: true}
	if err := New(repo).ClearCart(context.Background(), owner); err != nil {
		t.Fatalf("ClearCart: %v", err)
	}
	if !repo.clearCalled || repo.gotClearCart != cartID {
		t.Fatalf("expected ClearLines(%s), got called=%v cart=%s", cartID, repo.clearCalled, repo.gotClearCart)
	}
}

// FR-007, research D1: a customer with no cart is a no-op, so a retried clear
// changes nothing and does not touch the repository.
func TestClearCartIsANoOpWithNoCart(t *testing.T) {
	repo := &fakeRepository{found: false}
	if err := New(repo).ClearCart(context.Background(), uuid.New()); err != nil {
		t.Fatalf("ClearCart: %v", err)
	}
	if repo.clearCalled {
		t.Fatal("clearing a customer with no cart must not call ClearLines")
	}
}

// A repository failure is reported as itself, never swallowed or replaced.
func TestCartLinesReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	if _, err := New(&fakeRepository{findErr: want}).CartLines(context.Background(), uuid.New()); !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
