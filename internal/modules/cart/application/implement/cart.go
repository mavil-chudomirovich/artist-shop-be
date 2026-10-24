// Package implement holds the cart module's use cases: the reads and writes a
// customer reaches through the /cart routes.
//
// Every method takes the acting customer from the context the session filled,
// never from its input, so the cart an operation reaches is always the caller's
// own (FR-010). Every write runs inside the UnitOfWork transaction that locks the
// cart's row (research D8, D9), and no method reaches an inventory write: a cart
// line is not a reservation (FR-011, research D12).
package implement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/model"
)

// errNoActor reports a use case reached without a session behind the call. It
// cannot happen through the routes — the handler resolves the session first and
// answers UNAUTHENTICATED before calling — so it is a programming error that maps
// to INTERNAL_ERROR rather than a client-facing refusal.
var errNoActor = errors.New("cart: no acting account in the context")

// Service implements the cart module's use cases: read, add, change quantity and
// remove. It owns no lifecycle and no state machine; it applies the domain's money
// and quantity rules and reaches the two cross-module contracts for the facts it
// cannot read itself.
type Service struct {
	// Carts persists and reads the cart and its lines. It never opens a
	// transaction; the application owns every boundary (Constitution I).
	Carts appinterface.CartRepository
	// Variants answers a variant's product, name, on-sale state and effective
	// price. It is supplied by module 04's adapter at the composition root, so the
	// cart never reads module 04's table (research D6).
	Variants appinterface.ProductVariants
	// Availability answers what is currently available. It is supplied by module
	// 05's adapter at the composition root. It backs both the refusal an add or a
	// change makes when the quantity exceeds the shelf (FR-007) and the per-line
	// buyable projection a read computes (FR-012, research D2, D10).
	Availability appinterface.InventoryAvailability
	// Tx owns every write's transaction boundary, so the cart's row lock, the
	// line write and the availability decision commit together or not at all
	// (research D9, Constitution II).
	Tx appinterface.UnitOfWork
	// Clock stamps the cart's writes. Only "what time is it" is injected, never a
	// rule.
	Clock appinterface.Clock
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the cart use-case service.
func New(service Service) *Service { return &service }

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset.
func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// actor returns the acting customer taken from the context the session filled.
func (s *Service) actor(ctx context.Context) (appinterface.Actor, error) {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return appinterface.Actor{}, errNoActor
	}
	return actor, nil
}

// Get returns the caller's cart, re-checking each line's live variant facts. A
// customer with no cart answers an empty cart whose subtotal is absent, and a read
// never creates a row (FR-004, research D8).
func (s *Service) Get(ctx context.Context, _ dto.GetCartInput) (dto.CartView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.CartView{}, err
	}
	cart, ok, err := s.Carts.FindByOwner(ctx, actor.ID)
	if err != nil {
		return dto.CartView{}, err
	}
	if !ok {
		return s.emptyView(), nil
	}
	return s.readCart(ctx, cart.ID)
}

// Add adds a variant with a positive whole quantity, creating the cart on the
// first add and raising an existing line rather than adding a second one
// (FR-001, FR-002). The price is captured from the variant's effective price at
// add time (FR-018) and a variant absent from the catalogue is the shared
// not-found (FR-005).
//
// The variant must be on sale and the quantity the add would produce — the line's
// existing quantity plus the requested one — must not exceed what is available,
// or the add is refused and nothing is written (FR-005, FR-007). That judgement
// runs inside the transaction that locks the cart's row, so a concurrent add
// cannot raise the line past availability (research D9).
//
// It calls no inventory write: adding to a cart holds no stock (FR-011).
func (s *Service) Add(ctx context.Context, in dto.AddItemInput) (dto.CartView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.CartView{}, err
	}
	if err := model.ValidateQuantity(in.Quantity); err != nil {
		return dto.CartView{}, err
	}

	// The cart is created lazily, before the write transaction: the unique index
	// on user_id refuses a concurrent first add, and resolving that refusal by
	// re-finding the other writer's cart must not happen inside an aborted
	// transaction (research D8).
	cart, err := s.ensureCart(ctx, actor.ID)
	if err != nil {
		return dto.CartView{}, err
	}

	now := s.now()
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Carts.Lock(txCtx, cart.ID); err != nil {
			return err
		}
		summary, err := s.purchasableVariant(txCtx, in.VariantID)
		if err != nil {
			return err
		}
		held, err := s.heldQuantity(txCtx, cart.ID, in.VariantID)
		if err != nil {
			return err
		}
		if err := s.ensureAvailable(txCtx, in.VariantID, held+in.Quantity); err != nil {
			return err
		}
		line, err := model.NewCartLine(in.VariantID, in.Quantity, model.Price{
			Amount:   summary.Price.Amount,
			Currency: summary.Price.Currency,
		})
		if err != nil {
			return err
		}
		return s.Carts.UpsertLine(txCtx, cart.ID, line, now)
	}); err != nil {
		return dto.CartView{}, err
	}
	return s.readCart(ctx, cart.ID)
}

