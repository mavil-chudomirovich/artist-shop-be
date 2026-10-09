// Package availability adapts the product module's own use case and repository
// to the two cross-module contracts inventory depends on: the availability signal
// and the existence lookup. The product module is the provider, so the adapter
// lives here and is supplied at the composition root; inventory never imports this
// module's internals (Constitution I, research D4).
package availability

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Adapter satisfies both product contracts over the product module's own service
// and repository. The signal delegates to the system-facing use case; the lookup
// is a single existence read. It joins the transaction the caller put in the
// context and never opens one itself (Constitution I).
type Adapter struct {
	// Service is the product use-case surface. The signal is a use case, not a
	// table write, because the entity's transition functions are the only way a
	// sell state moves (Constitution III).
	Service appinterface.ProductService
	// Products is the product repository, used for the existence read. It joins
	// the caller's transaction and is never the source of a write here.
	Products domainrepo.ProductRepository
}

// New creates the availability adapter.
func New(service appinterface.ProductService, products domainrepo.ProductRepository) *Adapter {
	return &Adapter{Service: service, Products: products}
}

// MarkOutOfStock signals that a product must stop being buyable. It runs in the
// caller's transaction because the use case reads and writes through the
// transaction the context carries (FR-026).
func (a *Adapter) MarkOutOfStock(ctx context.Context, productID uuid.UUID) error {
	return a.Service.MarkOutOfStock(ctx, productID)
}

// MarkOnSale signals that a product must resume being buyable.
func (a *Adapter) MarkOnSale(ctx context.Context, productID uuid.UUID) error {
	return a.Service.MarkOnSale(ctx, productID)
}

// ProductExists reports whether the product carries the identifier. It is the
// question a read or a decrease must ask because the foreign key only fires on an
// insert (research D4, D12). It is a single existence read: it loads neither the
// row nor its pictures.
func (a *Adapter) ProductExists(ctx context.Context, productID uuid.UUID) (bool, error) {
	return a.Products.Exists(ctx, productID)
}

// The adapter must satisfy both cross-module contracts. The assertions live in
// the adapter's own package deliberately: placing them in internal/contracts
// would make that dependency-free package import a module, which is exactly
// backwards (internal/contracts/doc.go, module 03's visibility adapter).
var _ contracts.ProductAvailability = (*Adapter)(nil)
var _ contracts.ProductLookup = (*Adapter)(nil)
