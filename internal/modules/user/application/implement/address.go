package implement

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Pagination bounds of the address listing. They mirror the handler's, so a window
// that reaches the use case from any other entry point is bounded the same way
// instead of asking the database for an unbounded result (FR-018).
const (
	// defaultAddressPageSize is the window used when none is requested.
	defaultAddressPageSize = 20
	// maxAddressPageSize is the largest window a listing will return.
	maxAddressPageSize = 100
)

// ListAddresses returns one page of the account's non-hidden addresses, default
// address first.
//
// The account comes from the caller, which presentation always fills from the
// authenticated session, so a listing can never walk into another customer's
// addresses (FR-006, SC-003). A read changes nothing, so it records no audit event.
func (s *Service) ListAddresses(ctx context.Context, in dto.ListAddressesInput) (dto.AddressPageOutput, error) {
	page, pageSize := addressWindow(in.Page, in.PageSize)
	list, total, err := s.Addresses.ListByOwner(ctx, in.UserID, page, pageSize)
	if err != nil {
		return dto.AddressPageOutput{}, err
	}
	return dto.AddressPageOutput{
		Addresses: s.Mapper.Addresses(ctx, list),
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
	}, nil
}

// CreateAddress stores a new address and returns it.
//
// The divisions are resolved first: a province or ward outside the official
// dataset, and a ward that belongs to another province, are refused before anything
// is written, and the shared dataset's own sentinel errors travel back untouched so
// presentation can map them to the USER_* codes (FR-007a, FR-007b, research D1).
//
// The division display names are captured from the dataset when the client omits
// them, so a later rename of an administrative unit cannot rewrite what the customer
// entered (research D10).
//
// An account's first address becomes its default (FR-009). That decision and the
// insert run inside one transaction, and the flag is set through the entity's
// explicit transition rather than by writing the column. The decision reads the
// listing's first row, which the repository orders default-first, so it does not
// depend on a second query shape.
func (s *Service) CreateAddress(ctx context.Context, in dto.CreateAddressInput) (dto.AddressOutput, error) {
	draft := model.AddressDraft{
		RecipientName:  in.RecipientName,
		RecipientPhone: in.RecipientPhone,
		ProvinceCode:   in.ProvinceCode,
		WardCode:       in.WardCode,
		StreetAddress:  in.StreetAddress,
	}

	var stored model.Address
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		names, err := s.captureDivisions(txCtx, in.ProvinceCode, in.WardCode,
			optional(in.ProvinceName), optional(in.WardName))
		if err != nil {
			return err
		}
		draft.ProvinceName, draft.WardName = names.province, names.ward

		// The entity validates structure and the recipient phone; a rejection leaves
		// nothing to write and the transaction is rolled back empty.
		address, err := model.NewAddress(in.UserID, draft, now())
		if err != nil {
			return err
		}

		hasDefault, err := s.hasDefaultAddress(txCtx, in.UserID)
		if err != nil {
			return err
		}
		if !hasDefault {
			// The very same transition a later "mark as default" request uses, so
			// there is no second way the flag is ever set (FR-009, Constitution III).
			if err := address.BecomeDefault(); err != nil {
				return err
			}
		}
		if err := s.Addresses.Create(txCtx, address); err != nil {
			return err
		}
		stored = *address
		return nil
	})
	if err != nil {
		return dto.AddressOutput{}, err
	}

	role, err := s.actorRole(ctx, in.UserID)
	if err != nil {
		return dto.AddressOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditAddressCreated, string(audit.OutcomeSuccess),
		&in.UserID, role, targetTypeAddress, stored.ID.String(),
		map[string]any{"addressId": stored.ID.String(), "becameDefault": stored.IsDefault},
	)
	if stored.IsDefault {
		// Becoming the account's default is itself a change of the default address,
		// so it is recorded as one even though the customer only asked to save an
		// address (constant.AuditAddressDefaultSet).
		s.Audit.Record(ctx, constant.AuditAddressDefaultSet, string(audit.OutcomeSuccess),
			&in.UserID, role, targetTypeAddress, stored.ID.String(),
			map[string]any{"addressId": stored.ID.String()},
		)
	}
	return s.Mapper.Address(ctx, stored), nil
}

