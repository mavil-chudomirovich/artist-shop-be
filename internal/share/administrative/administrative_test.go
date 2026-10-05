package administrative

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

const datasetPath = "data/vn-divisions.json"

// readDataset decodes the bundled file straight from disk, independently of the
// index the package builds, so an integrity test never validates the loader
// against itself.
func readDataset(t *testing.T) dataset {
	t.Helper()
	raw, err := os.ReadFile(datasetPath)
	if err != nil {
		t.Fatalf("read %s: %v", datasetPath, err)
	}
	var doc dataset
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", datasetPath, err)
	}
	return doc
}

func TestProvincesReturnsTheWholeDataset(t *testing.T) {
	provinces := Provinces()
	if len(provinces) == 0 {
		t.Fatal("expected provinces, got none")
	}
	for _, p := range provinces {
		if p.Code == "" {
			t.Fatalf("province %q has no code", p.Name)
		}
		if strings.TrimSpace(p.Name) == "" {
			t.Fatalf("province %q has no name", p.Code)
		}
	}
}

func TestProvincesResultIsACopy(t *testing.T) {
	first := Provinces()
	first[0].Name = "tampered"

	if second := Provinces(); second[0].Name == "tampered" {
		t.Fatal("Provinces exposed the shared index to the caller")
	}
}

func TestWardsReturnsOnlyTheRequestedProvince(t *testing.T) {
	doc := readDataset(t)
	if len(doc.Provinces) == 0 {
		t.Fatal("dataset has no provinces")
	}
	want := doc.Provinces[0]

	wards, err := Wards(want.Code)
	if err != nil {
		t.Fatalf("Wards(%q): %v", want.Code, err)
	}
	if len(wards) != len(want.Wards) {
		t.Fatalf("expected %d wards, got %d", len(want.Wards), len(wards))
	}
	for i, ward := range wards {
		if ward.Code != want.Wards[i].Code || ward.Name != want.Wards[i].Name {
			t.Fatalf("ward %d: expected %q/%q, got %q/%q",
				i, want.Wards[i].Code, want.Wards[i].Name, ward.Code, ward.Name)
		}
		if ward.ProvinceCode != want.Code {
			t.Fatalf("ward %q reported province %q, expected %q", ward.Code, ward.ProvinceCode, want.Code)
		}
	}
}

func TestWardsRejectsAnUnknownProvince(t *testing.T) {
	wards, err := Wards("does-not-exist")
	if !errors.Is(err, ErrUnknownProvince) {
		t.Fatalf("expected ErrUnknownProvince, got %v", err)
	}
	if wards != nil {
		t.Fatalf("expected no wards, got %v", wards)
	}
}

func TestProvinceAndWardLookupRejectUnknownCodes(t *testing.T) {
	if _, err := FindProvince("does-not-exist"); !errors.Is(err, ErrUnknownProvince) {
		t.Fatalf("expected ErrUnknownProvince, got %v", err)
	}
	if _, err := FindWard("does-not-exist"); !errors.Is(err, ErrUnknownWard) {
		t.Fatalf("expected ErrUnknownWard, got %v", err)
	}
}

func TestValidateAcceptsAStoredPair(t *testing.T) {
	doc := readDataset(t)
	for _, p := range doc.Provinces {
		if len(p.Wards) == 0 {
			continue
		}
		if err := Validate(p.Code, p.Wards[0].Code); err != nil {
			t.Fatalf("Validate(%q, %q): %v", p.Code, p.Wards[0].Code, err)
		}
		return
	}
	t.Fatal("dataset has no province with wards")
}

func TestValidateRejectsAnUnknownProvince(t *testing.T) {
	doc := readDataset(t)
	wardCode := doc.Provinces[0].Wards[0].Code

	err := Validate("does-not-exist", wardCode)
	if !errors.Is(err, ErrUnknownProvince) {
		t.Fatalf("expected ErrUnknownProvince, got %v", err)
	}
}

func TestValidateRejectsAnUnknownWard(t *testing.T) {
	doc := readDataset(t)

	err := Validate(doc.Provinces[0].Code, "does-not-exist")
	if !errors.Is(err, ErrUnknownWard) {
		t.Fatalf("expected ErrUnknownWard, got %v", err)
	}
}

