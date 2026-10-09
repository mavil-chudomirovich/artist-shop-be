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
	// Products answers a product's name, slug, on-sale state and price. It is
	// supplied by module 04's adapter at the composition root, so the cart never
	// reads module 04's table (research D1).
	Products appinterface.ProductCatalog
	// Availability answers what is currently available. It is supplied by module
	// 05's adapter at the composition root. US1 never reads it — the buyable
	// projection and the refusal it supports are US2's (research D2).
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

// Get returns the caller's cart, re-checking each line's live product facts. A
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

// Add adds a product with a positive whole quantity, creating the cart on the
// first add and raising an existing line rather than adding a second one
// (FR-001, FR-002). The price is captured from the catalogue at add time (FR-008)
// and a product absent from the result is the shared not-found (FR-005).
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
		summary, err := s.product(txCtx, in.ProductID)
		if err != nil {
			return err
		}
		line, err := model.NewCartLine(in.ProductID, in.Quantity, model.Price{
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

// ChangeQuantity sets the line's quantity. A line this cart does not hold answers
// the shared not-found (FR-003, error-codes.md). It runs inside the transaction
// that locks the cart's row (research D9).
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
		return s.Carts.SetQuantity(txCtx, cart.ID, in.ProductID, in.Quantity, now)
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
		return s.Carts.DeleteLine(txCtx, cart.ID, in.ProductID)
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

// product reads one product's facts through the bulk catalogue. A product the
// result does not carry is not found (research D1).
func (s *Service) product(ctx context.Context, productID uuid.UUID) (contracts.ProductSummary, error) {
	summaries, err := s.Products.Products(ctx, []uuid.UUID{productID})
	if err != nil {
		return contracts.ProductSummary{}, err
	}
	for _, summary := range summaries {
		if summary.ID == productID {
			return summary, nil
		}
	}
	return contracts.ProductSummary{}, domainerr.ErrProductNotFound
}

// readCart reads the cart's lines and maps them into the view.
func (s *Service) readCart(ctx context.Context, cartID uuid.UUID) (dto.CartView, error) {
	lines, err := s.Carts.Lines(ctx, cartID)
	if err != nil {
		return dto.CartView{}, err
	}
	return s.view(ctx, lines)
}

// view builds the cart view from its lines, reading the live name and slug of
// every product in one bulk call. US1 shows the product identifier, the current
// name and slug, the quantity, the captured price and the line total, plus the
// exact subtotal (FR-004, FR-009). The buyable projection and the available
// quantity are US2's.
func (s *Service) view(ctx context.Context, lines []model.CartLine) (dto.CartView, error) {
	if len(lines) == 0 {
		return s.emptyView(), nil
	}

	productIDs := make([]uuid.UUID, 0, len(lines))
	for _, line := range lines {
		productIDs = append(productIDs, line.ProductID)
	}
	summaries, err := s.Products.Products(ctx, productIDs)
	if err != nil {
		return dto.CartView{}, err
	}
	byID := make(map[uuid.UUID]contracts.ProductSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.ID] = summary
	}

	views := make([]dto.LineView, 0, len(lines))
	for _, line := range lines {
		var name, slug *string
		facts := model.ProductFacts{}
		if summary, ok := byID[line.ProductID]; ok {
			nameText, slugText := summary.Name, summary.Slug
			name, slug = &nameText, &slugText
			facts.OnSale = summary.OnSale
		}
		views = append(views, s.Mapper.Line(line, facts, name, slug))
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
