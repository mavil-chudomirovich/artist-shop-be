package implement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
)

// This file exercises the address use cases over in-memory storage: the
// single-default invariant (FR-008 to FR-011), the ownership boundary
// (FR-006, FR-013) and the audit trail (FR-019, SC-008).

// errSecondDefault stands in for what the partial unique index
// addresses_one_default_per_user does at the storage layer (ADR-003). The fake
// repository refuses a second visible default exactly like the index does, so a
// use case that writes two of them fails here instead of passing silently.
var errSecondDefault = errors.New("addresses_one_default_per_user: a second default address for one account")

// memoryAddresses is an in-memory AddressRepository. It copies every value in and
// out, mirrors the adapter's column-level writes (an update never touches
// is_default or deleted_at) and records whether a write happened outside a
// transaction, which is what proves the application layer owns the boundary
// (Constitution I).
type memoryAddresses struct {
	mu sync.Mutex
	// rows holds every address, hidden ones included, exactly as the table does.
	rows map[uuid.UUID]model.Address
	// seq is the insertion order, so a listing with equal sort keys is stable.
	seq []uuid.UUID
	// inTx is set by the fake UnitOfWork while a transaction is open.
	inTx bool
	// writesOutsideTx counts the writes that were not part of a transaction.
	writesOutsideTx int
	// operations names the calls the adapter received, in order.
	operations []string
}

func newMemoryAddresses(rows ...model.Address) *memoryAddresses {
	store := &memoryAddresses{rows: make(map[uuid.UUID]model.Address, len(rows))}
	for _, row := range rows {
		store.rows[row.ID] = row
		store.seq = append(store.seq, row.ID)
	}
	return store
}

// snapshot copies the whole store so a rolled-back transaction can be undone.
func (m *memoryAddresses) snapshot() (map[uuid.UUID]model.Address, []uuid.UUID) {
	rows := make(map[uuid.UUID]model.Address, len(m.rows))
	for id, row := range m.rows {
		rows[id] = row
	}
	return rows, append([]uuid.UUID(nil), m.seq...)
}

func (m *memoryAddresses) restore(rows map[uuid.UUID]model.Address, seq []uuid.UUID) {
	m.rows = rows
	m.seq = seq
}

// record notes a call and counts the writes that escaped a transaction.
func (m *memoryAddresses) record(operation string) {
	m.operations = append(m.operations, operation)
	if !m.inTx {
		m.writesOutsideTx++
	}
}

func (m *memoryAddresses) get(userID, addressID uuid.UUID) (*model.Address, error) {
	row, ok := m.rows[addressID]
	if !ok || row.UserID != userID || row.DeletedAt != nil {
		return nil, domainerr.ErrAddressNotFound
	}
	stored := row
	return &stored, nil
}

