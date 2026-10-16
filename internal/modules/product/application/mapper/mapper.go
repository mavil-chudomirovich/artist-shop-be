// Package mapper converts between the product domain models and application DTOs.
//
// It is the single place a model becomes a DTO. It carries no dependency: unlike
// the user module's mapper, resolving a product needs no external read — the
// per-variant availability a detail shows is passed in already resolved by the use
// case. The public and administrator families are mapped separately so a field only
// the administrator may see cannot reach a customer response by accident (FR-008).
package mapper

import (
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Mapper converts product models and their read projections into application
// DTOs. It carries no dependency.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// PublicProduct maps one catalogue entry to the customer-facing card. The code,
// the position and the variant count are deliberately not carried (FR-001,
// FR-008).
func (m *Mapper) PublicProduct(item repository.CatalogueEntry) dto.PublicProductOutput {
	return dto.PublicProductOutput{
		ID:       item.ID,
		Name:     item.Name,
		Slug:     item.Slug,
		Price:    item.Price,
		ImageURL: imageURL(item.ImageURL),
	}
}

// PublicProducts maps a page of catalogue entries, preserving the caller's order —
// the order the repository already applied. An empty list maps to a nil slice,
// never to an allocated empty one.
func (m *Mapper) PublicProducts(items []repository.CatalogueEntry) []dto.PublicProductOutput {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.PublicProductOutput, 0, len(items))
	for _, item := range items {
		out = append(out, m.PublicProduct(item))
	}
	return out
}

// PublicProductDetail maps one product with its pictures and its visible variants
// to the customer-facing detail (FR-003, FR-005). The card price and image come
// from the repository's summary — the lowest on-sale price and the default
// variant's image — while each variant's stock status comes from the availability
// the use case resolved (D10).
func (m *Mapper) PublicProductDetail(view repository.ProductView, available map[uuid.UUID]int64) dto.PublicProductDetailOutput {
	return dto.PublicProductDetailOutput{
		PublicProductOutput: dto.PublicProductOutput{
			ID:       view.Product.ID,
			Name:     view.Product.Name,
			Slug:     view.Product.Slug,
			Price:    view.Summary.Price,
			ImageURL: imageURL(view.Summary.ImageURL),
		},
		Description: view.Product.Description,
		Images:      publicImages(view.Pictures),
		Variants:    publicVariants(view.Variants, view.Product.Price, available),
	}
}

// AdminProduct maps one list row to the administrator entry, including the
// members a customer must not receive (FR-010).
func (m *Mapper) AdminProduct(item repository.ProductListItem) dto.AdminProductOutput {
	return adminProduct(item.Product, item.VariantCount, imageURL(item.ImageURL))
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

// AdminProductDetail maps one product with its pictures and every variant —
// including the variants withheld from customers — to the administrator detail
// (FR-010).
func (m *Mapper) AdminProductDetail(view repository.ProductView) dto.AdminProductDetailOutput {
	primary, _ := model.PrimaryPicture(view.Pictures)
	return dto.AdminProductDetailOutput{
		AdminProductOutput: adminProduct(view.Product, len(view.Variants), imageURL(primary.URL)),
		Images:             adminImages(view.Pictures),
		Variants:           adminVariants(view.Variants, view.Product.Price),
	}
}

// adminProduct maps the product fields both administrator shapes share. It is a
// free function because it has no mapper state.
func adminProduct(product model.Product, variantCount int, mainURL *string) dto.AdminProductOutput {
	return dto.AdminProductOutput{
		ID:           product.ID,
		Name:         product.Name,
		Code:         product.Code,
		Slug:         product.Slug,
		Description:  product.Description,
		Price:        product.Price,
		CategoryID:   product.CategoryID,
		Position:     product.Position,
		VariantCount: variantCount,
		ImageURL:     mainURL,
		CreatedAt:    product.CreatedAt,
		UpdatedAt:    product.UpdatedAt,
	}
}

// imageURL turns a stored link into the nullable value the contract exposes: an
// empty link is the entity having no image, never an empty URL.
func imageURL(url string) *string {
	if url == "" {
		return nil
	}
	return &url
}

// publicImages maps the gallery pictures in the order they were given — the
// display order the repository already applied. An empty list maps to a nil slice.
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

// adminImages maps the gallery pictures in the order they were given. An empty
// list maps to a nil slice.
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

// publicVariants maps the product's visible variants in display order, taking each
// variant's effective price and its stock status from the availability the use
// case resolved. A variant the availability map does not carry is read as not in
// stock, which is what a variant with no stock row means (D10).
func publicVariants(variants []model.Variant, productPrice model.Price, available map[uuid.UUID]int64) []dto.PublicVariantOutput {
	if len(variants) == 0 {
		return nil
	}
	out := make([]dto.PublicVariantOutput, 0, len(variants))
	for _, variant := range variants {
		out = append(out, dto.PublicVariantOutput{
			ID:        variant.ID,
			Name:      variant.Name,
			Price:     variant.EffectivePrice(productPrice),
			ImageURL:  variantImageURL(variant.Image),
			SellState: variant.SellState,
			InStock:   available[variant.ID] > 0,
		})
	}
	return out
}

// adminVariants maps every variant in display order, including the ones a customer
// cannot see. An empty list maps to a nil slice.
func adminVariants(variants []model.Variant, productPrice model.Price) []dto.AdminVariantOutput {
	if len(variants) == 0 {
		return nil
	}
	out := make([]dto.AdminVariantOutput, 0, len(variants))
	for _, variant := range variants {
		out = append(out, dto.AdminVariantOutput{
			ID:             variant.ID,
			Name:           variant.Name,
			SKU:            variant.SKU,
			PriceAmount:    variant.PriceAmount,
			EffectivePrice: variant.EffectivePrice(productPrice),
			ImageURL:       variantImageURL(variant.Image),
			Position:       variant.Position,
			SellState:      variant.SellState,
		})
	}
	return out
}

// variantImageURL turns a variant's optional image into the nullable link the
// contract exposes.
func variantImageURL(image *model.VariantImage) *string {
	if image == nil {
		return nil
	}
	return imageURL(image.URL)
}
