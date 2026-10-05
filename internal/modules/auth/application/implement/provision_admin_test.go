package implement

import (
	"context"
	"errors"
	"testing"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

func TestProvisionAdminCreatesActiveAdmin(t *testing.T) {
	h := newHarness()
	if err := h.svc.ProvisionAdmin(context.Background(), appdto.ProvisionAdminInput{
		Email: "Admin@Example.com ", Password: "Str0ng!Pass",
	}); err != nil {
		t.Fatalf("ProvisionAdmin: %v", err)
	}
	account, err := h.users.ByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("expected normalized admin account: %v", err)
	}
	if account.Role != access.RoleAdmin {
		t.Fatalf("expected admin role, got %s", account.Role)
	}
	if account.Status != constant.StatusActive {
		t.Fatalf("expected active, got %s", account.Status)
	}
	if account.PasswordHash != "h:Str0ng!Pass" {
		t.Fatalf("expected hashed password, got %q", account.PasswordHash)
	}
}

func TestProvisionAdminIsIdempotent(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	in := appdto.ProvisionAdminInput{Email: "admin@example.com", Password: "Str0ng!Pass"}
	if err := h.svc.ProvisionAdmin(ctx, in); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.svc.ProvisionAdmin(ctx, in); err != nil {
		t.Fatalf("second provisioning should upsert, got %v", err)
	}
}

func TestProvisionAdminRejectsWeakPassword(t *testing.T) {
	h := newHarness()
	err := h.svc.ProvisionAdmin(context.Background(), appdto.ProvisionAdminInput{
		Email: "admin@example.com", Password: "password",
	})
	if !errors.Is(err, domainerr.ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
	if _, err := h.users.ByEmail(context.Background(), model.NormalizeEmail("admin@example.com")); !errors.Is(err, domainerr.ErrUserNotFound) {
		t.Fatalf("expected no account created, got %v", err)
	}
}
