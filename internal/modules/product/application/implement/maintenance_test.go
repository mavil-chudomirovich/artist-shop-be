package implement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file exercises the administrator maintenance use cases over an in-memory
// repository and a recording auditor. Every write must record the audit action
// data-model.md's transition table specifies (FR-013, SC-006) and the actor used
// in the audit row must be the one the caller put in the context, never a value
// drawn from the request.

// repoCall is one repository operation and whether it ran inside the caller's
// transaction. Picture tests assert that the ceiling check and the insert share
// the transaction (research D6).
type repoCall struct {
	op   string
	inTx bool
}

// fauxProducts is an in-memory ProductRepository for the administrator use
// cases. It embeds the interface so only the operations the tests reach are
// implemented; calling an unimplemented one panics, which is a defect the tests
// must not paper over.
type fauxProducts struct {
	domainrepo.ProductRepository

	mu       sync.Mutex
	products map[uuid.UUID]model.Product
	pictures map[uuid.UUID][]model.Picture
	// members records a set's member list in order, keyed by the set's
	// identifier. It is what ListSetMembers answers from, so a test can observe
	// what the use case asked the repository to store (FR-039).
	members map[uuid.UUID][]uuid.UUID

	// categories, when non-nil, is the set of category identifiers this fake
	// knows. A create or update naming one outside it is refused the way the
	// adapter classifies the foreign-key violation (FR-032). A nil map accepts
	// any category, which keeps the public read tests independent of a fixture.
	categories map[uuid.UUID]bool

	// calls records the operations in order, with whether each ran inside the
	// caller's transaction.
	calls []repoCall
}

func newFauxProducts() *fauxProducts {
	return &fauxProducts{
		products: make(map[uuid.UUID]model.Product),
		pictures: make(map[uuid.UUID][]model.Picture),
		members:  make(map[uuid.UUID][]uuid.UUID),
	}
}

// record appends one operation and whether it ran inside a transaction.
func (m *fauxProducts) record(ctx context.Context, op string) {
	m.calls = append(m.calls, repoCall{op: op, inTx: inTx(ctx)})
}

// ops returns the recorded operations with the given name.
func (m *fauxProducts) ops(name string) []repoCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]repoCall, 0, len(m.calls))
	for _, call := range m.calls {
		if call.op == name {
			out = append(out, call)
		}
	}
	return out
}

// seed stores one product built through the domain constructor.
func (m *fauxProducts) seed(t *testing.T, draft model.ProductDraft, createdAt time.Time) *model.Product {
	t.Helper()
	product, err := model.NewProduct(draft, createdAt)
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.products[product.ID] = *product
	return product
}

// addSeedPicture stores one picture under a product without going through the
// use case, so a test can reach the ceiling.
func (m *fauxProducts) addSeedPicture(productID uuid.UUID, position int, primary bool) model.Picture {
	m.mu.Lock()
	defer m.mu.Unlock()
	picture := model.Picture{
		ID:        uuid.New(),
		ProductID: productID,
		PublicID:  "demo/seed",
		URL:       "https://cdn.example.test/seed.jpg",
		Width:     800,
		Height:    600,
		Position:  position,
		IsPrimary: primary,
		CreatedAt: time.Now().UTC(),
	}
	m.pictures[productID] = append(m.pictures[productID], picture)
	return picture
}

// categoryAllowed reports whether the fake knows the category. A nil set accepts
// every identifier.
func (m *fauxProducts) categoryAllowed(id uuid.UUID) bool {
	if m.categories == nil {
		return true
	}
	return m.categories[id]
}

// slugTaken reports whether another product already holds the folded slug.
func (m *fauxProducts) slugTaken(product *model.Product) bool {
	key := model.FoldKey(product.Slug)
	for _, existing := range m.products {
		if existing.ID == product.ID {
			continue
		}
		if model.FoldKey(existing.Slug) == key {
			return true
		}
	}
	return false
}

func (m *fauxProducts) Create(_ context.Context, product *model.Product) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.categoryAllowed(product.CategoryID) {
		return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
	}
	if m.slugTaken(product) {
		return domainerr.ErrProductSlugTaken
	}
	m.products[product.ID] = *product
	return nil
}

func (m *fauxProducts) Update(_ context.Context, product *model.Product) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.products[product.ID]; !ok {
		return domainerr.ErrProductNotFound
	}
	if !m.categoryAllowed(product.CategoryID) {
		return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
	}
	if m.slugTaken(product) {
		return domainerr.ErrProductSlugTaken
	}
	m.products[product.ID] = *product
	return nil
}

