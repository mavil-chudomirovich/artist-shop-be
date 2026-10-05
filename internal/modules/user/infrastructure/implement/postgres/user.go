package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// ProfileRepository persists the six profile columns this module owns on the
// accounts table the auth module created (research D2).
//
// It embeds share/repository.Base for its read plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`: a star projection ties the scan arity
// to the physical table, so a plain `ALTER TABLE users ADD COLUMN` would break
// every repository embedding Base. It never calls FindAll — an unscoped profile
// listing would enumerate every customer, which this module must never do.
type ProfileRepository struct {
	baserepo.Base[model.Profile, uuid.UUID]
	pool *pgxpool.Pool
}

// NewProfileRepository creates the profile repository.
func NewProfileRepository(pool *pgxpool.Pool) *ProfileRepository {
	r := &ProfileRepository{pool: pool}
	r.Base = baserepo.Base[model.Profile, uuid.UUID]{
		Pool:     pool,
		Table:    "users",
		IDColumn: "id",
		// The read projection is exactly what scanProfile reads: the key, the two
		// auth-owned columns the profile response must return, and the six profile
		// columns this module owns. The row timestamps are not read because no
		// profile response carries them.
		Columns: []string{
			"id",
			"email",
			"role",
			"display_name",
			"phone",
			"avatar_public_id",
			"avatar_secure_url",
			"avatar_width",
			"avatar_height",
		},
		IDValue: func(p *model.Profile) any { return p.ID },
		Scan:    scanProfile,
	}
	return r
}

var _ domainrepo.ProfileRepository = (*ProfileRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the
// adapter joins a caller's transaction without ever opening one itself
// (Constitution I).
func (r *ProfileRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// ByID returns the account's profile, reporting domainerr.ErrUserNotFound when
// no account carries the identifier.
func (r *ProfileRepository) ByID(ctx context.Context, id uuid.UUID) (*model.Profile, error) {
	return r.FindByID(ctx, id)
}

// Save writes the six profile columns this module owns.
//
// Email, role, password hash and status belong to the auth module and are never
// named here, so this module cannot overwrite them even by mistake. The four
// avatar columns are written in one statement, which is what keeps them all NULL
// or all populated alongside the storage-level CHECK constraint (FR-016).
//
// Every value is bound as a query parameter and the statement is a constant, so
// no caller-supplied text can reach the SQL (Constitution V).
func (r *ProfileRepository) Save(ctx context.Context, profile *model.Profile) error {
	avatar, err := avatarColumns(profile.Avatar)
	if err != nil {
		return err
	}
	const query = `
		UPDATE users
		SET display_name      = $2,
		    phone             = $3,
		    avatar_public_id  = $4,
		    avatar_secure_url = $5,
		    avatar_width      = $6,
		    avatar_height     = $7,
		    updated_at        = now()
		WHERE id = $1`

	tag, err := r.querier(ctx).Exec(ctx, query,
		profile.ID,
		profile.DisplayName,
		profile.Phone,
		avatar.publicID,
		avatar.secureURL,
		avatar.width,
		avatar.height,
	)
	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	// A profile is a column group on a row the auth module owns, so the row
	// disappearing is not something a customer can cause; reporting it keeps a
	// silent no-op from looking like a successful update.
	if tag.RowsAffected() == 0 {
		return domainerr.ErrUserNotFound
	}
	return nil
}

// avatarValues holds the four stored avatar values as nullable query arguments.
type avatarValues struct {
	publicID  *string
	secureURL *string
	width     *int
	height    *int
}

// avatarColumns maps the entity's single reference onto the four stored columns.
// A nil reference writes four NULLs, which is how "no photo" is stored; a
// non-nil one must be complete, because a half-written avatar is not a state the
// client could render (FR-016).
func avatarColumns(ref *model.AvatarReference) (avatarValues, error) {
	if ref == nil {
		return avatarValues{}, nil
	}
	if !ref.IsComplete() {
		return avatarValues{}, domainerr.ErrIncompleteAvatar
	}
	return avatarValues{
		publicID:  &ref.PublicID,
		secureURL: &ref.URL,
		width:     &ref.Width,
		height:    &ref.Height,
	}, nil
}

// scanProfile reads one row of the projection declared in NewProfileRepository.
// The number of destinations must match that projection exactly.
func scanProfile(row baserepo.Row) (*model.Profile, error) {
	var (
		profile   model.Profile
		role      string
		display   *string
		phone     *string
		publicID  *string
		secureURL *string
		width     *int
		height    *int
	)
	err := row.Scan(&profile.ID, &profile.Email, &role, &display, &phone,
		&publicID, &secureURL, &width, &height)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan profile: %w", err)
	}
	profile.Role = access.Role(role)
	// A display name that was never set is NULL in the row and empty in the
	// model: the contract reports an unset name as an empty string, and the two
	// round-trip through this adapter unchanged.
	if display != nil {
		profile.DisplayName = *display
	}
	profile.Phone = phone
	profile.Avatar = avatarReference(publicID, secureURL, width, height)
	return &profile, nil
}

// avatarReference rebuilds the stored reference. All four columns move together,
// so any missing value means the customer has no photo rather than a broken one.
func avatarReference(publicID, secureURL *string, width, height *int) *model.AvatarReference {
	if publicID == nil || secureURL == nil || width == nil || height == nil {
		return nil
	}
	return &model.AvatarReference{
		PublicID: *publicID,
		URL:      *secureURL,
		Width:    *width,
		Height:   *height,
	}
}
