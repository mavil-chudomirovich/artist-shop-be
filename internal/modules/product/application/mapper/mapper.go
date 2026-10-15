// Package mapper converts between the product domain models and application DTOs.
//
// It is the single place a model becomes a DTO. It carries no dependency: unlike
// the user module's mapper, resolving a product needs no external read. The
// public and administrator families are mapped separately so a field only the
// administrator may see cannot reach a customer response by accident (FR-008).
package mapper

import (
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Mapper converts product models and their read projections into application
// DTOs. It carries no dependency.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// PublicProduct maps one list row to the customer-facing entry. The sell state,
// the position, the category, the set flag and the folding key are deliberately
// not carried (FR-004, FR-008).
func (m *Mapper) PublicProduct(item repository.ProductListItem) dto.PublicProductOutput {
	return dto.PublicProductOutput{
		ID:         item.Product.ID,
		Name:       item.Product.Name,
		Slug:       item.Product.Slug,
		Price:      item.Product.Price,
		ImageURL:   imageURL(item.ImageURL),
		IsPreorder: item.Product.IsPreorder,
	}
}

// PublicProducts maps a list of rows, preserving the caller's order — the FR-009
// order the repository already applied. An empty list maps to a nil slice, never
// to an allocated empty one.
func (m *Mapper) PublicProducts(items []repository.ProductListItem) []dto.PublicProductOutput {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.PublicProductOutput, 0, len(items))
	for _, item := range items {
		out = append(out, m.PublicProduct(item))
	}
	return out
}

// PublicProductDetail maps one product with its pictures to the customer-facing
// detail: the list fields plus the description and every picture (FR-005). The
// main picture's link drives ImageURL; a product with no picture maps to nil.
func (m *Mapper) PublicProductDetail(view repository.ProductView) dto.PublicProductDetailOutput {
	primary, _ := model.PrimaryPicture(view.Pictures)
	return dto.PublicProductDetailOutput{
		PublicProductOutput: dto.PublicProductOutput{
			ID:         view.Product.ID,
			Name:       view.Product.Name,
			Slug:       view.Product.Slug,
			Price:      view.Product.Price,
			ImageURL:   imageURL(primary.URL),
			IsPreorder: view.Product.IsPreorder,
		},
		Description: view.Product.Description,
		Images:      publicImages(view.Pictures),
	}
}

// AdminProduct maps one list row to the administrator entry, including the
// members a customer must not receive (FR-011).
func (m *Mapper) AdminProduct(item repository.ProductListItem) dto.AdminProductOutput {
	return adminProduct(item.Product, item.PictureCount, imageURL(item.ImageURL))
}

// AdminProducts maps a list of rows, preserving the caller's order. An empty list
// maps to a nil slice.
func (m *Mapper) AdminProducts(items []repository.ProductListItem) []dto.AdminProductOutput {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.AdminProductOutput, 0, len(items))
	for _, item := range items {
		out = append(out, m.AdminProduct(item))
	}
	return out
}

// AdminProductDetail maps one product with its pictures and set members to the
// administrator detail (FR-011). The main picture's link drives ImageURL, and the
// member list is passed straight through — a non-set product maps to a nil
// member list (research D18).
func (m *Mapper) AdminProductDetail(view repository.ProductView, members []repository.SetMember) dto.AdminProductDetailOutput {
	primary, _ := model.PrimaryPicture(view.Pictures)
	return dto.AdminProductDetailOutput{
		AdminProductOutput: adminProduct(view.Product, len(view.Pictures), imageURL(primary.URL)),
		Images:             adminImages(view.Pictures),
		Members:            setMembers(members),
	}
}

// adminProduct maps the product fields both administrator shapes share. It is a
// free function because it has no mapper state.
func adminProduct(product model.Product, imageCount int, mainURL *string) dto.AdminProductOutput {
	return dto.AdminProductOutput{
		ID:                 product.ID,
		Name:               product.Name,
		Slug:               product.Slug,
		Description:        product.Description,
		Price:              product.Price,
		CategoryID:         product.CategoryID,
		Position:           product.Position,
		SellState:          product.SellState,
		IsSet:              product.IsSet,
		IsPreorder:         product.IsPreorder,
		PreorderExpectedAt: product.PreorderExpectedAt,
		ImageCount:         imageCount,
		ImageURL:           mainURL,
		CreatedAt:          product.CreatedAt,
		UpdatedAt:          product.UpdatedAt,
	}
}

