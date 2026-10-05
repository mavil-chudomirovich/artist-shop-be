package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
)

type stubProvisioner struct {
	called bool
	input  dto.ProvisionAdminInput
	err    error
}

func (s *stubProvisioner) ProvisionAdmin(_ context.Context, in dto.ProvisionAdminInput) error {
	s.called = true
	s.input = in
	return s.err
}

func TestSeedDelegatesToUseCase(t *testing.T) {
	stub := &stubProvisioner{}
	if err := Seed(context.Background(), stub, "admin@example.com", "Str0ng!Pass"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if !stub.called {
		t.Fatal("expected ProvisionAdmin to be called")
	}
	if stub.input.Email != "admin@example.com" || stub.input.Password != "Str0ng!Pass" {
		t.Fatalf("unexpected input: %+v", stub.input)
	}
}

func TestSeedPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	if err := Seed(context.Background(), &stubProvisioner{err: boom}, "admin@example.com", "weak"); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
}
