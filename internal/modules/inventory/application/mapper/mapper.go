package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// Mapper converts inventory domain models into application DTOs. It is the single
// place a model becomes a DTO and carries no dependency, so a change to one shape
// cannot silently reach the other.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Stock maps one stock to its three quantities. Availability is derived by the
// domain, never copied here, so the answer a client is shown is the same rule the
// use cases enforce (FR-007, FR-013).
func (m *Mapper) Stock(stock model.Stock) (dto.StockOutput, error) {
	available, err := stock.Available()
	if err != nil {
		return dto.StockOutput{}, err
	}
	return dto.StockOutput{
		ProductID:         stock.ProductID,
		PhysicalQuantity:  stock.Quantity,
		HeldQuantity:      stock.Held,
		AvailableQuantity: available,
	}, nil
}

// Movement maps one ledger row, carrying its note and its source reference so the
// two causes of a change stay distinguishable (FR-008).
func (m *Mapper) Movement(movement model.Movement) dto.MovementOutput {
	return dto.MovementOutput{
		ID:                movement.ID,
		ProductID:         movement.ProductID,
		Kind:              movement.Kind,
		Delta:             movement.Delta,
		ResultingQuantity: movement.ResultingQuantity,
		SourceReference:   movement.SourceReference,
		ActorID:           movement.ActorID,
		Note:              movement.Note,
		CreatedAt:         movement.CreatedAt,
	}
}

// Movements maps a page of ledger rows, preserving the caller's order — the
// oldest-first order the repository already applied. An empty list maps to a nil
// slice, never an allocated empty one.
func (m *Mapper) Movements(movements []model.Movement) []dto.MovementOutput {
	if len(movements) == 0 {
		return nil
	}
	out := make([]dto.MovementOutput, 0, len(movements))
	for _, movement := range movements {
		out = append(out, m.Movement(movement))
	}
	return out
}
