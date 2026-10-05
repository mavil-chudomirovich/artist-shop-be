package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file exercises the US2 routes end to end: the real handlers, the real
// authentication middleware and the real address use cases over an in-memory
// repository. router_test.go proves the wiring of every route with a stub; here the
// point is what the routes actually do to stored data and what they answer when a
// customer reaches for somebody else's address.

// memoryAddressRepo is an in-memory AddressRepository. It mirrors the adapter's
// column-level writes — an update never touches is_default or deleted_at — and
// refuses a second visible default exactly like the partial unique index does
// (ADR-003), so a use case that wrote two of them fails here instead of passing.
type memoryAddressRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]model.Address
	seq  []uuid.UUID
	// inTx mirrors the adapter's transaction awareness.
	inTx bool
	// writesOutsideTx counts writes that were not part of a transaction.
	writesOutsideTx int
}

// errSecondDefault stands in for the partial unique index
// addresses_one_default_per_user.
var errSecondDefault = errors.New("addresses_one_default_per_user: a second default address for one account")

func newMemoryAddressRepo(rows ...model.Address) *memoryAddressRepo {
	store := &memoryAddressRepo{rows: make(map[uuid.UUID]model.Address, len(rows))}
	for _, row := range rows {
		store.rows[row.ID] = row
		store.seq = append(store.seq, row.ID)
	}
	return store
}

func (m *memoryAddressRepo) record() {
	if !m.inTx {
		m.writesOutsideTx++
	}
}

