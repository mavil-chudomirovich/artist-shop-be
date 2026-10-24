//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
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
// first and stay separated by owner, that one (product code, SKU) pair is one line
// (FR-020), and that a removed product leaves its line in place — there is no
// foreign key on order_items.product_id (research D3, FR-001, FR-002, FR-018,
// FR-021).
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

// insertProduct writes one category, one on-sale product and its default variant
// straight into the database, so an order line has a real product and variant to
// snap. The test may do this because it is not production code: the boundary the
// constitution protects is between modules, not between a test and a fixture.
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
	code := "product-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, description, price_amount, currency,
			 category_id, position)
		VALUES ($1, $2, $3, '', 1000, 'VND', $4, 0)`,
		productID, "Product "+code, code, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	// The product's default variant: an order line records a variant, so a line
	// has a real variant to snap. It is on sale, the state a migrated product's
	// default variant carries.
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount, position, sell_state)
		VALUES ($1, $2, $3, $4, NULL, 0, 'ACTIVE')`,
		uuid.New(), productID, "Variant "+code, code); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	return productID
}

// testAddress is the delivery address the fixture's orders carry, including the
// Facebook link snapshotted at checkout (feature 021, FR-006).
func testAddress() model.Address {
	return model.Address{
		RecipientName:  "Nguyễn Văn A",
		RecipientPhone: "0912345678",
		ProvinceCode:   "01",
		ProvinceName:   "Hà Nội",
		WardCode:       "00001",
		WardName:       "Phúc Xá",
		StreetAddress:  "1 Đinh Tiên Hoàng",
		FacebookURL:    "https://facebook.com/nguyen.van.a",
	}
}

