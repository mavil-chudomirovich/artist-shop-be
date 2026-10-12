package model

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The reference must be decodable to the full 32 random bytes: a shorter buffer
// would lower the entropy the spec requires to make the reference unpredictable
// (FR-005).
func TestNewRegistrationIDDecodesToTheFullEntropy(t *testing.T) {
	id, err := NewRegistrationID()
	if err != nil {
		t.Fatalf("NewRegistrationID: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty reference")
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		t.Fatalf("reference must be unpadded base64url, decode failed: %v", err)
	}
	if len(raw) != RegistrationReferenceBytes {
		t.Fatalf("expected %d bytes of entropy, got %d", RegistrationReferenceBytes, len(raw))
	}
	if strings.ContainsAny(id, "+/=") {
		t.Fatalf("reference must be URL-safe and unpadded, got %q", id)
	}
}

// Two references generated back to back must not collide, and a run of them must
// stay distinct: the whole point of the reference is that it cannot be guessed
// or reused across registrations (FR-005).
func TestNewRegistrationIDIsUnpredictable(t *testing.T) {
	const runs = 1000
	seen := make(map[string]struct{}, runs)
	for i := 0; i < runs; i++ {
		id, err := NewRegistrationID()
		if err != nil {
			t.Fatalf("NewRegistrationID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate reference on run %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}
