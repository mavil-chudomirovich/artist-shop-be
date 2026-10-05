package httpdto

import (
	"encoding/json"
	"strings"
	"testing"
)

func newReader(body string) *strings.Reader { return strings.NewReader(body) }

func decodeUpdateProfile(t *testing.T, body string) UpdateProfileRequest {
	t.Helper()
	var req UpdateProfileRequest
	dec := json.NewDecoder(newReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return req
}

func TestUpdateProfilePhoneDistinguishesOmittedClearedAndSent(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		keep  bool
		value string
	}{
		{name: "omitted keeps the current value", body: `{"displayName":"Nguyen Van A"}`, keep: true},
		{name: "explicit null clears the value", body: `{"phone":null}`},
		{name: "empty string clears the value", body: `{"phone":""}`},
		{name: "a value replaces the current value", body: `{"phone":"0912345678"}`, value: "0912345678"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phone, err := decodeUpdateProfile(t, tc.body).PhoneUpdate()
			if err != nil {
				t.Fatalf("PhoneUpdate: %v", err)
			}
			if tc.keep {
				if phone != nil {
					t.Fatalf("expected the value to be kept, got %q", *phone)
				}
				return
			}
			if phone == nil {
				t.Fatal("expected the field to be changed")
			}
			if *phone != tc.value {
				t.Fatalf("expected %q, got %q", tc.value, *phone)
			}
		})
	}
}

func TestUpdateProfileRejectsAMemberOfTheWrongType(t *testing.T) {
	if _, err := decodeUpdateProfile(t, `{"phone":123}`).PhoneUpdate(); err == nil {
		t.Fatal("expected a non-string phone to be rejected")
	}
}

func TestUpdateProfileRejectsAnUnknownMember(t *testing.T) {
	var req UpdateProfileRequest
	dec := json.NewDecoder(newReader(`{"userId":"7f1a1f2e-0000-0000-0000-000000000000"}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("expected an owner identifier in the body to be rejected")
	}
}

func TestUpdateAddressKeepsAnOmittedFieldAndClearsAnEmptyOne(t *testing.T) {
	var req UpdateAddressRequest
	dec := json.NewDecoder(newReader(`{"streetAddress":""}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.RecipientName != nil {
		t.Fatal("expected an omitted member to stay nil so the use case keeps the current value")
	}
	if req.StreetAddress == nil || *req.StreetAddress != "" {
		t.Fatal("expected an empty string to clear the field")
	}
}
