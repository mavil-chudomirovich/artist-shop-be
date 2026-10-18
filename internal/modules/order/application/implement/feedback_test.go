package implement

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of US5: the customer's one order-level
// feedback. SubmitFeedback stores one row for the owner of a delivered or
// completed order, refuses any earlier state naming it, refuses a second feedback,
// validates the content (1 to 2000 characters after trimming), and answers another
// customer's order with the same not-found an unknown one answers (FR-015 to
// FR-018). It runs the real use case over an in-memory repository, so the answers
// asserted are the service's own.

// feedbackOrders is the in-memory OrderRepository the feedback use case uses. Its
// row lock returns a copy — the way the postgres adapter scans a fresh row — and it
// keeps the stored feedback rows, so a test can assert what was written and, just
// as importantly, that a refusal wrote nothing. duplicateOnInsert models a
// concurrent submission that passed the "no feedback yet" read: the storage unique
// index rejects its insert (FR-016, research D8).
type feedbackOrders struct {
	appinterface.OrderRepository
	byID              map[uuid.UUID]*model.Order
	feedback          map[uuid.UUID]model.Feedback
	inserts           []model.Feedback
	duplicateOnInsert bool
}

func (r *feedbackOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	clone := *order
	return &clone, nil
}

func (r *feedbackOrders) FindFeedback(_ context.Context, orderID uuid.UUID) (*model.Feedback, error) {
	feedback, ok := r.feedback[orderID]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	clone := feedback
	return &clone, nil
}

func (r *feedbackOrders) InsertFeedback(_ context.Context, feedback model.Feedback) error {
	if r.duplicateOnInsert {
		return domainerr.ErrFeedbackAlreadySent
	}
	if _, exists := r.feedback[feedback.OrderID]; exists {
		return domainerr.ErrFeedbackAlreadySent
	}
	r.feedback[feedback.OrderID] = feedback
	r.inserts = append(r.inserts, feedback)
	return nil
}

// feedbackFixture is the real feedback use case over in-memory fakes.
type feedbackFixture struct {
	svc    *Service
	orders *feedbackOrders
}

func newFeedbackFixture(orders ...*model.Order) *feedbackFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	store := &feedbackOrders{
		byID:     byID,
		feedback: map[uuid.UUID]model.Feedback{},
	}
	svc := New(Service{
		Orders: store,
		Tx:     passthroughTx{},
		Clock:  memoryClock{},
		Mapper: mapper.New(),
	})
	return &feedbackFixture{svc: svc, orders: store}
}

// completedOrder builds a COMPLETED order owned by owner, ready for feedback.
func completedOrder(owner uuid.UUID) *model.Order {
	order := deliveredOrder(owner)
	order.Status = constant.StatusCompleted
	return order
}

// FR-015, FR-017, FR-018: the owner may send one feedback once the order is
// DELIVERED or COMPLETED; it is stored trimmed, author from the session, stamped
// from the injected clock, and answered attached to the order.
func TestSubmitFeedbackStoresOneRowForADeliveredOrCompletedOrder(t *testing.T) {
	cases := []struct {
		name  string
		order func(owner uuid.UUID) *model.Order
	}{
		{"delivered", deliveredOrder},
		{"completed", completedOrder},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := uuid.New()
			order := tc.order(owner)
			f := newFeedbackFixture(order)

			view, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
				OrderID: order.ID,
				Content: "  Giao hàng cẩn thận  ",
			})
			if err != nil {
				t.Fatalf("SubmitFeedback: %v", err)
			}
			if view.Feedback == nil {
				t.Fatal("the answer must carry the recorded feedback")
			}
			if view.Feedback.Content != "Giao hàng cẩn thận" {
				t.Fatalf("the content must be stored trimmed, got %q", view.Feedback.Content)
			}
			if !view.Feedback.CreatedAt.Equal(fixedNow) {
				t.Fatalf("the feedback must be stamped from the injected clock, got %v", view.Feedback.CreatedAt)
			}

			if len(f.orders.inserts) != 1 {
				t.Fatalf("exactly one feedback row must be inserted, got %d", len(f.orders.inserts))
			}
			stored := f.orders.inserts[0]
			if stored.OrderID != order.ID {
				t.Fatalf("the row must name the order, got %s", stored.OrderID)
			}
			if stored.AuthorID != owner {
				t.Fatalf("the author must be the session's account %s, got %s", owner, stored.AuthorID)
			}
			if stored.Content != "Giao hàng cẩn thận" || !stored.CreatedAt.Equal(fixedNow) {
				t.Fatalf("the stored feedback does not match the answer: %+v", stored)
			}
		})
	}
}

// FR-015: content is counted in characters after trimming, and the 2000-character
// ceiling is inclusive.
func TestSubmitFeedbackAcceptsTheCharacterCeiling(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)

	content := strings.Repeat("ấ", model.MaxFeedbackLength)
	if _, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
		OrderID: order.ID,
		Content: content,
	}); err != nil {
		t.Fatalf("a 2000-character message must be accepted, got %v", err)
	}
	if len(f.orders.inserts) != 1 || f.orders.inserts[0].Content != content {
		t.Fatalf("the accepted message must be stored in full, got %+v", f.orders.inserts)
	}
}