func (m *memoryAddressRepo) visible(userID uuid.UUID) []model.Address {
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

func (m *memoryAddressRepo) Create(_ context.Context, address *model.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record()
	for _, row := range m.visible(address.UserID) {
		if row.IsDefault && address.IsDefault {
			return errSecondDefault
		}
	}
	m.rows[address.ID] = *address
	m.seq = append(m.seq, address.ID)
	return nil
}

func (m *memoryAddressRepo) Update(_ context.Context, address *model.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record()
	stored, ok := m.rows[address.ID]
	if !ok || stored.UserID != address.UserID || stored.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
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

func (m *memoryAddressRepo) FindByOwner(_ context.Context, userID, addressID uuid.UUID) (*model.Address, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[addressID]
	if !ok || row.UserID != userID || row.DeletedAt != nil {
		return nil, domainerr.ErrAddressNotFound
	}
	stored := row
	return &stored, nil
}

func (m *memoryAddressRepo) ListByOwner(_ context.Context, userID uuid.UUID, page, pageSize int) ([]model.Address, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *memoryAddressRepo) ClearDefault(_ context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record()
	for _, id := range m.seq {
		row := m.rows[id]
		if row.UserID == userID && row.IsDefault && row.DeletedAt == nil {
			row.IsDefault = false
			m.rows[id] = row
		}
	}
	return nil
}

func (m *memoryAddressRepo) SetDefault(_ context.Context, addressID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record()
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

func (m *memoryAddressRepo) Hide(_ context.Context, userID, addressID uuid.UUID, hiddenAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record()
	row, ok := m.rows[addressID]
	if !ok || row.UserID != userID || row.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
	row.DeletedAt = &hiddenAt
	row.IsDefault = false
	m.rows[addressID] = row
	return nil
}

func (m *memoryAddressRepo) writeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writesOutsideTx
}

var _ repository.AddressRepository = (*memoryAddressRepo)(nil)

// immediateUnitOfWork marks the store as inside a transaction without deferring
// anything: the HTTP tests care about what the routes change, and the rollback
// behaviour is covered by the use-case tests.
type immediateUnitOfWork struct {
	repo   *memoryAddressRepo
	opened int
}

func (u *immediateUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.opened++
	u.repo.mu.Lock()
	u.repo.inTx = true
	u.repo.mu.Unlock()
	defer func() {
		u.repo.mu.Lock()
		u.repo.inTx = false
		u.repo.mu.Unlock()
	}()
	return fn(ctx)
}

var _ appinterface.UnitOfWork = (*immediateUnitOfWork)(nil)

// cascadeDivisions serves two provinces so a stale code and a ward from another
// province are both reachable without reading the bundled dataset.
type cascadeDivisions struct{}

func (cascadeDivisions) Provinces(context.Context) ([]appinterface.Province, error) {
	return []appinterface.Province{
		{Code: "01", Name: "Ha Noi"},
		{Code: "79", Name: "Ho Chi Minh"},
	}, nil
}

func (cascadeDivisions) Wards(_ context.Context, provinceCode string) ([]appinterface.Ward, error) {
	if provinceCode != "79" {
		return nil, administrative.ErrUnknownProvince
	}
	return []appinterface.Ward{
		{Code: "26734", Name: "Phuong Ben Nghe", ProvinceCode: "79"},
		{Code: "26735", Name: "Phuong Ben Thanh", ProvinceCode: "79"},
	}, nil
}

func (cascadeDivisions) ValidateAddressDivisions(_ context.Context, provinceCode, wardCode string) error {
	// The province is checked first, exactly as the shared dataset does, so a stale
	// first select cannot produce a meaningless ward.
	if provinceCode != "79" && provinceCode != "01" {
		return administrative.ErrUnknownProvince
	}
	switch wardCode {
	case "26734", "26735":
		if provinceCode != "79" {
			return administrative.ErrWardProvinceMismatch
		}
		return nil
	case "99999":
		return administrative.ErrUnknownWard
	default:
		return administrative.ErrUnknownWard
	}
}

var _ appinterface.Divisions = cascadeDivisions{}

// addressService binds the real address use cases to the module's full use-case
// surface. The profile and avatar methods come from the existing stub, whose
// routes these tests never reach; this adapter disappears once T023 wires
// *implement.Service directly.
type addressService struct {
	*stubService
	addresses *implement.Service
}

func (s addressService) ListAddresses(ctx context.Context, in appdto.ListAddressesInput) (appdto.AddressPageOutput, error) {
	return s.addresses.ListAddresses(ctx, in)
}

func (s addressService) CreateAddress(ctx context.Context, in appdto.CreateAddressInput) (appdto.AddressOutput, error) {
	return s.addresses.CreateAddress(ctx, in)
}

func (s addressService) UpdateAddress(ctx context.Context, in appdto.UpdateAddressInput) (appdto.AddressOutput, error) {
	return s.addresses.UpdateAddress(ctx, in)
}

func (s addressService) DeleteAddress(ctx context.Context, in appdto.AddressRefInput) error {
	return s.addresses.DeleteAddress(ctx, in)
}

func (s addressService) SetDefaultAddress(ctx context.Context, in appdto.AddressRefInput) (appdto.AddressOutput, error) {
	return s.addresses.SetDefaultAddress(ctx, in)
}

var _ appinterface.UserService = addressService{}

// addressFixture is the whole US2 stack over in-memory storage.
type addressFixture struct {
	router http.Handler
	repo   *memoryAddressRepo
	audit  *profileAudit
	// customer is the account a valid session identifies.
	customer uuid.UUID
	// other is a second account whose addresses must stay invisible.
	other uuid.UUID
	// foreign is one address of the other account.
	foreign uuid.UUID
}

func newAddressFixture(t *testing.T) *addressFixture {
	t.Helper()
	customer := uuid.New()
	other := uuid.New()
	foreign := uuid.New()
	repo := newMemoryAddressRepo()
	audit := &profileAudit{}
	divisions := cascadeDivisions{}
	addresses := implement.New(implement.Service{
		Profiles: newMemoryProfiles(
			model.Profile{ID: customer, Email: "customer@example.com", Role: access.RoleCustomer},
			model.Profile{ID: other, Email: "other@example.com", Role: access.RoleCustomer},
		),
		Addresses: repo,
		Divisions: divisions,
		Tx:        &immediateUnitOfWork{repo: repo},
		Audit:     audit,
		Mapper:    mapper.New(divisions),
	})
	svc := addressService{stubService: newStub(access.RoleCustomer), addresses: addresses}
	handler := New(svc, appinterface.Config{}, testLogger)
	return &addressFixture{
		router:   handler.Router(openLimits, sessionHooks(customer)),
		repo:     repo,
		audit:    audit,
		customer: customer,
		other:    other,
		foreign:  foreign,
	}
}

// seedForeign stores one address of the other account, so the cross-account tests
// have a real target that must never be reached.
func (f *addressFixture) seedForeign(t *testing.T) {
	t.Helper()
	stamp := time.Now().UTC()
	f.repo.mu.Lock()
	defer f.repo.mu.Unlock()
	f.repo.rows[f.foreign] = model.Address{
		ID: f.foreign, UserID: f.other,
		RecipientName: "Someone Else", RecipientPhone: "0912345678",
		ProvinceCode: "79", ProvinceName: "Ho Chi Minh",
		WardCode: "26734", WardName: "Phuong Ben Nghe",
		StreetAddress: "99 Le Loi", IsDefault: true,
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	f.repo.seq = append(f.repo.seq, f.foreign)
}

const addressBody = `{"recipientName":"Nguyen Van A","recipientPhone":"0912 345 678",` +
	`"provinceCode":"79","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`

type addressListBody struct {
	Data []httpdtoAddress `json:"data"`
	Meta struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"meta"`
}

// httpdtoAddress mirrors the contract's Address shape. It is declared here rather
// than imported so the test asserts the exact JSON member names clients depend on.
type httpdtoAddress struct {
	ID                  string `json:"id"`
	RecipientName       string `json:"recipientName"`
	RecipientPhone      string `json:"recipientPhone"`
	ProvinceCode        string `json:"provinceCode"`
	ProvinceName        string `json:"provinceName"`
	WardCode            string `json:"wardCode"`
	WardName            string `json:"wardName"`
	StreetAddress       string `json:"streetAddress"`
	IsDefault           bool   `json:"isDefault"`
	DivisionNeedsReview bool   `json:"divisionNeedsReview"`
}

type addressItemBody struct {
	Data httpdtoAddress `json:"data"`
}

func decodeAddressList(t *testing.T, rec *httptest.ResponseRecorder) addressListBody {
	t.Helper()
	var body addressListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode address list %q: %v", rec.Body.String(), err)
	}
	return body
}

func decodeAddress(t *testing.T, rec *httptest.ResponseRecorder) httpdtoAddress {
	t.Helper()
	var body addressItemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode address %q: %v", rec.Body.String(), err)
	}
	return body.Data
}

// create stores one address for the session account and returns its identifier.
func (f *addressFixture) create(t *testing.T, token string) string {
	t.Helper()
	rec := doJSON(f.router, http.MethodPost, "/me/addresses", addressBody, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed an address: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAddress(t, rec).ID
}

func (f *addressFixture) list(t *testing.T, token, query string) addressListBody {
	t.Helper()
	rec := do(f.router, http.MethodGet, "/me/addresses"+query, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list addresses: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAddressList(t, rec)
}

// US2 acceptance scenario 1 and 2: the first address is stored and becomes the
// default, with the division names captured from the dataset.
func TestCreatingTheFirstAddressStoresItAndMakesItTheDefault(t *testing.T) {
	fixture := newAddressFixture(t)

	rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", addressBody, liveToken)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	created := decodeAddress(t, rec)
	if !created.IsDefault {
		t.Fatal("expected the first address to become the default")
	}
	if created.RecipientPhone != "0912345678" {
		t.Fatalf("expected the normalised recipient phone, got %q", created.RecipientPhone)
	}
	if created.ProvinceName != "Ho Chi Minh" || created.WardName != "Phuong Ben Nghe" {
		t.Fatalf("expected the captured dataset names, got %q / %q", created.ProvinceName, created.WardName)
	}
	if created.DivisionNeedsReview {
		t.Fatal("a pair taken from the dataset must not be flagged for review")
	}

	listed := fixture.list(t, liveToken, "")
	if listed.Meta.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != created.ID {
		t.Fatalf("expected the saved address in the list, got %+v", listed)
	}
}

// US2 acceptance scenario 3: a second address is not marked as default.
func TestCreatingASecondAddressLeavesTheOnlyDefaultAlone(t *testing.T) {
	fixture := newAddressFixture(t)
	first := fixture.create(t, liveToken)

	second := fixture.create(t, liveToken)

	listed := fixture.list(t, liveToken, "")
	if listed.Meta.Total != 2 {
		t.Fatalf("expected 2 addresses, got %d", listed.Meta.Total)
	}
	if listed.Data[0].ID != first || !listed.Data[0].IsDefault {
		t.Fatalf("expected the first address still default and first in the list, got %+v", listed.Data)
	}
	if listed.Data[1].ID != second {
		t.Fatal("the second address was not stored")
	}
	for _, row := range listed.Data[1:] {
		if row.IsDefault {
			t.Fatalf("a second address must not be default: %+v", row)
		}
	}
}

// US2 acceptance scenario 4 and FR-010: marking another address default clears the
// previous one, and both writes happen inside the transaction the use case opened.
func TestMarkingADefaultMovesTheSingleFlag(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.create(t, liveToken)
	second := fixture.create(t, liveToken)
	writesBefore := fixture.repo.writeCount()

	rec := do(fixture.router, http.MethodPost, "/me/addresses/"+second+"/default", liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !decodeAddress(t, rec).IsDefault {
		t.Fatal("expected the response to report the address as default")
	}
	// Both writes of the transition must be inside the transaction, so nothing
	// since the two creates escaped one.
	if got := fixture.repo.writeCount(); got != writesBefore {
		t.Fatalf("the default transition wrote outside its transaction: %d -> %d", writesBefore, got)
	}

	listed := fixture.list(t, liveToken, "")
	defaults := 0
	for _, row := range listed.Data {
		if row.IsDefault {
			defaults++
			if row.ID != second {
				t.Fatalf("expected %s to be the only default, got %+v", second, listed.Data)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default, got %d", defaults)
	}
}

// US2 acceptance scenario 5 and FR-011: an edit changes only that address and
// preserves the default flag.
func TestEditingAnAddressPreservesItsDefaultFlag(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.create(t, liveToken)
	second := fixture.create(t, liveToken)
	if rec := do(fixture.router, http.MethodPost, "/me/addresses/"+second+"/default", liveToken); rec.Code != http.StatusOK {
		t.Fatalf("mark the second address default: %d (%s)", rec.Code, rec.Body.String())
	}

	rec := doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+second,
		`{"recipientName":"Nguyen Van B","streetAddress":"1 Le Loi"}`, liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	edited := decodeAddress(t, rec)
	if !edited.IsDefault {
		t.Fatal("an edit must preserve the default flag")
	}
	if edited.RecipientName != "Nguyen Van B" || edited.StreetAddress != "1 Le Loi" {
		t.Fatalf("unexpected edited address: %+v", edited)
	}
	if edited.ProvinceCode != "79" || edited.WardCode != "26734" {
		t.Fatalf("omitted divisions must keep their values: %+v", edited)
	}
	if edited.RecipientPhone != "0912345678" {
		t.Fatalf("an omitted phone must keep its value: %+v", edited)
	}
}

// FR-018: the window is validated before the use case runs, and an accepted one
// comes back in the response envelope.
func TestTheAddressListWindowIsValidatedAndEchoed(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.create(t, liveToken)

	for _, query := range []string{"?page=0", "?page=abc", "?pageSize=0", "?pageSize=101"} {
		t.Run(query, func(t *testing.T) {
			rec := do(fixture.router, http.MethodGet, "/me/addresses"+query, liveToken)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", got)
			}
		})
	}

	listed := fixture.list(t, liveToken, "?page=1&pageSize=100")
	if listed.Meta.Page != 1 || listed.Meta.PageSize != 100 || listed.Meta.Total != 1 {
		t.Fatalf("unexpected envelope: %+v", listed.Meta)
	}
}

// An account with no address must answer an empty array, never null: the contract
// declares the list as an array, so `"data":null` is a shape no client can read and
// a nil slice from the conversion would put it on the wire. The operator lookup's
// own list already allocates for exactly this reason.
//
// The member is read as raw JSON on purpose. Decoding into a slice would hide the
// difference — a null decodes to a nil slice, which reads the same as an empty one —
// so only the raw bytes can tell [] apart from null.
func TestAnAccountWithNoAddressAnswersAnEmptyArrayAndNeverNull(t *testing.T) {
	fixture := newAddressFixture(t)

	rec := do(fixture.router, http.MethodGet, "/me/addresses", liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode address list %q: %v", rec.Body.String(), err)
	}
	if got := string(envelope.Data); got != "[]" {
		t.Fatalf(`expected "data":[] for an account with no address, got %s`, got)
	}
	listed := fixture.list(t, liveToken, "")
	if listed.Meta.Total != 0 || len(listed.Data) != 0 {
		t.Fatalf("expected an empty list, got %+v", listed)
	}
}

// SC-003, FR-013: another customer's address is answered exactly like an unknown
// id, so the response can never confirm that it exists.
func TestAnotherCustomersAddressIsAnsweredAsNotFound(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.seedForeign(t)
	unknown := uuid.New().String()
	fixture.create(t, liveToken)

	for _, id := range []string{fixture.foreign.String(), unknown} {
		t.Run(id, func(t *testing.T) {
			for _, call := range []struct {
				name   string
				invoke func() *httptest.ResponseRecorder
			}{
				{"edit", func() *httptest.ResponseRecorder {
					return doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+id, `{"streetAddress":"1 Le Loi"}`, liveToken)
				}},
				{"hide", func() *httptest.ResponseRecorder {
					return do(fixture.router, http.MethodDelete, "/me/addresses/"+id, liveToken)
				}},
				{"mark default", func() *httptest.ResponseRecorder {
					return do(fixture.router, http.MethodPost, "/me/addresses/"+id+"/default", liveToken)
				}},
			} {
				t.Run(call.name, func(t *testing.T) {
					rec := call.invoke()
					if rec.Code != http.StatusNotFound {
						t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
					}
					if got := errorCode(t, rec); got != constant.CodeAddressNotFound {
						t.Fatalf("expected %s, got %s", constant.CodeAddressNotFound, got)
					}
					if rec.Body.Len() > 0 && fixture.leaksForeignAddress(rec.Body.String()) {
						t.Fatalf("the response leaked address data: %s", rec.Body.String())
					}
				})
			}
		})
	}

	// The other account's address is untouched and still its default.
	row, ok := fixture.repo.rows[fixture.foreign]
	if !ok || !row.IsDefault || row.StreetAddress != "99 Le Loi" {
		t.Fatalf("another customer's address changed: %+v", row)
	}
}

// leaksForeignAddress reports whether a response body carries any of the other
// customer's stored values, including their address identifier.
func (f *addressFixture) leaksForeignAddress(body string) bool {
	for _, secret := range []string{"Someone Else", "99 Le Loi", f.foreign.String()} {
		if strings.Contains(body, secret) {
			return true
		}
	}
	return false
}

// FR-012, ADR-004: hiding answers 204 with no body, the address leaves the list,
// and the stored row keeps its text for order history.
func TestHidingAnAddressAnswers204AndRemovesItFromTheList(t *testing.T) {
	fixture := newAddressFixture(t)
	first := fixture.create(t, liveToken)
	second := fixture.create(t, liveToken)

	rec := do(fixture.router, http.MethodDelete, "/me/addresses/"+second, liveToken)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no body on a hide, got %q", rec.Body.String())
	}

	listed := fixture.list(t, liveToken, "")
	if listed.Meta.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != first {
		t.Fatalf("expected only the remaining address, got %+v", listed)
	}

	row, ok := fixture.repo.rows[uuid.MustParse(second)]
	if !ok {
		t.Fatal("the hidden row must survive in storage")
	}
	if row.DeletedAt == nil {
		t.Fatal("expected the row to carry a hidden marker")
	}
	if row.StreetAddress != "12 Nguyen Hue" || row.WardName != "Phuong Ben Nghe" {
		t.Fatalf("the hidden row lost its text: %+v", row)
	}
}

// A hidden address is answered as not found too: it has left the account's visible
// set, and a hidden address must not be confirmable as existing.
func TestTouchingAHiddenAddressAnswersNotFound(t *testing.T) {
	fixture := newAddressFixture(t)
	id := fixture.create(t, liveToken)
	if rec := do(fixture.router, http.MethodDelete, "/me/addresses/"+id, liveToken); rec.Code != http.StatusNoContent {
		t.Fatalf("hide: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	for _, call := range []struct {
		name   string
		invoke func() *httptest.ResponseRecorder
	}{
		{"hide again", func() *httptest.ResponseRecorder {
			return do(fixture.router, http.MethodDelete, "/me/addresses/"+id, liveToken)
		}},
		{"edit", func() *httptest.ResponseRecorder {
			return doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+id, `{"streetAddress":"1 Le Loi"}`, liveToken)
		}},
		{"mark default", func() *httptest.ResponseRecorder {
			return do(fixture.router, http.MethodPost, "/me/addresses/"+id+"/default", liveToken)
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			rec := call.invoke()
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != constant.CodeAddressNotFound {
				t.Fatalf("expected %s, got %s", constant.CodeAddressNotFound, got)
			}
		})
	}
}

// US2 acceptance scenario 6: hiding the only address leaves no default, and the
// next address created becomes it.
func TestHidingTheOnlyAddressLeavesNoDefault(t *testing.T) {
	fixture := newAddressFixture(t)
	only := fixture.create(t, liveToken)

	if rec := do(fixture.router, http.MethodDelete, "/me/addresses/"+only, liveToken); rec.Code != http.StatusNoContent {
		t.Fatalf("hide: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	listed := fixture.list(t, liveToken, "")
	if listed.Meta.Total != 0 || len(listed.Data) != 0 {
		t.Fatalf("expected an empty list, got %+v", listed)
	}

	rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", addressBody, liveToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !decodeAddress(t, rec).IsDefault {
		t.Fatal("the address created after the last one was hidden must become the default")
	}
}

// FR-007a, FR-007b and FR-020: a stale code, a stale ward and a ward from another
// province are refused with the module's own code and the offending member named.
func TestDivisionsOutsideTheDatasetAreRefusedWithTheFieldNamed(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  string
		field string
	}{
		{
			name:  "unknown province",
			body:  `{"recipientName":"A","recipientPhone":"0912345678","provinceCode":"99","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`,
			code:  constant.CodeUnknownProvince,
			field: fieldProvinceCode,
		},
		{
			name:  "unknown ward",
			body:  `{"recipientName":"A","recipientPhone":"0912345678","provinceCode":"79","wardCode":"99999","streetAddress":"12 Nguyen Hue"}`,
			code:  constant.CodeUnknownWard,
			field: fieldWardCode,
		},
		{
			name:  "a ward of another province",
			body:  `{"recipientName":"A","recipientPhone":"0912345678","provinceCode":"01","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`,
			code:  constant.CodeWardProvinceMismatch,
			field: fieldWardCode,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAddressFixture(t)

			rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", tc.body, liveToken)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, got)
			}
			if got := detailField(t, rec); got != tc.field {
				t.Fatalf("expected the detail to name %q, got %q", tc.field, got)
			}
			if listed := fixture.list(t, liveToken, ""); listed.Meta.Total != 0 {
				t.Fatalf("a refused save must persist nothing, got %+v", listed)
			}
		})
	}
}

