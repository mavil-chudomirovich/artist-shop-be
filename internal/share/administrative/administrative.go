// Package administrative serves the bundled official Vietnamese administrative
// dataset (province -> ward) as read-only reference data.
//
// The JSON file is embedded in the binary and parsed into an index on first
// use (ADR-002), so a lookup costs no database round trip and no code path can
// bypass it by forgetting a foreign key. The dataset is two levels only: the
// district level was abolished nationwide on 2025-07-01.
//
// This package owns its sentinel errors and imports no module, so the user,
// order, shipping and commission modules may all read it.
package administrative

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data/vn-divisions.json
var datasetJSON []byte

// Province is one first-level administrative unit.
type Province struct {
	Code string
	Name string
}

// Ward is one second-level administrative unit. ProvinceCode is carried on the
// ward so a consumer never has to join back to the province.
type Ward struct {
	Code         string
	Name         string
	ProvinceCode string
}

// datasetCounts mirrors the "counts" block of the bundled file.
type datasetCounts struct {
	Provinces int `json:"provinces"`
	Wards     int `json:"wards"`
}

// datasetProvince mirrors one entry of the "provinces" array, wards included.
type datasetProvince struct {
	Code  string        `json:"code"`
	Name  string        `json:"name"`
	Wards []datasetWard `json:"wards"`
}

// datasetWard mirrors one entry of a province's "wards" array.
type datasetWard struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// dataset mirrors the whole bundled file. The _provenance block is read by the
// refresh procedure in docs/configuration.md and is not needed at runtime.
type dataset struct {
	Counts    datasetCounts     `json:"counts"`
	Provinces []datasetProvince `json:"provinces"`
}

// catalog is the in-memory index built once from the embedded JSON.
type catalog struct {
	provinces       []Province
	provinceByCode  map[string]Province
	wardsByProvince map[string][]Ward
	wardByCode      map[string]Ward
}

// catalogOnce builds the index at most once, on first use.
var catalogOnce = sync.OnceValue(buildCatalog)

// buildCatalog parses the embedded file and builds the lookup indexes. A
// failure here means the binary itself is broken — the JSON is compiled in and
// its integrity is asserted by tests — so it panics instead of returning an
// error that every caller would have to handle.
func buildCatalog() *catalog {
	var doc dataset
	if err := json.Unmarshal(datasetJSON, &doc); err != nil {
		panic(fmt.Sprintf("administrative: embedded dataset is not valid JSON: %v", err))
	}

	c := &catalog{
		provinces:       make([]Province, 0, len(doc.Provinces)),
		provinceByCode:  make(map[string]Province, len(doc.Provinces)),
		wardsByProvince: make(map[string][]Ward, len(doc.Provinces)),
		wardByCode:      map[string]Ward{},
	}
	wardCount := 0
	for _, p := range doc.Provinces {
		province := Province{Code: p.Code, Name: p.Name}
		c.provinces = append(c.provinces, province)
		c.provinceByCode[p.Code] = province

		var wards []Ward
		for _, w := range p.Wards {
			ward := Ward{Code: w.Code, Name: w.Name, ProvinceCode: p.Code}
			wards = append(wards, ward)
			c.wardByCode[w.Code] = ward
		}
		c.wardsByProvince[p.Code] = wards
		wardCount += len(wards)
	}

	// The recorded counts travel with the file so a refresh that forgets to
	// update them fails loudly instead of shipping a dataset nobody verified.
	if doc.Counts.Provinces != len(c.provinces) || doc.Counts.Wards != wardCount {
		panic(fmt.Sprintf("administrative: dataset counts (%d provinces, %d wards) do not match the data (%d provinces, %d wards)",
			doc.Counts.Provinces, doc.Counts.Wards, len(c.provinces), wardCount))
	}
	return c
}

// Provinces returns every province in the dataset, in the order the file lists
// them. The result is a copy, so a caller cannot corrupt the shared index.
func Provinces() []Province {
	c := catalogOnce()
	out := make([]Province, len(c.provinces))
	copy(out, c.provinces)
	return out
}

// Wards returns the wards of one province, in the order the file lists them. A
// ward list is always scoped to a single province, so clients never receive the
// whole set at once. It returns ErrUnknownProvince for an unknown code.
func Wards(provinceCode string) ([]Ward, error) {
	c := catalogOnce()
	wards, ok := c.wardsByProvince[provinceCode]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvince, provinceCode)
	}
	out := make([]Ward, len(wards))
	copy(out, wards)
	return out, nil
}

// FindProvince returns one province by code, or ErrUnknownProvince when the code is
// absent. Consumers use it to capture the display name alongside the stored code
// (research D10).
func FindProvince(provinceCode string) (Province, error) {
	c := catalogOnce()
	province, ok := c.provinceByCode[provinceCode]
	if !ok {
		return Province{}, fmt.Errorf("%w: %q", ErrUnknownProvince, provinceCode)
	}
	return province, nil
}

// FindWard returns one ward by code, or ErrUnknownWard when the code is absent.
func FindWard(wardCode string) (Ward, error) {
	c := catalogOnce()
	ward, ok := c.wardByCode[wardCode]
	if !ok {
		return Ward{}, fmt.Errorf("%w: %q", ErrUnknownWard, wardCode)
	}
	return ward, nil
}

// Validate checks that the province and the ward both exist and that the ward
// belongs to the province. It returns ErrUnknownProvince, ErrUnknownWard or
// ErrWardProvinceMismatch; the province is checked first because a client whose
// first select is stale cannot produce a meaningful ward.
func Validate(provinceCode, wardCode string) error {
	if _, err := FindProvince(provinceCode); err != nil {
		return err
	}
	ward, err := FindWard(wardCode)
	if err != nil {
		return err
	}
	if ward.ProvinceCode != provinceCode {
		return fmt.Errorf("%w: ward %q belongs to province %q, not %q",
			ErrWardProvinceMismatch, ward.Code, ward.ProvinceCode, provinceCode)
	}
	return nil
}
