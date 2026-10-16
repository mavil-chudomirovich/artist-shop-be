// Package availability adapts the product module's own use case and repository
// to the two cross-module contracts inventory depends on: the variant-keyed
// availability signal and the variant-existence lookup. The product module is the
// provider, so the adapter lives here and is supplied at the composition root;
// inventory never imports this module's internals (Constitution I, research D4,
// D6).
package availability

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// VariantService is the slice of the product module's use-case surface this
// adapter needs: the two system-facing availability signals, keyed by variant id.
// Declaring the slice here keeps the adapter honest about what it depends on and
// lets a test supply a fake that records the calls, exactly as module 05's
// reservation adapter declares its own Service (research D4).
type VariantService interface {
	// MarkVariantOutOfStock signals that an on-sale variant must stop being
	// buyable. Its implementation is a tolerant no-op for a variant the signal
	// does not apply to (FR-015, research D4).
	MarkVariantOutOfStock(ctx context.Context, variantID uuid.UUID) error
	// MarkVariantOnSale signals that an out-of-stock variant must resume being
	// buyable. Its implementation is a tolerant no-op for a variant the signal
	// does not apply to (FR-015, research D4).
	MarkVariantOnSale(ctx context.Context, variantID uuid.UUID) error
}

// Adapter satisfies both product contracts over the product module's own variant
// service and repository. The signal delegates to the system-facing use case; the
// lookup is a single existence read. It joins the transaction the caller put in
// the context and never opens one itself (Constitution I).
type Adapter struct {
	// Service is the product variant use-case surface. The signal is a use case,
	// not a table write, because the entity's transition functions are the only
	// way a sell state moves (Constitution III).
	Service VariantService
	// Variants is the variant repository, used for the existence read. It joins
	// the caller's transaction and is never the source of a write here.
	Variants domainrepo.VariantRepository
}

// New creates the availability adapter.
func New(service VariantService, variants domainrepo.VariantRepository) *Adapter {
	return &Adapter{Service: service, Variants: variants}
}

// MarkVariantOutOfStock signals that a variant must stop being buyable. It runs
// in the caller's transaction because the use case reads and writes through the
// transaction the context carries (FR-026).
func (a *Adapter) MarkVariantOutOfStock(ctx context.Context, variantID uuid.UUID) error {
	return a.Service.MarkVariantOutOfStock(ctx, variantID)
}

// MarkVariantOnSale signals that a variant must resume being buyable.
func (a *Adapter) MarkVariantOnSale(ctx context.Context, variantID uuid.UUID) error {
	return a.Service.MarkVariantOnSale(ctx, variantID)
}

// VariantExists reports whether the variant carries the identifier. It is the
// question a read or a decrease must ask because the foreign key only fires on an
// insert (research D4, D6). A variant that does not exist answers (false, nil):
// the repository's not-found is the expected miss, not a failure.
func (a *Adapter) VariantExists(ctx context.Context, variantID uuid.UUID) (bool, error) {
	_, err := a.Variants.FindByID(ctx, variantID)
	if errors.Is(err, domainerr.ErrVariantNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// The adapter must satisfy both cross-module contracts. The assertions live in
// the adapter's own package deliberately: placing them in internal/contracts
// would make that dependency-free package import a module, which is exactly
// backwards (internal/contracts/doc.go, module 03's visibility adapter).
var _ contracts.ProductAvailability = (*Adapter)(nil)
var _ contracts.ProductLookup = (*Adapter)(nil)
