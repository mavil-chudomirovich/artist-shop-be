package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
)

// Address is one shipping address of an account.
//
// ProvinceCode and WardCode are the values that join to the official dataset;
// ProvinceName and WardName are the display names captured when the address was
// saved, so history stays truthful after the government renames a unit
// (research D10). The dataset itself is invisible here: whether a code still
// exists is answered in the application layer through the Divisions port,
// because the domain may not import internal/share/administrative
// (Constitution I).
//
// A non-nil DeletedAt means the address is hidden: it disappears from every
// customer-facing list and can never be the default, but the row survives so
// past orders keep the address text they used (research D4).
//
// Every change to this entity goes through one of its four transitions —
// NewAddress, Apply, BecomeDefault and Hide. Nothing writes IsDefault or
// DeletedAt directly, which is what keeps the "at most one default, and never a
// hidden one" invariant in one place (Constitution III, FR-008).
type Address struct {
	// ID identifies the address.
	ID uuid.UUID
	// UserID is the owning account. Every query filters on it, so one customer
	// can never reach another customer's address (FR-006).
	UserID uuid.UUID
	// RecipientName is the person the order goes to.
	RecipientName string
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string
	// ProvinceCode is the stored first-level administrative unit.
	ProvinceCode string
	// ProvinceName is the display name captured at save time.
	ProvinceName string
	// WardCode is the stored second-level administrative unit; it always
	// belongs to ProvinceCode.
	WardCode string
	// WardName is the display name captured at save time.
	WardName string
	// StreetAddress is the free-text house number and street.
	StreetAddress string
	// IsDefault reports whether this is the account's default address. At most
	// one non-hidden address of an account is default (FR-008).
	IsDefault bool
	// DeletedAt is when the address was hidden, or nil while it is visible.
	DeletedAt *time.Time
	// CreatedAt is when the address was created.
	CreatedAt time.Time
	// UpdatedAt is when the address was last written.
	UpdatedAt time.Time
}

// Contract member names of an address, used when a rejection has to say which
// input is wrong.
//
// They are the exact member names of contracts/openapi.yaml, so presentation can
// put one straight into the response detail without a second vocabulary that could
// drift away from the contract (FR-020). The presentation constants are defined
// from these values for the same reason.
const (
	// FieldRecipientName is the person the order goes to.
	FieldRecipientName = "recipientName"
	// FieldRecipientPhone is the recipient phone number.
	FieldRecipientPhone = "recipientPhone"
	// FieldProvinceCode is the first-level administrative unit.
	FieldProvinceCode = "provinceCode"
	// FieldProvinceName is the captured province display name.
	FieldProvinceName = "provinceName"
	// FieldWardCode is the second-level administrative unit.
	FieldWardCode = "wardCode"
	// FieldWardName is the captured ward display name.
	FieldWardName = "wardName"
	// FieldStreetAddress is the free-text house number and street.
	FieldStreetAddress = "streetAddress"
)

// requiredIssue is the explanation attached to every blank required member.
const requiredIssue = "is required"

// The ceilings the contract declares for the four free-text members of an address,
// as maxLength on recipientName, provinceName, wardName and streetAddress in
// contracts/openapi.yaml.
//
// They are checked here, beside the blank checks, and not in the handler: the
// entity is the one place every entry point passes through — create, edit and any
// later transition — so a limit enforced only in presentation could be bypassed by
// any caller that is not HTTP, and the contract would be advertising a limit the
// server does not actually apply.
//
// The division names are bounded for the same reason the other two are, and the
// reason matters most for them: a client may send the display name instead of
// letting the dataset supply it, so without a ceiling any authenticated customer
// could store unbounded text that an operator later receives verbatim in the
// customer lookup. The bound is on the name the entity writes, which is the client's
// word whenever it sends one.
//
// The columns stay unconstrained on purpose (migrations/00004_user.sql keeps
// street_address as text): a row saved before the rule existed may be longer than
// the contract allows, and narrowing the column would refuse to load it. The rule
// is therefore an application rule, and this is where it lives.
const (
	// maxRecipientNameRunes is the ceiling on RecipientName.
	maxRecipientNameRunes = 120
	// maxProvinceNameRunes is the ceiling on ProvinceName.
	maxProvinceNameRunes = 120
	// maxWardNameRunes is the ceiling on WardName.
	maxWardNameRunes = 120
	// maxStreetAddressRunes is the ceiling on StreetAddress.
	maxStreetAddressRunes = 255
)

