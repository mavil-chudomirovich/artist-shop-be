package mapper

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

func TestToIdentity(t *testing.T) {
	id := uuid.New()
	account := &model.Account{
		ID:        id,
		Email:     "user@example.com",
		Role:      access.RoleAdmin,
		Status:    constant.StatusActive,
		CreatedAt: time.Now(),
	}

	out := ToIdentity(account)
	if out.ID != id.String() || out.Email != "user@example.com" || out.Role != access.RoleAdmin {
		t.Fatalf("unexpected identity: %+v", out)
	}
}

func TestToIdentityNil(t *testing.T) {
	if out := ToIdentity(nil); out.ID != "" || out.Email != "" || out.Role != "" {
		t.Fatalf("expected zero identity, got %+v", out)
	}
}
