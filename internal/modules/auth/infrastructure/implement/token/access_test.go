package token

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	issuer := NewAccessIssuer("0123456789abcdef0123456789abcdef", 15*time.Minute)
	userID := uuid.New()

	raw, expiresAt, err := issuer.Issue(userID, access.RoleCustomer)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if expiresAt.Before(time.Now()) {
		t.Fatal("expected future expiry")
	}

	claims, err := issuer.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != userID || claims.Role != access.RoleCustomer || claims.ID == "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestAccessTokenExpired(t *testing.T) {
	issuer := NewAccessIssuer("0123456789abcdef0123456789abcdef", -time.Minute)
	raw, _, err := issuer.Issue(uuid.New(), access.RoleCustomer)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := issuer.Parse(raw); !errors.Is(err, domainerr.ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestAccessTokenRejectsTampering(t *testing.T) {
	issuer := NewAccessIssuer("0123456789abcdef0123456789abcdef", 15*time.Minute)
	raw, _, _ := issuer.Issue(uuid.New(), access.RoleCustomer)

	other := NewAccessIssuer("ffffffffffffffffffffffffffffffff", 15*time.Minute)
	if _, err := other.Parse(raw); !errors.Is(err, domainerr.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for wrong secret, got %v", err)
	}

	if _, err := issuer.Parse(raw + "x"); !errors.Is(err, domainerr.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for malformed token, got %v", err)
	}
}
