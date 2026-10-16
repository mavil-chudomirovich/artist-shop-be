package implement

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file holds the shared in-memory fakes the product use-case tests build on.
// The products fake and the variants fake share one backing state, so a view read
// through the product port sees the variants written through the variant port,
// exactly as the two adapters share one database. Each mirrors its adapter's
// observable behaviour where a use case depends on it — a code/SKU collision,
// a missing category, the catalogue visibility predicate and the card summary —
// because a fake more permissive than the adapter would let a use case pass a test
// the real storage would refuse.

// repoCall is one repository operation and whether it ran inside the caller's
// transaction. Picture tests assert that the ceiling check and the insert share the
// transaction.
type repoCall struct {
	op   string
	inTx bool
}

// memoryState is the shared backing store of the two fakes.
type memoryState struct {
	mu       sync.Mutex
	products map[uuid.UUID]model.Product
	variants map[uuid.UUID]model.Variant
	pictures map[uuid.UUID][]model.Picture

	// categories, when non-nil, is the set of category identifiers this store
	// knows. A create or update naming one outside it is refused the way the
	// adapter classifies the foreign-key violation. A nil map accepts any
	// category, which keeps tests independent of a fixture.
	categories map[uuid.UUID]bool

	// calls records the operations in order, with whether each ran inside the
	// caller's transaction.
	calls []repoCall
}

func newState() *memoryState {
	return &memoryState{
		products: make(map[uuid.UUID]model.Product),
		variants: make(map[uuid.UUID]model.Variant),
		pictures: make(map[uuid.UUID][]model.Picture),
	}
}

// record appends one operation and whether it ran inside a transaction. The caller
// holds the lock.
func (s *memoryState) record(ctx context.Context, op string) {
	s.calls = append(s.calls, repoCall{op: op, inTx: inTx(ctx)})
}

func (s *memoryState) ops(name string) []repoCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]repoCall, 0, len(s.calls))
	for _, call := range s.calls {
		if call.op == name {
			out = append(out, call)
		}
	}
	return out
}

// listVariantsLocked returns the product's variants in display order. The caller
// holds the lock.
func (s *memoryState) listVariantsLocked(productID uuid.UUID) []model.Variant {
	out := make([]model.Variant, 0)
	for _, variant := range s.variants {
		if variant.ProductID == productID {
			out = append(out, variant)
		}
	}
	sortVariants(out)
	return out
}

// memoryProducts is an in-memory ProductRepository over the shared state.
type memoryProducts struct {
	domainrepo.ProductRepository
	state *memoryState
}

var _ domainrepo.ProductRepository = (*memoryProducts)(nil)

func newMemoryProducts() *memoryProducts { return &memoryProducts{state: newState()} }

// variants returns the variants fake sharing this state.
func (m *memoryProducts) variants() *memoryVariants { return &memoryVariants{state: m.state} }

