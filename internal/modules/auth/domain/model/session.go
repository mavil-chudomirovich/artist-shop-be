package model

import (
	"time"

	"github.com/google/uuid"
)

// Session is a durable login session (refresh-token record).
type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	RotatedFrom      *uuid.UUID
	UserAgent        string
	IP               string
	CreatedAt        time.Time
}

// ResetRequest is a pending password reset.
type ResetRequest struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}
