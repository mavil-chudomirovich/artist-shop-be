package implement

import (
	"context"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file fans out the order module's live SSE events. Every helper is
// best-effort and runs AFTER the transaction committed, next to the matching
// notify* email helper in notify.go: a nil publisher skips silently, and a
// publisher failure never fails the order operation it describes (FR-005).

// publishNeedsConfirmation tells admins an order entered PENDING.
func (s *Service) publishNeedsConfirmation(ctx context.Context, order *model.Order) {
	if s.Events == nil || order == nil {
		return
	}
	s.Events.Publish(ctx, appinterface.TopicAdmins,
		appinterface.EventNeedsConfirmation,
		appinterface.EncodeNeedsConfirmation(order.ID, order.Version, s.now()))
}

// publishStatusChanged tells the order's owner it moved from one state to
// another. The owner is the order's, never an input, so an event never leaks
// to another customer.
func (s *Service) publishStatusChanged(ctx context.Context, order *model.Order, from constant.Status) {
	if s.Events == nil || order == nil {
		return
	}
	s.Events.Publish(ctx, appinterface.TopicForUser(order.UserID),
		appinterface.EventStatusChanged,
		appinterface.EncodeStatusChanged(order.ID, from, order.Status, order.Version, s.now()))
}

// publishFeedbackReceived tells admins an order carries feedback.
func (s *Service) publishFeedbackReceived(ctx context.Context, order *model.Order) {
	if s.Events == nil || order == nil {
		return
	}
	s.Events.Publish(ctx, appinterface.TopicAdmins,
		appinterface.EventFeedbackReceived,
		appinterface.EncodeFeedbackReceived(order.ID, s.now()))
}
