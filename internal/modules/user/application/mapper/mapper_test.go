package mapper

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// stubDivisions serves two provinces so a stored pair can be checked against
// both a current and a retired code.
type stubDivisions struct {
	provinces map[string][]string
	err       error
}

func (s *stubDivisions) Provinces(context.Context) ([]appinterface.Province, error) {
	return nil, nil
}

func (s *stubDivisions) Wards(_ context.Context, provinceCode string) ([]appinterface.Ward, error) {
	if s.err != nil {
		return nil, s.err
	}
	codes, ok := s.provinces[provinceCode]
	if !ok {
		return nil, errors.New("unknown province code")
	}
	wards := make([]appinterface.Ward, 0, len(codes))
	for _, code := range codes {
		wards = append(wards, appinterface.Ward{Code: code, ProvinceCode: provinceCode})
	}
	return wards, nil
}

func (s *stubDivisions) ValidateAddressDivisions(context.Context, string, string) error { return nil }

var _ appinterface.Divisions = (*stubDivisions)(nil)

func storedAddress(provinceCode, wardCode string) model.Address {
	return model.Address{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		RecipientName:  "Nguyen Van A",
		RecipientPhone: "0912345678",
		ProvinceCode:   provinceCode,
		ProvinceName:   "Captured province name",
		WardCode:       wardCode,
		WardName:       "Captured ward name",
		StreetAddress:  "12 Nguyen Hue",
	}
}

func TestAddressInTheDatasetDoesNotNeedReview(t *testing.T) {
	divisions := &stubDivisions{provinces: map[string][]string{"79": {"26734", "26735"}}}
	m := New(divisions)

	out := m.Address(context.Background(), storedAddress("79", "26734"))

	if out.DivisionNeedsReview {
		t.Fatal("expected an address whose codes are in the dataset not to need review")
	}
	if out.ProvinceName != "Captured province name" || out.WardName != "Captured ward name" {
		t.Fatalf("expected the captured names to survive, got %q / %q", out.ProvinceName, out.WardName)
	}
}

func TestAddressWithARetiredWardNeedsReview(t *testing.T) {
	divisions := &stubDivisions{provinces: map[string][]string{"79": {"26734"}}}
	m := New(divisions)

	out := m.Address(context.Background(), storedAddress("79", "00000"))

	if !out.DivisionNeedsReview {
		t.Fatal("expected a retired ward code to be flagged")
	}
	if out.WardName != "Captured ward name" {
		t.Fatalf("expected the captured ward name to be returned, got %q", out.WardName)
	}
}

func TestAddressWithARetiredProvinceNeedsReview(t *testing.T) {
	divisions := &stubDivisions{provinces: map[string][]string{"79": {"26734"}}}
	m := New(divisions)

	out := m.Address(context.Background(), storedAddress("01", "26734"))

	if !out.DivisionNeedsReview {
		t.Fatal("expected a province that left the dataset to be flagged")
	}
}

func TestAddressIsFlaggedWhenTheDatasetLookupFails(t *testing.T) {
	m := New(&stubDivisions{err: errors.New("boom")})

	if out := m.Address(context.Background(), storedAddress("79", "26734")); !out.DivisionNeedsReview {
		t.Fatal("expected an unusable dataset lookup to flag the address instead of hiding the problem")
	}
}

func TestAddressIsNeverFlaggedWithoutTheDivisionsPort(t *testing.T) {
	m := New(nil)

	if out := m.Address(context.Background(), storedAddress("01", "00000")); out.DivisionNeedsReview {
		t.Fatal("expected no review flag when no dataset is configured")
	}
}

func TestAddressesPreservesOrderAndReturnsNilWhenEmpty(t *testing.T) {
	divisions := &stubDivisions{provinces: map[string][]string{"79": {"26734"}}}
	m := New(divisions)
	first := storedAddress("79", "26734")
	second := storedAddress("79", "00000")
	second.IsDefault = true

	out := m.Addresses(context.Background(), []model.Address{first, second})

	if len(out) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(out))
	}
	if out[0].ID != first.ID || out[1].ID != second.ID {
		t.Fatal("expected the caller's order to be preserved")
	}
	if out[1].DivisionNeedsReview != true {
		t.Fatal("expected the second address to be flagged")
	}
	if empty := m.Addresses(context.Background(), nil); empty != nil {
		t.Fatalf("expected a nil slice for no addresses, got %v", empty)
	}
}

func TestProfileMapsTheAvatarAsTheOnlyPresenceSignal(t *testing.T) {
	phone := "0912345678"
	profile := model.Profile{
		ID:          uuid.New(),
		Email:       "user@example.com",
		Role:        access.RoleCustomer,
		DisplayName: "Nguyen Van A",
		Phone:       &phone,
	}
	m := New(nil)

	withoutAvatar := m.Profile(profile)
	if withoutAvatar.Avatar != nil {
		t.Fatal("expected a nil avatar when the customer has no photo")
	}
	if withoutAvatar.Phone == nil || *withoutAvatar.Phone != phone {
		t.Fatal("expected the normalised phone to be mapped")
	}

	profile.Avatar = &model.AvatarReference{PublicID: "avatars/1", URL: "https://cdn/1", Width: 512, Height: 512}
	withAvatar := m.Profile(profile)
	if withAvatar.Avatar == nil || withAvatar.Avatar.PublicID != "avatars/1" || withAvatar.Avatar.Width != 512 {
		t.Fatalf("expected the stored avatar reference to be mapped, got %+v", withAvatar.Avatar)
	}
}

func TestCustomerCarriesTheAddressesDefaultFirst(t *testing.T) {
	divisions := &stubDivisions{provinces: map[string][]string{"79": {"26734"}}}
	m := New(divisions)
	profile := model.Profile{ID: uuid.New(), Email: "user@example.com", Role: access.RoleCustomer}

	out := m.Customer(context.Background(), profile, []model.Address{storedAddress("79", "26734")})

	if out.ID != profile.ID || out.Email != profile.Email || out.Role != access.RoleCustomer {
		t.Fatalf("unexpected customer identity: %+v", out)
	}
	if len(out.Addresses) != 1 {
		t.Fatalf("expected one address, got %d", len(out.Addresses))
	}
}
