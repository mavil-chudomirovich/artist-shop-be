package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeProfile is the audit_logs target type of the profile events. The
// target is the account the profile columns belong to.
const targetTypeProfile = "user"

// targetTypeAddress is the audit_logs target type of the address events. The
// target of an address mutation is the address, so the audit index on
// (target_type, target_id, occurred_at) can reconstruct what happened to one
// address without scanning every customer event (Constitution VI). The acting
// account is carried separately as the actor.
const targetTypeAddress = "address"

// Service implements the user module's use cases. It is constructed once in the
// composition root and injected wherever the module is used.
//
// It implements appinterface.UserService in full once the avatar use cases land
// (T049); until then only the profile and address methods exist, and the
// compile-time assertion waits with them rather than being weakened.
type Service struct {
	// Profiles persists the six profile columns this module owns. It never opens
	// a transaction: the application layer owns every boundary (Constitution I).
	Profiles repository.ProfileRepository
	// Addresses persists the shipping addresses. It never opens a transaction
	// either; the default-flag transition joins the one this layer opens through
	// Tx (FR-010).
	Addresses repository.AddressRepository
	// Divisions is the only way the application layer reaches the official
	// administrative dataset. The domain may not import it at all, which is why
	// province and ward existence is a use-case concern (Constitution I,
	// research D1).
	Divisions appinterface.Divisions
	// Tx owns every transaction boundary. Clearing the previous default and
	// setting the new one must be a single indivisible outcome, and only the
	// application layer may decide where a transaction starts and ends
	// (Constitution I, FR-010).
	Tx appinterface.UnitOfWork
	// Audit records every change a customer makes and every operator read of
	// customer contact details (FR-019, FR-022a).
	Audit appinterface.Auditor
	// Media stores avatar bytes outside the service. The profile read and update
	// never touch it, which is what keeps a media outage from failing them
	// (FR-021); the avatar use cases use it from US3 on.
	Media appinterface.MediaStore
	// Mapper is the single conversion point between models and DTOs.
	Mapper *mapper.Mapper
}

// now reads the wall clock. Every use case that stamps a row goes through it, so
// the domain stays a pure function of its arguments and the clock is injected at
// exactly one place.
func now() time.Time { return time.Now().UTC() }

// New creates the user use-case service.
func New(deps Service) *Service { return &deps }

// GetProfile returns the signed-in customer's profile.
//
// The account comes from the caller, which presentation always fills from the
// authenticated session: no use case accepts an owner identifier the client
// chose, so cross-account access is impossible by construction rather than by a
// check that could be forgotten (FR-006, research D5).
//
// The answer comes from the stored columns alone — no media call — so a provider
// outage can never fail the read (FR-021).
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (dto.ProfileOutput, error) {
	profile, err := s.Profiles.ByID(ctx, userID)
	if err != nil {
		return dto.ProfileOutput{}, err
	}
	return s.Mapper.Profile(*profile), nil
}

// UpdateProfile changes the display name and/or the phone of the signed-in
// customer and returns the stored profile.
//
// The two fields are independent: a nil pointer keeps the current value and a
// pointer to an empty string clears it (FR-002, FR-004). The domain normalises
// and validates before anything is written, so a rejected phone or an over-long
// display name persists nothing at all and leaves the stored profile untouched
// (FR-003, FR-020).
//
// Every successful change records USER_PROFILE_UPDATED (FR-019).
func (s *Service) UpdateProfile(ctx context.Context, in dto.UpdateProfileInput) (dto.ProfileOutput, error) {
	profile, err := s.Profiles.ByID(ctx, in.UserID)
	if err != nil {
		return dto.ProfileOutput{}, err
	}

	// A body that names no field changes nothing: there is no write to make and
	// no change to trace, so the current profile is returned as it stands.
	if in.DisplayName == nil && in.Phone == nil {
		return s.Mapper.Profile(*profile), nil
	}

	changed := make([]string, 0, 2)
	if in.DisplayName != nil {
		// The entity validates before it mutates, so a name over the contract's
		// maxLength leaves the stored profile untouched and the whole update is
		// refused below, exactly as an invalid phone is (FR-020).
		if err := profile.SetDisplayName(*in.DisplayName); err != nil {
			return dto.ProfileOutput{}, err
		}
		changed = append(changed, "displayName")
	}
	if in.Phone != nil {
		if err := profile.SetPhone(*in.Phone); err != nil {
			return dto.ProfileOutput{}, err
		}
		changed = append(changed, "phone")
	}

	if err := s.Profiles.Save(ctx, profile); err != nil {
		return dto.ProfileOutput{}, err
	}
	s.Audit.Record(ctx, constant.AuditProfileUpdated, string(audit.OutcomeSuccess),
		&profile.ID, string(profile.Role), targetTypeProfile, profile.ID.String(),
		// The event names the fields that changed, never their values: contact
		// data must not be copied into the audit trail (Constitution VI).
		map[string]any{"changedFields": changed},
	)
	return s.Mapper.Profile(*profile), nil
}
