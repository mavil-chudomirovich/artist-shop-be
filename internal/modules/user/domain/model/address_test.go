package model

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// This file exercises the shipping-address entity and its four transitions:
// create, edit, mark default and hide. The entity validates structure only;
// whether a province or ward code still exists in the official dataset is
// answered in the application layer (Constitution I, research D1).

// fixedNow is the instant every transition in this file stamps, so the assertions
// never depend on the wall clock.
var fixedNow = time.Date(2026, time.October, 6, 9, 0, 0, 0, time.UTC)

func validDraft() AddressDraft {
	return AddressDraft{
		RecipientName:  "Nguyen Van A",
		RecipientPhone: "0912 345 678",
		ProvinceCode:   "79",
		ProvinceName:   "Ho Chi Minh",
		WardCode:       "26734",
		WardName:       "Phuong Ben Nghe",
		StreetAddress:  "12 Nguyen Hue",
	}
}

// mustAddress builds a visible address of the given account, failing the test if
// the draft is not acceptable.
func mustAddress(t *testing.T, userID uuid.UUID) *Address {
	t.Helper()
	address, err := NewAddress(userID, validDraft(), fixedNow)
	if err != nil {
		t.Fatalf("build a valid address: %v", err)
	}
	return address
}

func editPtr(raw string) *string {
	value := raw
	return &value
}

// repeat builds a free-text member of exactly n characters, so a boundary case can
// be stated as a number rather than counted by hand.
func repeat(char string, n int) string {
	return strings.Repeat(char, n)
}

// A new address is trimmed, carries the normalised recipient phone, starts
// visible and starts without the default flag: becoming the account's first
// default is a separate, explicit transition (FR-009).
func TestNewAddressTrimsNormalisesAndStartsWithoutTheDefaultFlag(t *testing.T) {
	owner := uuid.New()
	draft := validDraft()
	draft.RecipientName = "  Nguyen Van A  "
	draft.RecipientPhone = "+84 912 345 678"
	draft.StreetAddress = "  12 Nguyen Hue  "

	address, err := NewAddress(owner, draft, fixedNow)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}

	if address.ID == uuid.Nil {
		t.Fatal("a new address must be given an identifier")
	}
	if address.UserID != owner {
		t.Fatalf("the address must belong to the account %s, got %s", owner, address.UserID)
	}
	if address.RecipientName != "Nguyen Van A" {
		t.Fatalf("expected the trimmed recipient name, got %q", address.RecipientName)
	}
	if address.RecipientPhone != "0912345678" {
		t.Fatalf("expected the normalised recipient phone, got %q", address.RecipientPhone)
	}
	if address.StreetAddress != "12 Nguyen Hue" {
		t.Fatalf("expected the trimmed street address, got %q", address.StreetAddress)
	}
	if address.ProvinceCode != "79" || address.ProvinceName != "Ho Chi Minh" {
		t.Fatalf("the captured province must survive, got %q / %q", address.ProvinceCode, address.ProvinceName)
	}
	if address.WardCode != "26734" || address.WardName != "Phuong Ben Nghe" {
		t.Fatalf("the captured ward must survive, got %q / %q", address.WardCode, address.WardName)
	}
	if address.IsDefault {
		t.Fatal("a newly created address must not carry the default flag")
	}
	if address.DeletedAt != nil || address.IsHidden() {
		t.Fatal("a newly created address must be visible")
	}
	if !address.CreatedAt.Equal(fixedNow) || !address.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("both timestamps must be stamped, got %v / %v", address.CreatedAt, address.UpdatedAt)
	}
}

