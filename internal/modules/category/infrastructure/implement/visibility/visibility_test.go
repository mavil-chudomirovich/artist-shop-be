package visibility

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
)

// fakeRepository implements only the single read the adapter uses. The embedded
// interface is nil on purpose: any other method called on it panics, so a test
// that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.CategoryRepository
	ids []uuid.UUID
	err error

	// The slug lookup answers a fixed result so the adapter's pass-through can be
	// asserted without a second fake.
	slugID    uuid.UUID
	slugFound bool
}

func (f *fakeRepository) VisibleIDs(context.Context) ([]uuid.UUID, error) {
	return f.ids, f.err
}

func (f *fakeRepository) IDBySlug(context.Context, string) (uuid.UUID, bool, error) {
	return f.slugID, f.slugFound, f.err
}

// FR-002, research D1: the adapter answers with the identifiers of every
// category on display, exactly as the repository returned them and with no
// filtering of its own. The display state is module 03's to decide; this adapter
// is only the bridge.
func TestVisibleCategoryIDsReturnsTheRepositorysSet(t *testing.T) {
	want := []uuid.UUID{uuid.New(), uuid.New()}
	repo := &fakeRepository{ids: want}

	got, err := New(repo).VisibleCategoryIDs(context.Background())

	if err != nil {
		t.Fatalf("VisibleCategoryIDs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d identifiers, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// A catalogue with no visible category answers an empty set and no error, so a
// consumer can page over an empty result rather than fail.
func TestVisibleCategoryIDsAnswersAnEmptySet(t *testing.T) {
	got, err := New(&fakeRepository{ids: []uuid.UUID{}}).VisibleCategoryIDs(context.Background())
	if err != nil {
		t.Fatalf("an empty catalogue must not be an error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty set, got %v", got)
	}
}

// A repository failure is reported as itself, never swallowed or replaced: the
// consumer decides what an unavailable category read means for its own answer.
func TestVisibleCategoryIDsReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	_, err := New(&fakeRepository{err: want}).VisibleCategoryIDs(context.Background())

	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}

// FR-006, research D1: the adapter resolves a slug to the identifier the
// repository reports, so the public `?category=<slug>` filter can be built in
// SQL without module 04 reading module 03's table.
func TestCategoryIDBySlugReturnsTheRepositorysAnswer(t *testing.T) {
	want := uuid.New()
	repo := &fakeRepository{slugID: want, slugFound: true}

	got, found, err := New(repo).CategoryIDBySlug(context.Background(), "tranh-son-dau")

	if err != nil {
		t.Fatalf("CategoryIDBySlug: %v", err)
	}
	if !found {
		t.Fatal("expected the slug to be reported found")
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

// An unknown slug answers found=false and no error, so the caller can answer the
// public filter's empty list rather than translating a not-found into a success.
func TestCategoryIDBySlugReportsAnUnknownSlugAsNotFound(t *testing.T) {
	got, found, err := New(&fakeRepository{slugFound: false}).CategoryIDBySlug(context.Background(), "khong-ton-tai")

	if err != nil {
		t.Fatalf("an unknown slug must not be an error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false for an unknown slug, got id %s", got)
	}
}

// A repository failure is reported as itself, exactly as VisibleCategoryIDs
// reports one.
func TestCategoryIDBySlugReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	_, _, err := New(&fakeRepository{err: want}).CategoryIDBySlug(context.Background(), "bat-ky")

	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
