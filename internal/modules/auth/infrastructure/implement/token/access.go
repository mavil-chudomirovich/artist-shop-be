package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// accessClaims are the JWT claims carried by an access token.
type accessClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// AccessIssuer implements appinterface.AccessTokens with HS256 JWTs.
type AccessIssuer struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// NewAccessIssuer creates an access-token issuer.
func NewAccessIssuer(secret string, ttl time.Duration) *AccessIssuer {
	return &AccessIssuer{secret: []byte(secret), ttl: ttl, issuer: "artist-shop"}
}

// TTL returns the configured access-token lifetime.
func (a *AccessIssuer) TTL() time.Duration { return a.ttl }

// Issue creates a signed access token for the account.
func (a *AccessIssuer) Issue(userID uuid.UUID, role access.Role) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(a.ttl)
	claims := accessClaims{
		Role: string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			ID:        uuid.NewString(),
			Issuer:    a.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse verifies a raw access token and returns domain claims.
func (a *AccessIssuer) Parse(raw string) (appinterface.Claims, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, domainerr.ErrInvalidToken
		}
		return a.secret, nil
	}, jwt.WithIssuer(a.issuer), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return appinterface.Claims{}, domainerr.ErrExpiredToken
		}
		return appinterface.Claims{}, fmt.Errorf("%w: %v", domainerr.ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}

	subject, err := uuid.Parse(claims.Subject)
	if err != nil {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}
	issuedAt := time.Time{}
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}
	return appinterface.Claims{
		Subject:  subject,
		Role:     access.Role(claims.Role),
		ID:       claims.ID,
		IssuedAt: issuedAt,
	}, nil
}