// seedWithVariants builds a product with the given variants through the domain
// constructors and stores them, so a test arranges a state a real create could
// reach.
func (m *memoryProducts) seedWithVariants(t *testing.T, draft model.ProductDraft, variants []model.VariantDraft, createdAt time.Time) *model.Product {
	t.Helper()
	product, err := model.NewProduct(draft, createdAt)
	if err != nil {
		t.Fatalf("NewProduct(%q): %v", draft.Name, err)
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.products[product.ID] = *product
	for _, vd := range append(append([]model.VariantDraft(nil), draft.Variants...), variants...) {
		variant, err := model.NewVariant(product.ID, vd, createdAt)
		if err != nil {
			t.Fatalf("NewVariant(%q): %v", vd.Name, err)
		}
		m.state.variants[variant.ID] = *variant
	}
	return product
}

// launch moves a seeded variant to ACTIVE through the domain transition.
func (m *memoryProducts) launch(t *testing.T, id uuid.UUID) {
	t.Helper()
	m.transition(t, id, (*model.Variant).Launch)
}

// sellOut moves a seeded variant to OUT_OF_STOCK through the domain transition.
func (m *memoryProducts) sellOut(t *testing.T, id uuid.UUID) {
	t.Helper()
	m.transition(t, id, (*model.Variant).SellOut)
}

// retire moves a seeded variant to DISCONTINUED through the domain transition.
func (m *memoryProducts) retire(t *testing.T, id uuid.UUID) {
	t.Helper()
	m.transition(t, id, (*model.Variant).Retire)
}

// transition applies one variant transition to a seeded variant.
func (m *memoryProducts) transition(t *testing.T, id uuid.UUID, transition func(*model.Variant, time.Time) error) {
	t.Helper()
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	variant, ok := m.state.variants[id]
	if !ok {
		t.Fatalf("variant %s is not seeded", id)
	}
	if err := transition(&variant, time.Now().UTC()); err != nil {
		t.Fatalf("transition %s: %v", id, err)
	}
	m.state.variants[id] = variant
}

// variantsFor returns a product's seeded variants in display order.
func (m *memoryProducts) variantsFor(productID uuid.UUID) []model.Variant {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.listVariantsLocked(productID)
}

// variantBySKU returns a product's seeded variant with the given SKU.
func (m *memoryProducts) variantBySKU(t *testing.T, productID uuid.UUID, sku string) model.Variant {
	t.Helper()
	for _, variant := range m.variantsFor(productID) {
		if variant.SKU == sku {
			return variant
		}
	}
	t.Fatalf("no seeded variant with SKU %q", sku)
	return model.Variant{}
}

// addSeedPicture stores one picture under a product without going through the use
// case, so a test can reach the ceiling.
func (m *memoryProducts) addSeedPicture(productID uuid.UUID, position int, primary bool) model.Picture {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
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
	m.state.pictures[productID] = append(m.state.pictures[productID], picture)
	return picture
}

// categoryAllowed reports whether the store knows the category. A nil set accepts
// every identifier.
func (m *memoryProducts) categoryAllowed(id uuid.UUID) bool {
	if m.state.categories == nil {
		return true
	}
	return m.state.categories[id]
}

func (m *memoryProducts) Create(ctx context.Context, product *model.Product) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "createProduct")
	if !m.categoryAllowed(product.CategoryID) {
		return invalidCategory()
	}
	if m.codeTakenLocked(product) {
		return errCodeTaken()
	}
	m.state.products[product.ID] = *product
	return nil
}

func (m *memoryProducts) Update(ctx context.Context, product *model.Product) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "updateProduct")
	if _, ok := m.state.products[product.ID]; !ok {
		return errProductNotFound()
	}
	if !m.categoryAllowed(product.CategoryID) {
		return invalidCategory()
	}
	if m.codeTakenLocked(product) {
		return errCodeTaken()
	}
	m.state.products[product.ID] = *product
	return nil
}

func (m *memoryProducts) Delete(_ context.Context, id uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.state.products[id]; !ok {
		return errProductNotFound()
	}
	delete(m.state.products, id)
	delete(m.state.pictures, id)
	for variantID, variant := range m.state.variants {
		if variant.ProductID == id {
			delete(m.state.variants, variantID)
		}
	}
	return nil
}

func (m *memoryProducts) codeTakenLocked(product *model.Product) bool {
	key := model.FoldKey(product.Code)
	for _, existing := range m.state.products {
		if existing.ID != product.ID && model.FoldKey(existing.Code) == key {
			return true
		}
	}
	return false
}