// imageURL turns a stored link into the nullable value the contract exposes: an
// empty link is the product having no picture, never an empty URL.
func imageURL(url string) *string {
	if url == "" {
		return nil
	}
	return &url
}

// publicImages maps the pictures in the order they were given — the display order
// the repository already applied. An empty list maps to a nil slice.
func publicImages(pictures []model.Picture) []dto.PublicImageOutput {
	if len(pictures) == 0 {
		return nil
	}
	out := make([]dto.PublicImageOutput, 0, len(pictures))
	for _, picture := range pictures {
		out = append(out, dto.PublicImageOutput{
			ID:     picture.ID,
			URL:    picture.URL,
			Width:  picture.Width,
			Height: picture.Height,
		})
	}
	return out
}

// adminImages maps the pictures in the order they were given. An empty list maps
// to a nil slice.
func adminImages(pictures []model.Picture) []dto.AdminImageOutput {
	if len(pictures) == 0 {
		return nil
	}
	out := make([]dto.AdminImageOutput, 0, len(pictures))
	for _, picture := range pictures {
		out = append(out, dto.AdminImageOutput{
			ID:        picture.ID,
			PublicID:  picture.PublicID,
			URL:       picture.URL,
			Width:     picture.Width,
			Height:    picture.Height,
			Position:  picture.Position,
			IsPrimary: picture.IsPrimary,
		})
	}
	return out
}

// setMembers maps a set's members in the order they were given. An empty list
// maps to a nil slice.
func setMembers(members []repository.SetMember) []dto.SetMemberOutput {
	if len(members) == 0 {
		return nil
	}
	out := make([]dto.SetMemberOutput, 0, len(members))
	for _, member := range members {
		out = append(out, dto.SetMemberOutput{
			ID:   member.ID,
			Name: member.Name,
			Slug: member.Slug,
		})
	}
	return out
}

// PublicCatalogueEntry maps one merged-list row to the customer-facing entry,
// carrying the `type` discriminator a client picks the route by (FR-001,
// research D4, D12). A group's price and picture are what the repository already
// derived from its visible options; the mapper never derives them itself.
func (m *Mapper) PublicCatalogueEntry(item repository.CatalogueItem) dto.PublicCatalogueEntry {
	return dto.PublicCatalogueEntry{
		Type:       string(item.Type),
		ID:         item.ID,
		Name:       item.Name,
		Slug:       item.Slug,
		Price:      item.Price,
		ImageURL:   imageURL(item.ImageURL),
		IsPreorder: item.IsPreorder,
	}
}

// PublicCatalogueEntries maps a page of merged rows, preserving the caller's
// order — the FR-009 order the repository already applied. An empty list maps to
// a nil slice.
func (m *Mapper) PublicCatalogueEntries(items []repository.CatalogueItem) []dto.PublicCatalogueEntry {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.PublicCatalogueEntry, 0, len(items))
	for _, item := range items {
		out = append(out, m.PublicCatalogueEntry(item))
	}
	return out
}

// PublicVariantGroupDetail maps a group with its pictures and visible options to
// the customer-facing detail (FR-002, FR-005, FR-007). The provider's opaque
// image identifier is deliberately absent from the public shapes.
func (m *Mapper) PublicVariantGroupDetail(view repository.VariantGroupView) dto.PublicVariantGroupDetailOutput {
	return dto.PublicVariantGroupDetailOutput{
		ID:          view.Group.ID,
		Name:        view.Group.Name,
		Slug:        view.Group.Slug,
		Description: view.Group.Description,
		OptionName:  view.Group.OptionName,
		Images:      publicGroupImages(view.Images),
		Options:     publicVariantOptions(view.Options),
	}
}

// AdminVariantGroup maps one group list row to the administrator entry,
// including the members a customer must not receive (FR-013).
func (m *Mapper) AdminVariantGroup(item repository.VariantGroupListItem) dto.AdminVariantGroupOutput {
	return adminVariantGroup(item.Group, item.ImageCount, imageURL(item.ImageURL))
}

// AdminVariantGroups maps a list of rows, preserving the caller's order. An
// empty list maps to a nil slice.
func (m *Mapper) AdminVariantGroups(items []repository.VariantGroupListItem) []dto.AdminVariantGroupOutput {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.AdminVariantGroupOutput, 0, len(items))
	for _, item := range items {
		out = append(out, m.AdminVariantGroup(item))
	}
	return out
}

