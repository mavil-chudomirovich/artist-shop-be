package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// refreshTokenBytes is the entropy of an opaque refresh token.
const refreshTokenBytes = 32

// Generator implements domain.RefreshTokens with opaque random tokens.
type Generator struct{}

// Generate returns a random opaque refresh token and its storage hash.
func (Generator) Generate() (raw string, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, Generator{}.Hash(raw), nil
}

// Hash returns the hex SHA-256 of a refresh token for storage.
func (Generator) Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