func (m *memoryProducts) ListCatalogue(_ context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.CatalogueEntry, int64, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	visible := uuidSet(query.VisibleCategoryIDs)

	all := make([]domainrepo.CatalogueEntry, 0, len(m.state.products))
	for _, product := range m.state.products {
		if !visible[product.CategoryID] {
			continue
		}
		if query.CategoryID != nil && product.CategoryID != *query.CategoryID {
			continue
		}
		variants := m.state.listVariantsLocked(product.ID)
		summary := summarizeProduct(product, m.state.pictures[product.ID], variants)
		if summary.Price.Amount == 0 {
			// No on-sale variant: the product has no catalogue presence.
			continue
		}
		all = append(all, domainrepo.CatalogueEntry{
			ID:        product.ID,
			Name:      product.Name,
			Code:      product.Code,
			Price:     summary.Price,
			ImageURL:  summary.ImageURL,
			Position:  product.Position,
			CreatedAt: product.CreatedAt,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Position != all[j].Position {
			return all[i].Position < all[j].Position
		}
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].ID.String() < all[j].ID.String()
	})
	total := int64(len(all))
	start, end := window(len(all), query.Page, query.PageSize)
	return all[start:end], total, nil
}

func (m *memoryProducts) FindVisibleByCode(_ context.Context, code string, visibleCategoryIDs []uuid.UUID) (*domainrepo.ProductView, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	visible := uuidSet(visibleCategoryIDs)
	for _, product := range m.state.products {
		if product.Code != code {
			continue
		}
		if !visible[product.CategoryID] || !m.hasOnSaleVariantLocked(product.ID) {
			return nil, errProductNotFound()
		}
		return m.viewLocked(product, true), nil
	}
	return nil, errProductNotFound()
}

func (m *memoryProducts) ListAll(_ context.Context, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	all := make([]model.Product, 0, len(m.state.products))
	for _, product := range m.state.products {
		all = append(all, product)
	}
	sortProducts(all)

	total := int64(len(all))
	start, end := window(len(all), page, pageSize)
	if start >= len(all) {
		return []domainrepo.ProductListItem{}, total, nil
	}
	out := make([]domainrepo.ProductListItem, 0, end-start)
	for _, product := range all[start:end] {
		item := domainrepo.ProductListItem{
			Product:      product,
			PictureCount: len(m.state.pictures[product.ID]),
			VariantCount: len(m.state.listVariantsLocked(product.ID)),
		}
		if primary, ok := model.PrimaryPicture(m.state.pictures[product.ID]); ok {
			item.ImageURL = primary.URL
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (m *memoryProducts) FindByID(_ context.Context, id uuid.UUID) (*domainrepo.ProductView, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	product, ok := m.state.products[id]
	if !ok {
		return nil, errProductNotFound()
	}
	return m.viewLocked(product, false), nil
}

// viewLocked assembles a detail view. The caller holds the lock.
func (m *memoryProducts) viewLocked(product model.Product, visibleOnly bool) *domainrepo.ProductView {
	variants := m.state.listVariantsLocked(product.ID)
	return &domainrepo.ProductView{
		Product:  product,
		Pictures: m.state.pictures[product.ID],
		Variants: visibleVariants(variants, visibleOnly),
		Summary:  summarizeProduct(product, m.state.pictures[product.ID], variants),
	}
}

func (m *memoryProducts) hasOnSaleVariantLocked(productID uuid.UUID) bool {
	for _, variant := range m.state.variants {
		if variant.ProductID == productID && variant.Buyable() {
			return true
		}
	}
	return false
}

func (m *memoryProducts) LockProduct(ctx context.Context, id uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "lock")
	if _, ok := m.state.products[id]; !ok {
		return errProductNotFound()
	}
	return nil
}

func (m *memoryProducts) CountPictures(ctx context.Context, productID uuid.UUID) (int, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "count")
	return len(m.state.pictures[productID]), nil
}

func (m *memoryProducts) ListPictures(_ context.Context, productID uuid.UUID) ([]model.Picture, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return orderedPictures(m.state.pictures[productID]), nil
}

func (m *memoryProducts) FindPicture(_ context.Context, productID, pictureID uuid.UUID) (*model.Picture, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	for i := range m.state.pictures[productID] {
		if m.state.pictures[productID][i].ID == pictureID {
			picture := m.state.pictures[productID][i]
			return &picture, nil
		}
	}
	return nil, errProductNotFound()
}

func (m *memoryProducts) AddPicture(ctx context.Context, picture *model.Picture) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "insert")
	if _, ok := m.state.products[picture.ProductID]; !ok {
		return errProductNotFound()
	}
	m.state.pictures[picture.ProductID] = append(m.state.pictures[picture.ProductID], *picture)
	return nil
}