// A member over the maxLength the contract declares for it must be refused the
// same way a blank one is: 400 VALIDATION_ERROR with exactly one field detail
// naming the member the customer has to shorten. Nothing is persisted and nothing
// is audited, because nothing changed. This is the check that keeps the advertised
// maxLength from being a limit the server quietly ignores (FR-020).
func TestAnAddressMemberOverTheContractLengthCeilingIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		field string
		body  string
	}{
		{
			name:  "the recipient name",
			field: fieldRecipientName,
			body: `{"recipientName":"` + strings.Repeat("a", 121) + `",` +
				`"recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734",` +
				`"streetAddress":"12 Nguyen Hue"}`,
		},
		{
			name:  "the street address",
			field: fieldStreet,
			body: `{"recipientName":"Nguyen Van A","recipientPhone":"0912345678",` +
				`"provinceCode":"79","wardCode":"26734","streetAddress":"` +
				strings.Repeat("a", 256) + `"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAddressFixture(t)

			rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", tc.body, liveToken)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != string(httpx.CodeValidation) {
				t.Fatalf("expected VALIDATION_ERROR, got %s", got)
			}
			if got := detailField(t, rec); got != tc.field {
				t.Fatalf("expected the detail to name %q, got %q", tc.field, got)
			}
			if listed := fixture.list(t, liveToken, ""); listed.Meta.Total != 0 {
				t.Fatalf("a refused save must persist nothing, got %+v", listed)
			}
			if got := fixture.audit.countOf(constant.AuditAddressCreated); got != 0 {
				t.Fatalf("a refused save must leave no create event, got %d", got)
			}
		})
	}
}

// A member at exactly the advertised length is accepted through the wire, and so
// is a multi-byte one: the ceiling counts characters, so a Vietnamese name of 120
// characters passes even though it is far more than 120 bytes.
func TestAnAddressMemberAtTheContractLengthCeilingIsAccepted(t *testing.T) {
	cases := []struct {
		name          string
		recipientName string
		streetAddress string
	}{
		{"ascii members at the limit", strings.Repeat("a", 120), strings.Repeat("b", 255)},
		{"a multi-byte recipient name at the limit", strings.Repeat("Ữ", 120), "12 Nguyen Hue"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAddressFixture(t)

			rec := doJSON(fixture.router, http.MethodPost, "/me/addresses",
				`{"recipientName":`+mustJSONString(t, tc.recipientName)+`,`+
					`"recipientPhone":"0912345678","provinceCode":"79","wardCode":"26734",`+
					`"streetAddress":`+mustJSONString(t, tc.streetAddress)+`}`,
				liveToken)

			if rec.Code != http.StatusCreated {
				t.Fatalf("a member at the advertised limit must be accepted, got %d (%s)", rec.Code, rec.Body.String())
			}
			created := decodeAddress(t, rec)
			if created.RecipientName != tc.recipientName {
				t.Fatalf("the stored recipient name must survive unchanged, got %d characters",
					len([]rune(created.RecipientName)))
			}
			if created.StreetAddress != tc.streetAddress {
				t.Fatalf("the stored street address must survive unchanged, got %d characters",
					len([]rune(created.StreetAddress)))
			}
		})
	}
}

// mustJSONString encodes a raw string as a JSON string literal, so a body built
// from a repeated character cannot be malformed by hand.
func mustJSONString(t *testing.T, raw string) string {
	t.Helper()
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("encode %q as a JSON string: %v", raw, err)
	}
	return string(encoded)
}

// The ceiling is enforced on edit as well as on create, and a rejected edit leaves
// the stored address exactly as it was: the previous default is still the default
// and the text is unchanged.
func TestEditingAnAddressOverTheContractLengthCeilingIsRefusedAndChangesNothing(t *testing.T) {
	fixture := newAddressFixture(t)
	id := fixture.create(t, liveToken)

	cases := []struct {
		name  string
		field string
		body  string
	}{
		{
			name:  "the recipient name",
			field: fieldRecipientName,
			body:  `{"recipientName":"` + strings.Repeat("a", 121) + `"}`,
		},
		{
			name:  "the street address",
			field: fieldStreet,
			body:  `{"streetAddress":"` + strings.Repeat("b", 256) + `"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := fixture.list(t, liveToken, "")
			if len(before.Data) != 1 {
				t.Fatalf("expected the seeded address, got %+v", before.Data)
			}

			rec := doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+id, tc.body, liveToken)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != string(httpx.CodeValidation) {
				t.Fatalf("expected VALIDATION_ERROR, got %s", got)
			}
			if got := detailField(t, rec); got != tc.field {
				t.Fatalf("expected the detail to name %q, got %q", tc.field, got)
			}
			listed := fixture.list(t, liveToken, "")
			if !reflect.DeepEqual(listed.Data, before.Data) {
				t.Fatalf("a refused edit changed the stored address:\n before %+v\n after  %+v", before.Data, listed.Data)
			}
			if !listed.Data[0].IsDefault {
				t.Fatal("a refused edit must leave the previous default in place")
			}
		})
	}
}