// visible returns every non-hidden address of an account, default first and then
// most recently updated, which is the order the repository contract promises.
func (m *memoryAddresses) visible(userID uuid.UUID) []model.Address {
	out := make([]model.Address, 0, len(m.rows))
	for _, id := range m.seq {
		row := m.rows[id]
		if row.UserID != userID || row.DeletedAt != nil {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

func (m *memoryAddresses) Create(_ context.Context, address *model.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Create")
	for _, row := range m.visible(address.UserID) {
		if row.IsDefault && address.IsDefault {
			return errSecondDefault
		}
	}
	m.rows[address.ID] = *address
	m.seq = append(m.seq, address.ID)
	return nil
}

func (m *memoryAddresses) Update(_ context.Context, address *model.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Update")
	stored, ok := m.rows[address.ID]
	if !ok || stored.UserID != address.UserID || stored.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
	// The statement writes the editable columns only, so the hidden marker and the
	// default flag survive an edit whatever the caller handed over (FR-011).
	stored.RecipientName = address.RecipientName
	stored.RecipientPhone = address.RecipientPhone
	stored.ProvinceCode = address.ProvinceCode
	stored.ProvinceName = address.ProvinceName
	stored.WardCode = address.WardCode
	stored.WardName = address.WardName
	stored.StreetAddress = address.StreetAddress
	stored.UpdatedAt = address.UpdatedAt
	m.rows[address.ID] = stored
	return nil
}

func (m *memoryAddresses) FindByOwner(_ context.Context, userID, addressID uuid.UUID) (*model.Address, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations = append(m.operations, "FindByOwner")
	return m.get(userID, addressID)
}

func (m *memoryAddresses) ListByOwner(_ context.Context, userID uuid.UUID, page, pageSize int) ([]model.Address, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations = append(m.operations, "ListByOwner")
	list := m.visible(userID)
	total := int64(len(list))
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start > len(list) {
		start = len(list)
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	out := make([]model.Address, 0, end-start)
	out = append(out, list[start:end]...)
	return out, total, nil
}

func (m *memoryAddresses) ClearDefault(_ context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("ClearDefault")
	for _, id := range m.seq {
		row := m.rows[id]
		if row.UserID == userID && row.IsDefault && row.DeletedAt == nil {
			row.IsDefault = false
			m.rows[id] = row
		}
	}
	return nil
}

func (m *memoryAddresses) SetDefault(_ context.Context, addressID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("SetDefault")
	row, ok := m.rows[addressID]
	if !ok || row.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
	for id, other := range m.rows {
		if id != addressID && other.IsDefault && other.DeletedAt == nil {
			return errSecondDefault
		}
	}
	row.IsDefault = true
	m.rows[addressID] = row
	return nil
}

func (m *memoryAddresses) Hide(_ context.Context, userID, addressID uuid.UUID, hiddenAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Hide")
	if _, err := m.get(userID, addressID); err != nil {
		return err
	}
	stored := m.rows[addressID]
	stored.DeletedAt = &hiddenAt
	// The statement clears the flag in the same write, so an account never keeps a
	// default that points at a hidden address (SC-004).
	stored.IsDefault = false
	m.rows[addressID] = stored
	return nil
}

var _ repository.AddressRepository = (*memoryAddresses)(nil)

// txUnitOfWork is the module's UnitOfWork port. It marks the store as inside a
// transaction, counts the transactions that were opened and undoes every write
// when the function fails, which is what makes "all or nothing" observable.
type txUnitOfWork struct {
	repo    *memoryAddresses
	opened  int
	depth   int
	failure error
}

func (u *txUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.opened++
	u.depth++
	u.repo.inTx = true
	rows, seq := u.repo.snapshot()
	err := fn(ctx)
	if err == nil && u.failure != nil {
		// A rejected commit is indistinguishable from a failed statement as far as
		// the caller is concerned: the work is undone and the error surfaces.
		err = u.failure
	}
	if err != nil {
		u.repo.inTx = false
		u.repo.restore(rows, seq)
		u.depth--
		return err
	}
	u.repo.inTx = false
	u.depth--
	return nil
}

var _ appinterface.UnitOfWork = (*txUnitOfWork)(nil)

// fakeDivisions serves two provinces so a test can tell a valid pair from a stale
// code without reading the bundled dataset.
//
// The test file imports internal/share/administrative for the sentinels alone: the
// use case must propagate the very errors presentation maps to USER_UNKNOWN_PROVINCE
// and friends, so identity is what needs asserting. The production package never
// imports the dataset (Constitution I, research D1).
type fakeDivisions struct {
	provinces map[string]string
	wards     map[string]map[string]string
}

func newFakeDivisions() *fakeDivisions {
	return &fakeDivisions{
		provinces: map[string]string{"79": "Ho Chi Minh", "01": "Ha Noi"},
		wards: map[string]map[string]string{
			"79": {"26734": "Phuong Ben Nghe", "26735": "Phuong Ben Thanh"},
			"01": {"00001": "Phuong Cau Giay"},
		},
	}
}

func (d *fakeDivisions) Provinces(context.Context) ([]appinterface.Province, error) {
	out := make([]appinterface.Province, 0, len(d.provinces))
	for code, name := range d.provinces {
		out = append(out, appinterface.Province{Code: code, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func (d *fakeDivisions) Wards(_ context.Context, provinceCode string) ([]appinterface.Ward, error) {
	wards, ok := d.wards[provinceCode]
	if !ok {
		return nil, administrative.ErrUnknownProvince
	}
	out := make([]appinterface.Ward, 0, len(wards))
	for code, name := range wards {
		out = append(out, appinterface.Ward{Code: code, Name: name, ProvinceCode: provinceCode})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func (d *fakeDivisions) ValidateAddressDivisions(_ context.Context, provinceCode, wardCode string) error {
	if _, ok := d.provinces[provinceCode]; !ok {
		return administrative.ErrUnknownProvince
	}
	for code := range d.wards {
		if _, ok := d.wards[code][wardCode]; ok && code != provinceCode {
			return administrative.ErrWardProvinceMismatch
		}
	}
	if _, ok := d.wards[provinceCode][wardCode]; !ok {
		return administrative.ErrUnknownWard
	}
	return nil
}

var _ appinterface.Divisions = (*fakeDivisions)(nil)

// addressHarness is the whole address stack over in-memory storage.
type addressHarness struct {
	svc       *Service
	repo      *memoryAddresses
	tx        *txUnitOfWork
	audit     *recordingAudit
	divisions *fakeDivisions
	owner     uuid.UUID
	others    uuid.UUID
}

// clock is the fixed instant the harness stores, so the listing order and the
// row timestamps never depend on the wall clock.
var clock = time.Date(2026, time.October, 6, 9, 0, 0, 0, time.UTC)

func newAddressHarness(t *testing.T) *addressHarness {
	t.Helper()
	owner := uuid.New()
	other := uuid.New()
	repo := newMemoryAddresses()
	tx := &txUnitOfWork{repo: repo}
	audit := &recordingAudit{}
	divisions := newFakeDivisions()
	svc := New(Service{
		Profiles: newMemoryProfiles(
			model.Profile{ID: owner, Email: "owner@example.com", Role: access.RoleCustomer},
			model.Profile{ID: other, Email: "other@example.com", Role: access.RoleCustomer},
		),
		Addresses: repo,
		Divisions: divisions,
		Tx:        tx,
		Audit:     audit,
		Mapper:    mapper.New(divisions),
	})
	return &addressHarness{
		svc: svc, repo: repo, tx: tx, audit: audit,
		divisions: divisions, owner: owner, others: other,
	}
}

// seedAddress stores one address directly, so a test can set up a state a use case
// cannot produce on its own (a foreign address, a hidden row).
func (h *addressHarness) seedAddress(t *testing.T, userID uuid.UUID, address model.Address) model.Address {
	t.Helper()
	if address.UserID != userID {
		address.UserID = userID
	}
	address.ID = uuid.New()
	if address.RecipientName == "" {
		address.RecipientName = "Nguyen Van A"
	}
	if address.RecipientPhone == "" {
		address.RecipientPhone = "0912345678"
	}
	if address.ProvinceCode == "" {
		address.ProvinceCode = "79"
	}
	if address.ProvinceName == "" {
		address.ProvinceName = "Ho Chi Minh"
	}
	if address.WardCode == "" {
		address.WardCode = "26734"
	}
	if address.WardName == "" {
		address.WardName = "Phuong Ben Nghe"
	}
	if address.StreetAddress == "" {
		address.StreetAddress = "12 Nguyen Hue"
	}
	if address.CreatedAt.IsZero() {
		address.CreatedAt = clock
		address.UpdatedAt = clock
	}
	h.repo.mu.Lock()
	defer h.repo.mu.Unlock()
	h.repo.rows[address.ID] = address
	h.repo.seq = append(h.repo.seq, address.ID)
	return address
}

func (h *addressHarness) create(t *testing.T, userID uuid.UUID) appdto.AddressOutput {
	t.Helper()
	out, err := h.svc.CreateAddress(context.Background(), appdto.CreateAddressInput{
		UserID:         userID,
		RecipientName:  "Nguyen Van A",
		RecipientPhone: "0912 345 678",
		ProvinceCode:   "79",
		WardCode:       "26734",
		StreetAddress:  "12 Nguyen Hue",
	})
	if err != nil {
		t.Fatalf("create an address: %v", err)
	}
	return out
}

// list pages through the acting account's addresses, always naming the account the
// session identified — exactly as presentation fills the input.
func (h *addressHarness) list(t *testing.T, page, pageSize int) appdto.AddressPageOutput {
	t.Helper()
	out, err := h.svc.ListAddresses(context.Background(), appdto.ListAddressesInput{
		UserID: h.owner, Page: page, PageSize: pageSize,
	})
	if err != nil {
		t.Fatalf("ListAddresses: %v", err)
	}
	return out
}

// defaults returns the identifiers of the account's visible default addresses, so
// a test can assert the invariant rather than a single boolean.
func (h *addressHarness) defaults(userID uuid.UUID) []uuid.UUID {
	h.repo.mu.Lock()
	defer h.repo.mu.Unlock()
	var out []uuid.UUID
	for _, row := range h.repo.visible(userID) {
		if row.IsDefault {
			out = append(out, row.ID)
		}
	}
	return out
}

// FR-009: the very first address of an account becomes its default, and the
// transition goes through the explicit flag change rather than a direct write.
func TestTheFirstAddressBecomesTheDefault(t *testing.T) {
	h := newAddressHarness(t)

	out := h.create(t, h.owner)

	if !out.IsDefault {
		t.Fatal("expected the first address to become the default")
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != out.ID {
		t.Fatalf("expected exactly one default, got %v", got)
	}
}

// US2 acceptance scenario 3: a second address is not marked as default and the
// account still has exactly one (FR-008).
func TestASecondAddressDoesNotBecomeTheDefault(t *testing.T) {
	h := newAddressHarness(t)
	first := h.create(t, h.owner)

	second := h.create(t, h.owner)

	if second.IsDefault {
		t.Fatal("a second address must not steal the default flag")
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != first.ID {
		t.Fatalf("expected the first address to stay the only default, got %v", got)
	}
}

// The province and ward names are captured from the dataset when the client omits
// them, so a later rename of an administrative unit cannot rewrite history
// (research D10).
func TestCreateCapturesTheDivisionNamesFromTheDataset(t *testing.T) {
	h := newAddressHarness(t)

	out := h.create(t, h.owner)

	if out.ProvinceName != "Ho Chi Minh" || out.WardName != "Phuong Ben Nghe" {
		t.Fatalf("expected the captured dataset names, got %q / %q", out.ProvinceName, out.WardName)
	}
	if out.DivisionNeedsReview {
		t.Fatal("a pair taken from the dataset must not be flagged for review")
	}
}

// FR-010: clearing the previous default and setting the new one is a single
// indivisible outcome. Both writes must happen inside one transaction opened by
// the application layer, and the new default must be set only after the old one is
// cleared — setting it first is what the partial unique index would reject.
func TestSettingADefaultClearsThePreviousOneInsideOneTransaction(t *testing.T) {
	h := newAddressHarness(t)
	first := h.create(t, h.owner)
	second := h.create(t, h.owner)
	before := h.tx.opened

	out, err := h.svc.SetDefaultAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: second.ID,
	})

	if err != nil {
		t.Fatalf("SetDefaultAddress: %v", err)
	}
	if !out.IsDefault {
		t.Fatal("expected the response to report the address as default")
	}
	if h.tx.opened != before+1 {
		t.Fatalf("expected exactly one transaction, got %d", h.tx.opened-before)
	}
	operations := h.repo.operations
	if !containsAll(operations, "ClearDefault", "SetDefault") {
		t.Fatalf("expected the flag to be cleared before it is set, got %v", operations)
	}
	if indexOf(operations, "ClearDefault") > indexOf(operations, "SetDefault") {
		t.Fatalf("the previous default must be cleared first, got %v", operations)
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != second.ID {
		t.Fatalf("expected exactly one default and it must be the new one, got %v (was %s)", got, first.ID)
	}
}

// When the transaction fails, neither write survives: the previous default stays
// in place instead of the account ending up with none or two (FR-010).
func TestAFailedDefaultChangeRollsBothWritesBack(t *testing.T) {
	h := newAddressHarness(t)
	first := h.create(t, h.owner)
	second := h.create(t, h.owner)
	// Every write so far ran inside its own transaction, so nothing escaped one.
	if got := h.repo.writesOutsideTx; got != 0 {
		t.Fatalf("expected no write outside a transaction, got %d", got)
	}
	rejected := errors.New("the commit was rejected")
	h.tx.failure = rejected

	_, err := h.svc.SetDefaultAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: second.ID,
	})
	h.tx.failure = nil

	if !errors.Is(err, rejected) {
		t.Fatalf("expected the transaction failure to surface, got %v", err)
	}
	if got := h.repo.writesOutsideTx; got != 0 {
		t.Fatalf("a write escaped its transaction: %d", got)
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != first.ID {
		t.Fatalf("a failed change must leave the previous default in place, got %v", got)
	}
	if h.audit.countOf(constant.AuditAddressDefaultSet) != 1 {
		t.Fatal("only the first address becoming the default may be audited")
	}
}

// A default flag on a hidden address would break the invariant in the shape SC-004
// forbids, so hiding the only address leaves the account with none and the next
// address created becomes the default (US2 acceptance scenario 6).
func TestHidingTheOnlyAddressLeavesNoDefaultAndTheNextOneTakesIt(t *testing.T) {
	h := newAddressHarness(t)
	only := h.create(t, h.owner)

	if err := h.svc.DeleteAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: only.ID,
	}); err != nil {
		t.Fatalf("DeleteAddress: %v", err)
	}

	if got := h.defaults(h.owner); len(got) != 0 {
		t.Fatalf("expected no default after hiding the only address, got %v", got)
	}
	list := h.list(t, 1, 20)
	if len(list.Addresses) != 0 || list.Total != 0 {
		t.Fatalf("a hidden address must leave the list, got %+v", list)
	}

	next := h.create(t, h.owner)
	if !next.IsDefault {
		t.Fatal("the address created after the last one was hidden must become the default")
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != next.ID {
		t.Fatalf("expected exactly one default, got %v", got)
	}
}

// A hidden row survives in storage so past orders keep the text they used
// (research D4, FR-012). The list is where it disappears, not the table.
func TestAHiddenAddressStaysInStorageWithItsText(t *testing.T) {
	h := newAddressHarness(t)
	only := h.create(t, h.owner)

	if err := h.svc.DeleteAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: only.ID,
	}); err != nil {
		t.Fatalf("DeleteAddress: %v", err)
	}

	h.repo.mu.Lock()
	defer h.repo.mu.Unlock()
	row, ok := h.repo.rows[only.ID]
	if !ok {
		t.Fatal("the hidden row must survive in storage")
	}
	if row.DeletedAt == nil {
		t.Fatal("expected the row to carry a hidden marker")
	}
	if row.RecipientName != "Nguyen Van A" || row.ProvinceName != "Ho Chi Minh" ||
		row.WardName != "Phuong Ben Nghe" || row.StreetAddress != "12 Nguyen Hue" {
		t.Fatalf("the hidden row lost its text: %+v", row)
	}
}

// Hiding the same address twice is reported as not found, because a hidden
// address is no longer part of the account's visible set.
func TestHidingAnAlreadyHiddenAddressReportsNotFound(t *testing.T) {
	h := newAddressHarness(t)
	only := h.create(t, h.owner)
	ref := appdto.AddressRefInput{UserID: h.owner, AddressID: only.ID}
	if err := h.svc.DeleteAddress(context.Background(), ref); err != nil {
		t.Fatalf("first hide: %v", err)
	}

	err := h.svc.DeleteAddress(context.Background(), ref)

	if !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound, got %v", err)
	}
}

// FR-007a and FR-007b: a province or ward outside the dataset, and a ward that
// belongs to another province, are refused before anything is written. The
// sentinels are the shared ones presentation maps to the USER_* codes.
func TestDivisionsOutsideTheDatasetAreRejectedWithoutPersisting(t *testing.T) {
	cases := []struct {
		name         string
		provinceCode string
		wardCode     string
		want         error
	}{
		{"unknown province", "99", "26734", administrative.ErrUnknownProvince},
		{"unknown ward", "79", "99999", administrative.ErrUnknownWard},
		{"a ward of another province", "01", "26734", administrative.ErrWardProvinceMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAddressHarness(t)

			out, err := h.svc.CreateAddress(context.Background(), appdto.CreateAddressInput{
				UserID:         h.owner,
				RecipientName:  "Nguyen Van A",
				RecipientPhone: "0912345678",
				ProvinceCode:   tc.provinceCode,
				WardCode:       tc.wardCode,
				StreetAddress:  "12 Nguyen Hue",
			})

			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if out.ID != uuid.Nil {
				t.Fatalf("a refused save must return no address, got %+v", out)
			}
			if len(h.repo.rows) != 0 {
				t.Fatalf("a refused save must not persist anything, got %+v", h.repo.rows)
			}
			if len(h.audit.events) != 0 {
				t.Fatalf("a refused save must leave no audit event, got %+v", h.audit.events)
			}
		})
	}
}

// The same rule guards an edit, and an edit that fails leaves the stored address
// — including its default flag — exactly as it was (spec edge case: a customer's
// default address edited to become invalid).
func TestAnEditToDivisionsOutsideTheDatasetIsRejectedAndChangesNothing(t *testing.T) {
	h := newAddressHarness(t)
	address := h.create(t, h.owner)
	before := h.repo.rows[address.ID]

	for _, edit := range []appdto.UpdateAddressInput{
		{UserID: h.owner, AddressID: address.ID, ProvinceCode: stringPtr("99")},
		{UserID: h.owner, AddressID: address.ID, WardCode: stringPtr("99999")},
		{UserID: h.owner, AddressID: address.ID, ProvinceCode: stringPtr("01")},
	} {
		if _, err := h.svc.UpdateAddress(context.Background(), edit); err == nil {
			t.Fatalf("expected the edit %+v to be rejected", edit)
		}
	}

	after := h.repo.rows[address.ID]
	if after.ProvinceCode != before.ProvinceCode || after.WardCode != before.WardCode {
		t.Fatalf("a rejected edit changed the divisions: %+v -> %+v", before, after)
	}
	if !after.IsDefault {
		t.Fatal("a rejected edit must leave the previous default in place")
	}
	if h.audit.countOf(constant.AuditAddressUpdated) != 0 {
		t.Fatal("a rejected edit must leave no audit event")
	}
}

// FR-007b names the mismatched field, so an edit that pairs a ward with a province
// that does not contain it is refused as such rather than as an unknown code.
func TestAnEditPairingAWardWithTheWrongProvinceReportsTheMismatch(t *testing.T) {
	h := newAddressHarness(t)
	address := h.create(t, h.owner)

	_, err := h.svc.UpdateAddress(context.Background(), appdto.UpdateAddressInput{
		UserID: h.owner, AddressID: address.ID, ProvinceCode: stringPtr("01"),
	})

	if !errors.Is(err, administrative.ErrWardProvinceMismatch) {
		t.Fatalf("expected ErrWardProvinceMismatch, got %v", err)
	}
}

// The recipient phone is normalised and validated before anything is written, so
// an unreachable number never reaches the table (FR-003, research D9).
func TestAnInvalidRecipientPhoneIsRejectedWithoutPersisting(t *testing.T) {
	h := newAddressHarness(t)

	out, err := h.svc.CreateAddress(context.Background(), appdto.CreateAddressInput{
		UserID:         h.owner,
		RecipientName:  "Nguyen Van A",
		RecipientPhone: "12345",
		ProvinceCode:   "79",
		WardCode:       "26734",
		StreetAddress:  "12 Nguyen Hue",
	})

	if !errors.Is(err, domainerr.ErrInvalidPhone) {
		t.Fatalf("expected ErrInvalidPhone, got %v", err)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("a refused save must return no address, got %+v", out)
	}
	if len(h.repo.rows) != 0 {
		t.Fatal("a refused save must not persist anything")
	}
}

// FR-011: an edit changes the named members and leaves the default flag alone,
// which the returned row proves because the repository never writes that column.
func TestAnEditPreservesTheDefaultFlag(t *testing.T) {
	h := newAddressHarness(t)
	address := h.create(t, h.owner)

	out, err := h.svc.UpdateAddress(context.Background(), appdto.UpdateAddressInput{
		UserID:        h.owner,
		AddressID:     address.ID,
		StreetAddress: stringPtr("1 Le Loi"),
		RecipientName: stringPtr("Nguyen Van B"),
	})

	if err != nil {
		t.Fatalf("UpdateAddress: %v", err)
	}
	if !out.IsDefault {
		t.Fatal("an edit must preserve the default flag")
	}
	if out.StreetAddress != "1 Le Loi" || out.RecipientName != "Nguyen Van B" {
		t.Fatalf("the named members were not edited: %+v", out)
	}
	if out.ProvinceCode != "79" || out.WardCode != "26734" {
		t.Fatalf("omitted divisions must keep their values: %+v", out)
	}
	if !h.repo.rows[address.ID].IsDefault {
		t.Fatal("the stored row lost its default flag")
	}
}

// Changing the province recaptures both division names from the dataset, so the
// stored names keep describing the stored codes (research D10).
func TestChangingTheProvinceRecapturesTheDivisionNames(t *testing.T) {
	h := newAddressHarness(t)
	address := h.create(t, h.owner)

	out, err := h.svc.UpdateAddress(context.Background(), appdto.UpdateAddressInput{
		UserID: h.owner, AddressID: address.ID,
		ProvinceCode: stringPtr("01"), WardCode: stringPtr("00001"),
	})

	if err != nil {
		t.Fatalf("UpdateAddress: %v", err)
	}
	if out.ProvinceName != "Ha Noi" || out.WardName != "Phuong Cau Giay" {
		t.Fatalf("expected the recaptured names, got %q / %q", out.ProvinceName, out.WardName)
	}
}

// FR-018: the list is paginated, returns the total and puts the default first so a
// checkout can offer it without reordering on the client (FR-007d).
func TestListAddressesIsPaginatedAndPutsTheDefaultFirst(t *testing.T) {
	h := newAddressHarness(t)
	first := h.create(t, h.owner)
	second := h.create(t, h.owner)
	third := h.create(t, h.owner)

	page := h.list(t, 1, 2)
	if page.Total != 3 {
		t.Fatalf("expected the total count, got %d", page.Total)
	}
	if len(page.Addresses) != 2 {
		t.Fatalf("expected 2 addresses on the page, got %d", len(page.Addresses))
	}
	if !page.Addresses[0].IsDefault || page.Addresses[0].ID != first.ID {
		t.Fatalf("expected the default address first, got %+v", page.Addresses[0])
	}
	rest := h.list(t, 2, 2)
	if len(rest.Addresses) != 1 {
		t.Fatalf("expected 1 address on the second page, got %d", len(rest.Addresses))
	}
	if rest.Addresses[0].ID == first.ID {
		t.Fatal("the second page repeated an address from the first")
	}
	for _, id := range []uuid.UUID{second.ID, third.ID} {
		if rest.Addresses[0].ID != id && page.Addresses[1].ID != id {
			t.Fatalf("expected %s in the listing, got %+v and %+v", id, page.Addresses[1], rest.Addresses[0])
		}
	}
}

// An out-of-range window is clamped rather than refused, so a client that asks
// for page 0 or a huge page still gets a usable answer.
func TestListAddressesClampsAnUnusableWindow(t *testing.T) {
	h := newAddressHarness(t)
	h.create(t, h.owner)

	page := h.list(t, 0, 1000)

	if page.Page != 1 || page.PageSize != 100 {
		t.Fatalf("expected the window to be clamped to 1/100, got %d/%d", page.Page, page.PageSize)
	}
	if len(page.Addresses) != 1 {
		t.Fatalf("expected the one address, got %d", len(page.Addresses))
	}
}

// FR-006, FR-013, SC-003: the acting account comes from the session, so another
// customer's address is simply not found — the same answer as an unknown id, and
// never a disclosure.
func TestEveryOperationIsScopedToTheSessionAccount(t *testing.T) {
	h := newAddressHarness(t)
	foreign := h.seedAddress(t, h.others, model.Address{IsDefault: true})
	mine := h.create(t, h.owner)
	beforeForeign := h.repo.rows[foreign.ID]

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"edit", func() error {
			_, err := h.svc.UpdateAddress(context.Background(), appdto.UpdateAddressInput{
				UserID: h.owner, AddressID: foreign.ID, StreetAddress: stringPtr("1 Le Loi"),
			})
			return err
		}},
		{"hide", func() error {
			return h.svc.DeleteAddress(context.Background(), appdto.AddressRefInput{
				UserID: h.owner, AddressID: foreign.ID,
			})
		}},
		{"mark default", func() error {
			_, err := h.svc.SetDefaultAddress(context.Background(), appdto.AddressRefInput{
				UserID: h.owner, AddressID: foreign.ID,
			})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, domainerr.ErrAddressNotFound) {
				t.Fatalf("expected ErrAddressNotFound for another customer's address, got %v", err)
			}
		})
	}

	if after := h.repo.rows[foreign.ID]; after != beforeForeign {
		t.Fatalf("another customer's address changed: %+v -> %+v", beforeForeign, after)
	}
	if got := h.defaults(h.others); len(got) != 1 || got[0] != foreign.ID {
		t.Fatalf("another customer's default moved: %v", got)
	}
	if got := h.defaults(h.owner); len(got) != 1 || got[0] != mine.ID {
		t.Fatalf("the acting account's default changed: %v", got)
	}
}

// A refused operation is never audited: the audit trail must not record a change
// that did not happen.
func TestARefusedOperationLeavesNoAuditEvent(t *testing.T) {
	h := newAddressHarness(t)
	h.create(t, h.owner)
	baseline := len(h.audit.events)

	if _, err := h.svc.SetDefaultAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: uuid.New(),
	}); !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound, got %v", err)
	}

	if len(h.audit.events) != baseline {
		t.Fatalf("a refused operation must leave no audit event, got %+v", h.audit.events[baseline:])
	}
}