// UpdateAddress edits an address and returns it, preserving the default flag.
//
// The address is read through FindByOwner first, so an unknown id, a hidden address
// and another customer's address are all the same not-found error and the response
// never confirms that somebody else's address exists (FR-013, SC-003).
//
// Divisions are re-resolved only when the request touches them: an address whose ward
// has left the dataset must stay editable for the members that are still valid,
// because research D10 requires a retired code to remain readable and usable rather
// than to freeze the whole row.
//
// Every change records USER_ADDRESS_UPDATED (FR-019), naming the members that moved
// and never their values.
func (s *Service) UpdateAddress(ctx context.Context, in dto.UpdateAddressInput) (dto.AddressOutput, error) {
	address, err := s.Addresses.FindByOwner(ctx, in.UserID, in.AddressID)
	if err != nil {
		return dto.AddressOutput{}, err
	}

	edit := model.AddressEdit{
		RecipientName:  in.RecipientName,
		RecipientPhone: in.RecipientPhone,
		StreetAddress:  in.StreetAddress,
	}
	if in.ProvinceCode != nil || in.WardCode != nil || in.ProvinceName != nil || in.WardName != nil {
		provinceCode := address.ProvinceCode
		if in.ProvinceCode != nil {
			provinceCode = strings.TrimSpace(*in.ProvinceCode)
		}
		wardCode := address.WardCode
		if in.WardCode != nil {
			wardCode = strings.TrimSpace(*in.WardCode)
		}
		names, err := s.captureDivisions(ctx, provinceCode, wardCode, in.ProvinceName, in.WardName)
		if err != nil {
			return dto.AddressOutput{}, err
		}
		edit.ProvinceCode = in.ProvinceCode
		edit.WardCode = in.WardCode
		// A name is only recaptured when its code moved. A request that sends a name
		// on its own still stores it, but a code change always brings the current
		// name with it so the two can never describe different units.
		if names.province != "" {
			edit.ProvinceName = &names.province
		}
		if names.ward != "" {
			edit.WardName = &names.ward
		}
	}

	before := *address
	// The entity validates the whole change before mutating anything, so a rejected
	// phone or a cleared required member leaves the stored address — and the
	// account's default flag — exactly as it was (FR-003, FR-011).
	if err := address.Apply(edit, now()); err != nil {
		return dto.AddressOutput{}, err
	}

	changed := changedAddressFields(before, *address)
	if len(changed) > 0 {
		if err := s.Addresses.Update(ctx, address); err != nil {
			return dto.AddressOutput{}, err
		}
		role, err := s.actorRole(ctx, in.UserID)
		if err != nil {
			return dto.AddressOutput{}, err
		}
		s.Audit.Record(ctx, constant.AuditAddressUpdated, string(audit.OutcomeSuccess),
			&in.UserID, role, targetTypeAddress, address.ID.String(),
			map[string]any{"addressId": address.ID.String(), "changedFields": changed},
		)
	}
	return s.Mapper.Address(ctx, *address), nil
}

// DeleteAddress hides an address.
//
// The row survives so past orders keep the address text they used (FR-012, ADR-004);
// the address leaves the customer's list and can no longer be the default. Hiding
// the account's only address therefore leaves it with no default, and the next
// address created becomes one (US2 acceptance scenario 6).
//
// Every change records USER_ADDRESS_DELETED (FR-019). It does not record a default
// change: nothing became the default, so claiming one in the audit trail would
// misdescribe what happened.
func (s *Service) DeleteAddress(ctx context.Context, in dto.AddressRefInput) error {
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		address, err := s.Addresses.FindByOwner(txCtx, in.UserID, in.AddressID)
		if err != nil {
			return err
		}
		hiddenAt := now()
		if err := address.Hide(hiddenAt); err != nil {
			return err
		}
		return s.Addresses.Hide(txCtx, in.UserID, in.AddressID, hiddenAt)
	})
	if err != nil {
		return err
	}

	role, err := s.actorRole(ctx, in.UserID)
	if err != nil {
		return err
	}
	s.Audit.Record(ctx, constant.AuditAddressDeleted, string(audit.OutcomeSuccess),
		&in.UserID, role, targetTypeAddress, in.AddressID.String(),
		map[string]any{"addressId": in.AddressID.String()},
	)
	return nil
}

// SetDefaultAddress makes one address the account's single default.
//
// The whole transition runs inside one transaction: the address is resolved through
// FindByOwner — so ownership is decided by the same query that writes, and an
// unknown, hidden or not-owned address is the same not-found error — the previous
// default is cleared, and only then is the new flag set. Clearing before setting is
// what the partial unique index on (user_id) WHERE is_default AND deleted_at IS
// NULL requires, and running both writes in one transaction is what makes the
// outcome indivisible: a failure between them would otherwise leave the account with
// no default at all (FR-010, ADR-003, Constitution I).
//
// Every change records USER_ADDRESS_DEFAULT_SET (FR-019).
func (s *Service) SetDefaultAddress(ctx context.Context, in dto.AddressRefInput) (dto.AddressOutput, error) {
	var stored model.Address
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		address, err := s.Addresses.FindByOwner(txCtx, in.UserID, in.AddressID)
		if err != nil {
			return err
		}
		if err := address.BecomeDefault(); err != nil {
			return err
		}
		if err := s.Addresses.ClearDefault(txCtx, in.UserID); err != nil {
			return err
		}
		if err := s.Addresses.SetDefault(txCtx, in.AddressID); err != nil {
			return err
		}
		stored = *address
		return nil
	})
	if err != nil {
		return dto.AddressOutput{}, err
	}

	role, err := s.actorRole(ctx, in.UserID)
	if err != nil {
		return dto.AddressOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditAddressDefaultSet, string(audit.OutcomeSuccess),
		&in.UserID, role, targetTypeAddress, in.AddressID.String(),
		map[string]any{"addressId": in.AddressID.String()},
	)
	return s.Mapper.Address(ctx, stored), nil
}

