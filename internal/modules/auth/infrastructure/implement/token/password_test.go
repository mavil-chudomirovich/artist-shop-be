package token

import (
	"errors"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	var hasher Hasher

	encoded, err := hasher.Hash("Str0ng!Pass")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if encoded == "Str0ng!Pass" {
		t.Fatal("password must not be stored in plaintext")
	}

	ok, err := hasher.Verify(encoded, "Str0ng!Pass")
	if err != nil || !ok {
		t.Fatalf("expected match, got %v err %v", ok, err)
	}

	ok, err = hasher.Verify(encoded, "wrong")
	if err != nil || ok {
		t.Fatalf("expected mismatch, got %v err %v", ok, err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	var hasher Hasher
	if _, err := hasher.Verify("not-a-hash", "x"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("expected ErrInvalidHash, got %v", err)
	}
}