// FR-019, SC-008: each of the four address actions is recorded with the acting
// account, the address it touched and what changed. The event names the members
// that changed, never their values — contact data must not be copied into the
// audit trail (Constitution VI).
func TestTheFourAddressActionsAreAudited(t *testing.T) {
	h := newAddressHarness(t)

	first := h.create(t, h.owner)
	second := h.create(t, h.owner)
	if _, err := h.svc.UpdateAddress(context.Background(), appdto.UpdateAddressInput{
		UserID: h.owner, AddressID: second.ID, StreetAddress: stringPtr("1 Le Loi"),
	}); err != nil {
		t.Fatalf("UpdateAddress: %v", err)
	}
	if _, err := h.svc.SetDefaultAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: second.ID,
	}); err != nil {
		t.Fatalf("SetDefaultAddress: %v", err)
	}
	if err := h.svc.DeleteAddress(context.Background(), appdto.AddressRefInput{
		UserID: h.owner, AddressID: second.ID,
	}); err != nil {
		t.Fatalf("DeleteAddress: %v", err)
	}

	for action, want := range map[string]int{
		constant.AuditAddressCreated:    2,
		constant.AuditAddressUpdated:    1,
		constant.AuditAddressDefaultSet: 2,
		constant.AuditAddressDeleted:    1,
	} {
		if got := h.audit.countOf(action); got != want {
			t.Errorf("%s: expected %d events, got %d", action, want, got)
		}
	}

	// The first address becoming the default is a default change too, so the
	// action is recorded alongside the creation.
	if h.audit.countOf(constant.AuditAddressCreated) != h.audit.countOf(constant.AuditAddressDefaultSet) {
		t.Fatal("expected the automatic first default to be audited as a default change")
	}

	addressEvents := h.audit.events[:0:0]
	for _, e := range h.audit.events {
		if e.targetType != "address" {
			continue
		}
		addressEvents = append(addressEvents, e)
		if e.actorID == nil || *e.actorID != h.owner {
			t.Fatalf("expected the session account as the actor, got %v", e.actorID)
		}
		if e.actorRole != string(access.RoleCustomer) {
			t.Fatalf("expected the actor role, got %q", e.actorRole)
		}
		if e.outcome != "SUCCESS" {
			t.Fatalf("expected a SUCCESS outcome, got %q", e.outcome)
		}
		if len(e.metadata) == 0 {
			t.Fatalf("expected %s to say what it touched", e.action)
		}
		for key := range e.metadata {
			switch key {
			case "addressId", "changedFields", "becameDefault":
			default:
				t.Fatalf("unexpected audit metadata key %q", key)
			}
		}
		for _, value := range e.metadata {
			for _, secret := range []string{"Nguyen Van A", "Nguyen Van B", "0912345678", "12 Nguyen Hue", "1 Le Loi"} {
				if text, ok := value.(string); ok && text == secret {
					t.Fatalf("the audit trail copied a stored value: %q", secret)
				}
			}
		}
	}
	if len(addressEvents) != len(h.audit.events) {
		t.Fatalf("every address event must target the address, got %+v", h.audit.events)
	}
	// The two created addresses are both named by the creation events, and the
	// promoted one by the default event.
	seenTargets := map[string]bool{}
	for _, e := range addressEvents {
		seenTargets[e.targetID] = true
	}
	if !seenTargets[first.ID.String()] || !seenTargets[second.ID.String()] {
		t.Fatalf("expected both addresses in the trail, got %v", seenTargets)
	}

	// The edit names the members that changed.
	var updated []recordedEvent
	for _, e := range h.audit.events {
		if e.action == constant.AuditAddressUpdated {
			updated = append(updated, e)
		}
	}
	if len(updated) != 1 {
		t.Fatalf("expected one update event, got %d", len(updated))
	}
	fields, ok := updated[0].metadata["changedFields"].([]string)
	if !ok || len(fields) != 1 || fields[0] != model.FieldStreetAddress {
		t.Fatalf("expected the event to name %q, got %v", model.FieldStreetAddress, updated[0].metadata)
	}
}