// limitIssue explains a rejection caused by a contract maxLength. It counts
// characters rather than bytes, matching the contract's own wording.
func limitIssue(limit int) string {
	return fmt.Sprintf("must be at most %d characters", limit)
}

// checkRuneLimit refuses a member that does not fit the contract's ceiling,
// naming the member so the client can fix the exact input (FR-020).
//
// Boundary semantics, chosen deliberately:
//
//   - The count is in runes, not bytes. The API serves Vietnamese text, so a name
//     of 120 multi-byte characters has to pass; counting bytes would reject it at
//     roughly a third of the advertised length and would be wrong for every
//     non-ASCII name.
//   - The count is taken on the trimmed value, which is the value that gets
//     stored. Enforcing the limit on the stored text is what guarantees no row the
//     entity writes can exceed the contract, and it does not reject a caller for
//     surrounding whitespace the entity would have discarded anyway.
//   - The limit is inclusive: a member of exactly the limit characters is
//     accepted, and one character more is rejected outright rather than truncated.
//     Truncating would silently store something the customer never typed and would
//     break the "no half of an address is persisted" promise of a rejected draft.
//   - A blank member is not a length violation, so this accepts it. That matters
//     for the optional division names, where blank is a meaningful state — "capture
//     it from the dataset" — and must not become a rejection the customer cannot
//     act on. The required members carry their own blank rule.
func checkRuneLimit(field, value string, limit int) error {
	if utf8.RuneCountInString(value) > limit {
		return domainerr.InvalidAddressField(field, limitIssue(limit))
	}
	return nil
}

// AddressDraft carries the caller-supplied members of a new address.
//
// The division names are optional because the application layer captures them
// from the dataset when the client omits them (research D10); the entity only
// requires that they be clean when present.
type AddressDraft struct {
	// RecipientName is the person the order goes to. It must not be blank.
	RecipientName string
	// RecipientPhone is the recipient phone number. It is normalised here, so one
	// person is stored one way whatever spelling the customer typed (research D9).
	RecipientPhone string
	// ProvinceCode is the chosen first-level administrative unit. Whether it
	// exists in the dataset is a use-case concern; it must not be blank.
	ProvinceCode string
	// ProvinceName is the captured province display name. It may be blank, which
	// means the caller left the capture to the application layer; when it is not
	// blank it must fit maxProvinceNameRunes.
	ProvinceName string
	// WardCode is the chosen second-level administrative unit. Whether it belongs
	// to ProvinceCode is a use-case concern; it must not be blank.
	WardCode string
	// WardName is the captured ward display name. It may be blank, which means the
	// caller left the capture to the application layer; when it is not blank it
	// must fit maxWardNameRunes.
	WardName string
	// StreetAddress is the free-text house number and street. It must not be blank.
	StreetAddress string
}

// AddressEdit carries the changes of a partial edit.
//
// A nil member keeps its current value; a member set to an empty string is a
// request to clear it. Clearing is accepted only where an empty value is a
// representable state — the division names, which are recaptured from the dataset
// — and refused for the members the row and the specification require to be
// non-empty (data-model.md §3).
//
// The default flag is deliberately absent: it belongs to its own route, so an edit
// can never move it (FR-011).
type AddressEdit struct {
	// RecipientName is the new recipient name; nil keeps the current value.
	RecipientName *string
	// RecipientPhone is the new recipient phone; nil keeps the current value.
	RecipientPhone *string
	// ProvinceCode is the new province; nil keeps the current value.
	ProvinceCode *string
	// ProvinceName is the new captured province name; nil keeps the current value.
	ProvinceName *string
	// WardCode is the new ward; nil keeps the current value.
	WardCode *string
	// WardName is the new captured ward name; nil keeps the current value.
	WardName *string
	// StreetAddress is the new street address; nil keeps the current value.
	StreetAddress *string
}

