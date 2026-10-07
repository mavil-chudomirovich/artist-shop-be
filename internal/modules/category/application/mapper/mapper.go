// Package mapper converts between category domain models and application DTOs.
// It is the single place where those two shapes are translated.
package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
)

// Mapper converts category models into application DTOs. It carries no
// dependency: unlike the user module's mapper, resolving a category needs no
// external read.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// PublicCategory maps one category to the customer-facing DTO. The display
// state, the position and the folding keys are deliberately not carried
// (FR-004, FR-007).
func (m *Mapper) PublicCategory(category model.Category) dto.PublicCategoryOutput {
	return dto.PublicCategoryOutput{
		ID:          category.ID,
		Name:        category.Name,
		Slug:        category.Slug,
		Description: category.Description,
	}
}

// PublicCategories maps a list of categories, preserving the caller's order —
// the FR-003 order the repository already applied. An empty list maps to a nil
// slice, never to an allocated empty one.
func (m *Mapper) PublicCategories(list []model.Category) []dto.PublicCategoryOutput {
	if len(list) == 0 {
		return nil
	}
	out := make([]dto.PublicCategoryOutput, 0, len(list))
	for _, category := range list {
		out = append(out, m.PublicCategory(category))
	}
	return out
}

// AdminCategory maps one category to the administrator DTO, including the
// members a customer must not receive (FR-009).
func (m *Mapper) AdminCategory(category model.Category) dto.AdminCategoryOutput {
	return dto.AdminCategoryOutput{
		ID:          category.ID,
		Name:        category.Name,
		Slug:        category.Slug,
		Description: category.Description,
		Position:    category.Position,
		IsVisible:   category.IsVisible,
		CreatedAt:   category.CreatedAt,
		UpdatedAt:   category.UpdatedAt,
	}
}

// AdminCategories maps a list of categories, preserving the caller's order. An
// empty list maps to a nil slice.
func (m *Mapper) AdminCategories(list []model.Category) []dto.AdminCategoryOutput {
	if len(list) == 0 {
		return nil
	}
	out := make([]dto.AdminCategoryOutput, 0, len(list))
	for _, category := range list {
		out = append(out, m.AdminCategory(category))
	}
	return out
}
