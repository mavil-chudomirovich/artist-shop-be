package implement

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// memoryProfiles is an in-memory ProfileRepository. It copies every value in and
// out so a use case can never reach a stored row through the pointer it was
// handed, which is exactly the aliasing a real database would prevent.
type memoryProfiles struct {
	rows  map[uuid.UUID]model.Profile
	saves int
}

func newMemoryProfiles(rows ...model.Profile) *memoryProfiles {
	store := &memoryProfiles{rows: make(map[uuid.UUID]model.Profile, len(rows))}
	for _, row := range rows {
		store.rows[row.ID] = clone(row)
	}
	return store
}

func clone(profile model.Profile) model.Profile {
	out := profile
	if profile.Phone != nil {
		phone := *profile.Phone
		out.Phone = &phone
	}
	if profile.Avatar != nil {
		avatar := *profile.Avatar
		out.Avatar = &avatar
	}
	return out
}

func (m *memoryProfiles) ByID(_ context.Context, id uuid.UUID) (*model.Profile, error) {
	row, ok := m.rows[id]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	stored := clone(row)
	return &stored, nil
}

func (m *memoryProfiles) Save(_ context.Context, profile *model.Profile) error {
	if _, ok := m.rows[profile.ID]; !ok {
		return domainerr.ErrUserNotFound
	}
	m.saves++
	m.rows[profile.ID] = clone(*profile)
	return nil
}

var _ repository.ProfileRepository = (*memoryProfiles)(nil)

// recordedEvent keeps enough of an audit event to assert who did what to whom.
type recordedEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

type recordingAudit struct {
	events []recordedEvent
}

func (a *recordingAudit) Record(_ context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any) {
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

func (a *recordingAudit) countOf(action string) int {
	total := 0
	for _, e := range a.events {
		if e.action == action {
			total++
		}
	}
	return total
}

// eventsByAction returns the recorded events of one action, so a test can assert
// on the actor, the target and the metadata of the event that matters rather
// than only on how many there were.
func (a *recordingAudit) eventsByAction(action string) []recordedEvent {
	var out []recordedEvent
	for _, e := range a.events {
		if e.action == action {
			out = append(out, e)
		}
	}
	return out
}

var _ appinterface.Auditor = (*recordingAudit)(nil)

type harness struct {
	svc    *Service
	repo   *memoryProfiles
	audit  *recordingAudit
	owner  uuid.UUID
	others uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	owner := uuid.New()
	other := uuid.New()
	repo := newMemoryProfiles(
		model.Profile{ID: owner, Email: "owner@example.com", Role: access.RoleCustomer},
		model.Profile{
			ID:          other,
			Email:       "other@example.com",
			Role:        access.RoleCustomer,
			DisplayName: "Other Customer",
			Phone:       phone("0987654321"),
		},
	)
	audit := &recordingAudit{}
	// No Divisions port is needed for a profile: the profile carries no address,
	// and the dataset is invisible to this layer's profile rules.
	svc := New(Service{
		Profiles: repo,
		Audit:    audit,
		Mapper:   mapper.New(nil),
	})
	return &harness{svc: svc, repo: repo, audit: audit, owner: owner, others: other}
}

func phone(raw string) *string {
	value := raw
	return &value
}

func (h *harness) stored(t *testing.T, id uuid.UUID) model.Profile {
	t.Helper()
	row, err := h.repo.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read the stored profile: %v", err)
	}
	return *row
}

// sameProfile compares two rows by value. Comparing the structs directly would
// compare the phone pointer addresses, which the in-memory store deliberately
// allocates afresh on every read.
func sameProfile(a, b model.Profile) bool {
	if a.ID != b.ID || a.Email != b.Email || a.Role != b.Role {
		return false
	}
	if a.DisplayName != b.DisplayName {
		return false
	}
	if (a.Phone == nil) != (b.Phone == nil) {
		return false
	}
	if a.Phone != nil && *a.Phone != *b.Phone {
		return false
	}
	if (a.Avatar == nil) != (b.Avatar == nil) {
		return false
	}
	if a.Avatar != nil && *a.Avatar != *b.Avatar {
		return false
	}
	return true
}

func TestGetProfileReturnsTheAccountProfile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	out, err := h.svc.GetProfile(ctx, h.others)

	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if out.ID != h.others || out.Email != "other@example.com" {
		t.Fatalf("expected the account %s, got %+v", h.others, out)
	}
	if out.DisplayName != "Other Customer" {
		t.Fatalf("expected the stored display name, got %q", out.DisplayName)
	}
	if out.Phone == nil || *out.Phone != "0987654321" {
		t.Fatalf("expected the stored phone, got %v", out.Phone)
	}
	if out.Avatar != nil {
		t.Fatalf("expected no avatar, got %+v", out.Avatar)
	}
}

