package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// variantGroupTable is the table this adapter owns. The group's own pictures live
// in product_variant_group_images and its options are rows of products; this
// adapter is the only code that reads or writes the group and its pictures.
const variantGroupTable = "product_variant_groups"

// The constraint names that carry a meaning this adapter has to translate.
// Attribution is by the constraint name PostgreSQL reports, never by its message
// text, because the message is localized and free to change while the constraint
// name is part of the schema (FR-019, FR-023, FR-025, research D3).
const (
	// constraintVariantGroupSlugKey is the unique index on the group slug
	// (FR-019).
	constraintVariantGroupSlugKey = "product_variant_groups_slug_key"
	// constraintVariantGroupCategoryFK is the restricting foreign key to
	// categories (FR-023).
	constraintVariantGroupCategoryFK = "product_variant_groups_category_fk"
	// constraintVariantGroupImageGroupFK is the cascading foreign key from a
	// group picture to its group (FR-018).
	constraintVariantGroupImageGroupFK = "product_variant_group_images_group_fk"
)

// variantGroupColumns is the projection every group read selects. It matches the
// scan arity of scanVariantGroup; there is deliberately no `SELECT *` fallback,
// so a column added to the physical table later cannot change a scan's arity.
var variantGroupColumns = []string{
	"id",
	"name",
	"slug",
	"description",
	"category_id",
	"position",
	"option_name",
	"created_at",
	"updated_at",
}

// variantGroupImageColumns is the projection every group-picture read selects.
// public_id is read because the use case needs it to release the stored asset
// (FR-018); it is never part of a public response.
var variantGroupImageColumns = []string{
	"id",
	"group_id",
	"public_id",
	"secure_url",
	"width",
	"height",
	"position",
	"is_primary",
	"created_at",
}

// The shared option projection. An option is an ordinary products row, so the
// projection carries the option's own label, price, currency, preorder flag,
// position, sell state and primary picture; nothing is inherited from the group
// or its siblings (FR-016, FR-018). The `p.` alias is fixed because every caller
// wraps this fragment in its own predicate over products.
const groupOptionSelect = `SELECT
	p.id,
	p.name,
	p.slug,
	COALESCE(p.variant_label, '') AS variant_label,
	p.price_amount,
	p.currency,
	p.position,
	p.sell_state,
	p.is_preorder,
	COALESCE((
		SELECT i.secure_url FROM product_images i
		WHERE i.product_id = p.id AND i.is_primary
		ORDER BY i.position, i.id
		LIMIT 1
	), '') AS image_url
	FROM products p`

// The option predicates. The visibility half mirrors the product adapter exactly
// — an on-sale option, or an announced non-retired pre-order, in a category the
// caller reports visible — so "what a customer sees" keeps one definition
// (FR-002, FR-027, research D7).
const (
	// groupOptionsByGroup is every option of one group, for the administrator
	// read (FR-013).
	groupOptionsByGroup = ` WHERE p.variant_group_id = $1`
	// groupVisibleOptionsByGroup is the visible options of one group, decided by
	// the option's own state. The category half is composed by the caller that
	// already holds the visible set.
	groupVisibleOptionsByGroup = ` WHERE p.variant_group_id = $1
		AND (p.sell_state = 'ACTIVE' OR (p.is_preorder AND p.sell_state <> 'DISCONTINUED'))`
	// groupVisibleOptionsByCategory is the fully-visible options of one group:
	// the option's category is in the visible set AND it is on sale or an
	// announced non-retired pre-order (FR-002, FR-027).
	groupVisibleOptionsByCategory = ` WHERE p.variant_group_id = $1
		AND p.category_id = ANY($2)
		AND (p.sell_state = 'ACTIVE' OR (p.is_preorder AND p.sell_state <> 'DISCONTINUED'))`
)

// VariantGroupRepository persists variant groups, their own pictures and their
// options.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, exactly as ProductRepository does. It never calls the
// generic FindAll (the administrator list needs the FR-009 order and the picture
// facts) and never opens a transaction: the application layer owns the boundary
// through the UnitOfWork port, and this adapter joins a caller's transaction
// (Constitution I).
type VariantGroupRepository struct {
	baserepo.Base[model.VariantGroup, uuid.UUID]
	pool *pgxpool.Pool
}

