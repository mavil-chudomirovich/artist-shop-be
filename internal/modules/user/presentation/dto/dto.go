// Package httpdto defines the HTTP request/response payloads for the user API.
// Field names match specs/003-user-profile/contracts/openapi.yaml exactly.
package httpdto

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// OptionalString is a JSON member that carries one of three instructions:
// absent, explicitly null, or a value. The contract makes them different:
// an absent member keeps the current value while null clears it
// (UpdateProfileRequest). A plain *string cannot express that, because decoding
// null into a nil pointer is indistinguishable from an absent member.
//
// A request DTO therefore keeps such a member as raw JSON and lets the handler
// turn it into a pointer with Pointer: nil to keep, a pointer to an empty string
// to clear.
type OptionalString struct {
	// Present reports whether the member appeared in the body at all.
	Present bool
	// Value holds the string when the member was present and not null.
	Value string
}

// NewOptionalString reads one raw JSON member. A JSON null is a valid input for
// these fields and must not be reported as a parse error: it clears the field.
func NewOptionalString(raw json.RawMessage) (OptionalString, error) {
	if len(raw) == 0 {
		return OptionalString{}, nil
	}
	if string(raw) == "null" {
		return OptionalString{Present: true}, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return OptionalString{}, err
	}
	return OptionalString{Present: true, Value: value}, nil
}

// Pointer returns the value to hand to the use case: nil keeps the current
// value, a pointer to an empty string clears it.
func (o OptionalString) Pointer() *string {
	if !o.Present {
		return nil
	}
	value := o.Value
	return &value
}

// UpdateProfileRequest is the PATCH /users/me body. Both fields are independent:
// sending one leaves the other unchanged, and an empty string clears a field.
type UpdateProfileRequest struct {
	// DisplayName is omitted to keep the current value, empty to clear it.
	DisplayName *string `json:"displayName"`
	// Phone is omitted to keep the current value, empty or null to clear it.
	Phone json.RawMessage `json:"phone"`
}

// PhoneUpdate turns the raw phone member into the use-case input.
func (r UpdateProfileRequest) PhoneUpdate() (*string, error) {
	optional, err := NewOptionalString(r.Phone)
	if err != nil {
		return nil, err
	}
	return optional.Pointer(), nil
}

// AvatarResponse is the reference to the stored avatar. It is null when the
// customer has no photo, which is the only signal for that: there is no
// hasAvatar field.
type AvatarResponse struct {
	// PublicID is the media provider's opaque identifier.
	PublicID string `json:"publicId"`
	// URL is the displayable link.
	URL string `json:"url"`
	// Width is the stored width, at most 512.
	Width int `json:"width"`
	// Height is the stored height.
	Height int `json:"height"`
}

// ProfileResponse is the customer-facing profile.
type ProfileResponse struct {
	// ID is the account identifier.
	ID uuid.UUID `json:"id"`
	// Email is the account email.
	Email string `json:"email"`
	// Role is CUSTOMER or ADMIN.
	Role access.Role `json:"role"`
	// DisplayName may be empty when the customer never set one.
	DisplayName string `json:"displayName"`
	// Phone is null when unset.
	Phone *string `json:"phone"`
	// Avatar is null when the customer has no photo.
	Avatar *AvatarResponse `json:"avatar"`
}

// CreateAddressRequest is the POST /users/me/addresses body. provinceName and
// wardName are optional; they are captured from the dataset when omitted.
type CreateAddressRequest struct {
	// RecipientName is the person the order goes to.
	RecipientName string `json:"recipientName"`
	// RecipientPhone is the recipient phone number.
	RecipientPhone string `json:"recipientPhone"`
	// ProvinceCode must exist in the official dataset.
	ProvinceCode string `json:"provinceCode"`
	// ProvinceName is optional; captured when omitted.
	ProvinceName string `json:"provinceName"`
	// WardCode must exist and belong to ProvinceCode.
	WardCode string `json:"wardCode"`
	// WardName is optional; captured when omitted.
	WardName string `json:"wardName"`
	// StreetAddress is the free-text house number and street.
	StreetAddress string `json:"streetAddress"`
}

// UpdateAddressRequest is the PATCH /users/me/addresses/{addressId} body. Every
// field is optional: an omitted field keeps its current value and an empty
// string clears a text field. The default flag is not settable here.
type UpdateAddressRequest struct {
	// RecipientName is omitted to keep the current value.
	RecipientName *string `json:"recipientName"`
	// RecipientPhone is omitted to keep the current value.
	RecipientPhone *string `json:"recipientPhone"`
	// ProvinceCode is omitted to keep the current province.
	ProvinceCode *string `json:"provinceCode"`
	// ProvinceName is optional; recaptured when the province changes.
	ProvinceName *string `json:"provinceName"`
	// WardCode is omitted to keep the current ward.
	WardCode *string `json:"wardCode"`
	// WardName is optional; recaptured when the ward changes.
	WardName *string `json:"wardName"`
	// StreetAddress is omitted to keep the current street address.
	StreetAddress *string `json:"streetAddress"`
}

