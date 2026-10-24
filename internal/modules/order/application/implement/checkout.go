// Package implement holds the order module's use cases. The checkout use case
// turns a signed-in customer's cart into an order; the lifecycle, desk and
// transfer use cases of later phases live beside it.
package implement

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// errNoActor reports a use case reached without a session behind the call. It
// cannot happen through the routes — the handler resolves the session first and
// answers UNAUTHENTICATED before calling — so it is a programming error that maps
// to INTERNAL_ERROR rather than a client-facing refusal.
var errNoActor = errors.New("order: no acting account in the context")

// Service implements the order module's use cases. It owns the order's own state
// machine (in the domain) and reaches every other module through the contracts in
// internal/contracts; it never reads another module's tables (Constitution I).
type Service struct {
	// Orders persists and reads the order and its snapshot lines. It never opens
	// a transaction; the application owns every boundary (Constitution I).
	Orders appinterface.OrderRepository
	// Carts reads the caller's cart and empties it once the order exists. It is
	// supplied by module 06's adapter at the composition root, so the order never
	// reads the cart's tables (research D1).
	Carts appinterface.CartCheckout
	// Variants answers a line's current product and variant facts: the product
	// name and code, the variant name and SKU, whether the variant may be sold and
	// its current effective price. It is supplied by module 04's adapter at the
	// composition root (research D6).
	Variants appinterface.ProductVariants
	// Availability answers what is currently available for a line. It is
	// supplied by module 05's adapter at the composition root (research D2).
	Availability appinterface.InventoryAvailability
	// Reservations holds, consumes and releases goods through module 05, and
	// answers the hold window the order derives its own expiry from (research
	// D4, D6). The order stores no stock of its own.
	Reservations appinterface.InventoryReservation
	// Customers answers the customer's delivery addresses. It is supplied by
	// module 02 at the composition root (research D7).
	Customers appinterface.CustomerLookupService
	// Accounts resolves a transfer recipient by email, so the order can name a
	// recipient without reading the auth module's table. It is supplied by
	// module 01's adapter at the composition root (research D8).
	Accounts appinterface.AccountLookup
	// Tx owns every transaction boundary, so the order, its lines, the held
	// goods and the emptied cart commit together or not at all (Constitution II).
	Tx appinterface.UnitOfWork
	// Clock stamps the order's creation and expiry. Only "what time is it" is
	// injected, never a rule.
	Clock appinterface.Clock
	// Audit records the module's mutations — the operator's confirmation,
	// rejection, dispatch, delivery and transfer, and the customer's confirmation
	// of receipt — naming the acting account and the order (FR-019, FR-023,
	// Constitution VI). A nil auditor is tolerated so a read-only construction
	// cannot panic on a mutation it never serves.
	Audit appinterface.Auditor
	// Notifier sends the module's emails: the artist's confirmation-needed note
	// and the customer's status-change note. Sending is best-effort and runs
	// after the transaction commits, so it never fails a business operation
	// (FR-019 to FR-021, research D9). A nil notifier is tolerated.
	Notifier appinterface.Notifier
	// Events fans live SSE events out to subscribers: admins learn an order
	// needs confirmation or carries feedback, the owner learns its status
	// changed. Publishing is best-effort after commit, like the Notifier, and
	// never fails a business operation (specs/020-order-sse-stream). A nil
	// publisher is tolerated.
	Events appinterface.EventPublisher
	// Media stores the order's dispatch and delivery evidence photos through the
	// shared media port — the same Cloudinary-backed store the user and product
	// modules already use. It is supplied by the composition root; the read paths
	// never call it, and a delivery use case refuses an upload when it is unset
	// (FR-009, FR-010, research D4). Photos are stored outside any transaction and
	// their rows inserted inside, so no database lock is held across a provider
	// call (research D5).
	Media appinterface.MediaStore
	// Cancellations tells the payment module (08) that an order was cancelled, so
	// the provider payment request it created is cancelled too. It is the
	// contract module 08 provides at the composition root (research D9). The call
	// is made after the transaction commits and is best-effort: a nil port is a
	// no-op, and a provider failure is logged by the payment module and never
	// fails the cancellation. A nil port is tolerated so a construction without
	// module 08 wired (or a read-only one) cannot panic.
	Cancellations appinterface.PaymentCancellation
	// ArtistEmail is the shop operator's address, the recipient of the
	// confirmation-needed note (research D9).
	ArtistEmail string
	// AppName is the shop name rendered in the notification emails.
	AppName string
	// FrontendURL is the customer-facing origin the email call-to-action links to.
	FrontendURL string
	// SupportEmail is the reply-to address shown in the notification emails. It
	// falls back to ArtistEmail when unset.
	SupportEmail string
	// Location formats the timestamps shown in the notification emails. It falls
	// back to UTC when unset.
	Location *time.Location
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// New creates the order use-case service.
func New(service Service) *Service { return &service }

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset.
func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// actor returns the acting account taken from the context the session filled.
func (s *Service) actor(ctx context.Context) (appinterface.Actor, error) {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return appinterface.Actor{}, errNoActor
	}
	return actor, nil
}

