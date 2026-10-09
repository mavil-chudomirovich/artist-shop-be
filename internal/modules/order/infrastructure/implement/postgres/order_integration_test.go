//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the order adapter against a real PostgreSQL container. Its
// centre of gravity is what a fake repository cannot prove: that an order and its
// snapshot lines round-trip, that the lines come back in position order whatever
// order they were written in, that the owner and admin lists come back newest
// first and stay separated by owner, and that a removed product leaves its line in
// place — there is no foreign key on order_items.product_id (research D3, FR-001,
// FR-002, FR-018, FR-021).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// orderFixture is one migrated PostgreSQL container plus the adapter under test.
type orderFixture struct {
	pool *pgxpool.Pool
	repo *OrderRepository
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return &orderFixture{pool: pool, repo: NewOrderRepository(pool)}
}

// insertProduct writes one category and one product straight into the database, so
// an order line has a real product to record. The test may do this because it is
// not production code: the boundary the constitution protects is between modules,
// not between a test and a fixture.
func (f *orderFixture) insertProduct(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	categoryID := uuid.New()
	suffix := categoryID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`,
		categoryID, "Category "+suffix, "category-"+suffix,
		"category-"+suffix, "category-"+suffix); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	productID := uuid.New()
	slug := "product-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder)
		VALUES ($1, $2, $3, $4, '', 1000, 'VND', $5, 0, 'ACTIVE', false, false)`,
		productID, "Product "+slug, slug, slug, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return productID
}

// testAddress is the delivery address the fixture's orders carry.
func testAddress() model.Address {
	return model.Address{
		RecipientName:  "Nguyễn Văn A",
		RecipientPhone: "0912345678",
		ProvinceCode:   "01",
		ProvinceName:   "Hà Nội",
		WardCode:       "00001",
		WardName:       "Phúc Xá",
		StreetAddress:  "1 Đinh Tiên Hoàng",
	}
}

// buildOrder builds an order over the given lines, stamping the lines with their
// identifiers, the order's identifier and the created instant. It does not use
// model.NewOrder because the ordering test needs to control each line's position.
func buildOrder(owner uuid.UUID, at time.Time, lines []model.OrderLine) *model.Order {
	order := &model.Order{
		ID:        uuid.New(),
		UserID:    owner,
		Status:    constant.StatusPendingPayment,
		Address:   testAddress(),
		ExpiresAt: at.Add(15 * time.Minute),
		CreatedAt: at,
		UpdatedAt: at,
	}
	for i := range lines {
		lines[i].ID = uuid.New()
		lines[i].OrderID = order.ID
		lines[i].CreatedAt = at
	}
	order.Lines = lines
	order.Total = model.LinesTotal(lines)
	return order
}

// newLine builds one snapshot line at the given position.
func newLine(productID uuid.UUID, name string, price, quantity int64, position int) model.OrderLine {
	return model.OrderLine{
		ProductID: productID,
		Name:      name,
		Slug:      name,
		UnitPrice: model.Price{Amount: price, Currency: "VND"},
		Quantity:  quantity,
		Position:  position,
	}
}

// FR-001, FR-002: an order and its snapshot lines round-trip, carrying the
// committed total and the delivery-address snapshot.
func TestOrderRoundTripsWithItsSnapshot(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	owner := uuid.New()
	first, second := f.insertProduct(t), f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)

	order := buildOrder(owner, at, []model.OrderLine{
		newLine(first, "Ink wash", 120000, 2, 0),
		newLine(second, "Silk scroll", 33333, 3, 1),
	})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != constant.StatusPendingPayment {
		t.Errorf("status = %s, want PENDING_PAYMENT", got.Status)
	}
	if got.UserID != owner {
		t.Errorf("owner = %s, want %s", got.UserID, owner)
	}
	if got.Total.Amount != 2*120000+3*33333 || got.Total.Currency != "VND" {
		t.Errorf("total = %+v, want 339999 VND", got.Total)
	}
	if got.Address != testAddress() {
		t.Errorf("address = %+v, want the captured snapshot %+v", got.Address, testAddress())
	}
	if len(got.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got.Lines))
	}
	if got.Lines[0].ProductID != first || got.Lines[0].Name != "Ink wash" ||
		got.Lines[0].UnitPrice.Amount != 120000 || got.Lines[0].Quantity != 2 {
		t.Errorf("first line does not round-trip: %+v", got.Lines[0])
	}
	if !got.ExpiresAt.Equal(order.ExpiresAt) {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, order.ExpiresAt)
	}
}

