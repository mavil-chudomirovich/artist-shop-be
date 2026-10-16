package model

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// FieldSKU is the request member carrying a variant's SKU (FR-011). It is the
// member name of the variant create/update request, so a rejection can name the
// exact input the client sent. The variant's display name reuses FieldName and its
// own price reuses FieldPrice, because they are the same members on the wire.
const FieldSKU = "sku"

// FieldVariantImage is the request member carrying a variant's single image
// (FR-016). It is the member name the variant image operation reports against.
const FieldVariantImage = "image"

// VariantImage is a variant's single image, held outside the service by the media
// provider (FR-016). The service keeps the reference, never the bytes.
//
// A variant has at most one image; the four fields are all present or the whole
// value is absent (a nil *VariantImage), which matches the storage check
// product_variants_image_ck.
type VariantImage struct {
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients. It must be HTTPS.
	URL string
	// Width and Height are the stored pixel dimensions the provider reported.
	Width  int
	Height int
}

// Variant is one sellable choice within a product — for example a card set whose
// variants are "Flins" and "Dottore" (FR-003, research D3).
//
// It carries its own name, globally unique SKU, optional price (a null price falls
// back to the product's default), its own image, display order, sell state and
// stock. It is the unit a customer selects, adds to the cart and buys.
//
// The sell state is method-driven, never settable (Constitution III, research D4):
// a caller asks the entity to transition and the entity refuses a move the
// transition table does not have an edge for. SellState is exported only so the
// persistence adapter can scan it; nothing else may assign it.
type Variant struct {
	// ID identifies the variant. It is generated once and is stable for the
	// variant's lifetime (FR-003).
	ID uuid.UUID
	// ProductID is the parent product the variant belongs to (FR-003).
	ProductID uuid.UUID
	// Name is the display name the operator typed, trimmed. Not unique (FR-003).
	Name string
	// SKU is the operator-written globally unique stock-keeping unit, trimmed
	// (FR-011).
	SKU string
	// PriceAmount is the variant's own price in the currency's minor unit, or nil
	// to fall back to the product's default price (FR-017).
	PriceAmount *int64
	// Image is the variant's single image, or nil when it has none (FR-016).
	Image *VariantImage
	// Position is the display order; the smallest is the default variant. Any
	// whole number, ties broken by created_at then id (FR-002).
	Position int
	// SellState is where the variant is in its selling life. It is changed only
	// through the transition methods below, never assigned (FR-015).
	SellState constant.SellState
	// CreatedAt is when the variant was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is when the variant was last written.
	UpdatedAt time.Time
}

// VariantDraft carries the caller-supplied members of a new variant.
type VariantDraft struct {
	// Name is the variant display name. It must not be blank and must fit
	// MaxNameRunes characters.
	Name string
	// SKU is the globally unique SKU. It must not be blank.
	SKU string
	// PriceAmount is the variant's own price, or nil to fall back to the product's
	// default price. When present it must be positive.
	PriceAmount *int64
	// Image is the variant's single image, or nil when it has none.
	Image *VariantImage
	// Position is the ordering preference.
	Position int
}

// VariantEdit carries the changes of a partial edit.
//
// A nil member keeps its current value. The image is deliberately absent: a
// variant's image is set and cleared through its own media operations, not through
// a field edit (FR-016). PriceAmountSet distinguishes "keep the price" from
// "clear it and fall back to the product's default", which a plain pointer cannot.
type VariantEdit struct {
	// Name is the new name; nil keeps the current value.
	Name *string
	// SKU is the new SKU; nil keeps the current value.
	SKU *string
	// PriceAmount is the new own price. It is consulted only when PriceAmountSet
	// is true: a non-nil value sets it, a nil value clears it back to the
	// product's default (FR-017).
	PriceAmount *int64
	// PriceAmountSet reports whether the edit is changing the price at all.
	PriceAmountSet bool
	// Position is the new position; nil keeps the current value.
	Position *int
}

