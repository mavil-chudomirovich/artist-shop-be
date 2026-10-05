// Package token implements the auth module's cryptographic adapters (password
// hashing, JWT access tokens, opaque refresh tokens) behind domain ports.
package token

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024
	argonIterations  = 1
	argonParallelism = 4
	argonSaltLength  = 16
	argonKeyLength   = 32
)

// ErrInvalidHash indicates a malformed stored password hash.
var ErrInvalidHash = errors.New("invalid password hash format")

// Hasher implements domain.PasswordHasher using Argon2id.
type Hasher struct{}

// Hash returns a self-describing Argon2id hash for the password.
func (Hasher) Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)

	encoder := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		encoder.EncodeToString(salt), encoder.EncodeToString(hash)), nil
}

// Verify reports whether the password matches the encoded hash.
func (Hasher) Verify(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrInvalidHash
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, ErrInvalidHash
	}

	encoder := base64.RawStdEncoding
	salt, err := encoder.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := encoder.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1, nil
}
