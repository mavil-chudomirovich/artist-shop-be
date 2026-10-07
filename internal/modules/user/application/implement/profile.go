package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
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
	// Media stores avatar bytes outside the service through the shared media
	// port. The profile read and update never touch it, which is what keeps a
	// media outage from failing them (FR-021); only the avatar use cases reach
	// for it.
	Media media.Store
	// Config carries the avatar ceilings the composition resolved. A zero value
	// falls back to the documented contract defaults, so a use case constructed
	// without it still enforces FR-014 and FR-015 rather than accepting anything.
	Config appinterface.Config
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

// SetAvatar validates an uploaded photo, stores it through the media service and
// returns the profile carrying the new reference.
//
// The order of the steps is the specification (FR-014, FR-017):
//
//  1. The payload is decided on its own bytes, from the format signature and the
//     size ceiling, before anything is sent anywhere. A rejected upload therefore
//     never reaches the media service, never touches the row and leaves the current
//     avatar exactly as it was — the client can retry unchanged, and nothing is
//     audited because nothing changed.
//  2. The bytes go up unchanged and the provider is asked to resize them to the
//     target width as part of the upload (ADR-005, research D6). This service adds
//     no imaging dependency and never touches a pixel.
//  3. The provider's answer becomes a domain value object, which refuses anything
//     the contract cannot describe — a half-filled reference, a width above the
//     ceiling, a link that is not a fetchable URL. Such an asset is released again
//     immediately, so a refusal never orphans one.
//  4. Only then is the row written, and only then is the previous asset released.
//     The row is the truth the customer sees, so the reference is committed before
//     the cleanup runs and a cleanup failure is not turned into a failed request.
//
// Every provider failure — a transport error, a non-2xx status, an unreadable body
// — is flattened to the module's retryable media error, with no provider text
// attached. The alternative would be to hand an error containing the provider's
// own message to presentation, which logs it and can echo it: neither a
// credential, an internal hostname nor a provider's error prose belongs in this
// service's logs (Constitution V, VI).
func (s *Service) SetAvatar(ctx context.Context, in dto.SetAvatarInput) (dto.ProfileOutput, error) {
	if err := model.ValidateAvatarUpload(in.Content, s.Config.AvatarMaxBytesOrDefault()); err != nil {
		return dto.ProfileOutput{}, err
	}

	profile, err := s.Profiles.ByID(ctx, in.UserID)
	if err != nil {
		return dto.ProfileOutput{}, err
	}

	stored, err := s.Media.Upload(ctx, in.Content, s.Config.AvatarTargetWidthOrDefault())
	if err != nil {
		// Whatever the provider said is dropped here. See the note above.
		return dto.ProfileOutput{}, domainerr.ErrMediaUnavailable
	}

	reference, err := model.NewAvatar(stored.PublicID, stored.URL, stored.Width, stored.Height)
	if err != nil {
		// The asset exists but must not be referenced, so it is released rather
		// than left behind.
		_ = s.Media.Remove(ctx, stored)
		return dto.ProfileOutput{}, err
	}

	previous := profile.Avatar
	if err := profile.SetAvatar(reference); err != nil {
		_ = s.Media.Remove(ctx, stored)
		return dto.ProfileOutput{}, err
	}
	if err := s.Profiles.Save(ctx, profile); err != nil {
		// The row still points at the previous photo, so the new asset is ours to
		// clean up; failing the request here would leave the customer with the
		// photo they already had and a stored reference they never saw.
		_ = s.Media.Remove(ctx, stored)
		return dto.ProfileOutput{}, err
	}
	if previous != nil {
		s.releaseAvatar(ctx, *previous)
	}

	s.Audit.Record(ctx, constant.AuditAvatarSet, string(audit.OutcomeSuccess),
		&profile.ID, string(profile.Role), targetTypeProfile, profile.ID.String(),
		// The event names the stored dimensions and whether a photo was replaced,
		// never the link: the audit trail must not become a second copy of the
		// profile (Constitution VI).
		map[string]any{
			"width":    reference.Width,
			"height":   reference.Height,
			"replaced": previous != nil,
		},
	)
	return s.Mapper.Profile(*profile), nil
}

// RemoveAvatar releases the customer's current photo and returns the profile
// without one.
//
// A customer who has no photo has already got the outcome they asked for, so the
// call is a no-op: nothing is written, nothing is audited and the media service is
// not called for an asset that was never stored.
//
// The reference is cleared from the row first and the asset released afterwards.
// The row is what the customer and every future read see, so it is the committed
// truth; an asset the provider failed to release is orphaned storage, which is
// recoverable, whereas failing the request after the row is written would tell a
// customer their photo is still there when it is not.
func (s *Service) RemoveAvatar(ctx context.Context, userID uuid.UUID) (dto.ProfileOutput, error) {
	profile, err := s.Profiles.ByID(ctx, userID)
	if err != nil {
		return dto.ProfileOutput{}, err
	}
	if profile.Avatar == nil {
		return s.Mapper.Profile(*profile), nil
	}
	previous := *profile.Avatar

	if err := profile.SetAvatar(nil); err != nil {
		return dto.ProfileOutput{}, err
	}
	if err := s.Profiles.Save(ctx, profile); err != nil {
		return dto.ProfileOutput{}, err
	}
	s.releaseAvatar(ctx, previous)

	s.Audit.Record(ctx, constant.AuditAvatarRemoved, string(audit.OutcomeSuccess),
		&profile.ID, string(profile.Role), targetTypeProfile, profile.ID.String(),
		// The stored dimensions and nothing else: the link is not copied into the
		// audit trail (Constitution VI).
		map[string]any{"width": previous.Width, "height": previous.Height},
	)
	return s.Mapper.Profile(*profile), nil
}

// releaseAvatar hands a replaced or removed reference back to the media service.
//
// It deliberately returns nothing. The caller has already committed the row, so the
// customer's outcome is decided; failing the request because a cleanup call failed
// would report a change that did happen as one that did not. The error is dropped
// rather than logged here because the adapter guarantees a provider failure carries
// nothing but the module's own sentinel — a log line would add no information a
// log line is allowed to hold.
func (s *Service) releaseAvatar(ctx context.Context, reference model.AvatarReference) {
	_ = s.Media.Remove(ctx, media.Reference{
		PublicID: reference.PublicID,
		URL:      reference.URL,
		Width:    reference.Width,
		Height:   reference.Height,
	})
}