func (m *memoryProducts) DeletePicture(_ context.Context, productID, pictureID uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	pictures := m.state.pictures[productID]
	for i := range pictures {
		if pictures[i].ID == pictureID {
			m.state.pictures[productID] = append(pictures[:i:i], pictures[i+1:]...)
			return nil
		}
	}
	return errProductNotFound()
}

func (m *memoryProducts) SetPrimaryPicture(_ context.Context, productID, pictureID uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	pictures := m.state.pictures[productID]
	found := false
	for i := range pictures {
		if pictures[i].ID == pictureID {
			found = true
		}
	}
	if !found {
		return errProductNotFound()
	}
	for i := range pictures {
		pictures[i].IsPrimary = pictures[i].ID == pictureID
	}
	m.state.pictures[productID] = pictures
	return nil
}

// memoryVariants is an in-memory VariantRepository over the shared state.
type memoryVariants struct {
	domainrepo.VariantRepository
	state *memoryState
}

var _ domainrepo.VariantRepository = (*memoryVariants)(nil)

func (m *memoryVariants) Create(ctx context.Context, variant *model.Variant) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "createVariant")
	if _, ok := m.state.products[variant.ProductID]; !ok {
		return errVariantNotFound()
	}
	if m.skuTakenLocked(variant) {
		return errSKUTaken()
	}
	m.state.variants[variant.ID] = *variant
	return nil
}

func (m *memoryVariants) Update(ctx context.Context, variant *model.Variant) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.record(ctx, "updateVariant")
	if _, ok := m.state.variants[variant.ID]; !ok {
		return errVariantNotFound()
	}
	if m.skuTakenLocked(variant) {
		return errSKUTaken()
	}
	m.state.variants[variant.ID] = *variant
	return nil
}

func (m *memoryVariants) Delete(_ context.Context, id uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.state.variants[id]; !ok {
		return errVariantNotFound()
	}
	delete(m.state.variants, id)
	return nil
}

func (m *memoryVariants) FindByID(_ context.Context, id uuid.UUID) (*model.Variant, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	variant, ok := m.state.variants[id]
	if !ok {
		return nil, errVariantNotFound()
	}
	return &variant, nil
}

func (m *memoryVariants) ListByProduct(_ context.Context, productID uuid.UUID) ([]model.Variant, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.listVariantsLocked(productID), nil
}

func (m *memoryVariants) CountByProduct(_ context.Context, productID uuid.UUID) (int, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return len(m.state.listVariantsLocked(productID)), nil
}

func (m *memoryVariants) FindFactsByIDs(_ context.Context, ids []uuid.UUID) ([]domainrepo.VariantFacts, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	out := make([]domainrepo.VariantFacts, 0, len(ids))
	for _, id := range ids {
		variant, ok := m.state.variants[id]
		if !ok {
			continue
		}
		product, ok := m.state.products[variant.ProductID]
		if !ok {
			continue
		}
		out = append(out, domainrepo.VariantFacts{
			VariantID:   variant.ID,
			ProductID:   product.ID,
			ProductName: product.Name,
			ProductCode: product.Code,
			VariantName: variant.Name,
			SKU:         variant.SKU,
			OnSale:      variant.Buyable(),
			Price:       variant.EffectivePrice(product.Price),
		})
	}
	return out, nil
}

func (m *memoryVariants) skuTakenLocked(variant *model.Variant) bool {
	key := model.FoldKey(variant.SKU)
	for _, existing := range m.state.variants {
		if existing.ID != variant.ID && model.FoldKey(existing.SKU) == key {
			return true
		}
	}
	return false
}