// ChangeQuantity sets the line's quantity. The variant must still be on sale and
// the new quantity must not exceed what is available, or the change is refused
// and the previous quantity is kept (FR-005, FR-007). A line this cart does not
// hold answers the shared not-found (FR-003, error-codes.md). It runs inside the
// transaction that locks the cart's row (research D9).
func (s *Service) ChangeQuantity(ctx context.Context, in dto.ChangeQuantityInput) (dto.CartView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.CartView{}, err
	}
	if err := model.ValidateQuantity(in.Quantity); err != nil {
		return dto.CartView{}, err
	}

	cart, err := s.ownerCart(ctx, actor.ID)
	if err != nil {
		return dto.CartView{}, err
	}

	now := s.now()
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Carts.Lock(txCtx, cart.ID); err != nil {
			return err
		}
		if _, err := s.purchasableVariant(txCtx, in.VariantID); err != nil {
			return err
		}
		if err := s.ensureAvailable(txCtx, in.VariantID, in.Quantity); err != nil {
			return err
		}
		return s.Carts.SetQuantity(txCtx, cart.ID, in.VariantID, in.Quantity, now)
	}); err != nil {
		return dto.CartView{}, err
	}
	return s.readCart(ctx, cart.ID)
}

// Remove deletes one line. A line this cart does not hold answers the shared
// not-found, so the route never confirms what is in another customer's cart
// (FR-003, error-codes.md). It runs inside the transaction that locks the cart's
// row (research D9).
func (s *Service) Remove(ctx context.Context, in dto.RemoveItemInput) error {
	actor, err := s.actor(ctx)
	if err != nil {
		return err
	}
	cart, err := s.ownerCart(ctx, actor.ID)
	if err != nil {
		return err
	}
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Carts.Lock(txCtx, cart.ID); err != nil {
			return err
		}
		return s.Carts.DeleteLine(txCtx, cart.ID, in.VariantID)
	})
}

// ownerCart finds the caller's cart or reports not-found, for the acts that need
// an existing line. It never creates a row.
func (s *Service) ownerCart(ctx context.Context, userID uuid.UUID) (*model.Cart, error) {
	cart, ok, err := s.Carts.FindByOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domainerr.ErrProductNotFound
	}
	return cart, nil
}

// ensureCart finds the caller's cart or creates it lazily on the first add
// (research D8). A concurrent first add is refused by the unique index on user_id
// and retried as a find, so two first adds converge on one cart.
func (s *Service) ensureCart(ctx context.Context, userID uuid.UUID) (*model.Cart, error) {
	cart, ok, err := s.Carts.FindByOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if ok {
		return cart, nil
	}

	created := model.NewCart(userID)
	if err := s.Carts.Create(ctx, created); err != nil {
		if errors.Is(err, domainerr.ErrCartAlreadyExists) {
			existing, ok, findErr := s.Carts.FindByOwner(ctx, userID)
			if findErr != nil {
				return nil, findErr
			}
			if ok {
				return existing, nil
			}
		}
		return nil, err
	}
	return created, nil
}

// variant reads one variant's facts through the bulk contract. A variant the
// result does not carry is not found (research D6).
func (s *Service) variant(ctx context.Context, variantID uuid.UUID) (contracts.VariantSummary, error) {
	summaries, err := s.Variants.Variants(ctx, []uuid.UUID{variantID})
	if err != nil {
		return contracts.VariantSummary{}, err
	}
	for _, summary := range summaries {
		if summary.VariantID == variantID {
			return summary, nil
		}
	}
	return contracts.VariantSummary{}, domainerr.ErrProductNotFound
}

// purchasableVariant reads one variant's facts and refuses it when it is not on
// sale, so an add or a change cannot touch a variant the shop does not offer for
// sale (FR-005). A variant absent from the result is the shared not-found.
func (s *Service) purchasableVariant(ctx context.Context, variantID uuid.UUID) (contracts.VariantSummary, error) {
	summary, err := s.variant(ctx, variantID)
	if err != nil {
		return contracts.VariantSummary{}, err
	}
	if !summary.OnSale {
		return contracts.VariantSummary{}, domainerr.ProductNotPurchasable(variantID)
	}
	return summary, nil
}

