package implement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// Refresh rotates a session using a valid refresh token. A rotated-out token is
// rejected without revoking other sessions (FR-007).
func (s *Service) Refresh(ctx context.Context, in dto.RefreshInput) (dto.SessionOutput, error) {
	session, err := s.Sessions.ByTokenHash(ctx, s.RefreshTokens.Hash(in.RefreshToken))
	if err != nil {
		if errors.Is(err, domainerr.ErrSessionNotFound) {
			return dto.SessionOutput{}, domainerr.ErrInvalidToken
		}
		return dto.SessionOutput{}, err
	}
	if session.RevokedAt != nil {
		return dto.SessionOutput{}, domainerr.ErrRefreshReused
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		return dto.SessionOutput{}, domainerr.ErrExpiredToken
	}

	account, err := s.Users.ByID(ctx, session.UserID)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	if err := account.CanSignIn(); err != nil {
		return dto.SessionOutput{}, err
	}

	accessToken, _, err := s.Access.Issue(account.ID, account.Role)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	rawRefresh, refreshHash, err := s.RefreshTokens.Generate()
	if err != nil {
		return dto.SessionOutput{}, err
	}
	next := &model.Session{
		ID:               uuid.New(),
		UserID:           account.ID,
		RefreshTokenHash: refreshHash,
		ExpiresAt:        time.Now().UTC().Add(s.Config.RefreshTokenTTL),
		UserAgent:        in.UserAgent,
		IP:               in.IP,
	}
	if err := s.Sessions.Rotate(ctx, session.ID, next); err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditSessionRefreshed, constant.OutcomeSuccess, &account.ID, string(account.Role), "session", next.ID.String(), nil)
	return dto.SessionOutput{AccessToken: accessToken, RefreshToken: rawRefresh, ExpiresIn: int(s.Access.TTL().Seconds())}, nil
}

// Logout revokes the session identified by the refresh token. It is idempotent.
func (s *Service) Logout(ctx context.Context, in dto.RefreshInput) error {
	session, err := s.Sessions.ByTokenHash(ctx, s.RefreshTokens.Hash(in.RefreshToken))
	if err != nil {
		if errors.Is(err, domainerr.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	if err := s.Sessions.Revoke(ctx, session.ID); err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditSignOut, constant.OutcomeSuccess, &session.UserID, "", "session", session.ID.String(), nil)
	return nil
}