// AdminVariantGroupDetail maps one group with its pictures and every option —
// including the ones withheld from customers — to the administrator detail
// (FR-013). The main picture's link drives ImageURL.
func (m *Mapper) AdminVariantGroupDetail(view repository.VariantGroupView) dto.AdminVariantGroupDetailOutput {
	primary := primaryGroupImage(view.Images)
	return dto.AdminVariantGroupDetailOutput{
		AdminVariantGroupOutput: adminVariantGroup(view.Group, len(view.Images), imageURL(primary)),
		Images:                  adminGroupImages(view.Images),
		Options:                 adminGroupOptions(view.Options),
	}
}

// adminVariantGroup maps the group fields both administrator shapes share. It is
// a free function because it has no mapper state.
func adminVariantGroup(group model.VariantGroup, imageCount int, mainURL *string) dto.AdminVariantGroupOutput {
	return dto.AdminVariantGroupOutput{
		ID:          group.ID,
		Name:        group.Name,
		Slug:        group.Slug,
		Description: group.Description,
		CategoryID:  group.CategoryID,
		Position:    group.Position,
		OptionName:  group.OptionName,
		ImageCount:  imageCount,
		ImageURL:    mainURL,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
}

// publicGroupImages maps a group's pictures in the order they were given — the
// display order the repository already applied. An empty list maps to a nil
// slice, and the provider's opaque identifier is dropped as it is for a product.
func publicGroupImages(images []model.VariantGroupImage) []dto.PublicImageOutput {
	if len(images) == 0 {
		return nil
	}
	out := make([]dto.PublicImageOutput, 0, len(images))
	for _, image := range images {
		out = append(out, dto.PublicImageOutput{
			ID:     image.ID,
			URL:    image.URL,
			Width:  image.Width,
			Height: image.Height,
		})
	}
	return out
}

// adminGroupImages maps a group's pictures in the order they were given,
// including the provider's opaque identifier, its position and the main flag. An
// empty list maps to a nil slice.
func adminGroupImages(images []model.VariantGroupImage) []dto.AdminGroupImageOutput {
	if len(images) == 0 {
		return nil
	}
	out := make([]dto.AdminGroupImageOutput, 0, len(images))
	for _, image := range images {
		out = append(out, dto.AdminGroupImageOutput{
			ID:        image.ID,
			PublicID:  image.PublicID,
			URL:       image.URL,
			Width:     image.Width,
			Height:    image.Height,
			Position:  image.Position,
			IsPrimary: image.IsPrimary,
		})
	}
	return out
}

// publicVariantOptions maps a group's options to the customer-facing shape,
// preserving the FR-009 order the repository already applied. An empty list maps
// to a nil slice.
func publicVariantOptions(options []repository.GroupOptionItem) []dto.PublicVariantOptionOutput {
	if len(options) == 0 {
		return nil
	}
	out := make([]dto.PublicVariantOptionOutput, 0, len(options))
	for _, option := range options {
		out = append(out, dto.PublicVariantOptionOutput{
			ID:         option.ID,
			Slug:       option.Slug,
			Label:      option.Label,
			Price:      option.Price,
			ImageURL:   imageURL(option.ImageURL),
			IsPreorder: option.IsPreorder,
		})
	}
	return out
}

// adminGroupOptions maps a group's options to the administrator shape, carrying
// each option's own name, position and sell state as well. An empty list maps to
// a nil slice.
func adminGroupOptions(options []repository.GroupOptionItem) []dto.AdminGroupOptionOutput {
	if len(options) == 0 {
		return nil
	}
	out := make([]dto.AdminGroupOptionOutput, 0, len(options))
	for _, option := range options {
		out = append(out, dto.AdminGroupOptionOutput{
			ID:         option.ID,
			Name:       option.Name,
			Slug:       option.Slug,
			Label:      option.Label,
			Price:      option.Price,
			ImageURL:   imageURL(option.ImageURL),
			Position:   option.Position,
			SellState:  option.SellState,
			IsPreorder: option.IsPreorder,
		})
	}
	return out
}

// primaryGroupImage returns the group's current main picture's link, or "" when
// the group has no main picture. A group with no pictures is valid, so the
// answer is an empty link rather than an error.
func primaryGroupImage(images []model.VariantGroupImage) string {
	for _, image := range images {
		if image.IsPrimary {
			return image.URL
		}
	}
	return ""
}