// Every member the row declares NOT NULL and non-empty is refused with a typed
// error that names the member, so the client can highlight the exact input
// (FR-020). The two free-text members are also refused when they exceed the
// maxLength the contract declares for them, because the contract may not
// advertise a limit the server ignores.
func TestNewAddressRefusesStructurallyInvalidInput(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*AddressDraft)
		field string
	}{
		{"blank recipient name", func(d *AddressDraft) { d.RecipientName = "   " }, FieldRecipientName},
		{"blank province code", func(d *AddressDraft) { d.ProvinceCode = " " }, FieldProvinceCode},
		{"blank ward code", func(d *AddressDraft) { d.WardCode = "" }, FieldWardCode},
		{"blank street address", func(d *AddressDraft) { d.StreetAddress = "\t\n" }, FieldStreetAddress},
		{
			"a recipient name one character over the contract limit",
			func(d *AddressDraft) { d.RecipientName = repeat("a", maxRecipientNameRunes+1) },
			FieldRecipientName,
		},
		{
			"a province name one character over the contract limit",
			func(d *AddressDraft) { d.ProvinceName = repeat("a", maxProvinceNameRunes+1) },
			FieldProvinceName,
		},
		{
			"a ward name one character over the contract limit",
			func(d *AddressDraft) { d.WardName = repeat("a", maxWardNameRunes+1) },
			FieldWardName,
		},
		{
			"a street address one character over the contract limit",
			func(d *AddressDraft) { d.StreetAddress = repeat("a", maxStreetAddressRunes+1) },
			FieldStreetAddress,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := validDraft()
			tc.alter(&draft)

			address, err := NewAddress(uuid.New(), draft, fixedNow)

			if address != nil {
				t.Fatalf("a refused draft must yield no address, got %+v", address)
			}
			if !errors.Is(err, domainerr.ErrAddressInvalid) {
				t.Fatalf("expected ErrAddressInvalid, got %v", err)
			}
			var invalid *domainerr.AddressFieldError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected an *AddressFieldError, got %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("expected the error to name %q, got %q", tc.field, invalid.Field)
			}
		})
	}
}

// The contract's ceilings are inclusive: a member of exactly the advertised
// number of characters is stored unchanged, and the rejection only starts one
// character beyond it. This is what makes the advertised maxLength truthful
// rather than off by one in the client's favour or against it.
func TestAMemberAtTheContractLengthCeilingIsAccepted(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*AddressDraft)
		check func(*Address)
	}{
		{
			name:  "recipient name of exactly the limit",
			alter: func(d *AddressDraft) { d.RecipientName = repeat("a", maxRecipientNameRunes) },
			check: func(a *Address) {
				if n := utf8.RuneCountInString(a.RecipientName); n != maxRecipientNameRunes {
					t.Errorf("expected the stored name to keep all %d characters, got %d", maxRecipientNameRunes, n)
				}
			},
		},
		{
			name:  "street address of exactly the limit",
			alter: func(d *AddressDraft) { d.StreetAddress = repeat("a", maxStreetAddressRunes) },
			check: func(a *Address) {
				if n := utf8.RuneCountInString(a.StreetAddress); n != maxStreetAddressRunes {
					t.Errorf("expected the stored address to keep all %d characters, got %d", maxStreetAddressRunes, n)
				}
			},
		},
		{
			name:  "province name of exactly the limit",
			alter: func(d *AddressDraft) { d.ProvinceName = repeat("a", maxProvinceNameRunes) },
			check: func(a *Address) {
				if n := utf8.RuneCountInString(a.ProvinceName); n != maxProvinceNameRunes {
					t.Errorf("expected the stored province name to keep all %d characters, got %d",
						maxProvinceNameRunes, n)
				}
			},
		},
		{
			name:  "ward name of exactly the limit",
			alter: func(d *AddressDraft) { d.WardName = repeat("a", maxWardNameRunes) },
			check: func(a *Address) {
				if n := utf8.RuneCountInString(a.WardName); n != maxWardNameRunes {
					t.Errorf("expected the stored ward name to keep all %d characters, got %d",
						maxWardNameRunes, n)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := validDraft()
			tc.alter(&draft)

			address, err := NewAddress(uuid.New(), draft, fixedNow)

			if err != nil {
				t.Fatalf("a member at the advertised limit must be accepted: %v", err)
			}
			tc.check(address)
		})
	}
}