// hasDefaultAddress reports whether the account already has a visible default.
//
// It reads the first row of the account's listing, which the repository orders
// default-first, so the answer needs one query rather than a dedicated existence
// check. The create decision depends on this ordering, which is why the ordering is
// part of the repository contract rather than an incidental detail.
func (s *Service) hasDefaultAddress(ctx context.Context, userID uuid.UUID) (bool, error) {
	list, total, err := s.Addresses.ListByOwner(ctx, userID, 1, 1)
	if err != nil {
		return false, err
	}
	return total > 0 && list[0].IsDefault, nil
}

// divisionNames is the pair of display names captured for one address.
type divisionNames struct {
	province string
	ward     string
}

// captureDivisions validates a province/ward pair against the official dataset and
// resolves the display names to store beside the codes.
//
// The pair is validated first, so an invalid save is refused before any name is
// looked up or written (FR-007a, FR-007b). The dataset's own sentinel errors are
// returned unwrapped in meaning — errors.Is still matches them — which is what lets
// presentation map them to USER_UNKNOWN_PROVINCE, USER_UNKNOWN_WARD and
// USER_WARD_PROVINCE_MISMATCH without this layer importing the shared package
// (research D1).
//
// A name the client sent wins when it is not blank, because the contract makes it
// optional with the description "captured when omitted"; otherwise the current name
// is read from the dataset so the stored text keeps describing the stored code
// (research D10). An empty result means the caller must keep its current value.
func (s *Service) captureDivisions(
	ctx context.Context,
	provinceCode, wardCode string,
	sentProvinceName, sentWardName *string,
) (divisionNames, error) {
	if err := s.Divisions.ValidateAddressDivisions(ctx, provinceCode, wardCode); err != nil {
		return divisionNames{}, err
	}
	var names divisionNames
	if strings.TrimSpace(deref(sentProvinceName)) == "" {
		names.province = s.provinceName(ctx, provinceCode)
	} else {
		names.province = strings.TrimSpace(*sentProvinceName)
	}
	if strings.TrimSpace(deref(sentWardName)) == "" {
		names.ward = s.wardName(ctx, provinceCode, wardCode)
	} else {
		names.ward = strings.TrimSpace(*sentWardName)
	}
	return names, nil
}

// provinceName reads one province's display name from the dataset. The province list
// is walked by code; it is embedded in the binary and far too small for a linear
// scan to matter, which is why no extra index or port method exists for the name
// alone (research D1).
func (s *Service) provinceName(ctx context.Context, code string) string {
	provinces, err := s.Divisions.Provinces(ctx)
	if err != nil {
		return ""
	}
	for _, province := range provinces {
		if province.Code == code {
			return province.Name
		}
	}
	return ""
}

// wardName reads one ward's display name from the dataset. A ward list is always
// scoped to a single province, so the province the address already chose is the
// only place its ward can be (FR-007c, research D1).
func (s *Service) wardName(ctx context.Context, provinceCode, code string) string {
	wards, err := s.Divisions.Wards(ctx, provinceCode)
	if err != nil {
		return ""
	}
	for _, ward := range wards {
		if ward.Code == code {
			return ward.Name
		}
	}
	return ""
}

// deref reads an optional string, treating an absent member as empty.
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// optional lifts a create-input display name into the optional form the update
// input uses. A blank name is the same as an absent one: both mean "capture it from
// the dataset".
func optional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

// changedAddressFields names the members an edit actually moved.
//
// The names are the contract's member names, so an auditor reading the trail can map
// one straight onto a form. Values are never included: contact data must not be
// copied into the audit log (Constitution VI).
//
// A captured division *name* is deliberately not listed. It is refreshed from the
// dataset whenever its code moves, so naming it would report a change the customer
// never asked for — and a request that changes only a name is a correction of
// display text, not of where the order goes.
func changedAddressFields(before, after model.Address) []string {
	var changed []string
	if before.RecipientName != after.RecipientName {
		changed = append(changed, model.FieldRecipientName)
	}
	if before.RecipientPhone != after.RecipientPhone {
		changed = append(changed, model.FieldRecipientPhone)
	}
	if before.ProvinceCode != after.ProvinceCode {
		changed = append(changed, model.FieldProvinceCode)
	}
	if before.WardCode != after.WardCode {
		changed = append(changed, model.FieldWardCode)
	}
	if before.StreetAddress != after.StreetAddress {
		changed = append(changed, model.FieldStreetAddress)
	}
	return changed
}

// actorRole reads the acting account's role for the audit row.
//
// The role comes from the stored row rather than from the request, exactly as the
// profile use cases do, so an audit event can never name a role the client chose.
func (s *Service) actorRole(ctx context.Context, userID uuid.UUID) (string, error) {
	profile, err := s.Profiles.ByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return string(profile.Role), nil
}

// addressWindow bounds a requested listing window. An out-of-range window is
// clamped rather than refused, so a caller that asks for page 0 or an enormous page
// still gets a usable answer instead of an error.
func addressWindow(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case pageSize < 1:
		pageSize = defaultAddressPageSize
	case pageSize > maxAddressPageSize:
		pageSize = maxAddressPageSize
	}
	return page, pageSize
}