// NewAddress builds a visible address of the given account.
//
// The address starts without the default flag. An account's first address becomes
// its default through BecomeDefault, the same explicit transition every later
// change uses, so there is no code path that sets the flag by writing the field
// (FR-009, Constitution III).
//
// It validates structure only: the recipient name, the province and ward codes and
// the street address must not be blank, the recipient name, both captured division
// names and the street address must fit the ceilings the contract declares for them
// (see checkRuneLimit), and the recipient phone must normalise to a Vietnamese
// mobile number. Whether a province or ward code exists in the official dataset is
// deliberately not checked here — the domain may not import
// internal/share/administrative, so the use case resolves it through the Divisions
// port before calling this (Constitution I, research D1).
//
// A rejected draft yields no address at all, so a caller cannot persist half of one.
func NewAddress(userID uuid.UUID, draft AddressDraft, now time.Time) (*Address, error) {
	recipientName := strings.TrimSpace(draft.RecipientName)
	if recipientName == "" {
		return nil, domainerr.InvalidAddressField(FieldRecipientName, requiredIssue)
	}
	if err := checkRuneLimit(FieldRecipientName, recipientName, maxRecipientNameRunes); err != nil {
		return nil, err
	}
	provinceCode := strings.TrimSpace(draft.ProvinceCode)
	if provinceCode == "" {
		return nil, domainerr.InvalidAddressField(FieldProvinceCode, requiredIssue)
	}
	wardCode := strings.TrimSpace(draft.WardCode)
	if wardCode == "" {
		return nil, domainerr.InvalidAddressField(FieldWardCode, requiredIssue)
	}
	// The captured division names are bounded like every other free-text member. They
	// are checked next to the codes they describe so a rejection names the member the
	// customer actually sent, and a blank one still passes: blank means the
	// application layer captures it from the dataset instead (research D10).
	provinceName := strings.TrimSpace(draft.ProvinceName)
	if err := checkRuneLimit(FieldProvinceName, provinceName, maxProvinceNameRunes); err != nil {
		return nil, err
	}
	wardName := strings.TrimSpace(draft.WardName)
	if err := checkRuneLimit(FieldWardName, wardName, maxWardNameRunes); err != nil {
		return nil, err
	}
	streetAddress := strings.TrimSpace(draft.StreetAddress)
	if streetAddress == "" {
		return nil, domainerr.InvalidAddressField(FieldStreetAddress, requiredIssue)
	}
	if err := checkRuneLimit(FieldStreetAddress, streetAddress, maxStreetAddressRunes); err != nil {
		return nil, err
	}
	recipientPhone, err := NormalizePhone(draft.RecipientPhone)
	if err != nil {
		return nil, err
	}

	return &Address{
		ID:             uuid.New(),
		UserID:         userID,
		RecipientName:  recipientName,
		RecipientPhone: recipientPhone,
		ProvinceCode:   provinceCode,
		ProvinceName:   provinceName,
		WardCode:       wardCode,
		WardName:       wardName,
		StreetAddress:  streetAddress,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Apply writes a partial edit.
//
// It validates the whole change before mutating anything, so a rejection leaves the
// address exactly as it was — including the default flag, which is never touched
// and never settable here (FR-011). That is what makes the specification's edge case
// hold: a customer's default address edited into an invalid state is rejected and
// the previous default stays in place.
//
// Because the whole staged draft is validated, the contract's length ceilings are
// re-checked on every edit even for a member the edit does not name: an edit cannot
// be a way to leave an over-long value in place, and conversely a legacy row that is
// already longer than the contract allows must be shortened, not merely touched.
//
// A hidden address cannot be edited: it has left the account's visible set, so a
// later save must not resurrect it (research D4).
func (a *Address) Apply(edit AddressEdit, now time.Time) error {
	if a.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}

	// Stage the change, validate it, and only then write. Anything rejected below
	// therefore leaves the entity untouched.
	draft := AddressDraft{
		RecipientName:  a.RecipientName,
		RecipientPhone: a.RecipientPhone,
		ProvinceCode:   a.ProvinceCode,
		ProvinceName:   a.ProvinceName,
		WardCode:       a.WardCode,
		WardName:       a.WardName,
		StreetAddress:  a.StreetAddress,
	}
	if edit.RecipientName != nil {
		draft.RecipientName = *edit.RecipientName
	}
	if edit.RecipientPhone != nil {
		draft.RecipientPhone = *edit.RecipientPhone
	}
	if edit.ProvinceCode != nil {
		draft.ProvinceCode = *edit.ProvinceCode
	}
	if edit.ProvinceName != nil {
		draft.ProvinceName = *edit.ProvinceName
	}
	if edit.WardCode != nil {
		draft.WardCode = *edit.WardCode
	}
	if edit.WardName != nil {
		draft.WardName = *edit.WardName
	}
	if edit.StreetAddress != nil {
		draft.StreetAddress = *edit.StreetAddress
	}

	recipientName := strings.TrimSpace(draft.RecipientName)
	if recipientName == "" {
		return domainerr.InvalidAddressField(FieldRecipientName, requiredIssue)
	}
	if err := checkRuneLimit(FieldRecipientName, recipientName, maxRecipientNameRunes); err != nil {
		return err
	}
	provinceCode := strings.TrimSpace(draft.ProvinceCode)
	if provinceCode == "" {
		return domainerr.InvalidAddressField(FieldProvinceCode, requiredIssue)
	}
	wardCode := strings.TrimSpace(draft.WardCode)
	if wardCode == "" {
		return domainerr.InvalidAddressField(FieldWardCode, requiredIssue)
	}
	// Re-checked on every edit, exactly like the members above: a client may supply
	// the division display name, so without this an edit would be the way to put an
	// unbounded word into the row. An empty name is still accepted — it is the way
	// a client asks for the dataset's current one (research D10).
	provinceName := strings.TrimSpace(draft.ProvinceName)
	if err := checkRuneLimit(FieldProvinceName, provinceName, maxProvinceNameRunes); err != nil {
		return err
	}
	wardName := strings.TrimSpace(draft.WardName)
	if err := checkRuneLimit(FieldWardName, wardName, maxWardNameRunes); err != nil {
		return err
	}
	streetAddress := strings.TrimSpace(draft.StreetAddress)
	if streetAddress == "" {
		return domainerr.InvalidAddressField(FieldStreetAddress, requiredIssue)
	}
	if err := checkRuneLimit(FieldStreetAddress, streetAddress, maxStreetAddressRunes); err != nil {
		return err
	}
	recipientPhone, err := NormalizePhone(draft.RecipientPhone)
	if err != nil {
		return err
	}

	a.RecipientName = recipientName
	a.RecipientPhone = recipientPhone
	a.ProvinceCode = provinceCode
	a.ProvinceName = provinceName
	a.WardCode = wardCode
	a.WardName = wardName
	a.StreetAddress = streetAddress
	a.UpdatedAt = now
	return nil
}

// BecomeDefault marks the address as its account's default.
//
// It is the only way the flag is ever set. A hidden address is refused with
// domainerr.ErrAddressNotFound, because a default that points at a hidden address
// is one of the two states SC-004 forbids — and because the partial unique index on
// (user_id) WHERE is_default AND deleted_at IS NULL already excludes hidden rows,
// so the storage layer refuses it too (ADR-003).
//
// Marking the account's current default again is not an error: the outcome the
// caller asked for already holds.
func (a *Address) BecomeDefault() error {
	if a.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
	a.IsDefault = true
	return nil
}

// Hide stamps the address as hidden and drops the default flag in the same
// transition.
//
// Clearing the flag here is not cosmetic: an account that keeps is_default on a
// hidden row would hold a default pointing at an address no client can see, and the
// partial unique index would no longer count that row, so a second default could
// then be created beside it (ADR-003, ADR-004, SC-004).
//
// Hiding an already hidden address is refused rather than re-stamped, so the stored
// hide time stays the first one.
func (a *Address) Hide(now time.Time) error {
	if a.DeletedAt != nil {
		return domainerr.ErrAddressNotFound
	}
	a.DeletedAt = &now
	a.IsDefault = false
	return nil
}

// IsHidden reports whether the address has been hidden.
func (a *Address) IsHidden() bool { return a.DeletedAt != nil }