// The ceilings count characters, not bytes: the API serves Vietnamese text, so a
// name of 120 multi-byte characters has to pass. Counting bytes would refuse it at
// roughly a third of the advertised length, and one character more still has to be
// refused.
func TestTheContractLengthCeilingsAreCountedInCharactersNotBytes(t *testing.T) {
	// "Ữ" is three bytes in UTF-8, so a name at the limit is 360 bytes long: a
	// byte count would have rejected it long before 120 characters.
	const vietnamese = "Ữ"
	atLimit := repeat(vietnamese, maxRecipientNameRunes)
	if len(atLimit) <= maxRecipientNameRunes {
		t.Fatalf("expected a multi-byte fixture, got %d bytes for %d characters", len(atLimit), maxRecipientNameRunes)
	}

	draft := validDraft()
	draft.RecipientName = atLimit
	address, err := NewAddress(uuid.New(), draft, fixedNow)
	if err != nil {
		t.Fatalf("%d Vietnamese characters (%d bytes) must fit the advertised limit: %v",
			maxRecipientNameRunes, len(atLimit), err)
	}
	if address.RecipientName != atLimit {
		t.Fatal("a name at the limit must be stored unchanged")
	}

	draft = validDraft()
	draft.RecipientName = repeat(vietnamese, maxRecipientNameRunes+1)
	address, err = NewAddress(uuid.New(), draft, fixedNow)
	if address != nil {
		t.Fatalf("a refused draft must yield no address, got %+v", address)
	}
	var invalid *domainerr.AddressFieldError
	if !errors.As(err, &invalid) || invalid.Field != FieldRecipientName {
		t.Fatalf("expected the rejection to name %q, got %v", FieldRecipientName, err)
	}
}

// The ceiling applies to the value that gets stored, which is the trimmed one, so
// surrounding whitespace cannot make an otherwise acceptable member fail: a client
// is never refused for text the entity was going to discard anyway.
func TestTheContractLengthCeilingIsAppliedToTheTrimmedValue(t *testing.T) {
	draft := validDraft()
	draft.RecipientName = "  " + repeat("a", maxRecipientNameRunes) + "\n"

	address, err := NewAddress(uuid.New(), draft, fixedNow)

	if err != nil {
		t.Fatalf("whitespace around a member at the limit must not cause a rejection: %v", err)
	}
	if n := utf8.RuneCountInString(address.RecipientName); n != maxRecipientNameRunes {
		t.Fatalf("expected the trimmed name of %d characters, got %d", maxRecipientNameRunes, n)
	}
}

// The ceiling is enforced by the entity rather than by the handler, so an edit
// cannot be a way to leave or to introduce an over-long value. This checks both
// directions: a name that is too long is refused, and a member at the limit is
// still accepted through the same transition.
func TestTheContractLengthCeilingsAreEnforcedOnEdit(t *testing.T) {
	address := mustAddress(t, uuid.New())
	later := fixedNow.Add(time.Hour)

	if err := address.Apply(AddressEdit{
		RecipientName: editPtr(repeat("a", maxRecipientNameRunes)),
		StreetAddress: editPtr(repeat("b", maxStreetAddressRunes)),
	}, later); err != nil {
		t.Fatalf("members at the advertised limit must be accepted on edit: %v", err)
	}
	if n := utf8.RuneCountInString(address.RecipientName); n != maxRecipientNameRunes {
		t.Errorf("expected the edited name of %d characters, got %d", maxRecipientNameRunes, n)
	}
	if n := utf8.RuneCountInString(address.StreetAddress); n != maxStreetAddressRunes {
		t.Errorf("expected the edited street address of %d characters, got %d", maxStreetAddressRunes, n)
	}

	// The stored members are now exactly at the ceiling, so an unrelated edit must
	// still pass: re-validating the untouched members cannot reject a row the entity
	// itself produced.
	if err := address.Apply(AddressEdit{ProvinceName: editPtr("Ho Chi Minh")}, later); err != nil {
		t.Fatalf("an unrelated edit of a row at the ceiling must pass: %v", err)
	}
}

// The rejection explains the ceiling in characters and names only the offending
// member, so the client can shorten that field without guessing at the others.
func TestTheLengthRejectionExplainsTheCeilingInCharacters(t *testing.T) {
	draft := validDraft()
	draft.StreetAddress = repeat("a", maxStreetAddressRunes+1)

	_, err := NewAddress(uuid.New(), draft, fixedNow)

	var invalid *domainerr.AddressFieldError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected the typed carrier, got %v", err)
	}
	if invalid.Field != FieldStreetAddress {
		t.Fatalf("expected the error to name %q, got %q", FieldStreetAddress, invalid.Field)
	}
	if want := limitIssue(maxStreetAddressRunes); invalid.Issue != want {
		t.Fatalf("expected the issue %q, got %q", want, invalid.Issue)
	}
	if strings.Contains(invalid.Issue, "byte") {
		t.Fatalf("the issue must not talk about bytes: %q", invalid.Issue)
	}
}

