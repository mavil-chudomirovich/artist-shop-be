// Package cli exposes auth commands as presentation for cmd/* composition roots.
package cli

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
)

// Seed provisions the single admin account via the application use case.
func Seed(ctx context.Context, svc appinterface.UserProvisioner, email, password string) error {
	return svc.ProvisionAdmin(ctx, dto.ProvisionAdminInput{Email: email, Password: password})
}
