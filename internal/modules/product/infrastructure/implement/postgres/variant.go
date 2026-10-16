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

// variantTable is the table this adapter owns. Every query here reads or writes
// only product_variants; the parent product is joined for the facts read, never
// written (specs/015-product-sku-model data-model.md — Table product_variants).
const variantTable = "product_variants"

// The constraint names that carry a meaning this adapter has to translate.
// Attribution is by the constraint name PostgreSQL reports, never by the text of
// its message, because the message is localized and free to change while the
// constraint name is part of the schema (FR-011, FR-012).
const (
	// constraintVariantSKUKey is the unique index on the trimmed, case-folded
	// SKU (FR-011).
	constraintVariantSKUKey = "product_variants_sku_key"
	// constraintVariantProductFK is the cascading foreign key to products
	// (FR-012).
	constraintVariantProductFK = "product_variants_product_fk"
)

// variantColumns is the projection every variant read selects. It is the variant
// response columns of data-model.md and matches the scan arity of scanVariant.
// There is deliberately no `SELECT *` fallback: a column added to the physical
// table later cannot change a scan's arity. The identifier is read but never
// named in an UPDATE, so no code path can rewrite it (FR-003).
var variantColumns = []string{
	"id",
	"product_id",
	"name",
	"sku",
	"price_amount",
	"image_public_id",
	"image_secure_url",
	"image_width",
	"image_height",
	"position",
	"sell_state",
	"created_at",
	"updated_at",
}

// variantFactsColumns is the projection behind the cross-module variant-facts
// read (research D6). The effective price is the variant's own amount when set,
// else the product's default, in the product's currency (FR-017, FR-024), so the
// consumer never has to re-derive the fallback. The `v.`/`p.` aliases are fixed
// because every caller wraps this fragment in its own FROM and WHERE.
const variantFactsColumns = `
	v.id,
	v.product_id,
	p.name,
	p.code,
	v.name,
	v.sku,
	v.sell_state,
	COALESCE(v.price_amount, p.price_amount),
	p.currency`

// VariantRepository persists a product's variants and reads them back.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, exactly as ProductRepository does. Every port method is
// stated explicitly: the generic Update cannot classify a SKU collision into the
// sentinel the use cases answer with, the ordered list is a projection over the
// product and the facts read is a join across two tables. The promoted generic
// reads (FindAll, Exists) are never used. No method opens a transaction: each
// joins the transaction the application put in the context (Constitution I).
type VariantRepository struct {
	baserepo.Base[model.Variant, uuid.UUID]
	pool *pgxpool.Pool
}

// NewVariantRepository creates the variant repository.
func NewVariantRepository(pool *pgxpool.Pool) *VariantRepository {
	r := &VariantRepository{pool: pool}
	r.Base = baserepo.Base[model.Variant, uuid.UUID]{
		Pool:     pool,
		Table:    variantTable,
		IDColumn: "id",
		Columns:  variantColumns,
		IDValue:  func(v *model.Variant) any { return v.ID },
		Scan:     scanVariant,
	}
	return r
}

