package implement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the sell-state lifecycle use case over the in-memory
// repository and a recording auditor shared with the maintenance tests. The
// transition table of data-model.md is the authority (FR-021, FR-022): every
// listed edge must succeed and record PRODUCT_STATE_CHANGED, and every edge the
// table does not list must be refused naming the current state and leave the row
// exactly as it was (FR-023 to FR-026, SC-004).

// seedInSellState stores a product already in the requested state, reaching it
// through the domain's own transitions so the seeded row is one the state machine
// can actually produce.
func seedInSellState(t *testing.T, repo *fauxProducts, state constant.SellState, preorder bool) *model.Product {
	t.Helper()
	slug := "p-" + strings.ReplaceAll(strings.ToLower(string(state)), "_", "-")
	if preorder {
		slug += "-pre"
	}
	product, err := model.NewProduct(model.ProductDraft{
		Name: "Product " + slug, Slug: slug, Description: "Mô tả",
		Price: price(100), CategoryID: uuid.New(), Position: 1, IsPreorder: preorder,
	}, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", slug, err)
	}

	switch state {
	case constant.SellStateComingSoon:
		// The constructor already produces this state.
	case constant.SellStateActive:
		if err := product.Launch(time.Now().UTC()); err != nil {
			t.Fatalf("set up ACTIVE: %v", err)
		}
	case constant.SellStateOutOfStock:
		if err := product.Launch(time.Now().UTC()); err != nil {
			t.Fatalf("set up OUT_OF_STOCK: %v", err)
		}
		if err := product.SellOut(time.Now().UTC()); err != nil {
			t.Fatalf("set up OUT_OF_STOCK: %v", err)
		}
	case constant.SellStateDiscontinued:
		if err := product.Retire(time.Now().UTC()); err != nil {
			t.Fatalf("set up DISCONTINUED: %v", err)
		}
	default:
		t.Fatalf("unhandled state %q", state)
	}

	repo.mu.Lock()
	repo.products[product.ID] = *product
	repo.mu.Unlock()
	return product
}

// storedProduct reads one product back through the repository, so a test can
// assert what the refused or accepted transition actually wrote.
func storedProduct(t *testing.T, repo *fauxProducts, id uuid.UUID) model.Product {
	t.Helper()
	view, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", id, err)
	}
	return view.Product
}

// listedTransitions is the transition table data-model.md and research D8 record:
// exactly five edges. It is written out here rather than derived, so a change to
// the table in code cannot silently change what the test believes is legal.
func listedTransitions() []struct {
	from constant.SellState
	to   constant.SellState
} {
	return []struct {
		from constant.SellState
		to   constant.SellState
	}{
		{constant.SellStateComingSoon, constant.SellStateActive},
		{constant.SellStateActive, constant.SellStateOutOfStock},
		{constant.SellStateOutOfStock, constant.SellStateActive},
		{constant.SellStateComingSoon, constant.SellStateDiscontinued},
		{constant.SellStateActive, constant.SellStateDiscontinued},
		{constant.SellStateOutOfStock, constant.SellStateDiscontinued},
	}
}

// FR-021 to FR-023: every edge the table lists succeeds, moves the stored row and
// records PRODUCT_STATE_CHANGED with the acting administrator.
func TestChangeSellStateAllowsEveryListedTransition(t *testing.T) {
	for _, edge := range listedTransitions() {
		t.Run(string(edge.from)+"->"+string(edge.to), func(t *testing.T) {
			repo := newFauxProducts()
			recorder := &recordingAuditor{}
			actor := adminActor()
			ctx := appinterface.WithActor(context.Background(), actor)
			seeded := seedInSellState(t, repo, edge.from, false)

			out, err := maintenanceService(repo, recorder).ChangeSellState(ctx, dto.ChangeSellStateInput{
				ID: seeded.ID,
				To: edge.to,
			})
			if err != nil {
				t.Fatalf("ChangeSellState(%s->%s): %v", edge.from, edge.to, err)
			}
			if out.SellState != edge.to {
				t.Fatalf("response state = %q, want %q", out.SellState, edge.to)
			}
			if got := storedProduct(t, repo, seeded.ID).SellState; got != edge.to {
				t.Fatalf("stored state = %q, want %q", got, edge.to)
			}
			requireAudited(t, recorder, constant.AuditProductStateChanged, actor, seeded.ID)
		})
	}
}