// available reads one variant's currently available quantity through the bulk
// contract. The contract answers one entry per requested identifier, so a variant
// it does not carry cannot happen; a missing entry is read as zero, the same as a
// variant with no stock row (research D2).
func (s *Service) available(ctx context.Context, variantID uuid.UUID) (int64, error) {
	items, err := s.Availability.AvailableQuantity(ctx, []uuid.UUID{variantID})
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if item.VariantID == variantID {
			return item.Available, nil
		}
	}
	return 0, nil
}

// ensureAvailable refuses a resulting quantity above what is available, naming
// the field the customer must change and the amount they can take (FR-007,
// error-codes.md). `resulting` is the quantity the operation would leave on the
// line, not necessarily the amount in the request.
func (s *Service) ensureAvailable(ctx context.Context, variantID uuid.UUID, resulting int64) error {
	available, err := s.available(ctx, variantID)
	if err != nil {
		return err
	}
	if resulting > available {
		return domainerr.QuantityExceedsAvailable(variantID, available, resulting)
	}
	return nil
}

// heldQuantity reads how many units of one variant the cart already holds, so an
// add can judge the quantity it would produce rather than the amount alone
// (FR-007). A variant the cart does not hold answers zero.
func (s *Service) heldQuantity(ctx context.Context, cartID, variantID uuid.UUID) (int64, error) {
	lines, err := s.Carts.Lines(ctx, cartID)
	if err != nil {
		return 0, err
	}
	for _, line := range lines {
		if line.VariantID == variantID {
			return line.Quantity, nil
		}
	}
	return 0, nil
}

// readCart reads the cart's lines and maps them into the view.
func (s *Service) readCart(ctx context.Context, cartID uuid.UUID) (dto.CartView, error) {
	lines, err := s.Carts.Lines(ctx, cartID)
	if err != nil {
		return dto.CartView{}, err
	}
	return s.view(ctx, lines)
}

// view builds the cart view from its lines, reading the live facts of every
// variant in one bulk call and what is available for all of them in a second, so
// the read costs a fixed two cross-module calls however many lines it holds
// (research D6). It shows the variant identifier, the product name and the variant
// name, the quantity, the captured price, the line total and the exact subtotal
// (FR-004, FR-009, FR-018), plus each line's buyable decision and — when the line
// is on sale but short — the currently available quantity (FR-012, research D10).
// A variant that is gone has no name and no availability to report.
func (s *Service) view(ctx context.Context, lines []model.CartLine) (dto.CartView, error) {
	if len(lines) == 0 {
		return s.emptyView(), nil
	}

	variantIDs := make([]uuid.UUID, 0, len(lines))
	for _, line := range lines {
		variantIDs = append(variantIDs, line.VariantID)
	}
	summaries, err := s.Variants.Variants(ctx, variantIDs)
	if err != nil {
		return dto.CartView{}, err
	}
	byID := make(map[uuid.UUID]contracts.VariantSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.VariantID] = summary
	}

	availabilities, err := s.Availability.AvailableQuantity(ctx, variantIDs)
	if err != nil {
		return dto.CartView{}, err
	}
	availableByID := make(map[uuid.UUID]int64, len(availabilities))
	for _, item := range availabilities {
		availableByID[item.VariantID] = item.Available
	}

	views := make([]dto.LineView, 0, len(lines))
	for _, line := range lines {
		var productID uuid.UUID
		var productName, variantName *string
		var productCode, imageURL string
		facts := model.VariantFacts{}
		if summary, ok := byID[line.VariantID]; ok {
			productID = summary.ProductID
			productText, variantText := summary.ProductName, summary.VariantName
			productName, variantName = &productText, &variantText
			productCode = summary.ProductCode
			imageURL = summary.ImageURL
			facts.OnSale = summary.OnSale
		}
		facts.Available = availableByID[line.VariantID]
		views = append(views, s.Mapper.Line(line, facts, productID, productName, variantName, productCode, imageURL))
	}

	cart := model.Cart{Lines: lines}
	subtotal, present := cart.Subtotal()
	return s.Mapper.Cart(views, s.Mapper.Subtotal(subtotal, present)), nil
}

// emptyView is the answer for a customer with no cart and for a cart with no
// lines: an empty list and no subtotal, because an empty cart carries no money
// (FR-004, spec Assumptions).
func (s *Service) emptyView() dto.CartView {
	return s.Mapper.Cart(nil, s.Mapper.Subtotal(model.Price{}, false))
}
