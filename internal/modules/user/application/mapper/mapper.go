// Package mapper converts between user domain models and application DTOs. It is
// the single mapping point between the domain and the application DTOs.
package mapper

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// Mapper converts persisted models into application DTOs. It needs the Divisions
// port because resolving whether a stored province or ward code still exists is
// a read of the official dataset, and the domain may not touch that dataset
// (Constitution I).
type Mapper struct {
	divisions appinterface.Divisions
}

// New creates a Mapper. A nil divisions port disables the dataset lookup, in
// which case no address is ever reported as needing review.
func New(divisions appinterface.Divisions) *Mapper {
	return &Mapper{divisions: divisions}
}

// Profile maps the profile entity to its DTO. A nil avatar reference maps to a
// nil avatar, which is the only signal that the customer has no photo.
func (m *Mapper) Profile(profile model.Profile) dto.ProfileOutput {
	return dto.ProfileOutput{
		ID:          profile.ID,
		Email:       profile.Email,
		Role:        profile.Role,
		DisplayName: profile.DisplayName,
		Phone:       profile.Phone,
		Avatar:      avatarOutput(profile.Avatar),
	}
}

// Address maps one address, resolving whether its divisions are still in the
// official dataset (research D10).
func (m *Mapper) Address(ctx context.Context, address model.Address) dto.AddressOutput {
	return dto.AddressOutput{
		ID:                  address.ID,
		RecipientName:       address.RecipientName,
		RecipientPhone:      address.RecipientPhone,
		ProvinceCode:        address.ProvinceCode,
		ProvinceName:        address.ProvinceName,
		WardCode:            address.WardCode,
		WardName:            address.WardName,
		StreetAddress:       address.StreetAddress,
		IsDefault:           address.IsDefault,
		DivisionNeedsReview: m.divisionNeedsReview(ctx, address.ProvinceCode, address.WardCode),
	}
}

// Addresses maps a list of addresses, preserving the caller's order — default
// address first, then most recently updated (FR-007d). An empty list maps to a
// nil slice, never to an allocated empty one.
func (m *Mapper) Addresses(ctx context.Context, list []model.Address) []dto.AddressOutput {
	if len(list) == 0 {
		return nil
	}
	out := make([]dto.AddressOutput, 0, len(list))
	for _, address := range list {
		out = append(out, m.Address(ctx, address))
	}
	return out
}

// Customer maps a profile and its addresses into the read-only administrator
// view of a customer.
func (m *Mapper) Customer(ctx context.Context, profile model.Profile, list []model.Address) dto.CustomerLookupOutput {
	return dto.CustomerLookupOutput{
		ID:          profile.ID,
		Email:       profile.Email,
		Role:        profile.Role,
		DisplayName: profile.DisplayName,
		Phone:       profile.Phone,
		Addresses:   m.Addresses(ctx, list),
	}
}

func avatarOutput(avatar *model.AvatarReference) *dto.AvatarOutput {
	if avatar == nil {
		return nil
	}
	return &dto.AvatarOutput{
		PublicID: avatar.PublicID,
		URL:      avatar.URL,
		Width:    avatar.Width,
		Height:   avatar.Height,
	}
}

// divisionNeedsReview reports whether a stored province or ward code has left the
// official dataset.
//
// A missing code must never invalidate a stored address: administrative units
// get merged and renamed, so the address keeps its captured names and is only
// flagged for remapping (research D10). The province is asked for first
// because a ward list is always scoped to one province, so an unknown province
// is answered by the very lookup that would list its wards.
func (m *Mapper) divisionNeedsReview(ctx context.Context, provinceCode, wardCode string) bool {
	if m.divisions == nil {
		return false
	}
	wards, err := m.divisions.Wards(ctx, provinceCode)
	if err != nil {
		return true
	}
	for _, ward := range wards {
		if ward.Code == wardCode {
			return false
		}
	}
	return true
}