// buildOrder builds an awaiting-payment order over the given lines, stamping the
// lines with their identifiers, the order's identifier and the created instant. It
// does not use model.NewOrder because the ordering test needs to control each
// line's position.
func buildOrder(owner uuid.UUID, at time.Time, lines []model.OrderLine) *model.Order {
	confirmedAt := at
	paymentDeadline := at.Add(60 * time.Minute)
	order := &model.Order{
		ID:               uuid.New(),
		UserID:           owner,
		Status:           constant.StatusPaymentPending,
		Version:          1,
		Address:          testAddress(),
		ConfirmedAt:      &confirmedAt,
		PaymentExpiresAt: &paymentDeadline,
		CreatedAt:        at,
		UpdatedAt:        at,
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

// newLine builds one snapshot line at the given position. The name doubles as the
// product code and the SKU, which keeps each test's (order_id, product_code, sku)
// natural key distinct without a second parameter.
func newLine(productID uuid.UUID, name string, price, quantity int64, position int) model.OrderLine {
	return model.OrderLine{
		ProductID:   productID,
		VariantID:   uuid.New(),
		ProductName: name,
		ProductCode: name,
		VariantName: name,
		SKU:         name,
		UnitPrice:   model.Price{Amount: price, Currency: "VND"},
		ImageURL:    "https://cdn.example/" + name + ".jpg",
		Quantity:    quantity,
		Position:    position,
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
	if got.Status != constant.StatusPaymentPending {
		t.Errorf("status = %s, want PAYMENT_PENDING", got.Status)
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
	if got.Lines[0].ProductID != first || got.Lines[0].ProductName != "Ink wash" ||
		got.Lines[0].UnitPrice.Amount != 120000 || got.Lines[0].Quantity != 2 {
		t.Errorf("first line does not round-trip: %+v", got.Lines[0])
	}
	if got.Lines[0].ImageURL != order.Lines[0].ImageURL {
		t.Errorf("line image = %q, want the snapshot %q", got.Lines[0].ImageURL, order.Lines[0].ImageURL)
	}
	if got.Version != 1 {
		t.Errorf("order_version = %d, want 1", got.Version)
	}
	if got.ConfirmedAt == nil || !got.ConfirmedAt.Equal(*order.ConfirmedAt) {
		t.Errorf("confirmed_at = %v, want %v", got.ConfirmedAt, order.ConfirmedAt)
	}
	if got.PaymentExpiresAt == nil || !got.PaymentExpiresAt.Equal(*order.PaymentExpiresAt) {
		t.Errorf("payment_expires_at = %v, want %v", got.PaymentExpiresAt, order.PaymentExpiresAt)
	}
}

// T020 [US3]: the storage layer refuses an empty or overlong facebook_url where
// it is stored — the NOT NULL plus length CHECK counterpart of the snapshot rule
// — so an order row can never carry a missing or unbounded link.
func TestTheStorageLayerRefusesAnEmptyOrOverlongOrderFacebookURL(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// The column list mirrors the adapter's own insert, so the test proves what
	// the database refuses, not what the application withholds.
	const query = `
		INSERT INTO orders
			(id, user_id, status, total_amount, currency,
			 recipient_name, recipient_phone,
			 province_code, province_name, ward_code, ward_name, street_address,
			 facebook_url,
			 payment_expires_at, order_version, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, 'PENDING', 1000, 'VND',
			'Nguyễn Văn A', '0912345678',
			'01', 'Hà Nội', '00001', 'Phúc Xá', '1 Đinh Tiên Hoàng',
			$3,
			NULL, 1, NULL, $4, $4)`
	tooLong := "https://facebook.com/" + strings.Repeat("a", 500)
	for _, link := range []string{"", tooLong} {
		if _, err := f.pool.Exec(ctx, query, uuid.New(), uuid.New(), link, now); err == nil {
			t.Fatalf("the storage layer accepted facebook_url %q", link)
		}
	}

	// A missing link is refused by NOT NULL, not by the CHECK.
	const nullQuery = `
		INSERT INTO orders
			(id, user_id, status, total_amount, currency,
			 recipient_name, recipient_phone,
			 province_code, province_name, ward_code, ward_name, street_address,
			 payment_expires_at, order_version, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, 'PENDING', 1000, 'VND',
			'Nguyễn Văn A', '0912345678',
			'01', 'Hà Nội', '00001', 'Phúc Xá', '1 Đinh Tiên Hoàng',
			NULL, 1, NULL, $3, $3)`
	if _, err := f.pool.Exec(ctx, nullQuery, uuid.New(), uuid.New(), now); err == nil {
		t.Fatal("the storage layer accepted a NULL facebook_url")
	}
}

// FR-018, FR-021: the lines come back in position order, whatever order they were
// written in.
func TestLinesComeBackInPositionOrder(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	// One product per line: each (product code, SKU) pair is one order line, so
	// the (order_id, product_code, sku) unique index would refuse a repeated line.
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
		if line.Position != i || line.ProductName != want[i] {
			t.Fatalf("line %d = %q position %d, want %q position %d", i, line.ProductName, line.Position, want[i], i)
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

	got, total, err := f.repo.ListAll(ctx, 1, 20, nil, constant.SortNewest)
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
	if line.ProductID != product || line.ProductName != "Gone" || line.UnitPrice.Amount != 120000 || line.Quantity != 2 {
		t.Fatalf("the snapshot must be unchanged: %+v", line)
	}
	if line.ImageURL != "https://cdn.example/Gone.jpg" {
		t.Fatalf("the line image must survive its product's removal, got %q", line.ImageURL)
	}
}

// FR-002, FR-016: the storage layer refuses a non-HTTPS line image where it is
// stored, so a snapshot can never carry an undisplayable link. An empty value is
// still valid: it means the line was sold without a picture.
func TestTheStorageLayerRefusesANonHTTPSOrderLineImage(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	order := buildOrder(uuid.New(), time.Now().UTC(), []model.OrderLine{newLine(product, "first", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`UPDATE order_items SET image_url = 'http://insecure.example/a.jpg' WHERE order_id = $1`,
		order.ID); err == nil {
		t.Fatal("the storage must refuse a non-HTTPS line image")
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE order_items SET image_url = '' WHERE order_id = $1`, order.ID); err != nil {
		t.Fatalf("the storage must accept a line without an image: %v", err)
	}
}

// FR-015, FR-016, research D6: ReplaceLines replaces the order's line set — it
// drops the old set, inserts the new one and persists the order's total, version
// and state — and the replacement still holds the one-line-per-(product code, SKU)
// rule. The lines come back in position order whatever order they were written in.
func TestReplaceLinesReplacesTheSetAndKeepsOneLinePerProduct(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	first, second, third := f.insertProduct(t), f.insertProduct(t), f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)

	order := buildOrder(uuid.New(), at, []model.OrderLine{
		newLine(first, "first", 1000, 1, 0),
		newLine(second, "second", 2000, 1, 1),
	})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Replace with a new set: drop `second`, add `third`, change first's quantity.
	// Written deliberately out of position order.
	order.Lines = []model.OrderLine{
		newLine(third, "third", 3000, 2, 1),
		newLine(first, "first", 1000, 5, 0),
	}
	order.Total = model.LinesTotal(order.Lines)
	order.Version = 2
	order.Status = constant.StatusPending
	now := at.Add(time.Hour)
	if err := f.repo.ReplaceLines(ctx, order, now); err != nil {
		t.Fatalf("ReplaceLines: %v", err)
	}

	got, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("expected the replaced set of 2 lines, got %d", len(got.Lines))
	}
	if got.Lines[0].ProductID != first || got.Lines[0].Quantity != 5 || got.Lines[0].Position != 0 {
		t.Fatalf("first replaced line does not come first: %+v", got.Lines[0])
	}
	if got.Lines[1].ProductID != third || got.Lines[1].Quantity != 2 || got.Lines[1].Position != 1 {
		t.Fatalf("second replaced line is out of position: %+v", got.Lines[1])
	}
	if got.Total.Amount != 5*1000+2*3000 || got.Version != 2 || got.Status != constant.StatusPending {
		t.Fatalf("the replaced order did not persist its total/version/state: %+v", got)
	}

	// The unique (order_id, product_code, sku) still holds: a second line naming
	// the same product code and SKU on one order is refused by the database.
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO order_items
			(id, order_id, product_id, variant_id, name, product_code, variant_name, sku,
			 unit_price_amount, currency, quantity, position, created_at)
		VALUES ($1, $2, $3, $4, 'dup', 'first', 'dup', 'first', 1000, 'VND', 1, 2, now())`,
		uuid.New(), order.ID, first, uuid.New()); err == nil {
		t.Fatal("the storage must refuse a duplicate (product code, SKU) on one order")
	}
}

// FR-017, research D14: an accepted edit writes an order_edit_history row carrying
// the version the edit produced and the actor; a non-positive version is refused by
// the storage check; and deleting the order cascades to its history.
func TestEditHistoryIsRecordedAndCascades(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	order := buildOrder(uuid.New(), at, []model.OrderLine{newLine(product, "first", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}
	actor := uuid.New()

	record := model.EditHistory{
		OrderID:   order.ID,
		Version:   2,
		ActorID:   actor,
		Before:    model.EditSnapshot{Address: testAddress(), Lines: order.Lines},
		After:     model.EditSnapshot{Address: testAddress(), Lines: order.Lines},
		CreatedAt: at.Add(time.Minute),
	}
	if err := f.repo.InsertEditHistory(ctx, record); err != nil {
		t.Fatalf("InsertEditHistory: %v", err)
	}

	var (
		version     int64
		storedActor uuid.UUID
		beforeLen   int
		afterLen    int
	)
	if err := f.pool.QueryRow(ctx,
		`SELECT version, actor_id, pg_column_size("before"), pg_column_size("after")
		 FROM order_edit_history WHERE order_id = $1`, order.ID).
		Scan(&version, &storedActor, &beforeLen, &afterLen); err != nil {
		t.Fatalf("read edit history: %v", err)
	}
	if version != 2 || storedActor != actor || beforeLen == 0 || afterLen == 0 {
		t.Fatalf("edit history = version %d actor %s before %d after %d", version, storedActor, beforeLen, afterLen)
	}

	// A version below 1 is refused by the storage check (FR-017).
	if err := f.repo.InsertEditHistory(ctx, model.EditHistory{
		OrderID: order.ID, Version: 0, ActorID: actor, CreatedAt: at,
	}); err == nil {
		t.Fatal("the storage must refuse an edit-history version below 1")
	}

	// Deleting the order takes its history with it (ON DELETE CASCADE).
	if _, err := f.pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, order.ID); err != nil {
		t.Fatalf("delete order: %v", err)
	}
	var remaining int64
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_edit_history WHERE order_id = $1`, order.ID).Scan(&remaining); err != nil {
		t.Fatalf("count edit history: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("deleting the order must cascade to its edit history, %d rows remain", remaining)
	}
}

// FR-008, FR-014, research D6, D10: the delivery and completion columns
// round-trip through the adapter: dispatch writes SHIPPING, delivery stamps
// delivered_at, and completion stamps completed_at and completed_by.
func TestDeliveryAndCompletionColumnsRoundTrip(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	order := buildOrder(uuid.New(), at, []model.OrderLine{newLine(product, "ship", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Dispatch.
	if err := order.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if err := order.StartShipping(); err != nil {
		t.Fatalf("StartShipping: %v", err)
	}
	if err := f.repo.StartShipping(ctx, order, at.Add(time.Minute)); err != nil {
		t.Fatalf("repo StartShipping: %v", err)
	}
	dispatched, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID after dispatch: %v", err)
	}
	if dispatched.Status != constant.StatusShipping || dispatched.DeliveredAt != nil || dispatched.CompletedAt != nil {
		t.Fatalf("dispatch must leave the order SHIPPING with no delivery facts: %+v", dispatched)
	}

	// Deliver.
	deliveredAt := at.Add(time.Hour)
	if err := order.MarkDelivered(deliveredAt); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := f.repo.SaveDelivered(ctx, order, deliveredAt.Add(time.Second)); err != nil {
		t.Fatalf("SaveDelivered: %v", err)
	}

	// Complete by a customer.
	completedAt := deliveredAt.Add(2 * time.Hour)
	actor := uuid.New()
	if err := order.Complete(); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	order.CompletedAt = &completedAt
	order.CompletedBy = &actor
	if err := f.repo.SaveCompleted(ctx, order, completedAt.Add(time.Second)); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}

	got, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID after completion: %v", err)
	}
	if got.Status != constant.StatusCompleted {
		t.Fatalf("status = %s, want COMPLETED", got.Status)
	}
	if got.DeliveredAt == nil || !got.DeliveredAt.Equal(deliveredAt) {
		t.Fatalf("delivered_at = %v, want %v", got.DeliveredAt, deliveredAt)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("completed_at = %v, want %v", got.CompletedAt, completedAt)
	}
	if got.CompletedBy == nil || *got.CompletedBy != actor {
		t.Fatalf("completed_by = %v, want %s", got.CompletedBy, actor)
	}
}

// FR-010, research D3: delivery evidence is inserted per stage and reads back
// ordered by (stage, position, id); the count recounts per stage.
func TestDeliveryPhotosRoundTripInStageThenPositionOrder(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	order := buildOrder(uuid.New(), at, []model.OrderLine{newLine(product, "photo", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Written deliberately out of order.
	photos := []model.DeliveryPhoto{
		{Stage: model.StageDelivered, PublicID: "d1", URL: "https://cdn.example/d1", Width: 10, Height: 20, Position: 1},
		{Stage: model.StageShipping, PublicID: "s0", URL: "https://cdn.example/s0", Width: 10, Height: 20, Position: 0},
		{Stage: model.StageShipping, PublicID: "s1", URL: "https://cdn.example/s1", Width: 10, Height: 20, Position: 1},
		{Stage: model.StageDelivered, PublicID: "d0", URL: "https://cdn.example/d0", Width: 10, Height: 20, Position: 0},
	}
	if err := f.repo.AddDeliveryPhotos(ctx, order.ID, photos, at); err != nil {
		t.Fatalf("AddDeliveryPhotos: %v", err)
	}

	got, err := f.repo.ListDeliveryPhotos(ctx, order.ID)
	if err != nil {
		t.Fatalf("ListDeliveryPhotos: %v", err)
	}
	// ORDER BY stage, position: DELIVERED sorts before SHIPPING.
	want := []string{"d0", "d1", "s0", "s1"}
	if len(got) != len(want) {
		t.Fatalf("expected %d photos, got %d", len(want), len(got))
	}
	for i, publicID := range want {
		if got[i].PublicID != publicID {
			t.Fatalf("photo %d = %s, want %s (stage then position order)", i, got[i].PublicID, publicID)
		}
	}

	// The detail read attaches the evidence (research D11).
	detail, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(detail.DeliveryPhotos) != len(want) {
		t.Fatalf("the detail must carry the evidence, got %d", len(detail.DeliveryPhotos))
	}

	// The per-stage count backs the five-photo ceiling (FR-006, FR-008).
	shippingCount, err := f.repo.CountDeliveryPhotos(ctx, order.ID, model.StageShipping)
	if err != nil {
		t.Fatalf("CountDeliveryPhotos(shipping): %v", err)
	}
	if shippingCount != 2 {
		t.Fatalf("shipping count = %d, want 2", shippingCount)
	}
	deliveredCount, err := f.repo.CountDeliveryPhotos(ctx, order.ID, model.StageDelivered)
	if err != nil {
		t.Fatalf("CountDeliveryPhotos(delivered): %v", err)
	}
	if deliveredCount != 2 {
		t.Fatalf("delivered count = %d, want 2", deliveredCount)
	}
}

// FR-016, research D8: an order takes exactly one feedback; the unique index
// refuses a second row, and the detail read projects the feedback.
func TestFeedbackIsOnePerOrder(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	order := buildOrder(uuid.New(), at, []model.OrderLine{newLine(product, "fb", 1000, 1, 0)})
	if err := f.repo.Create(ctx, order); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// No feedback yet.
	if _, err := f.repo.FindFeedback(ctx, order.ID); !errors.Is(err, domainerr.ErrNotFound) {
		t.Fatalf("an order with no feedback must answer not-found, got %v", err)
	}

	author := uuid.New()
	if err := f.repo.InsertFeedback(ctx, model.Feedback{
		OrderID: order.ID, AuthorID: author, Content: "Cảm ơn bạn", CreatedAt: at,
	}); err != nil {
		t.Fatalf("InsertFeedback: %v", err)
	}

	stored, err := f.repo.FindFeedback(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindFeedback: %v", err)
	}
	if stored.Content != "Cảm ơn bạn" || stored.AuthorID != author {
		t.Fatalf("feedback did not round-trip: %+v", stored)
	}

	// FR-025: feedback photos insert and read back in position order, and the
	// feedback read attaches them.
	photos := []model.FeedbackPhoto{
		{PublicID: "p1", URL: "https://cdn.example/p1", Width: 10, Height: 20, Position: 1},
		{PublicID: "p0", URL: "https://cdn.example/p0", Width: 10, Height: 20, Position: 0},
	}
	if err := f.repo.InsertFeedbackPhotos(ctx, stored.ID, photos, at); err != nil {
		t.Fatalf("InsertFeedbackPhotos: %v", err)
	}
	count, err := f.repo.CountFeedbackPhotos(ctx, stored.ID)
	if err != nil {
		t.Fatalf("CountFeedbackPhotos: %v", err)
	}
	if count != 2 {
		t.Fatalf("feedback photo count = %d, want 2", count)
	}
	listed, err := f.repo.ListFeedbackPhotos(ctx, stored.ID)
	if err != nil {
		t.Fatalf("ListFeedbackPhotos: %v", err)
	}
	if len(listed) != 2 || listed[0].PublicID != "p0" || listed[1].PublicID != "p1" {
		t.Fatalf("feedback photos must read back in position order, got %+v", listed)
	}
	attached, err := f.repo.FindFeedback(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindFeedback: %v", err)
	}
	if len(attached.Photos) != 2 || attached.Photos[0].PublicID != "p0" {
		t.Fatalf("FindFeedback must attach the photos in position order, got %+v", attached.Photos)
	}

	// A second feedback is refused by the unique index.
	if err := f.repo.InsertFeedback(ctx, model.Feedback{
		OrderID: order.ID, AuthorID: uuid.New(), Content: "again", CreatedAt: at,
	}); err == nil {
		t.Fatal("the unique index must refuse a second feedback for one order")
	}

	// The detail read attaches the feedback (research D11).
	detail, err := f.repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if detail.Feedback == nil || detail.Feedback.Content != "Cảm ơn bạn" {
		t.Fatalf("the detail must carry the feedback, got %+v", detail.Feedback)
	}
	if len(detail.Feedback.Photos) != 2 {
		t.Fatalf("the detail must carry the feedback photos, got %d", len(detail.Feedback.Photos))
	}

	// FR-025: a removed feedback takes its photos with it, through the cascading
	// foreign key.
	if _, err := f.pool.Exec(ctx, `DELETE FROM order_feedback WHERE id = $1`, stored.ID); err != nil {
		t.Fatalf("delete the feedback: %v", err)
	}
	var remaining int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_feedback_photos WHERE feedback_id = $1`, stored.ID).Scan(&remaining); err != nil {
		t.Fatalf("count the remaining photos: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("removing the feedback must cascade to its photos, got %d", remaining)
	}
}

// FR-012, research D6: ListAwaitingCompletion selects only DELIVERED orders whose
// delivered_at is at or before the cutoff, oldest delivered first; an order still
// in transit is never selected.
func TestListAwaitingCompletionSelectsOnlyDeliveredPastTheCutoff(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	deliver := func(at time.Time) *model.Order {
		order := buildOrder(uuid.New(), now.Add(-3*time.Hour), []model.OrderLine{newLine(product, "o-"+at.Format("150405.000"), 1000, 1, 0)})
		if err := f.repo.Create(ctx, order); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := order.MarkPaid(); err != nil {
			t.Fatalf("MarkPaid: %v", err)
		}
		if err := order.StartShipping(); err != nil {
			t.Fatalf("StartShipping: %v", err)
		}
		if err := f.repo.StartShipping(ctx, order, at); err != nil {
			t.Fatalf("repo StartShipping: %v", err)
		}
		if err := order.MarkDelivered(at); err != nil {
			t.Fatalf("MarkDelivered: %v", err)
		}
		if err := f.repo.SaveDelivered(ctx, order, at); err != nil {
			t.Fatalf("repo SaveDelivered: %v", err)
		}
		return order
	}

	older := deliver(now.Add(-2 * time.Hour))
	newer := deliver(now.Add(-time.Hour))
	deliver(now.Add(time.Hour)) // delivered, but within the window

	// An in-transit order with no delivered_at must never be selected.
	shipping := buildOrder(uuid.New(), now.Add(-3*time.Hour), []model.OrderLine{newLine(product, "transit", 1000, 1, 0)})
	if err := f.repo.Create(ctx, shipping); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := shipping.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if err := shipping.StartShipping(); err != nil {
		t.Fatalf("StartShipping: %v", err)
	}
	if err := f.repo.StartShipping(ctx, shipping, now); err != nil {
		t.Fatalf("repo StartShipping: %v", err)
	}

	ids, err := f.repo.ListAwaitingCompletion(ctx, now)
	if err != nil {
		t.Fatalf("ListAwaitingCompletion: %v", err)
	}
	if len(ids) != 2 || ids[0] != older.ID || ids[1] != newer.ID {
		t.Fatalf("ListAwaitingCompletion = %v, want the two past-window delivered orders oldest first (%s, %s)",
			ids, older.ID, newer.ID)
	}
}

// FR-008, FR-009, research D5: ListExpiredPending selects only awaiting-payment
// orders whose payment deadline has passed; an order awaiting the artist (no
// deadline) and one whose deadline is still in the future are never selected.
func TestListExpiredPendingSelectsOnlyAwaitingPaymentPastTheDeadline(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	expired := buildOrder(uuid.New(), now.Add(-2*time.Hour), []model.OrderLine{newLine(product, "expired", 1000, 1, 0)})
	expired.PaymentExpiresAt = &past
	fresh := buildOrder(uuid.New(), now.Add(-2*time.Hour), []model.OrderLine{newLine(product, "fresh", 1000, 1, 0)})
	fresh.PaymentExpiresAt = &future
	awaitingArtist := buildOrder(uuid.New(), now.Add(-2*time.Hour), []model.OrderLine{newLine(product, "pending", 1000, 1, 0)})
	awaitingArtist.Status = constant.StatusPending
	awaitingArtist.ConfirmedAt = nil
	awaitingArtist.PaymentExpiresAt = nil

	for _, order := range []*model.Order{expired, fresh, awaitingArtist} {
		if err := f.repo.Create(ctx, order); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	ids, err := f.repo.ListExpiredPending(ctx, now)
	if err != nil {
		t.Fatalf("ListExpiredPending: %v", err)
	}
	if len(ids) != 1 || ids[0] != expired.ID {
		t.Fatalf("ListExpiredPending = %v, want only the past-deadline awaiting-payment order %s", ids, expired.ID)
	}
}

// FR-021, FR-026, research D11: the administrator list filters by state and orders
// oldest-first on request — status=PENDING&sort=oldest is the FIFO confirmation
// queue — while the default remains newest-first over every order.
func TestAdminListFiltersByStatusAndSortsOldestFirst(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	product := f.insertProduct(t)
	base := time.Now().UTC().Truncate(time.Millisecond)

	olderPending := buildOrder(uuid.New(), base.Add(-2*time.Hour), []model.OrderLine{newLine(product, "p1", 1000, 1, 0)})
	olderPending.Status = constant.StatusPending
	newerPending := buildOrder(uuid.New(), base.Add(-time.Hour), []model.OrderLine{newLine(product, "p2", 1000, 1, 0)})
	newerPending.Status = constant.StatusPending
	paid := buildOrder(uuid.New(), base, []model.OrderLine{newLine(product, "paid", 1000, 1, 0)})
	paid.Status = constant.StatusPaid

	for _, order := range []*model.Order{newerPending, paid, olderPending} {
		if err := f.repo.Create(ctx, order); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	pendingStatus := constant.StatusPending
	got, total, err := f.repo.ListAll(ctx, 1, 20, &pendingStatus, constant.SortOldest)
	if err != nil {
		t.Fatalf("ListAll(PENDING, oldest): %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("the status filter must return exactly the two PENDING orders, got %d/%d", total, len(got))
	}
	if got[0].ID != olderPending.ID || got[1].ID != newerPending.ID {
		t.Fatalf("status=PENDING&sort=oldest must be FIFO, got %s then %s", got[0].ID, got[1].ID)
	}

	all, allTotal, err := f.repo.ListAll(ctx, 1, 20, nil, constant.SortNewest)
	if err != nil {
		t.Fatalf("ListAll(default): %v", err)
	}
	if allTotal != 3 || len(all) != 3 {
		t.Fatalf("an unfiltered list must return every order, got %d/%d", allTotal, len(all))
	}
	if all[0].ID != paid.ID {
		t.Fatalf("the default list must be newest first, got %s first", all[0].ID)
	}
}