var _ domainrepo.VariantRepository = (*VariantRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the
// adapter joins a caller's transaction without ever opening one itself
// (Constitution I).
func (r *VariantRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// projection returns the SELECT list for this adapter's own queries. An empty
// projection is a configuration error, reported before the database is touched.
func (r *VariantRepository) projection() (string, error) {
	if len(r.Columns) == 0 {
		return "", fmt.Errorf("repository %s: Columns is empty, it must list the projection matching Scan", r.Table)
	}
	return strings.Join(r.Columns, ", "), nil
}

// Create inserts a new variant.
//
// A SKU another variant already holds is reported as
// domainerr.ErrVariantSKUTaken so the use case can answer VARIANT_SKU_TAKEN
// (FR-011). A product identifier no row carries is reported as
// domainerr.ErrVariantNotFound, translated from the foreign-key refusal rather
// than checked first, so no code path can store a variant with no product.
func (r *VariantRepository) Create(ctx context.Context, variant *model.Variant) error {
	const query = `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount,
			 image_public_id, image_secure_url, image_width, image_height,
			 position, sell_state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	publicID, secureURL, width, height := variantImageValues(variant.Image)
	_, err := r.querier(ctx).Exec(ctx, query,
		variant.ID,
		variant.ProductID,
		variant.Name,
		variant.SKU,
		variant.PriceAmount,
		publicID,
		secureURL,
		width,
		height,
		variant.Position,
		string(variant.SellState),
		variant.CreatedAt,
		variant.UpdatedAt,
	)
	if err != nil {
		return classifyVariantError("create variant", err)
	}
	return nil
}

// Update writes the editable columns of the variant named by its identifier.
//
// It writes the image columns too: a variant's single image is set and cleared
// through its own media operations, which persist the changed entity through
// this method; the entity is the one place the image reference moves. It never
// names id or product_id in the SET list, so no code path can rewrite an
// identifier or move a variant to another product. The sell state is written by
// the same path, because the entity's transitions move it (FR-015).
//
// A collision with a *different* variant is reported as
// domainerr.ErrVariantSKUTaken; writing the SKU a variant already holds succeeds,
// because the unique index excludes the row being updated (FR-011). An unknown
// identifier is reported as domainerr.ErrVariantNotFound.
func (r *VariantRepository) Update(ctx context.Context, variant *model.Variant) error {
	const query = `
		UPDATE product_variants
		SET name             = $2,
		    sku              = $3,
		    price_amount     = $4,
		    image_public_id  = $5,
		    image_secure_url = $6,
		    image_width      = $7,
		    image_height     = $8,
		    position         = $9,
		    sell_state       = $10,
		    updated_at       = now()
		WHERE id = $1`
	publicID, secureURL, width, height := variantImageValues(variant.Image)
	tag, err := r.querier(ctx).Exec(ctx, query,
		variant.ID,
		variant.Name,
		variant.SKU,
		variant.PriceAmount,
		publicID,
		secureURL,
		width,
		height,
		variant.Position,
		string(variant.SellState),
	)
	if err != nil {
		return classifyVariantError("update variant", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrVariantNotFound
	}
	return nil
}

// Delete removes the variant row. Removal is a hard delete; the audit entry is
// what survives it. An unknown identifier is reported as
// domainerr.ErrVariantNotFound.
func (r *VariantRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM product_variants WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete variant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrVariantNotFound
	}
	return nil
}

// FindByID returns one variant, whatever its sell state, for the administrator
// surface (FR-010). An unknown identifier is reported as
// domainerr.ErrVariantNotFound.
func (r *VariantRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Variant, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT %s FROM product_variants WHERE id = $1`, list)
	return scanVariant(r.querier(ctx).QueryRow(ctx, query, id))
}

// ListByProduct returns the product's variants in display order (position, then
// created_at, then id) — the order product_variants_product_idx carries. A
// product with no variant answers an empty slice, never an error (FR-010).
func (r *VariantRepository) ListByProduct(ctx context.Context, productID uuid.UUID) ([]model.Variant, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		`SELECT %s FROM product_variants WHERE product_id = $1 ORDER BY position, created_at, id`, list)
	rows, err := r.querier(ctx).Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}
	defer rows.Close()

	out := make([]model.Variant, 0)
	for rows.Next() {
		variant, err := scanVariant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *variant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CountByProduct reports how many variants the product has. The application uses
// it to refuse removing the last variant (FR-012).
func (r *VariantRepository) CountByProduct(ctx context.Context, productID uuid.UUID) (int, error) {
	const query = `SELECT count(*) FROM product_variants WHERE product_id = $1`
	var total int
	if err := r.querier(ctx).QueryRow(ctx, query, productID).Scan(&total); err != nil {
		return 0, fmt.Errorf("count variants: %w", err)
	}
	return total, nil
}

// FindFactsByIDs returns the facts of every requested variant in one read. It is
// the read behind the cross-module variant-facts contract that the cart and order
// consume (research D6). An identifier no variant carries is absent from the
// result rather than an error, so the caller can tell a removed variant from an
// available one. OnSale is true exactly when the variant is ACTIVE, so the
// consumer never re-derives the rule (FR-007).
func (r *VariantRepository) FindFactsByIDs(ctx context.Context, ids []uuid.UUID) ([]domainrepo.VariantFacts, error) {
	if len(ids) == 0 {
		return []domainrepo.VariantFacts{}, nil
	}
	query := `SELECT` + variantFactsColumns + `
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1)
		ORDER BY v.position, v.created_at, v.id`
	rows, err := r.querier(ctx).Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("find variant facts: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.VariantFacts, 0, len(ids))
	for rows.Next() {
		var facts domainrepo.VariantFacts
		var sellState string
		if err := rows.Scan(
			&facts.VariantID,
			&facts.ProductID,
			&facts.ProductName,
			&facts.ProductCode,
			&facts.VariantName,
			&facts.SKU,
			&sellState,
			&facts.Price.Amount,
			&facts.Price.Currency,
		); err != nil {
			return nil, fmt.Errorf("scan variant facts: %w", err)
		}
		facts.OnSale = constant.SellState(sellState) == constant.SellStateActive
		out = append(out, facts)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanVariant reads one row of the projection declared in NewVariantRepository.
// The number of destinations must match that projection exactly.
//
// The four image columns are nullable and all-present-or-all-null at rest
// (product_variants_image_ck); a row without them rehydrates to a nil
// *VariantImage, which is a variant with no image, not a malformed one.
func scanVariant(row baserepo.Row) (*model.Variant, error) {
	var variant model.Variant
	var sellState string
	var publicID, secureURL *string
	var width, height *int
	err := row.Scan(
		&variant.ID,
		&variant.ProductID,
		&variant.Name,
		&variant.SKU,
		&variant.PriceAmount,
		&publicID,
		&secureURL,
		&width,
		&height,
		&variant.Position,
		&sellState,
		&variant.CreatedAt,
		&variant.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrVariantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan variant: %w", err)
	}
	variant.SellState = constant.SellState(sellState)
	variant.Image = variantImage(publicID, secureURL, width, height)
	return &variant, nil
}

// variantImageValues flattens an optional variant image into the four nullable
// column values Create and Update write. A nil image writes four NULLs, which
// product_variants_image_ck accepts as "no image".
func variantImageValues(image *model.VariantImage) (publicID, secureURL, width, height any) {
	if image == nil {
		return nil, nil, nil, nil
	}
	return image.PublicID, image.URL, image.Width, image.Height
}

// variantImage rebuilds the optional image from the four nullable columns. A
// column missing from any of them yields no image: the storage check guarantees
// the four are present together, so this never hides a half-written reference.
func variantImage(publicID, secureURL *string, width, height *int) *model.VariantImage {
	if publicID == nil || secureURL == nil || width == nil || height == nil {
		return nil
	}
	return &model.VariantImage{
		PublicID: *publicID,
		URL:      *secureURL,
		Width:    *width,
		Height:   *height,
	}
}

// classifyVariantError labels a failed variant write and maps a constraint
// violation to the module sentinel that names the offending field. Attribution
// is by the constraint name PostgreSQL reports, never by its message text.
func classifyVariantError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch pgErr.Code {
	case uniqueViolationCode:
		if pgErr.ConstraintName == constraintVariantSKUKey {
			return domainerr.ErrVariantSKUTaken
		}
	case foreignKeyViolationCode:
		if pgErr.ConstraintName == constraintVariantProductFK {
			return domainerr.ErrVariantNotFound
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