// A member that is blank is still reported as missing rather than as too short or
// too long, so the two structural rules keep reporting the problem the client has.
func TestABlankMemberIsReportedAsMissingRatherThanOutOfRange(t *testing.T) {
	draft := validDraft()
	draft.RecipientName = ""

	_, err := NewAddress(uuid.New(), draft, fixedNow)

	var invalid *domainerr.AddressFieldError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected the typed carrier, got %v", err)
	}
	if invalid.Field != FieldRecipientName || invalid.Issue != requiredIssue {
		t.Fatalf("expected %q to be reported as %q, got %q %q",
			FieldRecipientName, requiredIssue, invalid.Field, invalid.Issue)
	}
}

// The recipient phone is validated by the same value object the profile uses
// (research D9), so a customer cannot store an unreachable number here either.
func TestNewAddressRefusesAnInvalidRecipientPhone(t *testing.T) {
	draft := validDraft()
	draft.RecipientPhone = "12345"

	address, err := NewAddress(uuid.New(), draft, fixedNow)

	if address != nil {
		t.Fatalf("a refused draft must yield no address, got %+v", address)
	}
	if !errors.Is(err, domainerr.ErrInvalidPhone) {
		t.Fatalf("expected ErrInvalidPhone, got %v", err)
	}
}

// An edit changes only the members it names and preserves the default flag: the
// dedicated route owns the flag, so a plain edit can never move it (FR-011).
func TestApplyEditsOnlyTheNamedMembersAndPreservesTheDefaultFlag(t *testing.T) {
	address := mustAddress(t, uuid.New())
	if err := address.BecomeDefault(); err != nil {
		t.Fatalf("BecomeDefault: %v", err)
	}
	later := fixedNow.Add(2 * time.Hour)

	err := address.Apply(AddressEdit{
		RecipientName: editPtr("  Nguyen Van B  "),
		StreetAddress: editPtr("  1 Le Loi  "),
	}, later)

	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if address.RecipientName != "Nguyen Van B" {
		t.Fatalf("expected the trimmed recipient name, got %q", address.RecipientName)
	}
	if address.StreetAddress != "1 Le Loi" {
		t.Fatalf("expected the trimmed street address, got %q", address.StreetAddress)
	}
	// Untouched members keep their stored values.
	if address.RecipientPhone != "0912345678" {
		t.Fatalf("an omitted phone must keep its value, got %q", address.RecipientPhone)
	}
	if address.ProvinceCode != "79" || address.WardCode != "26734" {
		t.Fatalf("omitted divisions must keep their values, got %q / %q", address.ProvinceCode, address.WardCode)
	}
	if !address.IsDefault {
		t.Fatal("an edit must preserve the default flag")
	}
	if !address.UpdatedAt.Equal(later) {
		t.Fatalf("expected the edit to stamp the row, got %v", address.UpdatedAt)
	}
	if !address.CreatedAt.Equal(fixedNow) {
		t.Fatalf("an edit must not move the creation timestamp, got %v", address.CreatedAt)
	}
}

func TestApplyNormalisesTheEditedRecipientPhone(t *testing.T) {
	address := mustAddress(t, uuid.New())

	if err := address.Apply(AddressEdit{RecipientPhone: editPtr("0988 765 432")}, fixedNow); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if address.RecipientPhone != "0988765432" {
		t.Fatalf("expected the normalised phone, got %q", address.RecipientPhone)
	}
}