// A profile read must not need the media service: the stored reference is
// returned as saved, so an outage can never fail the read (FR-021).
func TestGetProfileAnswersWithoutAMediaStore(t *testing.T) {
	h := newHarness(t)

	if h.svc.Media != nil {
		t.Fatal("the harness must wire no media store at all")
	}
	out, err := h.svc.GetProfile(context.Background(), h.others)
	if err != nil {
		t.Fatalf("GetProfile without media: %v", err)
	}
	if out.Avatar != nil {
		t.Fatalf("expected a null avatar, got %+v", out.Avatar)
	}
}

// The account that is written is the one the session named: a second customer is
// never touched (FR-006, SC-003).
func TestUpdateProfileOnlyTouchesTheSessionAccount(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	before := h.stored(t, h.others)

	name := "Owner"
	out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{UserID: h.owner, DisplayName: &name})

	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if out.ID != h.owner {
		t.Fatalf("expected the owner's profile, got %s", out.ID)
	}
	if h.stored(t, h.owner).DisplayName != "Owner" {
		t.Fatal("the owner row was not updated")
	}
	if after := h.stored(t, h.others); !sameProfile(after, before) {
		t.Fatalf("another customer's row changed: %+v", after)
	}
}

func TestPartialUpdateLeavesTheOmittedFieldUntouched(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.others,
		DisplayName: stringPtr("Edited Name"),
		Phone:       phone("0912345678"),
	}); err != nil {
		t.Fatalf("seed the profile: %v", err)
	}

	// Only the display name is sent.
	name := "  Second Edit  "
	out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{UserID: h.others, DisplayName: &name})

	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if out.DisplayName != "Second Edit" {
		t.Fatalf("expected the trimmed display name, got %q", out.DisplayName)
	}
	if out.Phone == nil || *out.Phone != "0912345678" {
		t.Fatalf("an omitted phone must keep its value, got %v", out.Phone)
	}
	if h.stored(t, h.others).DisplayName != "Second Edit" {
		t.Fatal("the trimmed name was not stored")
	}
}

// An intentionally emptied field is stored as empty rather than keeping the old
// value, and the profile stays valid (FR-004).
func TestUpdateProfileClearsAFieldSentAsAnEmptyString(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.others,
		DisplayName: stringPtr("Named"),
		Phone:       phone("0912345678"),
	}); err != nil {
		t.Fatalf("seed the profile: %v", err)
	}

	empty := "  "
	out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.others,
		DisplayName: &empty,
		Phone:       &empty,
	})

	if err != nil {
		t.Fatalf("clearing must not be an error: %v", err)
	}
	if out.DisplayName != "" {
		t.Fatalf("expected the display name to be cleared, got %q", out.DisplayName)
	}
	if out.Phone != nil {
		t.Fatalf("expected the phone to be cleared, got %q", *out.Phone)
	}
	stored := h.stored(t, h.others)
	if stored.DisplayName != "" || stored.Phone != nil {
		t.Fatalf("the cleared values were not stored: %+v", stored)
	}
}

func TestUpdateProfileStoresThePhoneNormalised(t *testing.T) {
	h := newHarness(t)

	out, err := h.svc.UpdateProfile(context.Background(), appdto.UpdateProfileInput{
		UserID: h.owner,
		Phone:  stringPtr("+84 912 345 678"),
	})

	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if out.Phone == nil || *out.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone in the response, got %v", out.Phone)
	}
	if stored := h.stored(t, h.owner); stored.Phone == nil || *stored.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone to be stored, got %v", stored.Phone)
	}
}

// A rejected phone must leave the database exactly as it was: the whole update
// is refused, not applied halfway (FR-003, US1 acceptance scenario 3).
func TestUpdateProfileWithAnInvalidPhonePersistsNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.owner,
		DisplayName: stringPtr("Named"),
		Phone:       phone("0912345678"),
	}); err != nil {
		t.Fatalf("seed the profile: %v", err)
	}
	before := h.stored(t, h.owner)
	savesBefore := h.repo.saves

	out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.owner,
		DisplayName: stringPtr("Renamed"),
		Phone:       stringPtr("12345"),
	})

	if !errors.Is(err, domainerr.ErrInvalidPhone) {
		t.Fatalf("expected ErrInvalidPhone, got %v", err)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("expected no profile in the result, got %+v", out)
	}
	if after := h.stored(t, h.owner); !sameProfile(after, before) {
		t.Fatalf("a rejected phone changed the row: %+v", after)
	}
	if h.repo.saves != savesBefore {
		t.Fatalf("a rejected phone must not be persisted, saves went from %d to %d", savesBefore, h.repo.saves)
	}
	if h.audit.countOf(constant.AuditProfileUpdated) != 1 {
		t.Fatal("only the successful seed update may be audited")
	}
}

