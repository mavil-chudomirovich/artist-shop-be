package administrative

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
)

// storedPair returns a province with at least one ward, read straight from the
// bundled dataset so the test never validates the adapter against itself.
func storedPair(t *testing.T) (provinceCode, wardCode string) {
	t.Helper()
	for _, province := range administrative.Provinces() {
		wards, err := administrative.Wards(province.Code)
		if err != nil || len(wards) == 0 {
			continue
		}
		return province.Code, wards[0].Code
	}
	t.Fatal("bundled dataset has no province with wards")
	return "", ""
}

func TestProvincesAreOrderedByName(t *testing.T) {
	provinces, err := New().Provinces(context.Background())
	if err != nil {
		t.Fatalf("Provinces: %v", err)
	}
	if len(provinces) == 0 {
		t.Fatal("expected provinces, got none")
	}
	if !sort.SliceIsSorted(provinces, func(i, j int) bool { return provinces[i].Name < provinces[j].Name }) {
		t.Fatal("expected the province list ordered by name")
	}
	if provinces[0].Code == "" || provinces[0].Name == "" {
		t.Fatalf("province is missing its code or name: %+v", provinces[0])
	}
}

func TestWardsAreScopedToOneProvince(t *testing.T) {
	provinceCode, wardCode := storedPair(t)

	wards, err := New().Wards(context.Background(), provinceCode)
	if err != nil {
		t.Fatalf("Wards(%q): %v", provinceCode, err)
	}
	if len(wards) == 0 {
		t.Fatal("expected wards")
	}
	for _, ward := range wards {
		if ward.ProvinceCode != provinceCode {
			t.Fatalf("ward %q belongs to province %q, expected %q", ward.Code, ward.ProvinceCode, provinceCode)
		}
	}
	if wards[0].Code != wardCode {
		t.Fatalf("expected the dataset order to be preserved, got %q first", wards[0].Code)
	}
}

func TestWardsReportsAnUnknownProvince(t *testing.T) {
	if _, err := New().Wards(context.Background(), "does-not-exist"); !errors.Is(err, administrative.ErrUnknownProvince) {
		t.Fatalf("expected ErrUnknownProvince, got %v", err)
	}
}

func TestValidateReportsTheDatasetErrorsThroughThePort(t *testing.T) {
	provinceCode, wardCode := storedPair(t)
	var otherProvince, otherWard string
	for _, province := range administrative.Provinces() {
		if province.Code == provinceCode {
			continue
		}
		wards, err := administrative.Wards(province.Code)
		if err != nil || len(wards) == 0 {
			continue
		}
		otherProvince, otherWard = province.Code, wards[0].Code
		break
	}
	if otherProvince == "" {
		t.Skip("bundled dataset has fewer than two provinces with wards")
	}

	if err := New().ValidateAddressDivisions(context.Background(), provinceCode, wardCode); err != nil {
		t.Fatalf("expected the stored pair to validate, got %v", err)
	}
	if err := New().ValidateAddressDivisions(context.Background(), "does-not-exist", wardCode); !errors.Is(err, administrative.ErrUnknownProvince) {
		t.Fatalf("expected ErrUnknownProvince, got %v", err)
	}
	if err := New().ValidateAddressDivisions(context.Background(), provinceCode, "does-not-exist"); !errors.Is(err, administrative.ErrUnknownWard) {
		t.Fatalf("expected ErrUnknownWard, got %v", err)
	}
	if err := New().ValidateAddressDivisions(context.Background(), provinceCode, otherWard); !errors.Is(err, administrative.ErrWardProvinceMismatch) {
		t.Fatalf("expected ErrWardProvinceMismatch, got %v", err)
	}
	if err := New().ValidateAddressDivisions(context.Background(), otherProvince, wardCode); !errors.Is(err, administrative.ErrWardProvinceMismatch) {
		t.Fatalf("expected ErrWardProvinceMismatch, got %v", err)
	}
}