func (m *fauxProducts) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.products[id]; !ok {
		return domainerr.ErrProductNotFound
	}
	delete(m.products, id)
	delete(m.pictures, id)
	// Removing a set removes its membership rows; removing a member removes it
	// from any set it was in. Both mirror the cascading foreign keys, so the
	// fake answers the way storage does (research D13, US5 scenario 3).
	delete(m.members, id)
	for setID, memberIDs := range m.members {
		kept := memberIDs[:0:0]
		for _, memberID := range memberIDs {
			if memberID != id {
				kept = append(kept, memberID)
			}
		}
		if len(kept) == 0 {
			delete(m.members, setID)
		} else {
			m.members[setID] = kept
		}
	}
	return nil
}

func (m *fauxProducts) ListAll(_ context.Context, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]model.Product, 0, len(m.products))
	for _, product := range m.products {
		all = append(all, product)
	}
	sortProducts(all)
	total := int64(len(all))
	start := (page - 1) * pageSize
	if page < 1 || start >= len(all) {
		return []domainrepo.ProductListItem{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	out := make([]domainrepo.ProductListItem, 0, end-start)
	for _, product := range all[start:end] {
		item := domainrepo.ProductListItem{Product: product, PictureCount: len(m.pictures[product.ID])}
		if primary, ok := model.PrimaryPicture(m.pictures[product.ID]); ok {
			item.ImageURL = primary.URL
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (m *fauxProducts) FindByID(_ context.Context, id uuid.UUID) (*domainrepo.ProductView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	product, ok := m.products[id]
	if !ok {
		return nil, domainerr.ErrProductNotFound
	}
	return &domainrepo.ProductView{Product: product, Pictures: orderedPictures(m.pictures[id])}, nil
}

func (m *fauxProducts) LockProduct(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(ctx, "lock")
	if _, ok := m.products[id]; !ok {
		return domainerr.ErrProductNotFound
	}
	return nil
}

func (m *fauxProducts) CountPictures(ctx context.Context, productID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(ctx, "count")
	return len(m.pictures[productID]), nil
}

func (m *fauxProducts) ListPictures(_ context.Context, productID uuid.UUID) ([]model.Picture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return orderedPictures(m.pictures[productID]), nil
}

func (m *fauxProducts) FindPicture(_ context.Context, productID, pictureID uuid.UUID) (*model.Picture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.pictures[productID] {
		if m.pictures[productID][i].ID == pictureID {
			picture := m.pictures[productID][i]
			return &picture, nil
		}
	}
	return nil, domainerr.ErrProductNotFound
}

func (m *fauxProducts) AddPicture(ctx context.Context, picture *model.Picture) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(ctx, "insert")
	if _, ok := m.products[picture.ProductID]; !ok {
		return domainerr.ErrProductNotFound
	}
	m.pictures[picture.ProductID] = append(m.pictures[picture.ProductID], *picture)
	return nil
}

func (m *fauxProducts) DeletePicture(_ context.Context, productID, pictureID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pictures := m.pictures[productID]
	for i := range pictures {
		if pictures[i].ID == pictureID {
			m.pictures[productID] = append(pictures[:i:i], pictures[i+1:]...)
			return nil
		}
	}
	return domainerr.ErrProductNotFound
}

func (m *fauxProducts) SetPrimaryPicture(_ context.Context, productID, pictureID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pictures := m.pictures[productID]
	found := false
	for i := range pictures {
		if pictures[i].ID == pictureID {
			found = true
		}
	}
	if !found {
		return domainerr.ErrProductNotFound
	}
	for i := range pictures {
		pictures[i].IsPrimary = pictures[i].ID == pictureID
	}
	m.pictures[productID] = pictures
	return nil
}

// ReplaceSetMembers stores a set's whole member list in order. It mirrors the
// adapter: a member no product carries, a duplicate and a self-reference are all
// refused naming memberProductIds, exactly as the storage constraints classify
// them (FR-039, research D10). The call is recorded so a test can prove it ran
// inside the caller's transaction.
func (m *fauxProducts) ReplaceSetMembers(ctx context.Context, setProductID uuid.UUID, memberIDs []uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(ctx, "replaceMembers")
	if _, ok := m.products[setProductID]; !ok {
		return domainerr.ErrProductNotFound
	}
	seen := make(map[uuid.UUID]bool, len(memberIDs))
	for _, memberID := range memberIDs {
		if memberID == setProductID || seen[memberID] {
			return domainerr.InvalidProductField(model.FieldMemberProductIDs, "is not a valid member")
		}
		if _, ok := m.products[memberID]; !ok {
			return domainerr.InvalidProductField(model.FieldMemberProductIDs, "does not exist")
		}
		seen[memberID] = true
	}
	if len(memberIDs) == 0 {
		delete(m.members, setProductID)
	} else {
		m.members[setProductID] = append([]uuid.UUID(nil), memberIDs...)
	}
	return nil
}

// ListSetMembers returns a set's members in order, as the administrator detail
// reads them. The public shape never calls it (research D18).
func (m *fauxProducts) ListSetMembers(_ context.Context, setProductID uuid.UUID) ([]domainrepo.SetMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	memberIDs := m.members[setProductID]
	if len(memberIDs) == 0 {
		return nil, nil
	}
	out := make([]domainrepo.SetMember, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		member, ok := m.products[memberID]
		if !ok {
			continue
		}
		out = append(out, domainrepo.SetMember{ID: member.ID, Name: member.Name, Slug: member.Slug})
	}
	return out, nil
}

// orderedPictures returns the pictures in the FR-017 display order.
func orderedPictures(pictures []model.Picture) []model.Picture {
	if len(pictures) == 0 {
		return nil
	}
	return model.OrderPictures(pictures)
}

// sortProducts applies the FR-009 order: position, then creation time, then id.
func sortProducts(products []model.Product) {
	sort.Slice(products, func(i, j int) bool {
		if products[i].Position != products[j].Position {
			return products[i].Position < products[j].Position
		}
		if !products[i].CreatedAt.Equal(products[j].CreatedAt) {
			return products[i].CreatedAt.Before(products[j].CreatedAt)
		}
		return products[i].ID.String() < products[j].ID.String()
	})
}

// txKey marks a context that is inside the fake transaction, so a repository or
// media call can record whether it ran there.
type txKey struct{}

func inTx(ctx context.Context) bool {
	value, _ := ctx.Value(txKey{}).(bool)
	return value
}

// recordedEvent is one call the use cases made to the auditor.
type recordedEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

// recordingAuditor captures the events the use cases emit.
type recordingAuditor struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (a *recordingAuditor) Record(_ context.Context, action, outcome string, actorID *uuid.UUID,
	actorRole, targetType, targetID string, metadata map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, recordedEvent{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
		metadata:   metadata,
	})
}

var _ appinterface.Auditor = (*recordingAuditor)(nil)

func (a *recordingAuditor) snapshot() []recordedEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]recordedEvent, len(a.events))
	copy(out, a.events)
	return out
}