// AddressResponse is one shipping address as the owner of the account sees it.
//
// It is a distinct type from OperatorAddressResponse on purpose: the two views
// can answer different questions about the review flag, and one
// `,omitempty` cannot be right for both (research D10).
type AddressResponse struct {
	// ID identifies the address.
	ID uuid.UUID `json:"id"`
	// RecipientName is the person the order goes to.
	RecipientName string `json:"recipientName"`
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string `json:"recipientPhone"`
	// ProvinceCode is the stored first-level administrative unit.
	ProvinceCode string `json:"provinceCode"`
	// ProvinceName is the display name captured at save time.
	ProvinceName string `json:"provinceName"`
	// WardCode is the stored second-level administrative unit.
	WardCode string `json:"wardCode"`
	// WardName is the display name captured at save time.
	WardName string `json:"wardName"`
	// StreetAddress is the free-text house number and street.
	StreetAddress string `json:"streetAddress"`
	// IsDefault reports whether this is the account's default address.
	IsDefault bool `json:"isDefault"`
	// DivisionNeedsReview is true when a stored code is no longer in the official
	// dataset; the captured names are still returned so the customer can see what
	// was saved.
	//
	// It is always stated, false included: the customer's own list knows the
	// answer, because the use case checks every stored code against the dataset.
	// Omitting a known false would make a client read "absent" as "unknown",
	// which in JavaScript is `undefined` rather than `false`.
	DivisionNeedsReview bool `json:"divisionNeedsReview"`
}

// OperatorAddressResponse is one shipping address in the administrator's
// read-only view of a customer.
//
// It has no divisionNeedsReview member, and that absence is deliberate rather
// than an omission. The cross-module contract DTO the lookup is built from
// carries no such flag, so nothing on that path checks a stored code against the
// official dataset: answering `false` would claim every code is current when
// nothing verified it. Absent means "this view cannot say", while the customer
// view above means it and therefore states it. Splitting the two types is what
// lets each answer honestly — one `,omitempty` shared by both could not.
type OperatorAddressResponse struct {
	// ID identifies the address.
	ID uuid.UUID `json:"id"`
	// RecipientName is the person the order goes to.
	RecipientName string `json:"recipientName"`
	// RecipientPhone is the normalised recipient phone number.
	RecipientPhone string `json:"recipientPhone"`
	// ProvinceCode is the stored first-level administrative unit.
	ProvinceCode string `json:"provinceCode"`
	// ProvinceName is the display name captured at save time.
	ProvinceName string `json:"provinceName"`
	// WardCode is the stored second-level administrative unit.
	WardCode string `json:"wardCode"`
	// WardName is the display name captured at save time.
	WardName string `json:"wardName"`
	// StreetAddress is the free-text house number and street.
	StreetAddress string `json:"streetAddress"`
	// IsDefault reports whether the account's default address.
	IsDefault bool `json:"isDefault"`
}

// CustomerLookupResponse is the read-only administrator view of a customer.
type CustomerLookupResponse struct {
	// ID identifies the account.
	ID uuid.UUID `json:"id"`
	// Email is the account email.
	Email string `json:"email"`
	// Role is CUSTOMER or ADMIN.
	Role access.Role `json:"role"`
	// DisplayName may be empty when the customer never set one.
	DisplayName string `json:"displayName"`
	// Phone is null when unset.
	Phone *string `json:"phone"`
	// Addresses holds the customer's non-hidden addresses, default first and then
	// most recently updated. They are the operator type, which carries no
	// divisionNeedsReview member.
	Addresses []OperatorAddressResponse `json:"addresses"`
}

// ProvinceResponse is one first-level administrative unit for the cascading
// select.
type ProvinceResponse struct {
	// Code is the stable identifier stored on an address.
	Code string `json:"code"`
	// Name is the display name.
	Name string `json:"name"`
}

// WardResponse is one second-level administrative unit, always scoped to its
// province.
type WardResponse struct {
	// Code is the stable identifier stored on an address.
	Code string `json:"code"`
	// Name is the display name.
	Name string `json:"name"`
	// ProvinceCode is the owning province, so a consumer never has to join back.
	ProvinceCode string `json:"provinceCode"`
}