// FR-015, FR-003: feedback before delivery is refused naming the current state,
// and nothing is written.
func TestSubmitFeedbackRefusesAnOrderThatIsNotDelivered(t *testing.T) {
	for _, status := range []constant.Status{
		constant.StatusPending,
		constant.StatusPaymentPending,
		constant.StatusPaid,
		constant.StatusShipping,
		constant.StatusCancelled,
	} {
		owner := uuid.New()
		order := orderAt(owner, fixedNow, line(uuid.New(), 1000, 1))
		order.Status = status
		f := newFeedbackFixture(order)

		_, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
			OrderID: order.ID,
			Content: "Sớm quá",
		})

		var refusal *domainerr.FeedbackNotAllowedError
		if !errors.As(err, &refusal) {
			t.Fatalf("status %s: expected a feedback-not-allowed refusal, got %v", status, err)
		}
		if refusal.From != status {
			t.Fatalf("status %s: the refusal must name the current state, got %s", status, refusal.From)
		}
		if len(f.orders.inserts) != 0 {
			t.Fatalf("status %s: a refused feedback must write nothing, got %+v", status, f.orders.inserts)
		}
	}
}

// FR-016: an order takes one feedback; a second submission is refused and does not
// overwrite or duplicate the first.
func TestSubmitFeedbackRefusesASecondFeedback(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)
	f.orders.feedback[order.ID] = model.Feedback{
		ID:        uuid.New(),
		OrderID:   order.ID,
		AuthorID:  owner,
		Content:   "Đã gửi trước đó",
		CreatedAt: fixedNow,
	}

	_, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
		OrderID: order.ID,
		Content: "Gửi lần hai",
	})
	if !errors.Is(err, domainerr.ErrFeedbackAlreadySent) {
		t.Fatalf("a second feedback must answer ORDER_FEEDBACK_ALREADY_SENT, got %v", err)
	}
	if len(f.orders.inserts) != 0 {
		t.Fatalf("a refused second feedback must write nothing, got %+v", f.orders.inserts)
	}
	if stored := f.orders.feedback[order.ID]; stored.Content != "Đã gửi trước đó" {
		t.Fatalf("a refused second feedback must leave the first intact, got %q", stored.Content)
	}
}

// FR-016, research D8: a submission that loses the race to a concurrent one still
// answers ORDER_FEEDBACK_ALREADY_SENT — the storage unique index rejects the insert
// and the repository surfaces it as the documented refusal.
func TestSubmitFeedbackSurfacesAConcurrentDuplicate(t *testing.T) {
	owner := uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)
	f.orders.duplicateOnInsert = true

	_, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
		OrderID: order.ID,
		Content: "Đua nhau",
	})
	if !errors.Is(err, domainerr.ErrFeedbackAlreadySent) {
		t.Fatalf("a concurrent duplicate must answer ORDER_FEEDBACK_ALREADY_SENT, got %v", err)
	}
}

// FR-015: content must carry 1 to 2000 characters after trimming; an empty, blank
// or over-long message is a validation refusal naming `content`.
func TestSubmitFeedbackRefusesInvalidContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"blank", "   \n\t "},
		{"over the ceiling", strings.Repeat("a", model.MaxFeedbackLength+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := uuid.New()
			order := deliveredOrder(owner)
			f := newFeedbackFixture(order)

			_, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
				OrderID: order.ID,
				Content: tc.content,
			})

			var invalid *domainerr.InvalidValueError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected a validation refusal, got %v", err)
			}
			if invalid.Field != model.FieldFeedbackContent {
				t.Fatalf("the refusal must name %q, got %q", model.FieldFeedbackContent, invalid.Field)
			}
			if len(f.orders.inserts) != 0 {
				t.Fatalf("a refused content must write nothing, got %+v", f.orders.inserts)
			}
		})
	}
}

// FR-018: another customer's order answers the same not-found an unknown order
// does, and is left exactly as it was.
func TestSubmitFeedbackAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	order := deliveredOrder(owner)
	f := newFeedbackFixture(order)

	_, err := f.svc.SubmitFeedback(actorContext(other), appdto.FeedbackInput{
		OrderID: order.ID,
		Content: "Không phải đơn của tôi",
	})
	if !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("another customer's order must answer not-found, got %v", err)
	}
	if len(f.orders.inserts) != 0 {
		t.Fatalf("a foreign feedback must write nothing, got %+v", f.orders.inserts)
	}

	if _, err := f.svc.SubmitFeedback(actorContext(owner), appdto.FeedbackInput{
		OrderID: uuid.New(),
		Content: "Đơn không tồn tại",
	}); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("an unknown order must answer not-found, got %v", err)
	}
}

// FR-018: a feedback reached without a session is a programming error, not a
// client-facing refusal — the transport's session guard answers UNAUTHENTICATED.
func TestSubmitFeedbackRequiresASession(t *testing.T) {
	order := deliveredOrder(uuid.New())
	f := newFeedbackFixture(order)

	if _, err := f.svc.SubmitFeedback(context.Background(), appdto.FeedbackInput{
		OrderID: order.ID,
		Content: "Không phiên",
	}); !errors.Is(err, errNoActor) {
		t.Fatalf("SubmitFeedback without a session must fail with errNoActor, got %v", err)
	}
}
