package implement

import (
	"context"
	"errors"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// Login authenticates an account and issues a token pair, applying the failed
// attempt lockout.
func (s *Service) Login(ctx context.Context, in dto.LoginInput) (dto.SessionOutput, error) {
	normalized := model.NormalizeEmail(in.Email)

	blocked, err := s.Guard.Blocked(ctx, in.Source)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	if blocked {
		return dto.SessionOutput{}, domainerr.ErrAccountLocked
	}

	account, err := s.Users.ByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domainerr.ErrUserNotFound) {
			_, _ = s.Guard.RecordFailure(ctx, in.Source)
			s.Audit.Record(ctx, constant.AuditSignInFailed, constant.OutcomeFailure, nil, "", "user", "", map[string]any{"reason": "unknown_email"})
			return dto.SessionOutput{}, domainerr.ErrInvalidCredentials
		}
		return dto.SessionOutput{}, err
	}

	ok, err := s.Hasher.Verify(account.PasswordHash, in.Password)
	if err != nil || !ok {
		nowBlocked, _ := s.Guard.RecordFailure(ctx, in.Source)
		s.Audit.Record(ctx, constant.AuditSignInFailed, constant.OutcomeFailure, &account.ID, string(account.Role), "user", account.ID.String(), nil)
		if nowBlocked {
			return dto.SessionOutput{}, domainerr.ErrAccountLocked
		}
		return dto.SessionOutput{}, domainerr.ErrInvalidCredentials
	}

	if err := account.CanSignIn(); err != nil {
		s.Audit.Record(ctx, constant.AuditSignInFailed, constant.OutcomeFailure, &account.ID, string(account.Role), "user", account.ID.String(), map[string]any{"status": string(account.Status)})
		return dto.SessionOutput{}, err
	}

	if err := s.Guard.Reset(ctx, in.Source); err != nil {
		return dto.SessionOutput{}, err
	}

	session, issued, err := s.issueSession(ctx, account, time.Now().UTC().Add(s.Config.RefreshTokenTTL), in.UserAgent, in.IP)
	if err != nil {
		return dto.SessionOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditSignInSucceeded, constant.OutcomeSuccess, &account.ID, string(account.Role), "session", issued.ID.String(), nil)
	return session, nil
}