// NewVariant builds a variant in COMING_SOON, the first state of the selling life
// (FR-015, research D4). A newly created variant is not yet on sale.
//
// It trims the name and the SKU, validates them, validates the optional price and
// the optional image, and copies the price amount so the entity never aliases the
// caller's value. A rejected draft yields no variant at all, so a caller cannot
// persist half of one.
func NewVariant(productID uuid.UUID, draft VariantDraft, now time.Time) (*Variant, error) {
	name := strings.TrimSpace(draft.Name)
	sku := strings.TrimSpace(draft.SKU)

	if err := validateVariantName(name); err != nil {
		return nil, err
	}
	if err := validateSKU(sku); err != nil {
		return nil, err
	}
	priceAmount, err := normalizeVariantPrice(draft.PriceAmount)
	if err != nil {
		return nil, err
	}
	image, err := normalizeVariantImage(draft.Image)
	if err != nil {
		return nil, err
	}

	return &Variant{
		ID:          uuid.New(),
		ProductID:   productID,
		Name:        name,
		SKU:         sku,
		PriceAmount: priceAmount,
		Image:       image,
		Position:    draft.Position,
		SellState:   constant.SellStateComingSoon,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Apply writes a partial edit.
//
// It stages the whole change, validates it, and only then writes, so a rejection
// leaves the variant exactly as it was — including its sell state, which is never
// touched here (FR-010, FR-017). The identifier, the product and the image are
// never touched.
func (v *Variant) Apply(edit VariantEdit, now time.Time) error {
	name := v.Name
	sku := v.SKU
	priceAmount := v.PriceAmount
	position := v.Position

	if edit.Name != nil {
		name = strings.TrimSpace(*edit.Name)
	}
	if edit.SKU != nil {
		sku = strings.TrimSpace(*edit.SKU)
	}
	if edit.PriceAmountSet {
		normalized, err := normalizeVariantPrice(edit.PriceAmount)
		if err != nil {
			return err
		}
		priceAmount = normalized
	}
	if edit.Position != nil {
		position = *edit.Position
	}

	if err := validateVariantName(name); err != nil {
		return err
	}
	if err := validateSKU(sku); err != nil {
		return err
	}

	v.Name = name
	v.SKU = sku
	v.PriceAmount = priceAmount
	v.Position = position
	v.UpdatedAt = now
	return nil
}

// EffectivePrice returns the price a customer pays for this variant: its own amount
// when set, otherwise the product's default price (FR-017). The currency is always
// the product's, so a variant never introduces a second currency for the same
// product (FR-024).
func (v *Variant) EffectivePrice(productPrice Price) Price {
	if v.PriceAmount == nil {
		return productPrice
	}
	return Price{Amount: *v.PriceAmount, Currency: productPrice.Currency}
}

// Launch moves a COMING_SOON variant to ACTIVE (FR-015). A variant in any other
// state is refused naming the state it is in.
func (v *Variant) Launch(now time.Time) error {
	if v.SellState != constant.SellStateComingSoon {
		return domainerr.InvalidVariantStateTransition(v.SellState, constant.SellStateActive)
	}
	return v.applyState(constant.SellStateActive, now)
}

// SellOut moves an ACTIVE variant to OUT_OF_STOCK (FR-015). It is called by the
// operator or automatically as the variant's availability crosses zero.
func (v *Variant) SellOut(now time.Time) error {
	if v.SellState != constant.SellStateActive {
		return domainerr.InvalidVariantStateTransition(v.SellState, constant.SellStateOutOfStock)
	}
	return v.applyState(constant.SellStateOutOfStock, now)
}

// Restock returns an OUT_OF_STOCK variant to ACTIVE (FR-015). It is called by the
// operator or automatically as the variant's availability returns, so a sold-out
// variant can be sold again without duplicating it.
func (v *Variant) Restock(now time.Time) error {
	if v.SellState != constant.SellStateOutOfStock {
		return domainerr.InvalidVariantStateTransition(v.SellState, constant.SellStateActive)
	}
	return v.applyState(constant.SellStateActive, now)
}

// Retire moves any non-terminal variant to DISCONTINUED. The state is terminal: a
// retired variant never returns to sale (FR-015).
func (v *Variant) Retire(now time.Time) error {
	if !constant.IsAllowedTransition(v.SellState, constant.SellStateDiscontinued) {
		return domainerr.InvalidVariantStateTransition(v.SellState, constant.SellStateDiscontinued)
	}
	return v.applyState(constant.SellStateDiscontinued, now)
}

// ChangeSellState routes a requested target through the transition table, so a
// lifecycle use case never writes a state directly (FR-015, Constitution III). A
// target the table has no edge for is refused with the current state named.
func (v *Variant) ChangeSellState(to constant.SellState, now time.Time) error {
	if !constant.IsAllowedTransition(v.SellState, to) {
		return domainerr.InvalidVariantStateTransition(v.SellState, to)
	}
	return v.applyState(to, now)
}

// applyState is the single write of SellState.
func (v *Variant) applyState(to constant.SellState, now time.Time) error {
	v.SellState = to
	v.UpdatedAt = now
	return nil
}

// VisibleToCustomers reports whether a variant may be served on a customer-facing
// route: a variant is visible when it is on sale or out of stock, and hidden while
// it is not yet on sale or retired (FR-005).
func (v *Variant) VisibleToCustomers() bool {
	return v.SellState == constant.SellStateActive || v.SellState == constant.SellStateOutOfStock
}

// Buyable reports whether a customer may add the variant to the cart: only a
// variant that is on sale is buyable (FR-007).
func (v *Variant) Buyable() bool {
	return v.SellState == constant.SellStateActive
}

// validateVariantName refuses a blank variant name or one over its character bound,
// naming the member (FR-003). It reuses the product name bound and the same
// storage check (product_variants_name_length_ck).
func validateVariantName(name string) error {
	if name == "" {
		return domainerr.InvalidProductField(FieldName, requiredIssue)
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return domainerr.InvalidProductField(FieldName, limitIssue(MaxNameRunes))
	}
	return nil
}

// validateSKU refuses a blank SKU, naming the member (FR-011). Uniqueness is a
// catalogue-wide invariant the storage layer enforces and the adapter reports.
func validateSKU(sku string) error {
	if sku == "" {
		return domainerr.InvalidProductField(FieldSKU, requiredIssue)
	}
	return nil
}

// normalizeVariantPrice copies an optional price amount and refuses a non-positive
// one when present, naming the member (FR-017). A nil amount is a valid value: it
// means "use the product's default price".
func normalizeVariantPrice(amount *int64) (*int64, error) {
	if amount == nil {
		return nil, nil
	}
	if *amount <= 0 {
		return nil, domainerr.InvalidProductField(
			FieldPrice, "must be a positive amount in the currency's minor unit")
	}
	value := *amount
	return &value, nil
}

// normalizeVariantImage trims a variant image reference and refuses an incomplete
// or malformed one (FR-016). It mirrors the storage checks of product_variants:
// the URL must be HTTPS and the dimensions positive. A nil image is a valid value.
func normalizeVariantImage(image *VariantImage) (*VariantImage, error) {
	if image == nil {
		return nil, nil
	}
	normalized := *image
	normalized.PublicID = strings.TrimSpace(normalized.PublicID)
	normalized.URL = strings.TrimSpace(normalized.URL)
	if normalized.PublicID == "" || normalized.URL == "" {
		return nil, domainerr.InvalidProductField(FieldVariantImage, requiredIssue)
	}
	if !strings.HasPrefix(normalized.URL, "https://") {
		return nil, domainerr.InvalidProductField(FieldVariantImage, "must be an https link")
	}
	if normalized.Width <= 0 || normalized.Height <= 0 {
		return nil, domainerr.InvalidProductField(FieldVariantImage, "must carry positive dimensions")
	}
	return &normalized, nil
}