func TestValidateRejectsAWardFromAnotherProvince(t *testing.T) {
	doc := readDataset(t)
	var provinceCode, otherWardCode string
	for _, p := range doc.Provinces {
		if len(p.Wards) == 0 {
			continue
		}
		if provinceCode == "" {
			provinceCode = p.Code
			continue
		}
		otherWardCode = p.Wards[0].Code
		break
	}
	if otherWardCode == "" {
		t.Skip("dataset has fewer than two provinces with wards")
	}

	err := Validate(provinceCode, otherWardCode)
	if !errors.Is(err, ErrWardProvinceMismatch) {
		t.Fatalf("expected ErrWardProvinceMismatch, got %v", err)
	}
}

func TestDatasetHasNoDuplicateCodes(t *testing.T) {
	doc := readDataset(t)

	seenProvinces := map[string]bool{}
	seenWards := map[string]bool{}
	for _, p := range doc.Provinces {
		if seenProvinces[p.Code] {
			t.Fatalf("duplicate province code %q", p.Code)
		}
		seenProvinces[p.Code] = true

		for _, w := range p.Wards {
			if seenWards[w.Code] {
				t.Fatalf("ward code %q appears more than once", w.Code)
			}
			seenWards[w.Code] = true
		}
	}
}

func TestEveryWardBelongsToExactlyOneProvince(t *testing.T) {
	doc := readDataset(t)

	owner := map[string]string{}
	for _, p := range doc.Provinces {
		for _, w := range p.Wards {
			if previous, duplicated := owner[w.Code]; duplicated {
				t.Fatalf("ward %q belongs to both province %q and %q", w.Code, previous, p.Code)
			}
			owner[w.Code] = p.Code

			ward, err := FindWard(w.Code)
			if err != nil {
				t.Fatalf("FindWard(%q): %v", w.Code, err)
			}
			if ward.ProvinceCode != p.Code {
				t.Fatalf("ward %q resolved to province %q, expected %q", w.Code, ward.ProvinceCode, p.Code)
			}
		}
	}

	total := 0
	for _, p := range doc.Provinces {
		total += len(p.Wards)
	}
	if len(owner) != total {
		t.Fatalf("expected %d distinct wards, indexed %d", total, len(owner))
	}
}

func TestDatasetCountsMatchTheRecordedMetadata(t *testing.T) {
	doc := readDataset(t)

	wardCount := 0
	for _, p := range doc.Provinces {
		wardCount += len(p.Wards)
	}
	if doc.Counts.Provinces != len(doc.Provinces) {
		t.Fatalf("counts.provinces is %d but the file holds %d", doc.Counts.Provinces, len(doc.Provinces))
	}
	if doc.Counts.Wards != wardCount {
		t.Fatalf("counts.wards is %d but the file holds %d", doc.Counts.Wards, wardCount)
	}

	if len(Provinces()) != doc.Counts.Provinces {
		t.Fatalf("Provinces returned %d provinces, metadata says %d", len(Provinces()), doc.Counts.Provinces)
	}
	for _, p := range doc.Provinces {
		wards, err := Wards(p.Code)
		if err != nil {
			t.Fatalf("Wards(%q): %v", p.Code, err)
		}
		if len(wards) != len(p.Wards) {
			t.Fatalf("province %q: index holds %d wards, file holds %d", p.Code, len(wards), len(p.Wards))
		}
	}
}

func TestDatasetFileIsUTF8WithoutBOMAndUsesLF(t *testing.T) {
	raw, err := os.ReadFile(datasetPath)
	if err != nil {
		t.Fatalf("read %s: %v", datasetPath, err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("dataset is not valid UTF-8")
	}
	if strings.HasPrefix(string(raw), "\uFEFF") {
		t.Fatal("dataset starts with a UTF-8 BOM")
	}
	if strings.Contains(string(raw), "\r") {
		t.Fatal("dataset contains a carriage return; it must use LF endings")
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("dataset does not end with a newline")
	}
}