// research D10: an address whose ward code has left the dataset is still readable
// and still shows the names captured when it was saved, flagged so the entry can
// be remapped rather than silently dropped.
func TestAStoredCodeThatLeftTheDatasetStillReadsBackWithItsCapturedNames(t *testing.T) {
	h := newAddressHarness(t)
	retired := h.seedAddress(t, h.owner, model.Address{
		ProvinceCode:  "79",
		ProvinceName:  "Ho Chi Minh (cu)",
		WardCode:      "11111",
		WardName:      "Phuong Da Xa",
		StreetAddress: "12 Nguyen Hue",
	})

	page := h.list(t, 1, 20)
	if len(page.Addresses) != 1 {
		t.Fatalf("expected the retired address to stay readable, got %+v", page.Addresses)
	}
	got := page.Addresses[0]
	if got.ID != retired.ID {
		t.Fatalf("expected address %s, got %s", retired.ID, got.ID)
	}
	if !got.DivisionNeedsReview {
		t.Fatal("expected a retired ward code to be flagged for review")
	}
	if got.WardName != "Phuong Da Xa" || got.ProvinceName != "Ho Chi Minh (cu)" {
		t.Fatalf("the captured names must survive, got %q / %q", got.ProvinceName, got.WardName)
	}
	if got.WardCode != "11111" || got.ProvinceCode != "79" {
		t.Fatalf("the stored codes must stay joinable to the new dataset, got %q / %q", got.ProvinceCode, got.WardCode)
	}
}