// --- availability fake ---

// fakeAvailability answers the inventory read contract from a fixed map. It
// records how often it was asked, so a test can prove the detail read costs one
// bulk call.
type fakeAvailability struct {
	mu        sync.Mutex
	available map[uuid.UUID]int64
	calls     int
}

func newFakeAvailability() *fakeAvailability {
	return &fakeAvailability{available: make(map[uuid.UUID]int64)}
}

func (f *fakeAvailability) AvailableQuantity(_ context.Context, variantIDs []uuid.UUID) ([]contracts.Availability, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := make([]contracts.Availability, 0, len(variantIDs))
	for _, id := range variantIDs {
		out = append(out, contracts.Availability{VariantID: id, Available: f.available[id]})
	}
	return out, nil
}

// --- visibility fake ---

// fakeVisibility is the cross-module category query the use case consumes. It
// records how often the visible set was asked for, so a test can prove it is
// fetched once per request rather than per product.
type fakeVisibility struct {
	visible      []uuid.UUID
	slugs        map[string]uuid.UUID
	visibleCalls int
	slugCalls    int
}

func (f *fakeVisibility) VisibleCategoryIDs(context.Context) ([]uuid.UUID, error) {
	f.visibleCalls++
	return f.visible, nil
}

func (f *fakeVisibility) CategoryIDBySlug(_ context.Context, slug string) (uuid.UUID, bool, error) {
	f.slugCalls++
	id, ok := f.slugs[slug]
	return id, ok, nil
}

// --- unit of work, media and auditor fakes ---

// fauxUnitOfWork is an in-memory UnitOfWork. It marks the context so the
// repository and the media fake can record whether a call ran inside the
// transaction.
type fauxUnitOfWork struct {
	mu     sync.Mutex
	calls  int
	active int
}

func (u *fauxUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.mu.Lock()
	u.calls++
	u.active++
	u.mu.Unlock()
	err := fn(context.WithValue(ctx, txKey{}, true))
	u.mu.Lock()
	u.active--
	u.mu.Unlock()
	return err
}

// fakeMedia answers the MediaStore port without a provider. It records whether
// each upload ran inside a transaction, which is what proves the provider call
// stays outside it.
type fakeMedia struct {
	mu         sync.Mutex
	uploads    int
	removed    int
	uploadInTx []bool
	reference  appinterface.MediaReference
	err        error
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{reference: appinterface.MediaReference{
		PublicID: "artist-shop/products/one",
		URL:      "https://cdn.example.test/products/one.jpg",
		Width:    1600,
		Height:   1200,
	}}
}

func (m *fakeMedia) Upload(ctx context.Context, _ []byte, _ int) (appinterface.MediaReference, error) {
	m.mu.Lock()
	m.uploads++
	m.uploadInTx = append(m.uploadInTx, inTx(ctx))
	reference, err := m.reference, m.err
	m.mu.Unlock()
	if err != nil {
		return appinterface.MediaReference{}, err
	}
	return reference, nil
}

func (m *fakeMedia) Remove(context.Context, appinterface.MediaReference) error {
	m.mu.Lock()
	m.removed++
	err := m.err
	m.mu.Unlock()
	return err
}

var _ appinterface.MediaStore = (*fakeMedia)(nil)

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

// --- shared helpers ---

// summarizeProduct derives the card figure: the lowest effective price among the
// product's on-sale variants and the default variant's image, falling back to the
// product's primary gallery picture.
func summarizeProduct(product model.Product, pictures []model.Picture, variants []model.Variant) domainrepo.ProductSummary {
	var summary domainrepo.ProductSummary
	var lowest *int64
	for i := range variants {
		if !variants[i].Buyable() {
			continue
		}
		amount := variants[i].EffectivePrice(product.Price).Amount
		if lowest == nil || amount < *lowest {
			value := amount
			lowest = &value
		}
	}
	if lowest != nil {
		summary.Price = model.Price{Amount: *lowest, Currency: product.Price.Currency}
	}
	if len(variants) > 0 && variants[0].Image != nil {
		summary.ImageURL = variants[0].Image.URL
	}
	if summary.ImageURL == "" {
		if picture, ok := model.PrimaryPicture(pictures); ok {
			summary.ImageURL = picture.URL
		}
	}
	return summary
}