// A rejected create is never audited, because nothing changed.
func TestARefusedAddressWriteIsNotAudited(t *testing.T) {
	fixture := newAddressFixture(t)
	body := `{"recipientName":"A","recipientPhone":"0912345678","provinceCode":"99","wardCode":"26734","streetAddress":"12 Nguyen Hue"}`

	rec := doJSON(fixture.router, http.MethodPost, "/me/addresses", body, liveToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	for _, action := range []string{
		constant.AuditAddressCreated, constant.AuditAddressUpdated,
		constant.AuditAddressDeleted, constant.AuditAddressDefaultSet,
	} {
		if got := fixture.audit.countOf(action); got != 0 {
			t.Fatalf("a refused save must leave no %s event, got %d", action, got)
		}
	}
}

// FR-019, SC-008: every successful address operation leaves a trace.
func TestEverySuccessfulAddressOperationIsAudited(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.create(t, liveToken)
	second := fixture.create(t, liveToken)

	if rec := doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+second,
		`{"streetAddress":"1 Le Loi"}`, liveToken); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := do(fixture.router, http.MethodPost, "/me/addresses/"+second+"/default", liveToken); rec.Code != http.StatusOK {
		t.Fatalf("mark default: %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := do(fixture.router, http.MethodDelete, "/me/addresses/"+second, liveToken); rec.Code != http.StatusNoContent {
		t.Fatalf("hide: %d (%s)", rec.Code, rec.Body.String())
	}

	for action, want := range map[string]int{
		constant.AuditAddressCreated:    2,
		constant.AuditAddressUpdated:    1,
		constant.AuditAddressDefaultSet: 2,
		constant.AuditAddressDeleted:    1,
	} {
		if got := fixture.audit.countOf(action); got != want {
			t.Errorf("%s: expected %d events, got %d", action, want, got)
		}
	}
}

// research D10 through the wire: a stored code that has left the dataset still
// reads back with its captured names and the review marker set.
func TestAStoredCodeThatLeftTheDatasetIsFlaggedInTheResponse(t *testing.T) {
	fixture := newAddressFixture(t)
	stamp := time.Now().UTC()
	retired := uuid.New()
	fixture.repo.mu.Lock()
	fixture.repo.rows[retired] = model.Address{
		ID: retired, UserID: fixture.customer,
		RecipientName: "Nguyen Van A", RecipientPhone: "0912345678",
		ProvinceCode: "79", ProvinceName: "Ho Chi Minh (cu)",
		WardCode: "11111", WardName: "Phuong Da Xa",
		StreetAddress: "12 Nguyen Hue",
		CreatedAt:     stamp, UpdatedAt: stamp,
	}
	fixture.repo.seq = append(fixture.repo.seq, retired)
	fixture.repo.mu.Unlock()

	listed := fixture.list(t, liveToken, "")

	if len(listed.Data) != 1 || listed.Data[0].ID != retired.String() {
		t.Fatalf("expected the retired address to stay listed, got %+v", listed.Data)
	}
	row := listed.Data[0]
	if !row.DivisionNeedsReview {
		t.Fatal("expected divisionNeedsReview to be true for a retired ward code")
	}
	if row.WardName != "Phuong Da Xa" || row.ProvinceName != "Ho Chi Minh (cu)" {
		t.Fatalf("the captured names must be returned, got %q / %q", row.ProvinceName, row.WardName)
	}
}

// research D10 through the wire, the other way round: codes that are still current
// answer an explicit false rather than an absent member.
//
// The body is decoded into a map on purpose. A typed decode cannot tell an absent
// member from a false one — both read as the zero value — so only a map proves the
// member is stated.
func TestTheCustomerListStatesDivisionNeedsReviewWhenTheCodesAreCurrent(t *testing.T) {
	fixture := newAddressFixture(t)
	fixture.create(t, liveToken)

	rec := do(fixture.router, http.MethodGet, "/me/addresses", liveToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode address list %q: %v", rec.Body.String(), err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("expected the one saved address, got %d", len(envelope.Data))
	}
	value, stated := envelope.Data[0]["divisionNeedsReview"]
	if !stated {
		t.Fatalf("a known answer must be stated, not omitted: %s", rec.Body.String())
	}
	if flag, isBool := value.(bool); !isBool || flag {
		t.Fatalf("expected an explicit false for codes still in the dataset, got %v", value)
	}
}

// SC-002, FR-005: no address route runs without a usable session, so nothing is
// read or written.
func TestAddressRoutesRefuseACallerWithoutAToken(t *testing.T) {
	fixture := newAddressFixture(t)
	id := fixture.create(t, liveToken)
	writesBefore := fixture.repo.writeCount()

	for _, rec := range []*httptest.ResponseRecorder{
		do(fixture.router, http.MethodGet, "/me/addresses", ""),
		doJSON(fixture.router, http.MethodPost, "/me/addresses", addressBody, ""),
		doJSON(fixture.router, http.MethodPatch, "/me/addresses/"+id, `{"streetAddress":"1 Le Loi"}`, ""),
		do(fixture.router, http.MethodDelete, "/me/addresses/"+id, ""),
		do(fixture.router, http.MethodPost, "/me/addresses/"+id+"/default", ""),
		do(fixture.router, http.MethodGet, "/me/addresses", expiredToken),
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
	if got := fixture.repo.writeCount(); got != writesBefore {
		t.Fatalf("a refused request wrote something: %d -> %d", writesBefore, got)
	}
	// Only the seeded create may be audited, and becoming the first default is a
	// default change the create records alongside itself.
	if len(fixture.audit.events) != 2 {
		t.Fatalf("expected only the seeded create to be audited, got %v", fixture.audit.events)
	}
}

// The cascading-select endpoints are unpaginated reference data (plan.md,
// Complexity Tracking): the whole set is smaller than any page a client would ask
// for, and the ward list stays scoped to one province (FR-007c).
func TestTheCascadingSelectEndpointsAreUnpaginatedAndScoped(t *testing.T) {
	router := NewDivisionsHandler(cascadeDivisions{}, testLogger).Router(
		testHooks(newStub(access.RoleCustomer), &recordingAuditor{}))

	var provinces struct {
		Data []map[string]string `json:"data"`
		Meta map[string]any      `json:"meta"`
	}
	rec := do(router, http.MethodGet, "/provinces", "token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provinces); err != nil {
		t.Fatalf("decode provinces: %v", err)
	}
	if len(provinces.Data) != 2 {
		t.Fatalf("expected the whole province list, got %d", len(provinces.Data))
	}
	for _, key := range []string{"page", "pageSize", "total"} {
		if _, ok := provinces.Meta[key]; ok {
			t.Fatalf("an unpaginated listing must not report %q, got %v", key, provinces.Meta)
		}
	}

	var wards struct {
		Data []map[string]string `json:"data"`
	}
	rec = do(router, http.MethodGet, "/provinces/79/wards", "token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wards); err != nil {
		t.Fatalf("decode wards: %v", err)
	}
	if len(wards.Data) == 0 {
		t.Fatal("expected the wards of the chosen province")
	}
	for _, ward := range wards.Data {
		if ward["provinceCode"] != "79" {
			t.Fatalf("a ward list must stay scoped to one province, got %+v", ward)
		}
	}
}
