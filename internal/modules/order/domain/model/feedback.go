package model

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// Feedback is the customer's one order-level message to the artist about a
// delivered order. It carries text only — no rating and no moderation, which
// belong to module 13's product reviews (FR-015, research D8).
type Feedback struct {
	// ID identifies the feedback.
	ID uuid.UUID
	// OrderID is the order the feedback is about. At most one feedback exists
	// per order (FR-016).
	OrderID uuid.UUID
	// AuthorID is the customer who wrote it. A loose reference with no foreign
	// key, like Order.UserID (FR-016, FR-018).
	AuthorID uuid.UUID
	// Content is the feedback text.
	Content string
	// Photos are the optional images attached to the feedback, at most five,
	// ordered by position. They are read alongside the feedback on the detail
	// paths, so they are empty when the feedback is not loaded through the
	// ordered read (FR-025, research D16).
	Photos []FeedbackPhoto
	// CreatedAt is when it was written. Set once.
	CreatedAt time.Time
}

// MaxFeedbackLength is the feedback message ceiling of FR-015, counted in
// characters after trimming. It matches the order_feedback_content_ck storage
// check, so the domain and the database agree on the bound.
const MaxFeedbackLength = 2000

// ValidateFeedbackContent decides whether a feedback message may be stored. The
// content is trimmed first, so a message of only whitespace is empty; the trimmed
// value must then carry between 1 and MaxFeedbackLength characters, counted as
// runes so the bound matches the storage check's character count rather than a
// byte count that would refuse an accented Vietnamese message early (FR-015,
// research D8).
func ValidateFeedbackContent(content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return domainerr.InvalidValue(FieldFeedbackContent, "must not be empty")
	}
	if utf8.RuneCountInString(trimmed) > MaxFeedbackLength {
		return domainerr.InvalidValue(FieldFeedbackContent, "must be at most 2000 characters")
	}
	return nil
}