// visibleVariants filters a product's variants for the public read.
func visibleVariants(variants []model.Variant, visibleOnly bool) []model.Variant {
	if !visibleOnly {
		return variants
	}
	out := make([]model.Variant, 0, len(variants))
	for _, variant := range variants {
		if variant.VisibleToCustomers() {
			out = append(out, variant)
		}
	}
	return out
}

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

func sortVariants(variants []model.Variant) {
	sort.Slice(variants, func(i, j int) bool {
		if variants[i].Position != variants[j].Position {
			return variants[i].Position < variants[j].Position
		}
		if !variants[i].CreatedAt.Equal(variants[j].CreatedAt) {
			return variants[i].CreatedAt.Before(variants[j].CreatedAt)
		}
		return variants[i].ID.String() < variants[j].ID.String()
	})
}

func orderedPictures(pictures []model.Picture) []model.Picture {
	if len(pictures) == 0 {
		return nil
	}
	return model.OrderPictures(pictures)
}

func uuidSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func window(total, page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

// pngBytes is a payload the domain sniffer recognises as a PNG.
func pngBytes() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x2A}, 16)...)
}

func adminActor() appinterface.Actor {
	return appinterface.Actor{ID: uuid.New(), Role: access.RoleAdmin}
}

// requireAudited asserts one event of the given action was recorded, carrying the
// acting administrator as the actor and the named target.
func requireAudited(t *testing.T, recorder *recordingAuditor, action string, actor appinterface.Actor, targetType string, targetID uuid.UUID) {
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
		if event.targetType != targetType {
			t.Fatalf("%s: expected target type %q, got %q", action, targetType, event.targetType)
		}
		if event.targetID != targetID.String() {
			t.Fatalf("%s: expected target %s, got %q", action, targetID, event.targetID)
		}
		return
	}
	t.Fatalf("%s: no audit event recorded, got %v", action, recorder.actions())
}

// serviceFor wires a Service over the given fakes.
func serviceFor(products *memoryProducts, availability *fakeAvailability, visibility appinterface.CategoryQuery, audit appinterface.Auditor, media appinterface.MediaStore, tx appinterface.UnitOfWork) *Service {
	return New(Service{
		Products:     products,
		Variants:     products.variants(),
		Visibility:   visibility,
		Availability: availability,
		Audit:        audit,
		Media:        media,
		Tx:           tx,
		Mapper:       mapper.New(),
	})
}

// publicService wires the public reads over the two fakes.
func publicService(products *memoryProducts, visibility appinterface.CategoryQuery) *Service {
	return serviceFor(products, newFakeAvailability(), visibility, nil, nil, nil)
}

// maintenanceService wires the administrator use cases over the fakes and a
// recording auditor.
func maintenanceService(products *memoryProducts, audit appinterface.Auditor) *Service {
	return serviceFor(products, newFakeAvailability(), nil, audit, nil, &fauxUnitOfWork{})
}

func adminContext(actor appinterface.Actor) context.Context {
	return appinterface.WithActor(context.Background(), actor)
}

// The fakes return the module's real sentinels, so a use case classifies a refusal
// exactly as it would against the adapters.
func errProductNotFound() error { return domainerr.ErrProductNotFound }
func errVariantNotFound() error { return domainerr.ErrVariantNotFound }
func errCodeTaken() error       { return domainerr.ErrProductCodeTaken }
func errSKUTaken() error        { return domainerr.ErrVariantSKUTaken }

func invalidCategory() error {
	return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
}