// A rejected edit must leave the entity exactly as it was — including the
// account's default flag, which is the edge case the specification calls out:
// "a customer's default address is edited to become invalid, the address is
// rejected and the previous default stays in place". An edit that breaks the
// contract's length ceilings is rejected the same way, and the length is checked
// on edit exactly as on create because Apply validates the whole staged draft.
func TestARejectedEditLeavesThePreviousDefaultAndValuesInPlace(t *testing.T) {
	cases := []struct {
		name  string
		edit  AddressEdit
		field string
	}{
		{"a cleared recipient name", AddressEdit{RecipientName: editPtr("  ")}, FieldRecipientName},
		{"a cleared street address", AddressEdit{StreetAddress: editPtr("")}, FieldStreetAddress},
		{"a cleared province code", AddressEdit{ProvinceCode: editPtr("")}, FieldProvinceCode},
		{"a cleared ward code", AddressEdit{WardCode: editPtr("  ")}, FieldWardCode},
		{"an invalid recipient phone", AddressEdit{RecipientPhone: editPtr("12345")}, ""},
		{
			"a recipient name one character over the contract limit",
			AddressEdit{RecipientName: editPtr(repeat("a", maxRecipientNameRunes+1))},
			FieldRecipientName,
		},
		{
			"a street address one character over the contract limit",
			AddressEdit{StreetAddress: editPtr(repeat("a", maxStreetAddressRunes+1))},
			FieldStreetAddress,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address := mustAddress(t, uuid.New())
			if err := address.BecomeDefault(); err != nil {
				t.Fatalf("BecomeDefault: %v", err)
			}
			before := *address

			err := address.Apply(tc.edit, fixedNow.Add(time.Hour))

			if tc.field == "" {
				if !errors.Is(err, domainerr.ErrInvalidPhone) {
					t.Fatalf("expected ErrInvalidPhone, got %v", err)
				}
			} else {
				if !errors.Is(err, domainerr.ErrAddressInvalid) {
					t.Fatalf("expected ErrAddressInvalid, got %v", err)
				}
				var invalid *domainerr.AddressFieldError
				if !errors.As(err, &invalid) || invalid.Field != tc.field {
					t.Fatalf("expected the error to name %q, got %v", tc.field, err)
				}
			}
			if !address.IsDefault {
				t.Fatal("a rejected edit must leave the previous default in place")
			}
			if *address != before {
				t.Fatalf("a rejected edit changed the entity:\n before %+v\n after  %+v", before, *address)
			}
		})
	}
}

// Marking an address default is the only way the flag is ever set, which is what
// keeps the transition auditable and refuses the one state the invariant forbids.
func TestBecomeDefaultSetsTheFlagAndRefusesAHiddenAddress(t *testing.T) {
	address := mustAddress(t, uuid.New())

	if err := address.BecomeDefault(); err != nil {
		t.Fatalf("BecomeDefault: %v", err)
	}
	if !address.IsDefault {
		t.Fatal("expected the address to carry the default flag")
	}

	// The transition is idempotent: marking the current default again is not an
	// error and changes nothing.
	if err := address.BecomeDefault(); err != nil {
		t.Fatalf("marking the current default again: %v", err)
	}

	if err := address.Hide(fixedNow.Add(time.Hour)); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	err := address.BecomeDefault()
	if !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("a hidden address must not become the default, got %v", err)
	}
}

// Hiding stamps the row and drops the default flag in the same transition, so an
// account never keeps a default that points at a hidden address (SC-004).
func TestHideStampsTheRowAndDropsTheDefaultFlag(t *testing.T) {
	address := mustAddress(t, uuid.New())
	if err := address.BecomeDefault(); err != nil {
		t.Fatalf("BecomeDefault: %v", err)
	}
	hiddenAt := fixedNow.Add(3 * time.Hour)

	if err := address.Hide(hiddenAt); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	if address.DeletedAt == nil || !address.DeletedAt.Equal(hiddenAt) {
		t.Fatalf("expected the row to be stamped at %v, got %v", hiddenAt, address.DeletedAt)
	}
	if !address.IsHidden() {
		t.Fatal("expected the address to report itself hidden")
	}
	if address.IsDefault {
		t.Fatal("a hidden address must not keep the default flag")
	}
	if !address.CreatedAt.Equal(fixedNow) {
		t.Fatalf("hiding must not move the creation timestamp, got %v", address.CreatedAt)
	}
}

// Hiding an address twice is refused rather than silently re-stamped, so the
// stored hide time stays the first one.
func TestHideRefusesAnAlreadyHiddenAddress(t *testing.T) {
	address := mustAddress(t, uuid.New())
	first := fixedNow.Add(time.Hour)
	if err := address.Hide(first); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	err := address.Hide(first.Add(time.Hour))

	if !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound, got %v", err)
	}
	if address.DeletedAt == nil || !address.DeletedAt.Equal(first) {
		t.Fatalf("the first hide time must survive, got %v", address.DeletedAt)
	}
}

// An address that is already hidden cannot be edited either: it has left the
// account's visible set, so a later save must not resurrect it.
func TestApplyRefusesAHiddenAddress(t *testing.T) {
	address := mustAddress(t, uuid.New())
	if err := address.Hide(fixedNow.Add(time.Hour)); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	before := *address

	err := address.Apply(AddressEdit{RecipientName: editPtr("Nguyen Van B")}, fixedNow.Add(2*time.Hour))

	if !errors.Is(err, domainerr.ErrAddressNotFound) {
		t.Fatalf("expected ErrAddressNotFound, got %v", err)
	}
	if *address != before {
		t.Fatalf("a refused edit changed the entity:\n before %+v\n after  %+v", before, *address)
	}
}