// An address whose codes are current is not flagged, so the marker stays a
// meaningful signal rather than noise on every row.
func TestACurrentAddressIsNotFlaggedForReview(t *testing.T) {
	h := newAddressHarness(t)

	out := h.create(t, h.owner)

	if out.DivisionNeedsReview {
		t.Fatal("an address taken from the dataset must not need review")
	}
}

// A listing never crosses accounts, so the page size cannot be used to walk into
// another customer's addresses.
func TestAListingOnlyEverReturnsTheSessionAccountsAddresses(t *testing.T) {
	h := newAddressHarness(t)
	h.seedAddress(t, h.others, model.Address{IsDefault: true})
	mine := h.create(t, h.owner)

	page := h.list(t, 1, 100)

	if page.Total != 1 || len(page.Addresses) != 1 {
		t.Fatalf("expected only the acting account's address, got %+v", page)
	}
	if page.Addresses[0].ID != mine.ID {
		t.Fatalf("expected %s, got %s", mine.ID, page.Addresses[0].ID)
	}
}

func containsAll(values []string, wanted ...string) bool {
	for _, want := range wanted {
		if indexOf(values, want) < 0 {
			return false
		}
	}
	return true
}

func indexOf(values []string, wanted string) int {
	for i, value := range values {
		if value == wanted {
			return i
		}
	}
	return -1
}
