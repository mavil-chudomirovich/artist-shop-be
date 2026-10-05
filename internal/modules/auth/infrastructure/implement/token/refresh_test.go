package token

import "testing"

func TestRefreshTokenUniquenessAndHash(t *testing.T) {
	var gen Generator

	raw1, hash1, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	raw2, hash2, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if raw1 == raw2 || hash1 == hash2 {
		t.Fatal("expected unique refresh tokens")
	}
	if hash1 == raw1 {
		t.Fatal("hash must differ from the raw token")
	}
	if gen.Hash(raw1) != hash1 {
		t.Fatal("hash must be deterministic")
	}
}
