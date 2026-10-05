// Package administrative adapts internal/share/administrative to the Divisions
// port declared in application/interface, so the use cases depend on an
// interface rather than on the shared dataset package.
package administrative

import (
	"context"
	"sort"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
)

// Divisions serves the official administrative dataset through the module's port.
//
// It exists so `application` never imports the shared dataset package directly:
// the domain cannot import it at all (Constitution I), and the use cases reach it
// through this interface so a module can be tested with a fake dataset
// (research D1).
type Divisions struct{}

// New creates the Divisions adapter.
func New() *Divisions { return &Divisions{} }

// Provinces returns every province, ordered by name because that is what the
// select shows. The bundled file is ordered by code, so the order is imposed here
// rather than inherited; the code breaks ties so two provinces with the same
// name keep a stable order.
func (d *Divisions) Provinces(context.Context) ([]appinterface.Province, error) {
	stored := administrative.Provinces()
	out := make([]appinterface.Province, 0, len(stored))
	for _, province := range stored {
		out = append(out, appinterface.Province{Code: province.Code, Name: province.Name})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

// Wards returns the wards of one province. It passes the shared package's own
// sentinel errors through unwrapped in meaning — errors.Is still matches
// ErrUnknownProvince — so presentation maps them to the USER_* codes without the
// application layer importing this package.
func (d *Divisions) Wards(_ context.Context, provinceCode string) ([]appinterface.Ward, error) {
	stored, err := administrative.Wards(provinceCode)
	if err != nil {
		return nil, err
	}
	out := make([]appinterface.Ward, 0, len(stored))
	for _, ward := range stored {
		out = append(out, appinterface.Ward{Code: ward.Code, Name: ward.Name, ProvinceCode: ward.ProvinceCode})
	}
	return out, nil
}

// ValidateAddressDivisions reports whether the ward exists and belongs to the
// province. The errors are the shared package's own sentinels, which is what the
// port promises: use cases branch on them and presentation maps them, while the
// dataset package stays free of any module's domain errors (Constitution I).
func (d *Divisions) ValidateAddressDivisions(_ context.Context, provinceCode, wardCode string) error {
	return administrative.Validate(provinceCode, wardCode)
}

var _ appinterface.Divisions = (*Divisions)(nil)