// A display name longer than the maxLength the contract declares for it must be
// refused the whole way through, exactly as an invalid phone is: nothing is
// persisted, nothing is audited, and the stored row keeps the name it had. This is
// the check that keeps the advertised maxLength from being a limit the server
// quietly ignores (FR-020).
func TestUpdateProfileWithAnOverLongDisplayNamePersistsNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.owner,
		DisplayName: stringPtr("Named"),
		Phone:       phone("0912345678"),
	}); err != nil {
		t.Fatalf("seed the profile: %v", err)
	}
	before := h.stored(t, h.owner)
	savesBefore := h.repo.saves

	// The phone is valid, so only the name can be the reason the update is refused.
	out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
		UserID:      h.owner,
		DisplayName: stringPtr(strings.Repeat("a", 121)),
		Phone:       phone("0987654321"),
	})

	if !errors.Is(err, domainerr.ErrAddressInvalid) {
		t.Fatalf("expected the module's field-error sentinel, got %v", err)
	}
	var carrier *domainerr.AddressFieldError
	if !errors.As(err, &carrier) {
		t.Fatalf("expected a named field error, got %v", err)
	}
	if carrier.Field != model.FieldDisplayName {
		t.Fatalf("expected the rejection to name %q, got %q", model.FieldDisplayName, carrier.Field)
	}
	if out.ID != uuid.Nil {
		t.Fatalf("expected no profile in the result, got %+v", out)
	}
	if after := h.stored(t, h.owner); !sameProfile(after, before) {
		t.Fatalf("a rejected name changed the row: %+v", after)
	}
	if h.repo.saves != savesBefore {
		t.Fatalf("a rejected name must not be persisted, saves went from %d to %d", savesBefore, h.repo.saves)
	}
	if h.audit.countOf(constant.AuditProfileUpdated) != 1 {
		t.Fatal("only the successful seed update may be audited")
	}
}

// The ceiling is inclusive and the value that gets stored is the trimmed one, so a
// name of exactly the advertised length is accepted through the use case.
func TestUpdateProfileStoresADisplayNameAtTheContractCeiling(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	for _, name := range []string{strings.Repeat("a", 120), strings.Repeat("Ữ", 120)} {
		out, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{
			UserID:      h.owner,
			DisplayName: stringPtr("  " + name + "  "),
		})

		if err != nil {
			t.Fatalf("a name of %d characters must be accepted, got %v", len([]rune(name)), err)
		}
		if out.DisplayName != name {
			t.Fatalf("expected the trimmed name of %d characters, got %d",
				len([]rune(name)), len([]rune(out.DisplayName)))
		}
		if stored := h.stored(t, h.owner); stored.DisplayName != name {
			t.Fatalf("expected the name to be stored, got %d characters", len([]rune(stored.DisplayName)))
		}
	}
}

// Every successful change leaves a trace naming who did it (FR-019, SC-008).
func TestEverySuccessfulUpdateIsAudited(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{UserID: h.owner, DisplayName: stringPtr("First")}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := h.svc.UpdateProfile(ctx, appdto.UpdateProfileInput{UserID: h.owner, Phone: stringPtr("0912345678")}); err != nil {
		t.Fatalf("second update: %v", err)
	}

	if got := h.audit.countOf(constant.AuditProfileUpdated); got != 2 {
		t.Fatalf("expected two audit events, got %d", got)
	}
	for _, e := range h.audit.events {
		if e.actorID == nil || *e.actorID != h.owner {
			t.Fatalf("expected the account %s as the actor, got %v", h.owner, e.actorID)
		}
		if e.actorRole != string(access.RoleCustomer) {
			t.Fatalf("expected the actor role, got %q", e.actorRole)
		}
		if e.targetID != h.owner.String() {
			t.Fatalf("expected the account as the target, got %q", e.targetID)
		}
		if e.outcome != "SUCCESS" {
			t.Fatalf("expected a SUCCESS outcome, got %q", e.outcome)
		}
		if len(e.metadata) == 0 {
			t.Fatal("expected the event to say what changed")
		}
	}
	// The record names the fields that changed, never their values: contact data
	// must not be copied into the audit trail.
	for _, e := range h.audit.events {
		for key := range e.metadata {
			switch key {
			case "changedFields":
			default:
				t.Fatalf("unexpected audit metadata key %q", key)
			}
		}
	}
}

// A read is not a change, so it records nothing.
func TestGetProfileIsNotAudited(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.GetProfile(context.Background(), h.owner); err != nil {
		t.Fatalf("GetProfile: %v", err)
	}

	if len(h.audit.events) != 0 {
		t.Fatalf("expected no audit event for a read, got %+v", h.audit.events)
	}
}

func TestGetProfileReportsAnAccountTheStoreDoesNotHold(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.GetProfile(context.Background(), uuid.New()); !errors.Is(err, domainerr.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func stringPtr(raw string) *string {
	value := raw
	return &value
}