// The division codes are stored as sent and trimmed; whether they exist in the
// official dataset is a use-case concern, so the entity must not reject a code it
// has never heard of (research D1, research D10).
func TestNewAddressStoresDivisionsWithoutConsultingTheDataset(t *testing.T) {
	draft := validDraft()
	draft.ProvinceCode = " 99 "
	draft.WardCode = " 99999 "

	address, err := NewAddress(uuid.New(), draft, fixedNow)

	if err != nil {
		t.Fatalf("a code the entity cannot check must still be storable: %v", err)
	}
	if address.ProvinceCode != "99" || address.WardCode != "99999" {
		t.Fatalf("expected the trimmed codes, got %q / %q", address.ProvinceCode, address.WardCode)
	}
}

// A blank captured name is stored as an empty string rather than as the
// surrounding whitespace: the dataset lookup decides whether the name is known,
// the entity only guarantees it is clean.
func TestNewAddressTrimsTheCapturedDivisionNames(t *testing.T) {
	draft := validDraft()
	draft.ProvinceName = "  Ho Chi Minh  "
	draft.WardName = "\tPhuong Ben Nghe\n"

	address, err := NewAddress(uuid.New(), draft, fixedNow)

	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if address.ProvinceName != "Ho Chi Minh" || address.WardName != "Phuong Ben Nghe" {
		t.Fatalf("expected the trimmed names, got %q / %q", address.ProvinceName, address.WardName)
	}
}

// The ceilings are the exact numbers contracts/openapi.yaml advertises as maxLength
// on recipientName, provinceName, wardName and streetAddress.
//
// This test exists because every other test in this file measures against the
// constants rather than against literals: a constant changed to the wrong number
// would otherwise keep the whole suite green while the server enforced a limit
// different from the one it advertises — exactly the gap this work exists to close.
func TestTheContractLengthCeilingsMatchTheAdvertisedMaxLength(t *testing.T) {
	if maxRecipientNameRunes != 120 {
		t.Errorf("recipientName: the contract advertises maxLength 120, the entity enforces %d",
			maxRecipientNameRunes)
	}
	if maxProvinceNameRunes != 120 {
		t.Errorf("provinceName: the contract advertises maxLength 120, the entity enforces %d",
			maxProvinceNameRunes)
	}
	if maxWardNameRunes != 120 {
		t.Errorf("wardName: the contract advertises maxLength 120, the entity enforces %d",
			maxWardNameRunes)
	}
	if maxStreetAddressRunes != 255 {
		t.Errorf("streetAddress: the contract advertises maxLength 255, the entity enforces %d",
			maxStreetAddressRunes)
	}
}

