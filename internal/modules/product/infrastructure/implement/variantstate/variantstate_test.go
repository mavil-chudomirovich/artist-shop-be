package variantstate

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fakeRepository implements only the read the adapter uses. The embedded field is
// deliberately absent: any other method would not compile here, so a test that
// drifts from the adapter's actual dependency fails loudly.
type fakeRepository struct {
	result   []uuid.UUID
	err      error
	received []uuid.UUID
}

func (f *fakeRepository) DiscontinuedIDs(_ context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	f.received = ids
	return f.result, f.err
}

// research D9: the adapter delegates the read unchanged, so the dashboard's
// DISCONTINUED filter is the product module's own answer.
func TestDiscontinuedVariantIDsDelegatesTheRead(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	repo := &fakeRepository{result: []uuid.UUID{second}}

	got, err := New(repo).DiscontinuedVariantIDs(context.Background(), []uuid.UUID{first, second})
	if err != nil {
		t.Fatalf("DiscontinuedVariantIDs: %v", err)
	}
	if len(got) != 1 || got[0] != second {
		t.Fatalf("expected only the discontinued id, got %+v", got)
	}
	if len(repo.received) != 2 || repo.received[0] != first || repo.received[1] != second {
		t.Fatalf("the adapter must pass the ids through unchanged, got %+v", repo.received)
	}
}

// FR-018: an empty candidate set needs no lookup; the repository answers an empty
// slice and the adapter carries it through.
func TestDiscontinuedVariantIDsWithNoCandidates(t *testing.T) {
	repo := &fakeRepository{result: []uuid.UUID{}}

	got, err := New(repo).DiscontinuedVariantIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("DiscontinuedVariantIDs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no discontinued ids, got %+v", got)
	}
}
