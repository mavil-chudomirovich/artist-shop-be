// Package mapper converts between auth domain models and application DTOs. It is
// the single mapping point between the domain and the application DTOs.
package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
)

// ToIdentity maps an account to the identity DTO.
func ToIdentity(account *model.Account) dto.IdentityOutput {
	if account == nil {
		return dto.IdentityOutput{}
	}
	return dto.IdentityOutput{
		ID:    account.ID.String(),
		Email: account.Email,
		Role:  account.Role,
	}
}