// Checkout turns the caller's cart into an order awaiting the artist's
// confirmation: it re-checks every line against the product's current sale state
// and price and against what is available, picks the delivery address, snapshots
// the lines and the address, creates the order holding nothing, and empties the
// cart — all inside one UnitOfWork transaction, so nothing is created from a
// partly-recorded cart (FR-001 to FR-008). The owner is the session's, never an
// input (FR-020).
func (s *Service) Checkout(ctx context.Context, in dto.CheckoutInput) (dto.OrderView, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return dto.OrderView{}, err
	}

	var view dto.OrderView
	var created *model.Order
	if err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		order, err := s.checkout(txCtx, actor.ID, in)
		if err != nil {
			return err
		}
		created = order
		view = s.Mapper.Order(*order)
		return nil
	}); err != nil {
		return dto.OrderView{}, err
	}
	// The artist is told the new order needs confirmation. The note is sent after
	// the transaction committed and is best-effort, so a delivery failure never
	// fails the checkout (FR-019, FR-021, research D9).
	s.notifyArtistConfirmationNeeded(ctx, created)
	s.publishNeedsConfirmation(ctx, created)
	return view, nil
}

// checkout is the body of the checkout, run inside the transaction. It refuses
// the whole checkout on the first line that is off sale or removed, whose price
// moved, or that asks for more than is available, and on a customer with no
// address; nothing is created when it refuses (FR-004, FR-005, FR-006, FR-007).
func (s *Service) checkout(ctx context.Context, ownerID uuid.UUID, in dto.CheckoutInput) (*model.Order, error) {
	lines, err := s.Carts.CartLines(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, domainerr.ErrCartEmpty
	}

	variantIDs := make([]uuid.UUID, 0, len(lines))
	for _, line := range lines {
		variantIDs = append(variantIDs, line.VariantID)
	}

	// One bulk read of the variant facts and one of availability, however many
	// lines the cart holds (plan Performance Goals).
	summaries, err := s.Variants.Variants(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]contracts.VariantSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.VariantID] = summary
	}

	availabilities, err := s.Availability.AvailableQuantity(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	availableByID := make(map[uuid.UUID]int64, len(availabilities))
	for _, item := range availabilities {
		availableByID[item.VariantID] = item.Available
	}

	snapshot := make([]model.OrderLine, 0, len(lines))
	for _, line := range lines {
		summary, ok := byID[line.VariantID]
		if !ok || !summary.OnSale {
			return nil, domainerr.ItemNotPurchasable(line.VariantID)
		}
		if summary.Price.Amount != line.UnitPrice.Amount || summary.Price.Currency != line.UnitPrice.Currency {
			return nil, domainerr.ItemPriceChanged(line.VariantID)
		}
		available := availableByID[line.VariantID]
		if line.Quantity > available {
			return nil, domainerr.QuantityExceedsAvailable(line.VariantID, available, line.Quantity)
		}
		snapshot = append(snapshot, model.OrderLine{
			ProductID:   summary.ProductID,
			VariantID:   summary.VariantID,
			ProductName: summary.ProductName,
			ProductCode: summary.ProductCode,
			VariantName: summary.VariantName,
			SKU:         summary.SKU,
			UnitPrice:   model.Price{Amount: summary.Price.Amount, Currency: summary.Price.Currency},
			Category:    categorySnapshot(summary),
			ImageURL:    summary.ImageURL,
			Quantity:    line.Quantity,
		})
	}

	address, err := s.deliveryAddress(ctx, ownerID, in.AddressID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	order := model.NewOrder(ownerID, address, snapshot, now)

	if err := s.Orders.Create(ctx, order); err != nil {
		return nil, err
	}

	// Checkout holds nothing: the order waits for the artist's confirmation and
	// the goods are set aside only then, all-or-nothing (FR-001, FR-002, research
	// D2). Clearing the cart completes the checkout.
	if err := s.Carts.ClearCart(ctx, ownerID); err != nil {
		return nil, err
	}
	return order, nil
}

// deliveryAddress picks the delivery address the order snapshots: the one the
// customer named, or the default when none is named. A customer with no address
// is refused, and an identifier that is not one of the customer's addresses is a
// request-shape refusal naming `addressId` (FR-003, error-codes.md, research D7).
func (s *Service) deliveryAddress(ctx context.Context, ownerID uuid.UUID, addressID *uuid.UUID) (model.Address, error) {
	customer, err := s.Customers.LookupCustomer(ctx, ownerID)
	if err != nil {
		if errors.Is(err, contracts.ErrCustomerNotFound) {
			return model.Address{}, domainerr.ErrNoAddress
		}
		return model.Address{}, err
	}

	if addressID != nil {
		for _, address := range customer.Addresses {
			if address.ID == *addressID {
				return toAddress(address)
			}
		}
		return model.Address{}, domainerr.InvalidValue(model.FieldAddressID, "is not one of the customer's addresses")
	}

	for _, address := range customer.Addresses {
		if address.IsDefault {
			return toAddress(address)
		}
	}
	if len(customer.Addresses) == 0 {
		return model.Address{}, domainerr.ErrNoAddress
	}
	// The contract orders addresses default-first; when none is flagged default,
	// the first is the most recently updated and is the fallback.
	return toAddress(customer.Addresses[0])
}

// available reads what is currently available for one variant through the bulk
// contract, used to name the remaining amount when a hold could not be taken. A
// variant the result does not carry is read as zero.
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

// toAddress maps the contract's address DTO onto the order's snapshot value. It
// copies the values, so a later edit to the customer's address never changes a
// placed order (FR-003).
//
// An address still carrying the legacy placeholder, or no link at all, is refused
// with a VALIDATION_ERROR naming `facebookUrl`: a new order must never snapshot
// a link the customer was already told to replace, and an empty link would only
// die at the storage CHECK as a 500-class error instead of the 400 the contract
// promises (feature 021, FR-007, FR-009). The two cases carry distinct messages so
// the customer knows whether the link is missing or still the legacy placeholder.
// The edit path shares this guard through deliveryAddress, so switching an order
// to such an address is refused the same way.
func toAddress(address contracts.CustomerAddress) (model.Address, error) {
	if strings.TrimSpace(address.FacebookURL) == "" {
		return model.Address{}, domainerr.InvalidValue(model.FieldFacebookURL,
			"is missing; update the address with a real Facebook link first")
	}
	if address.FacebookURL == model.PlaceholderFacebookURL {
		return model.Address{}, domainerr.InvalidValue(model.FieldFacebookURL,
			"carries the legacy placeholder; update the address with a real Facebook link first")
	}
	return model.Address{
		RecipientName:  address.RecipientName,
		RecipientPhone: address.RecipientPhone,
		ProvinceCode:   address.ProvinceCode,
		ProvinceName:   address.ProvinceName,
		WardCode:       address.WardCode,
		WardName:       address.WardName,
		StreetAddress:  address.StreetAddress,
		FacebookURL:    address.FacebookURL,
	}, nil
}

// categorySnapshot copies the category pair from the variant facts onto an order
// line. A zero identifier means the provider did not resolve a category (a
// misconfigured or test-only facts source; production facts always populate it for
// a visible variant), and is recorded as no snapshot rather than as the nil UUID
// (FR-022, data-model.md).
func categorySnapshot(summary contracts.VariantSummary) *model.CategorySnapshot {
	if summary.CategoryID == uuid.Nil {
		return nil
	}
	return &model.CategorySnapshot{ID: summary.CategoryID, Name: summary.CategoryName}
}