// FR-018, FR-021: the lines come back in position order, whatever order they were
// written in.
func TestLinesComeBackInPositionOrder(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	// One product per line: an order holds at most one line per product, so the
	// (order_id, product_id) unique index would refuse a repeated product.
	first, second, third := f.insertProduct(t), f.insertProduct(t), f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)

	// Written deliberately out of order; the read must still be 0, 1, 2.
	order := buildOrder(uuid.New(), at, []model.OrderLine{
		newLine(third, "third", 3000, 1, 2),
		newLine(first, "first", 1000, 1, 0),
		newLine(second, "second", 2000, 1, 1),
	})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	want := []string{"first", "second", "third"}
	for i, line := range got.Lines {
		if line.Position != i || line.Name != want[i] {
			t.Fatalf("line %d = %q position %d, want %q position %d", i, line.Name, line.Position, want[i], i)
		}
	}
}

// FR-018, research D14: a customer's list is their own orders only, newest first,
// with the line count.
func TestOwnerListIsTheOwnersOnlyNewestFirst(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	product := f.insertProduct(t)
	base := time.Now().UTC().Truncate(time.Millisecond)

	older := buildOrder(owner, base.Add(-2*time.Hour), []model.OrderLine{newLine(product, "older", 1000, 1, 0)})
	middle := buildOrder(owner, base.Add(-1*time.Hour), []model.OrderLine{newLine(product, "middle", 1000, 2, 0)})
	newest := buildOrder(owner, base, []model.OrderLine{newLine(product, "newest", 1000, 3, 0)})
	foreign := buildOrder(other, base, []model.OrderLine{newLine(product, "foreign", 1000, 1, 0)})
	for _, order := range []*model.Order{older, middle, newest, foreign} {
		if err := f.repo.Create(ctx, order); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, total, err := f.repo.ListOwner(ctx, owner, 1, 20)
	if err != nil {
		t.Fatalf("ListOwner: %v", err)
	}
	if total != 3 || len(got) != 3 {
		t.Fatalf("expected 3 orders for the owner, got %d/%d", total, len(got))
	}
	if got[0].ID != newest.ID || got[1].ID != middle.ID || got[2].ID != older.ID {
		t.Fatalf("the owner list is not newest first: %s, %s, %s", got[0].ID, got[1].ID, got[2].ID)
	}
	if got[0].ItemCount != 1 || got[2].ItemCount != 1 {
		t.Errorf("the list must carry the line count, got %d and %d", got[0].ItemCount, got[2].ItemCount)
	}

	otherList, otherTotal, err := f.repo.ListOwner(ctx, other, 1, 20)
	if err != nil {
		t.Fatalf("ListOwner(other): %v", err)
	}
	if otherTotal != 1 || len(otherList) != 1 || otherList[0].ID != foreign.ID {
		t.Fatalf("another customer's list leaked: total %d, ids %+v", otherTotal, otherList)
	}
}

// FR-021, research D14: the operator's list is every order, newest first, with the
// owner.
func TestAdminListIsEveryOrderNewestFirst(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	base := time.Now().UTC().Truncate(time.Millisecond)

	older := buildOrder(uuid.New(), base.Add(-time.Hour), []model.OrderLine{newLine(product, "older", 1000, 1, 0)})
	newest := buildOrder(uuid.New(), base, []model.OrderLine{newLine(product, "newest", 1000, 1, 0)})
	for _, order := range []*model.Order{older, newest} {
		if err := f.repo.Create(ctx, order); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, total, err := f.repo.ListAll(ctx, 1, 20)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("expected 2 orders, got %d/%d", total, len(got))
	}
	if got[0].ID != newest.ID || got[1].ID != older.ID {
		t.Fatalf("the admin list is not newest first: %s, %s", got[0].ID, got[1].ID)
	}
	if got[0].UserID != newest.UserID {
		t.Errorf("the admin list must carry the owner, got %s", got[0].UserID)
	}
}

// FR-020: reading an order by owner answers not-found for another customer, so the
// route never confirms an order that is not the caller's.
func TestFindByOwnerRefusesAnotherCustomersOrder(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	product := f.insertProduct(t)

	order := buildOrder(owner, time.Now().UTC(), []model.OrderLine{newLine(product, "thing", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := f.repo.FindByOwner(ctx, other, order.ID); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another customer, got %v", err)
	}
	if _, err := f.repo.FindByOwner(ctx, owner, order.ID); err != nil {
		t.Fatalf("the owner must read their own order: %v", err)
	}
}

// FR-002, research D3: a product removed after an order used it leaves the line in
// place — there is no foreign key on order_items.product_id — and the line's
// snapshot still reads fine.
func TestRemovedProductLeavesTheLine(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)

	order := buildOrder(uuid.New(), time.Now().UTC(), []model.OrderLine{newLine(product, "Gone", 120000, 2, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, product); err != nil {
		t.Fatalf("delete product: %v", err)
	}

	got, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID after removal: %v", err)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("the line must survive its product's removal, got %+v", got.Lines)
	}
	line := got.Lines[0]
	if line.ProductID != product || line.Name != "Gone" || line.UnitPrice.Amount != 120000 || line.Quantity != 2 {
		t.Fatalf("the snapshot must be unchanged: %+v", line)
	}
}