// FR-022, FR-024, SC-004: every pair the table does not list is refused naming the
// current state, leaves the product exactly as it was and records nothing. This is
// the negative half of the table, DISCONTINUED -> DISCONTINUED included.
func TestChangeSellStateRefusesEveryUnlistedTransition(t *testing.T) {
	states := []constant.SellState{
		constant.SellStateComingSoon,
		constant.SellStateActive,
		constant.SellStateOutOfStock,
		constant.SellStateDiscontinued,
	}
	legal := make(map[string]bool, len(listedTransitions()))
	for _, edge := range listedTransitions() {
		legal[string(edge.from)+"->"+string(edge.to)] = true
	}

	for _, from := range states {
		for _, to := range states {
			if legal[string(from)+"->"+string(to)] {
				continue
			}
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				repo := newFauxProducts()
				recorder := &recordingAuditor{}
				ctx := appinterface.WithActor(context.Background(), adminActor())
				seeded := seedInSellState(t, repo, from, false)
				before := storedProduct(t, repo, seeded.ID)

				out, err := maintenanceService(repo, recorder).ChangeSellState(ctx, dto.ChangeSellStateInput{
					ID: seeded.ID,
					To: to,
				})
				if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
					t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
				}
				var transition *domainerr.StateTransitionError
				if !errors.As(err, &transition) || transition.Current != from {
					t.Fatalf("the refusal must name the current state %q, got %v", from, err)
				}
				if out.ID != uuid.Nil {
					t.Fatalf("a refused transition must not answer a product, got %+v", out)
				}
				if after := storedProduct(t, repo, seeded.ID); after != before {
					t.Fatalf("a refused transition changed the row:\n before %+v\n after  %+v", before, after)
				}
				if len(recorder.snapshot()) != 0 {
					t.Fatalf("a refused transition must record nothing, got %v", recorder.actions())
				}
			})
		}
	}
}

// FR-024: an unlisted move leaves the product exactly as it was — the same fields,
// including updated_at — which is what an implementation that writes first and
// validates second would fail.
func TestARefusedTransitionLeavesTheProductUntouched(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	ctx := appinterface.WithActor(context.Background(), adminActor())
	seeded := seedInSellState(t, repo, constant.SellStateActive, false)
	before := storedProduct(t, repo, seeded.ID)

	_, err := maintenanceService(repo, recorder).ChangeSellState(ctx, dto.ChangeSellStateInput{
		ID: seeded.ID,
		To: constant.SellStateComingSoon,
	})
	if !errors.Is(err, domainerr.ErrProductStateTransitionInvalid) {
		t.Fatalf("expected ErrProductStateTransitionInvalid, got %v", err)
	}
	if after := storedProduct(t, repo, seeded.ID); after != before {
		t.Fatalf("the refused move edited the row:\n before %+v\n after  %+v", before, after)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused move must record nothing, got %v", recorder.actions())
	}
}

// FR-022, FR-023, quickstart 7h: a target that is not one of the four states is a
// request error naming `to`, not a transition refusal, and changes nothing.
func TestChangeSellStateRejectsAnUnknownTargetState(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	ctx := appinterface.WithActor(context.Background(), adminActor())
	seeded := seedInSellState(t, repo, constant.SellStateComingSoon, false)
	before := storedProduct(t, repo, seeded.ID)

	out, err := maintenanceService(repo, recorder).ChangeSellState(ctx, dto.ChangeSellStateInput{
		ID: seeded.ID,
		To: constant.SellState("ON_SALE"),
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldTo {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldTo, err)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("a rejected target must not answer a product, got %+v", out)
	}
	if after := storedProduct(t, repo, seeded.ID); after != before {
		t.Fatalf("a rejected target changed the row: %+v", after)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a rejected target must record nothing, got %v", recorder.actions())
	}
}

// FR-024: an unknown product is the module not-found, not a transition refusal.
func TestChangeSellStateOnAnUnknownProductAnswersNotFound(t *testing.T) {
	svc := maintenanceService(newFauxProducts(), &recordingAuditor{})

	_, err := svc.ChangeSellState(context.Background(), dto.ChangeSellStateInput{
		ID: uuid.New(),
		To: constant.SellStateActive,
	})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}
