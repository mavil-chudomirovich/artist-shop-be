package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// targetTypeProfile is the audit_logs target type of every event this module
// records. The target is always the account the row belongs to.
const targetTypeProfile = "user"

// Service implements the user module's use cases. It is constructed once in the
// composition root and injected wherever the module is used.
//
// It implements appinterface.UserService in full once the avatar and address
// use cases land (T049, T040); until then only the profile methods exist, and the
// compile-time assertion waits with them rather than being weakened.
type Service struct {
	// Profiles persists the six profile columns this module owns. It never opens
	// a transaction: the application layer owns every boundary (Constitution I).
	Profiles repository.ProfileRepository
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
// and validates before anything is written, so a rejected phone persists nothing
// at all and leaves the stored profile untouched (FR-003).
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
		profile.SetDisplayName(*in.DisplayName)
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