// NewVariantGroupRepository creates the variant-group repository.
func NewVariantGroupRepository(pool *pgxpool.Pool) *VariantGroupRepository {
	r := &VariantGroupRepository{pool: pool}
	r.Base = baserepo.Base[model.VariantGroup, uuid.UUID]{
		Pool:     pool,
		Table:    variantGroupTable,
		IDColumn: "id",
		Columns:  variantGroupColumns,
		IDValue:  func(g *model.VariantGroup) any { return g.ID },
		Scan:     scanVariantGroup,
	}
	return r
}

var _ domainrepo.VariantGroupRepository = (*VariantGroupRepository)(nil)

// querier resolves the transaction-aware querier, falling back to the pool, so
// the adapter joins a caller's transaction without ever opening one itself.
func (r *VariantGroupRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// Create inserts a new variant group.
//
// A slug another group already holds is reported as
// domainerr.ErrVariantGroupSlugTaken so the use case can answer
// VARIANT_GROUP_SLUG_TAKEN (FR-019). A category identifier no row carries is
// reported as domainerr.ErrProductInvalid naming model.FieldCategoryID
// (FR-023).
func (r *VariantGroupRepository) Create(ctx context.Context, group *model.VariantGroup) error {
	const query = `
		INSERT INTO product_variant_groups
			(id, name, slug, description, category_id, position, option_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.querier(ctx).Exec(ctx, query,
		group.ID,
		group.Name,
		group.Slug,
		group.Description,
		group.CategoryID,
		group.Position,
		group.OptionName,
		group.CreatedAt,
		group.UpdatedAt,
	)
	if err != nil {
		return classifyVariantGroupError("create variant group", err)
	}
	return nil
}

// Update writes the editable columns of the group named by its identifier.
//
// It never names id in the SET list, so no code path can rewrite an identifier
// after creation (FR-009). A collision with a *different* group is reported as
// domainerr.ErrVariantGroupSlugTaken; writing the slug a group already holds
// succeeds, because the unique index excludes the row being updated (FR-019). An
// unknown identifier is reported as domainerr.ErrVariantGroupNotFound.
func (r *VariantGroupRepository) Update(ctx context.Context, group *model.VariantGroup) error {
	const query = `
		UPDATE product_variant_groups
		SET name        = $2,
		    slug        = $3,
		    description = $4,
		    category_id = $5,
		    position    = $6,
		    option_name = $7,
		    updated_at  = now()
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query,
		group.ID,
		group.Name,
		group.Slug,
		group.Description,
		group.CategoryID,
		group.Position,
		group.OptionName,
	)
	if err != nil {
		return classifyVariantGroupError("update variant group", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrVariantGroupNotFound
	}
	return nil
}

// Delete removes the group row.
//
// Removal is refused while options remain: the products_variant_group_fk foreign
// key is ON DELETE RESTRICT, so the database refuses the delete and this maps it
// to domainerr.ErrVariantGroupHasOptions (FR-012, FR-025, research D10). On
// success the group's own pictures cascade away and no product is touched. An
// unknown identifier is reported as domainerr.ErrVariantGroupNotFound.
func (r *VariantGroupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM product_variant_groups WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id)
	if err != nil {
		return classifyVariantGroupError("delete variant group", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrVariantGroupNotFound
	}
	return nil
}

// FindByID returns one group with its pictures and its options, including the
// options a customer cannot see, so the operator can manage a group that is not
// currently visible (FR-013). An unknown identifier is reported as
// domainerr.ErrVariantGroupNotFound.
func (r *VariantGroupRepository) FindByID(ctx context.Context, id uuid.UUID) (*domainrepo.VariantGroupView, error) {
	group, err := r.findGroup(ctx, `WHERE g.id = $1`, id)
	if err != nil {
		return nil, err
	}
	images, err := r.ListImages(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	options, err := r.ListOptions(ctx, group.ID, true)
	if err != nil {
		return nil, err
	}
	return &domainrepo.VariantGroupView{Group: *group, Images: images, Options: options}, nil
}

// FindVisibleGroupBySlug returns one group a customer may see, with its pictures
// and its visible options.
//
// The whole visibility predicate runs in this query — the group's category is in
// visibleCategoryIDs AND at least one of its options is fully visible — so a
// group that is unknown, hidden by its category, has no visible option or was
// removed answers the same domainerr.ErrVariantGroupNotFound and the response
// never confirms that a hidden group exists (FR-006, FR-008, research D7). An
// empty visible set answers not-found without touching the database.
func (r *VariantGroupRepository) FindVisibleGroupBySlug(ctx context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*domainrepo.VariantGroupView, error) {
	if len(visibleCategoryIDs) == 0 {
		return nil, domainerr.ErrVariantGroupNotFound
	}
	const query = `
		SELECT ` + variantGroupQualifiedColumns + `
		FROM product_variant_groups g
		WHERE g.slug = $1
		  AND g.category_id = ANY($2)
		  AND EXISTS (
			SELECT 1 FROM products p
			WHERE p.variant_group_id = g.id
			  AND p.category_id = ANY($2)
			  AND (p.sell_state = 'ACTIVE' OR (p.is_preorder AND p.sell_state <> 'DISCONTINUED'))
		  )`
	group, err := scanVariantGroup(r.querier(ctx).QueryRow(ctx, query, slug, visibleCategoryIDs))
	if err != nil {
		return nil, err
	}
	images, err := r.ListImages(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	options, err := listGroupOptionItems(ctx, r.querier(ctx), groupVisibleOptionsByCategory, group.ID, visibleCategoryIDs)
	if err != nil {
		return nil, err
	}
	return &domainrepo.VariantGroupView{Group: *group, Images: images, Options: options}, nil
}

// ListAll returns a page of every group, including the ones withheld from
// customers, plus the total number of them, in the FR-009 order (position, then
// created_at, then id). It serves the administrator list (FR-013).
func (r *VariantGroupRepository) ListAll(ctx context.Context, page, pageSize int) ([]domainrepo.VariantGroupListItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	var total int64
	const countQuery = `SELECT count(*) FROM product_variant_groups`
	if err := r.querier(ctx).QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count variant groups: %w", err)
	}

	const pageQuery = `
		SELECT ` + variantGroupQualifiedColumns + `,
			(SELECT count(*) FROM product_variant_group_images i WHERE i.group_id = g.id) AS image_count,
			COALESCE((
				SELECT i.secure_url FROM product_variant_group_images i
				WHERE i.group_id = g.id AND i.is_primary
				ORDER BY i.position, i.id
				LIMIT 1
			), '') AS image_url
		FROM product_variant_groups g
		ORDER BY g.position, g.created_at, g.id
		LIMIT $1 OFFSET $2`
	rows, err := r.querier(ctx).Query(ctx, pageQuery, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list variant groups: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.VariantGroupListItem, 0, pageSize)
	for rows.Next() {
		item, err := scanVariantGroupListItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListImages returns the group's own pictures in display order. A nil slice is
// valid and means the group has no picture, which is a valid group (FR-018).
func (r *VariantGroupRepository) ListImages(ctx context.Context, groupID uuid.UUID) ([]model.VariantGroupImage, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM product_variant_group_images WHERE group_id = $1 ORDER BY position, id`,
		strings.Join(variantGroupImageColumns, ", "))
	rows, err := r.querier(ctx).Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("list variant group images: %w", err)
	}
	defer rows.Close()

	out := make([]model.VariantGroupImage, 0)
	for rows.Next() {
		image, err := scanVariantGroupImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// FindImage returns one picture that belongs to the group, including the stored
// reference the use case needs to release the asset. A picture that does not
// belong to the group, or a group that does not exist, answers
// domainerr.ErrVariantGroupNotFound, so the two are indistinguishable (FR-018).
func (r *VariantGroupRepository) FindImage(ctx context.Context, groupID, imageID uuid.UUID) (*model.VariantGroupImage, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM product_variant_group_images WHERE group_id = $1 AND id = $2`,
		strings.Join(variantGroupImageColumns, ", "))
	return scanVariantGroupImage(r.querier(ctx).QueryRow(ctx, query, groupID, imageID))
}

// AddImage inserts a picture. An unknown group is reported as
// domainerr.ErrVariantGroupNotFound, translated from the foreign-key refusal
// rather than checked first, so no code path can store a picture with no group.
func (r *VariantGroupRepository) AddImage(ctx context.Context, image *model.VariantGroupImage) error {
	const query = `
		INSERT INTO product_variant_group_images
			(id, group_id, public_id, secure_url, width, height, position, is_primary, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.querier(ctx).Exec(ctx, query,
		image.ID,
		image.GroupID,
		image.PublicID,
		image.URL,
		image.Width,
		image.Height,
		image.Position,
		image.IsPrimary,
		image.CreatedAt,
	)
	if err != nil {
		return classifyVariantGroupError("add variant group image", err)
	}
	return nil
}

// DeleteImage removes one picture that belongs to the group. A picture that does
// not belong to the group, or a group that does not exist, answers
// domainerr.ErrVariantGroupNotFound.
func (r *VariantGroupRepository) DeleteImage(ctx context.Context, groupID, imageID uuid.UUID) error {
	const query = `DELETE FROM product_variant_group_images WHERE group_id = $1 AND id = $2`
	tag, err := r.querier(ctx).Exec(ctx, query, groupID, imageID)
	if err != nil {
		return fmt.Errorf("delete variant group image: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrVariantGroupNotFound
	}
	return nil
}

// SetPrimaryImage makes one picture the group's main one, clearing any other so
// the partial unique index is never violated. It is idempotent: naming the
// picture that is already primary is a no-op, not an error (FR-018, research
// D2). A picture that does not belong to the group, or a group that does not
// exist, answers domainerr.ErrVariantGroupNotFound.
//
// The membership check comes first so the single UPDATE cannot clear every
// picture's flag for a picture that does not exist. The demote/promote split
// mirrors the product adapter: PostgreSQL checks the partial unique index as each
// row is written, so clearing before promoting is what keeps two primaries from
// briefly coexisting.
func (r *VariantGroupRepository) SetPrimaryImage(ctx context.Context, groupID, imageID uuid.UUID) error {
	const belongs = `SELECT 1 FROM product_variant_group_images WHERE group_id = $1 AND id = $2`
	var one int
	err := r.querier(ctx).QueryRow(ctx, belongs, groupID, imageID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.ErrVariantGroupNotFound
	}
	if err != nil {
		return fmt.Errorf("find variant group image: %w", err)
	}

	const demote = `UPDATE product_variant_group_images SET is_primary = false WHERE group_id = $1 AND id <> $2 AND is_primary`
	if _, err := r.querier(ctx).Exec(ctx, demote, groupID, imageID); err != nil {
		return fmt.Errorf("clear variant group primary image: %w", err)
	}
	const promote = `UPDATE product_variant_group_images SET is_primary = true WHERE group_id = $1 AND id = $2`
	if _, err := r.querier(ctx).Exec(ctx, promote, groupID, imageID); err != nil {
		return fmt.Errorf("set variant group primary image: %w", err)
	}
	return nil
}

// CountImages reports how many pictures the group has. The caller checks the
// ten-picture ceiling with it before calling the media provider, so a refused
// eleventh upload leaves no orphaned asset behind (FR-018).
func (r *VariantGroupRepository) CountImages(ctx context.Context, groupID uuid.UUID) (int, error) {
	const query = `SELECT count(*) FROM product_variant_group_images WHERE group_id = $1`
	var total int
	if err := r.querier(ctx).QueryRow(ctx, query, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("count variant group images: %w", err)
	}
	return total, nil
}

// ListOptions returns the group's options in display order (position, then
// created_at, then id).
//
// includeHidden selects the administrator variant (every option, so the operator
// sees the full group — FR-013) or the public one (visible options decided by the
// option's own state). The public group read composes the category half of the
// visibility predicate with the visible set, because this read is reached only
// after the group's own visibility has been established (research D7).
func (r *VariantGroupRepository) ListOptions(ctx context.Context, groupID uuid.UUID, includeHidden bool) ([]domainrepo.GroupOptionItem, error) {
	predicate := groupVisibleOptionsByGroup
	if includeHidden {
		predicate = groupOptionsByGroup
	}
	return listGroupOptionItems(ctx, r.querier(ctx), predicate, groupID)
}

// findGroup reads one group row with the caller's predicate.
func (r *VariantGroupRepository) findGroup(ctx context.Context, where string, args ...any) (*model.VariantGroup, error) {
	query := `SELECT ` + variantGroupQualifiedColumns + ` FROM product_variant_groups g ` + where
	return scanVariantGroup(r.querier(ctx).QueryRow(ctx, query, args...))
}

// variantGroupQualifiedColumns is the group projection with the `g.` alias every
// group query uses, so a join or a correlated subquery can never make a column
// name ambiguous.
const variantGroupQualifiedColumns = `g.id, g.name, g.slug, g.description, g.category_id, g.position, g.option_name, g.created_at, g.updated_at`

// listGroupOptionItems runs the shared option projection with the caller's
// predicate, in the FR-009 order. Every caller wraps it in its own predicate over
// products, so the visibility rule is one definition shared by the group and the
// product adapters (FR-002, FR-016, research D7).
func listGroupOptionItems(ctx context.Context, q database.Querier, predicate string, args ...any) ([]domainrepo.GroupOptionItem, error) {
	query := groupOptionSelect + predicate + ` ORDER BY p.position, p.created_at, p.id`
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list group options: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.GroupOptionItem, 0)
	for rows.Next() {
		var item domainrepo.GroupOptionItem
		var label string
		var sellState string
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Slug,
			&label,
			&item.Price.Amount,
			&item.Price.Currency,
			&item.Position,
			&sellState,
			&item.IsPreorder,
			&item.ImageURL,
		); err != nil {
			return nil, fmt.Errorf("scan group option: %w", err)
		}
		item.Label = label
		item.SellState = constant.SellState(sellState)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanVariantGroup reads one row of the group projection declared above. The
// number of destinations must match that projection exactly.
func scanVariantGroup(row baserepo.Row) (*model.VariantGroup, error) {
	var group model.VariantGroup
	err := row.Scan(
		&group.ID,
		&group.Name,
		&group.Slug,
		&group.Description,
		&group.CategoryID,
		&group.Position,
		&group.OptionName,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrVariantGroupNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan variant group: %w", err)
	}
	return &group, nil
}

// scanVariantGroupListItem reads one row of the administrator list projection:
// the nine group columns followed by the image count and the main picture's
// link.
func scanVariantGroupListItem(row baserepo.Row) (domainrepo.VariantGroupListItem, error) {
	var item domainrepo.VariantGroupListItem
	var imageURL string
	err := row.Scan(
		&item.Group.ID,
		&item.Group.Name,
		&item.Group.Slug,
		&item.Group.Description,
		&item.Group.CategoryID,
		&item.Group.Position,
		&item.Group.OptionName,
		&item.Group.CreatedAt,
		&item.Group.UpdatedAt,
		&item.ImageCount,
		&imageURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domainerr.ErrVariantGroupNotFound
	}
	if err != nil {
		return item, fmt.Errorf("scan variant group list item: %w", err)
	}
	item.ImageURL = imageURL
	return item, nil
}

// scanVariantGroupImage reads one row of variantGroupImageColumns. It translates
// no-rows to the group not-found sentinel, because a picture addressed through a
// group is indistinguishable from that group when it is missing (FR-018).
func scanVariantGroupImage(row baserepo.Row) (*model.VariantGroupImage, error) {
	var image model.VariantGroupImage
	err := row.Scan(
		&image.ID,
		&image.GroupID,
		&image.PublicID,
		&image.URL,
		&image.Width,
		&image.Height,
		&image.Position,
		&image.IsPrimary,
		&image.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrVariantGroupNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan variant group image: %w", err)
	}
	return &image, nil
}

// classifyVariantGroupError labels a failed group write and maps a constraint
// violation to the module sentinel that names the offending field.
func classifyVariantGroupError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch pgErr.Code {
	case uniqueViolationCode:
		if pgErr.ConstraintName == constraintVariantGroupSlugKey {
			return domainerr.ErrVariantGroupSlugTaken
		}
	case foreignKeyViolationCode:
		switch pgErr.ConstraintName {
		case constraintVariantGroupCategoryFK:
			return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
		case constraintVariantGroupImageGroupFK:
			return domainerr.ErrVariantGroupNotFound
		case constraintProductVariantGroupFK:
			// The products table refuses the delete of a group that still has
			// options, so this is the group-with-options refusal (FR-025).
			return domainerr.ErrVariantGroupHasOptions
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