// The captured division names are client-supplied text like the recipient name, so they
// carry the same ceiling. Without it any authenticated customer could store unbounded
// text — markup included — that an operator later receives verbatim in the customer
// lookup, and the contract's maxLength on the two members would be a promise the server
// never kept.
//
// The four cases are the whole rule: at the limit is accepted, one character over is
// rejected outright, the count is in characters so a Vietnamese name of 120 fits even at
// 360 bytes, and a blank name is not a violation — it is the way a client says "capture
// it from the dataset" (research D10). Both transitions are covered, because an edit
// that did not re-check the staged draft would be a way around the ceiling on create.
func TestTheCapturedDivisionNamesCarryTheContractLengthCeiling(t *testing.T) {
	const vietnamese = "Ữ" // three bytes in UTF-8

	values := []struct {
		name string
		// value is the captured name a client sends.
		value string
		// field is the member the entity must name in its refusal, empty when the
		// input has to be accepted.
		field string
		// multiByte marks a value whose byte length is well past the ceiling, so a
		// byte count could not have produced the same outcome.
		multiByte bool
	}{
		{"the limit", repeat("a", maxProvinceNameRunes), "", false},
		{"one character over the limit", repeat("a", maxProvinceNameRunes+1), FieldProvinceName, false},
		{"the limit plus surrounding whitespace", "  " + repeat("a", maxProvinceNameRunes) + "\n", "", false},
		{"the limit in multi-byte characters", repeat(vietnamese, maxProvinceNameRunes), "", true},
		{"one multi-byte character over the limit", repeat(vietnamese, maxProvinceNameRunes+1), FieldProvinceName, true},
		{"blank", "   ", "", false},
	}

	// The province name and the ward name are two members with one ceiling, and the
	// refusal has to name whichever one was sent — so both are exercised.
	members := []struct {
		name   string
		field  string
		apply  func(*AddressDraft, string)
		edit   func(string) AddressEdit
		stored func(*Address) string
	}{
		{
			name:   "provinceName",
			field:  FieldProvinceName,
			apply:  func(d *AddressDraft, value string) { d.ProvinceName = value },
			edit:   func(value string) AddressEdit { return AddressEdit{ProvinceName: editPtr(value)} },
			stored: func(a *Address) string { return a.ProvinceName },
		},
		{
			name:   "wardName",
			field:  FieldWardName,
			apply:  func(d *AddressDraft, value string) { d.WardName = value },
			edit:   func(value string) AddressEdit { return AddressEdit{WardName: editPtr(value)} },
			stored: func(a *Address) string { return a.WardName },
		},
	}

	for _, member := range members {
		for _, tc := range values {
			wantField := tc.field
			if tc.field == FieldProvinceName {
				wantField = member.field
			}
			if tc.multiByte && len(tc.value) <= maxProvinceNameRunes {
				t.Fatalf("%s/%s: expected a multi-byte fixture, got %d bytes for %d characters",
					member.name, tc.name, len(tc.value), utf8.RuneCountInString(tc.value))
			}

			t.Run(member.name+"/"+tc.name, func(t *testing.T) {
				t.Run("create", func(t *testing.T) {
					draft := validDraft()
					member.apply(&draft, tc.value)

					address, err := NewAddress(uuid.New(), draft, fixedNow)

					assertAddressFieldOutcome(t, err, wantField)
					if wantField != "" && address != nil {
						t.Fatalf("a refused draft must yield no address, got %+v", address)
					}
					if err == nil && member.stored(address) != strings.TrimSpace(tc.value) {
						t.Errorf("expected the stored name to keep the trimmed input of %d characters, got %d",
							utf8.RuneCountInString(strings.TrimSpace(tc.value)),
							utf8.RuneCountInString(member.stored(address)))
					}
				})

				t.Run("edit", func(t *testing.T) {
					address := mustAddress(t, uuid.New())
					before := *address

					err := address.Apply(member.edit(tc.value), fixedNow.Add(time.Hour))

					assertAddressFieldOutcome(t, err, wantField)
					if err != nil && *address != before {
						t.Errorf("a refused edit changed the entity:\n before %+v\n after  %+v", before, *address)
					}
					if err == nil && member.stored(address) != strings.TrimSpace(tc.value) {
						t.Errorf("expected the stored name to keep the trimmed input of %d characters, got %d",
							utf8.RuneCountInString(strings.TrimSpace(tc.value)),
							utf8.RuneCountInString(member.stored(address)))
					}
				})
			})
		}
	}
}

// assertAddressFieldOutcome states one case of the ceiling rule: an empty field means
// the input must be accepted, and a named field means it must be refused as an invalid
// address member naming exactly that member and stating the ceiling in characters.
func assertAddressFieldOutcome(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Fatalf("the input must be accepted, got %v", err)
		}
		return
	}
	if !errors.Is(err, domainerr.ErrAddressInvalid) {
		t.Fatalf("expected ErrAddressInvalid, got %v", err)
	}
	var invalid *domainerr.AddressFieldError
	if !errors.As(err, &invalid) || invalid.Field != field {
		t.Fatalf("expected the error to name %q, got %v", field, err)
	}
	if want := limitIssue(maxProvinceNameRunes); invalid.Issue != want {
		t.Fatalf("expected the issue %q, got %q", want, invalid.Issue)
	}
}

// The field keys the entity reports are the contract's member names, so
// presentation can put them straight into the field detail without a second
// vocabulary that could drift (FR-020).
func TestAddressFieldKeysMatchTheContractMemberNames(t *testing.T) {
	want := map[string]string{
		FieldRecipientName:  "recipientName",
		FieldRecipientPhone: "recipientPhone",
		FieldProvinceCode:   "provinceCode",
		FieldProvinceName:   "provinceName",
		FieldWardCode:       "wardCode",
		FieldWardName:       "wardName",
		FieldStreetAddress:  "streetAddress",
	}
	for got, expected := range want {
		if got != expected {
			t.Errorf("field key %q does not match the contract member name %q", got, expected)
		}
	}
}
