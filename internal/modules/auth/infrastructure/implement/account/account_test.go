package account

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/repository"
)

// fakeRepository implements only the single method the adapter uses. The embedded
// interface is nil on purpose: any other method called on it panics, so a test
// that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.UserRepository

	account      *model.Account
	err          error
	gotEmail     string
	byEmailCalls int
}

func (f *fakeRepository) ByEmail(_ context.Context, email string) (*model.Account, error) {
	f.byEmailCalls++
	f.gotEmail = email
	return f.account, f.err
}

// research D8: an existing account resolves to its identifier.
func TestUserIDByEmailResolvesAnAccount(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepository{account: &model.Account{ID: id}}

	got, err := New(repo).UserIDByEmail(context.Background(), "someone@example.com")
	if err != nil {
		t.Fatalf("UserIDByEmail: %v", err)
	}
	if got != id {
		t.Fatalf("UserIDByEmail = %s, want %s", got, id)
	}
}

// research D8: the email is normalised the way the auth module stores it, so a
// transfer named in mixed case still resolves.
func TestUserIDByEmailNormalisesTheEmail(t *testing.T) {
	repo := &fakeRepository{account: &model.Account{ID: uuid.New()}}

	if _, err := New(repo).UserIDByEmail(context.Background(), "  Someone@Example.COM "); err != nil {
		t.Fatalf("UserIDByEmail: %v", err)
	}
	if repo.gotEmail != "someone@example.com" {
		t.Fatalf("the adapter looked up %q, want the normalised %q", repo.gotEmail, "someone@example.com")
	}
}

// research D8: no account carrying the email is the contract sentinel, not the
// auth module's own not-found, so the order module can branch without importing
// module 01.
func TestUserIDByEmailTranslatesNotFoundToTheContractSentinel(t *testing.T) {
	repo := &fakeRepository{err: domainerr.ErrUserNotFound}

	_, err := New(repo).UserIDByEmail(context.Background(), "missing@example.com")
	if !errors.Is(err, contracts.ErrAccountNotFound) {
		t.Fatalf("expected contracts.ErrAccountNotFound, got %v", err)
	}
}

// Any other failure is reported as itself, never replaced by the sentinel.
func TestUserIDByEmailReportsOtherErrors(t *testing.T) {
	want := errors.New("storage unavailable")

	_, err := New(&fakeRepository{err: want}).UserIDByEmail(context.Background(), "x@example.com")
	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