func (a *recordingAuditor) actions() []string {
	events := a.snapshot()
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.action)
	}
	return out
}

func adminActor() appinterface.Actor {
	return appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin}
}

// requireAudited asserts one event of the given action was recorded, carrying the
// acting administrator as the actor and the product as the target.
func requireAudited(t *testing.T, recorder *recordingAuditor, action string, actor appinterface.Actor, targetID uuid.UUID) {
	t.Helper()
	for _, event := range recorder.snapshot() {
		if event.action != action {
			continue
		}
		if event.outcome != "SUCCESS" {
			t.Fatalf("%s: expected outcome SUCCESS, got %q", action, event.outcome)
		}
		if event.actorID == nil || *event.actorID != actor.ID {
			t.Fatalf("%s: expected actor %s, got %v", action, actor.ID, event.actorID)
		}
		if event.actorRole != string(actor.Role) {
			t.Fatalf("%s: expected actor role %q, got %q", action, actor.Role, event.actorRole)
		}
		if event.targetType != "product" {
			t.Fatalf("%s: expected the product as the target type, got %q", action, event.targetType)
		}
		if event.targetID != targetID.String() {
			t.Fatalf("%s: expected target %s, got %q", action, targetID, event.targetID)
		}
		return
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
}

func maintenanceService(repo *fauxProducts, authority appinterface.Auditor) *Service {
	return New(Service{Products: repo, Audit: authority, Mapper: mapper.New(), Tx: &fauxUnitOfWork{}})
}

// FR-009: create returns a product in COMING_SOON, persists it and records
// PRODUCT_CREATED with the acting administrator.
func TestCreateProductStartsInComingSoonAndRecordsTheAuditAction(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}

	out, err := maintenanceService(repo, recorder).CreateProduct(ctx, dto.CreateProductInput{
		Name:        "  Acrylic stand  ",
		Slug:        "acrylic-stand",
		Description: "Acrylic stand 15cm",
		Price:       price(120000),
		CategoryID:  category,
		Position:    10,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.SellState != constant.SellStateComingSoon {
		t.Fatalf("a created product must start in COMING_SOON, got %q", out.SellState)
	}
	if out.ID == uuid.Nil {
		t.Fatal("a created product must carry its identifier")
	}
	if out.Name != "Acrylic stand" || out.Slug != "acrylic-stand" ||
		out.Price.Amount != 120000 || out.Price.Currency != "VND" || out.Position != 10 {
		t.Fatalf("the created product does not carry the stored values: %+v", out)
	}
	if _, err := repo.FindByID(context.Background(), out.ID); err != nil {
		t.Fatalf("the created product must be persisted: %v", err)
	}
	requireAudited(t, recorder, constant.AuditProductCreated, actor, out.ID)
}

// FR-011, FR-012: an edit changes the fields it carries and leaves the rest.
func TestUpdateProductChangesCarriedFieldsAndLeavesTheRest(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	seeded := repo.seed(t, model.ProductDraft{
		Name: "Cũ", Slug: "cu", Description: "Mô tả cũ",
		Price: price(100), CategoryID: category, Position: 3,
	}, time.Now().UTC().Add(-time.Hour))

	name := "Mới"
	newPrice := price(555)
	out, err := maintenanceService(repo, recorder).UpdateProduct(ctx, dto.UpdateProductInput{
		ID:    seeded.ID,
		Name:  &name,
		Price: &newPrice,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if out.Name != name || out.Price.Amount != 555 {
		t.Fatalf("the edit did not change the fields it carried: %+v", out)
	}
	if out.Slug != "cu" || out.Description != "Mô tả cũ" || out.Position != 3 {
		t.Fatalf("the edit changed a field it did not carry: %+v", out)
	}
	requireAudited(t, recorder, constant.AuditProductUpdated, actor, seeded.ID)
}

// FR-027: an edit that re-sends the product's own slug succeeds — a product is
// never a duplicate of itself.
func TestAnEditThatResendsTheProductsOwnSlugSucceeds(t *testing.T) {
	repo := newFauxProducts()
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	seeded := repo.seed(t, model.ProductDraft{
		Name: "Cũ", Slug: "acrylic-stand", Price: price(100), CategoryID: category,
	}, time.Now().UTC().Add(-time.Hour))

	slug := seeded.Slug
	desc := "mô tả mới"
	out, err := maintenanceService(repo, &recordingAuditor{}).UpdateProduct(ctx, dto.UpdateProductInput{
		ID:          seeded.ID,
		Slug:        &slug,
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("an edit re-sending its own slug must succeed, got %v", err)
	}
	if out.Slug != "acrylic-stand" || out.Description != "mô tả mới" {
		t.Fatalf("the edit was not applied: %+v", out)
	}
}

// FR-012: remove deletes the row, leaves no trace and records PRODUCT_DELETED.
func TestDeleteProductRemovesItAndLeavesNoTrace(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	actor := adminActor()
	ctx := appinterface.WithActor(context.Background(), actor)
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	seeded := repo.seed(t, model.ProductDraft{
		Name: "Alpha", Slug: "alpha", Price: price(100), CategoryID: category,
	}, time.Now().UTC().Add(-time.Hour))

	if err := maintenanceService(repo, recorder).DeleteProduct(ctx, dto.AdminProductRefInput{ID: seeded.ID}); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
	if _, err := repo.FindByID(context.Background(), seeded.ID); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("the removed row must be gone, got %v", err)
	}
	page, err := maintenanceService(repo, recorder).ListAdmin(context.Background(), dto.ListAdminInput{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAdmin after remove: %v", err)
	}
	if page.Total != 0 || len(page.Products) != 0 {
		t.Fatalf("a removed product must leave no trace, got %+v", page.Products)
	}
	requireAudited(t, recorder, constant.AuditProductDeleted, actor, seeded.ID)
}

// FR-012: removing an unknown product is the module not-found, not a failure.
func TestDeleteUnknownProductAnswersNotFound(t *testing.T) {
	svc := maintenanceService(newFauxProducts(), &recordingAuditor{})

	err := svc.DeleteProduct(context.Background(), dto.AdminProductRefInput{ID: uuid.New()})
	if !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// FR-032: creating with a category that does not exist is refused naming the
// field, and nothing is written.
func TestCreatingWithAnUnknownCategoryIsRefusedNamingTheField(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	repo.categories = map[uuid.UUID]bool{uuid.New(): true}

	out, err := maintenanceService(repo, recorder).CreateProduct(context.Background(), dto.CreateProductInput{
		Name: "Acrylic stand", Slug: "acrylic-stand", Price: price(100),
		CategoryID: uuid.New(), Position: 1,
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldCategoryID, err)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("a refused create must not answer a product, got %+v", out)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused create must record nothing, got %v", recorder.actions())
	}
}

// FR-032: editing with a category that does not exist is refused naming the
// field and leaves the row untouched.
func TestEditingWithAnUnknownCategoryIsRefusedNamingTheField(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	seeded := repo.seed(t, model.ProductDraft{
		Name: "Cũ", Slug: "cu", Price: price(100), CategoryID: category,
	}, time.Now().UTC().Add(-time.Hour))

	unknown := uuid.New()
	_, err := maintenanceService(repo, recorder).UpdateProduct(context.Background(), dto.UpdateProductInput{
		ID: seeded.ID, CategoryID: &unknown,
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldCategoryID {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldCategoryID, err)
	}
	stored, err := repo.FindByID(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("read the product: %v", err)
	}
	if stored.Product.CategoryID != category {
		t.Fatalf("a refused edit changed the row: %+v", stored.Product)
	}
	if len(recorder.snapshot()) != 0 {
		t.Fatalf("a refused edit must record nothing, got %v", recorder.actions())
	}
}

// The actor recorded is the one from the session context, never a value drawn
// from the request: the input carries no actor member at all.
func TestTheAuditedActorComesFromTheSessionContext(t *testing.T) {
	repo := newFauxProducts()
	recorder := &recordingAuditor{}
	actor := appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin}
	ctx := appinterface.WithActor(context.Background(), actor)
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}

	if _, err := maintenanceService(repo, recorder).CreateProduct(ctx, dto.CreateProductInput{
		Name: "A", Slug: "a", Price: price(100), CategoryID: category,
	}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	events := recorder.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(events))
	}
	if events[0].actorID == nil || *events[0].actorID != actor.ID || events[0].actorRole != string(access.RoleAdmin) {
		t.Fatalf("the audit event must name the session's administrator, got %+v", events[0])
	}
}

// --- User Story 5: combo sets ---

// setFixture builds the maintenance service over the in-memory repository and a
// recording auditor, with two ordinary member products already stored.
type setFixture struct {
	service  *Service
	repo     *fauxProducts
	audit    *recordingAuditor
	actor    appinterface.Actor
	category uuid.UUID
}

func newSetFixture(t *testing.T) *setFixture {
	t.Helper()
	repo := newFauxProducts()
	audit := &recordingAuditor{}
	category := uuid.New()
	repo.categories = map[uuid.UUID]bool{category: true}
	return &setFixture{
		service:  maintenanceService(repo, audit),
		repo:     repo,
		audit:    audit,
		actor:    adminActor(),
		category: category,
	}
}

// member seeds one ordinary product a set can contain.
func (f *setFixture) member(t *testing.T, name, slug string, amount int64) *model.Product {
	t.Helper()
	return f.repo.seed(t, model.ProductDraft{
		Name: name, Slug: slug, Price: price(amount), CategoryID: f.category,
	}, time.Now().UTC().Add(-time.Hour))
}

// ctx is the request context carrying the session's administrator, never an
// actor drawn from the input (FR-015).
func (f *setFixture) ctx() context.Context {
	return appinterface.WithActor(context.Background(), f.actor)
}

// createSet stores one combo set through the use case.
func (f *setFixture) createSet(t *testing.T, slug string, amount int64, memberIDs []uuid.UUID) dto.AdminProductDetailOutput {
	t.Helper()
	out, err := f.service.CreateProduct(f.ctx(), dto.CreateProductInput{
		Name: "Combo " + slug, Slug: slug, Price: price(amount), CategoryID: f.category,
		Position: 20, IsSet: true, MemberProductIDs: memberIDs,
	})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", slug, err)
	}
	return out
}

// FR-039, research D10: creating a set records its members and keeps the price
// the operator set rather than summing the members, and the membership write runs
// inside the same transaction as the product write.
func TestCreatingASetRecordsItsMembersAndKeepsTheOperatorsPrice(t *testing.T) {
	f := newSetFixture(t)
	first := f.member(t, "First member", "first-member", 100000)
	second := f.member(t, "Second member", "second-member", 50000)

	out := f.createSet(t, "combo-aki", 300000, []uuid.UUID{first.ID, second.ID})

	if !out.IsSet {
		t.Fatalf("the created product must be a set, got %+v", out)
	}
	// The sum of the members would be 150000; the set must carry 300000.
	if out.Price.Amount != 300000 {
		t.Fatalf("a set keeps the operator's price, not the members' sum: got %d", out.Price.Amount)
	}
	if len(out.Members) != 2 || out.Members[0].ID != first.ID || out.Members[1].ID != second.ID {
		t.Fatalf("the set must record its members in order, got %+v", out.Members)
	}

	calls := f.repo.ops("replaceMembers")
	if len(calls) != 1 {
		t.Fatalf("expected one membership write, got %d", len(calls))
	}
	if !calls[0].inTx {
		t.Fatal("the membership write must run inside the caller's transaction (FR-039)")
	}
	requireAudited(t, f.audit, constant.AuditProductCreated, f.actor, out.ID)
}

// FR-039: editing a set replaces the whole member list rather than appending or
// merging.
func TestEditingASetReplacesTheWholeMemberList(t *testing.T) {
	f := newSetFixture(t)
	first := f.member(t, "First member", "first-member", 100000)
	second := f.member(t, "Second member", "second-member", 50000)
	replacement := f.member(t, "Replacement member", "replacement-member", 70000)
	set := f.createSet(t, "combo-aki", 300000, []uuid.UUID{first.ID, second.ID})

	next := []uuid.UUID{replacement.ID}
	out, err := f.service.UpdateProduct(f.ctx(), dto.UpdateProductInput{
		ID: set.ID, MemberProductIDs: &next,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if len(out.Members) != 1 || out.Members[0].ID != replacement.ID {
		t.Fatalf("an edit must replace the whole member list, got %+v", out.Members)
	}
	requireAudited(t, f.audit, constant.AuditProductUpdated, f.actor, set.ID)
}

// FR-039: a member identifier no product carries is refused naming the member
// field, not reported as a server failure.
func TestASetMemberThatDoesNotExistIsRefusedNamingTheField(t *testing.T) {
	f := newSetFixture(t)
	unknown := uuid.New()

	out, err := f.service.CreateProduct(f.ctx(), dto.CreateProductInput{
		Name: "Combo unknown", Slug: "combo-unknown", Price: price(100), CategoryID: f.category,
		Position: 1, IsSet: true, MemberProductIDs: []uuid.UUID{unknown},
	})
	if !errors.Is(err, domainerr.ErrProductInvalid) {
		t.Fatalf("expected ErrProductInvalid, got %v", err)
	}
	var fieldErr *domainerr.ProductFieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != model.FieldMemberProductIDs {
		t.Fatalf("expected the field %q to be named, got %v", model.FieldMemberProductIDs, err)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("a refused create must not answer a product, got %+v", out)
	}
	if len(f.audit.snapshot()) != 0 {
		t.Fatalf("a refused create must record nothing, got %v", f.audit.actions())
	}
}

// US5 scenario 3: removing a set removes its membership rows and touches none of
// its members.
func TestRemovingASetLeavesItsMembersAlone(t *testing.T) {
	f := newSetFixture(t)
	first := f.member(t, "First member", "first-member", 100000)
	second := f.member(t, "Second member", "second-member", 50000)
	set := f.createSet(t, "combo-aki", 300000, []uuid.UUID{first.ID, second.ID})

	if err := f.service.DeleteProduct(f.ctx(), dto.AdminProductRefInput{ID: set.ID}); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
	if _, err := f.repo.FindByID(context.Background(), set.ID); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("the removed set must be gone, got %v", err)
	}
	for _, member := range []*model.Product{first, second} {
		if _, err := f.repo.FindByID(context.Background(), member.ID); err != nil {
			t.Fatalf("removing a set must not touch its member %s: %v", member.Slug, err)
		}
	}
	requireAudited(t, f.audit, constant.AuditProductDeleted, f.actor, set.ID)
}
