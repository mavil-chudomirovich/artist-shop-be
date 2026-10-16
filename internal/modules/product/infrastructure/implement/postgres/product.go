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

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	baserepo "github.com/mavil-chudomirovich/artist-shop-be/internal/share/repository"
)

// table is the one table this adapter owns. The pictures live in product_images
// and the variants in product_variants; this adapter is the only code that reads
// or writes all three.
const table = "products"

// The constraint names that carry a meaning this adapter has to translate. A
// collision is attributed by the constraint name PostgreSQL reports, never by the
// text of its message, because the message is localized and free to change while
// the constraint name is part of the schema (FR-011).
const (
	constraintCodeKey        = "products_code_key"
	constraintCategoryFK     = "products_category_fk"
	constraintImageProductFK = "product_images_product_fk"
)

// SQLSTATE codes the adapter distinguishes.
const (
	// uniqueViolationCode is reported for a unique-index rejection (23505).
	uniqueViolationCode = "23505"
	// foreignKeyViolationCode is reported for a foreign-key rejection (23503).
	foreignKeyViolationCode = "23503"
)

// productColumns is the projection every product read selects.
//
// It is deliberately the product response columns of data-model.md and matches
// the scan arity of scanProduct. The code is both the operator-facing code and the
// product's public link segment, so it is the only link value the projection
// carries. The identifier is read but never named in an UPDATE, so no code path
// can rewrite it. Feature 014's attachment columns and
// the product-level sell state / pre-order / combo-set flags are gone: the sell
// state lives on the variant (research D2).
var productColumns = []string{
	"id",
	"name",
	"code",
	"description",
	"price_amount",
	"currency",
	"category_id",
	"position",
	"is_visible",
	"created_at",
	"updated_at",
}

// pictureColumns is the projection every picture read selects. public_id is read
// because the use case needs it to release the stored asset (FR-018); it is
// never part of a public response.
var pictureColumns = []string{
	"id",
	"product_id",
	"public_id",
	"secure_url",
	"width",
	"height",
	"position",
	"is_primary",
	"created_at",
}

// catalogueColumns is the outer SELECT list over the catalogue CTE. It matches
// the scan arity of scanCatalogueEntry; category_id is read by the optional
// filter only and is not scanned.
const catalogueColumns = "id, name, code, price_amount, currency, image_url, position, created_at"

// ProductRepository persists products, their pictures and — through the shared
// variant queries — their variants.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. The projection, not the physical table,
// drives the scan arity: a later `ALTER TABLE products ADD COLUMN` cannot break
// this adapter.
//
// It never calls the generic FindAll: the public reads carry the visibility
// predicate and the ordering tie-break, which the generic listing cannot express.
// Every write is a single statement except SetPrimaryPicture, which runs inside
// the caller's transaction; the adapter never opens one itself (Constitution I).
type ProductRepository struct {
	baserepo.Base[model.Product, uuid.UUID]
	pool *pgxpool.Pool
}

// NewProductRepository creates the product repository.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	r := &ProductRepository{pool: pool}
	r.Base = baserepo.Base[model.Product, uuid.UUID]{
		Pool:     pool,
		Table:    table,
		IDColumn: "id",
		Columns:  productColumns,
		IDValue:  func(p *model.Product) any { return p.ID },
		Scan:     scanProduct,
	}
	return r
}

var _ domainrepo.ProductRepository = (*ProductRepository)(nil)

// querier resolves the transaction-aware querier. It reads the transaction the
// application layer put in the context and falls back to the pool, so the
// adapter joins a caller's transaction without ever opening one itself
// (Constitution I).
func (r *ProductRepository) querier(ctx context.Context) database.Querier {
	return database.FromContext(ctx, r.pool)
}

// projection returns the SELECT list for this adapter's own queries.
//
// An empty projection is a configuration error, reported before the database is
// touched, exactly as the generic read paths do. There is deliberately no
// `SELECT *` fallback anywhere in this adapter.
func (r *ProductRepository) projection() (string, error) {
	if len(r.Columns) == 0 {
		return "", fmt.Errorf("repository %s: Columns is empty, it must list the projection matching Scan", r.Table)
	}
	return strings.Join(r.Columns, ", "), nil
}

// Create inserts a new product.
//
// A code another product already holds is reported as
// domainerr.ErrProductCodeTaken (PRODUCT_CODE_TAKEN). A
// category identifier no row carries is reported as domainerr.ErrProductInvalid
// naming model.FieldCategoryID.
func (r *ProductRepository) Create(ctx context.Context, product *model.Product) error {
	const query = `
		INSERT INTO products
			(id, name, code, description, price_amount, currency,
			 category_id, position, is_visible, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.querier(ctx).Exec(ctx, query,
		product.ID,
		product.Name,
		product.Code,
		product.Description,
		product.Price.Amount,
		product.Price.Currency,
		product.CategoryID,
		product.Position,
		product.IsVisible,
		product.CreatedAt,
		product.UpdatedAt,
	)
	if err != nil {
		return classifyWriteError("create product", err)
	}
	return nil
}

// Update writes the editable columns of the product named by its identifier.
//
// It never names id in the SET list, so no code path can rewrite an identifier
// after creation. The variants are managed through their own operations, so this
// statement never touches product_variants.
//
// A collision with a *different* product is reported as
// domainerr.ErrProductCodeTaken; writing the
// value the product already holds succeeds, because the unique index excludes the
// row being updated, so a product is never a duplicate of itself (FR-011). A
// category identifier no row carries is reported as domainerr.ErrProductInvalid
// naming model.FieldCategoryID. An unknown identifier is reported as
// domainerr.ErrProductNotFound.
func (r *ProductRepository) Update(ctx context.Context, product *model.Product) error {
	const query = `
		UPDATE products
		SET name            = $2,
		    code            = $3,
		    description     = $4,
		    price_amount    = $5,
		    currency        = $6,
		    category_id     = $7,
		    position        = $8,
		    is_visible      = $9,
		    updated_at      = now()
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query,
		product.ID,
		product.Name,
		product.Code,
		product.Description,
		product.Price.Amount,
		product.Price.Currency,
		product.CategoryID,
		product.Position,
		product.IsVisible,
	)
	if err != nil {
		return classifyWriteError("update product", err)
	}
	// A product the caller believed existed but that is already gone is not
	// something a client can cause; reporting it keeps a silent no-op from
	// looking like a successful write.
	if tag.RowsAffected() == 0 {
		return domainerr.ErrProductNotFound
	}
	return nil
}

// Delete removes the row. Removal is a hard delete; the audit entry is what
// survives it, and the product's pictures and variants are removed with it
// through the cascading foreign keys (FR-012). An unknown identifier is reported
// as domainerr.ErrProductNotFound.
func (r *ProductRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM products WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrProductNotFound
	}
	return nil
}

// FindByIDs returns every requested product that exists, without pictures or
// variants, in one read. An identifier no product carries is simply absent from
// the result, which is not an error. It joins the caller's transaction and never
// opens one itself.
func (r *ProductRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Product, error) {
	if len(ids) == 0 {
		return []model.Product{}, nil
	}
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT %s FROM products WHERE id = ANY($1)`, list)
	rows, err := r.querier(ctx).Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("find products by ids: %w", err)
	}
	defer rows.Close()

	out := make([]model.Product, 0, len(ids))
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *product)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// VariantIDsByCategories returns the identifiers of every variant whose product
// belongs to one of the given categories, so a category hide guard can resolve
// "my variants" without reading another module's tables (specs/016-hide-guard
// research D2, data-model.md).
//
// It is a single bounded statement however many categories are passed — the
// variants are read through their parent products — so hiding a category of
// hundreds of variants stays bounded (spec SC-004). An empty input answers an
// empty slice without touching the database. It joins the caller's transaction.
func (r *ProductRepository) VariantIDsByCategories(ctx context.Context, categoryIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(categoryIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	const query = `
		SELECT id FROM product_variants
		WHERE product_id IN (SELECT id FROM products WHERE category_id = ANY($1))
		ORDER BY id`
	rows, err := r.querier(ctx).Query(ctx, query, categoryIDs)
	if err != nil {
		return nil, fmt.Errorf("find variant ids by categories: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan variant id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// catalogueCTE is the one-statement public catalogue read: exactly one row per
// product, with the product's variants collapsed into it (FR-001).
//
// The whole predicate runs here, not in Go (FR-004): a product is visible when
// its own flag is on AND its category is in $1 AND at least one of its variants
// is on sale. The price is
// the lowest effective price among the on-sale variants — COALESCE(v.price_amount,
// p.price_amount) is the fallback a variant with no own price takes (FR-017) — so
// an out-of-stock variant's cheaper price never headlines the product (FR-002).
// The image is the default variant's image (the first by position, created_at,
// id) falling back to the product's primary gallery picture (FR-002). The outer
// query orders by position, created_at, id and pages, and a COUNT(*) over the
// same CTE gives the total, so pagination and the total stay correct.
const catalogueCTE = `
	WITH entries AS (
		SELECT
			p.id AS id,
			p.name AS name,
			p.code AS code,
			(SELECT MIN(COALESCE(v.price_amount, p.price_amount))
				FROM product_variants v
				WHERE v.product_id = p.id AND v.sell_state = 'ACTIVE') AS price_amount,
			p.currency AS currency,
		COALESCE(
			(SELECT v.image_secure_url FROM product_variants v
				WHERE v.product_id = p.id
				ORDER BY v.position, v.created_at, v.id
				LIMIT 1),
			(SELECT i.secure_url FROM product_images i
				WHERE i.product_id = p.id AND i.is_primary
				ORDER BY i.position, i.id
				LIMIT 1),
			'') AS image_url,
			p.category_id AS category_id,
			p.position AS position,
			p.created_at AS created_at
		FROM products p
		WHERE p.category_id = ANY($1)
		  AND p.is_visible
		  AND EXISTS (
			SELECT 1 FROM product_variants v
			WHERE v.product_id = p.id AND v.sell_state = 'ACTIVE'
		  )
	)`

// ListCatalogue returns one page of the public catalogue — exactly one entry per
// product — plus the total number of entries (FR-001, FR-004).
//
// Everything runs in one statement (catalogueCTE): the visibility predicate, the
// derived price and image, the ordering and the count, so a page and its total
// can never disagree. A product whose own flag is off is absent however many
// on-sale variants it holds (specs/016-hide-guard FR-006). An empty visible set
// answers an empty page without touching the database.
func (r *ProductRepository) ListCatalogue(ctx context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.CatalogueEntry, int64, error) {
	if len(query.VisibleCategoryIDs) == 0 {
		return []domainrepo.CatalogueEntry{}, 0, nil
	}
	page, pageSize := query.Page, query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	// The optional category filter narrows the entries: category_id is the one
	// column the outer projection exposes for it.
	args := []any{query.VisibleCategoryIDs}
	where := ""
	if query.CategoryID != nil {
		args = append(args, *query.CategoryID)
		where = fmt.Sprintf(" WHERE category_id = $%d", len(args))
	}

	var total int64
	countQuery := catalogueCTE + " SELECT count(*) FROM entries" + where
	if err := r.querier(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count catalogue: %w", err)
	}

	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	pageArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	pageQuery := fmt.Sprintf("%s SELECT %s FROM entries%s ORDER BY position, created_at, id LIMIT $%d OFFSET $%d",
		catalogueCTE, catalogueColumns, where, limitPos, offsetPos)
	rows, err := r.querier(ctx).Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list catalogue: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.CatalogueEntry, 0, pageSize)
	for rows.Next() {
		entry, err := scanCatalogueEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListAll returns a page of every product, including the ones withheld from
// customers, plus the total number of them, in the same order (position, then
// created_at, then id). It serves the administrator list (FR-010).
func (r *ProductRepository) ListAll(ctx context.Context, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	return r.list(ctx, "", nil, page, pageSize)
}

// FindVisibleByCode returns one product a customer may see, with its pictures,
// its visible variants and its card summary, addressing it by the code a
// customer-facing link is built from (FR-003).
//
// The same visibility predicate as ListCatalogue runs in this query — the
// product's own flag is on, the product's category is in visibleCategoryIDs
// AND at least one variant is on sale — so a product that is hidden (flag off
// or no on-sale variant), in a hidden category, removed or unknown answers the
// same domainerr.ErrProductNotFound and the response never confirms that a
// hidden product exists (FR-008). An empty visible set answers not-found
// without touching the database.
func (r *ProductRepository) FindVisibleByCode(ctx context.Context, code string, visibleCategoryIDs []uuid.UUID) (*domainrepo.ProductView, error) {
	if len(visibleCategoryIDs) == 0 {
		return nil, domainerr.ErrProductNotFound
	}
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT %s FROM products
		WHERE code = $1
		  AND category_id = ANY($2)
		  AND is_visible
		  AND EXISTS (
			SELECT 1 FROM product_variants v
			WHERE v.product_id = products.id AND v.sell_state = 'ACTIVE'
		  )`, list)
	product, err := scanProduct(r.querier(ctx).QueryRow(ctx, query, code, visibleCategoryIDs))
	if err != nil {
		return nil, err
	}
	return r.productView(ctx, product, true)
}

// FindByID returns one product with its pictures and every variant, for the
// administrator surface, including a product withheld from customers (FR-010).
// An unknown identifier is reported as domainerr.ErrProductNotFound.
func (r *ProductRepository) FindByID(ctx context.Context, id uuid.UUID) (*domainrepo.ProductView, error) {
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT %s FROM products WHERE id = $1`, list)
	product, err := scanProduct(r.querier(ctx).QueryRow(ctx, query, id))
	if err != nil {
		return nil, err
	}
	return r.productView(ctx, product, false)
}

// LockProduct takes the product's row lock inside the caller's transaction, so
// the picture count that follows cannot be read by two concurrent uploads as the
// same value. An unknown identifier is reported as
// domainerr.ErrProductNotFound.
//
// SELECT ... FOR UPDATE joins the transaction the application put in the context
// and never opens one itself; a caller that invokes it outside a transaction is
// not protected from the race.
func (r *ProductRepository) LockProduct(ctx context.Context, id uuid.UUID) error {
	const query = `SELECT id FROM products WHERE id = $1 FOR UPDATE`
	var locked uuid.UUID
	err := r.querier(ctx).QueryRow(ctx, query, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.ErrProductNotFound
	}
	if err != nil {
		return fmt.Errorf("lock product: %w", err)
	}
	return nil
}

// CountPictures reports how many pictures the product has. The caller checks the
// ten-picture ceiling with it before calling the media provider, so a refused
// eleventh upload leaves no orphaned asset behind (FR-020).
func (r *ProductRepository) CountPictures(ctx context.Context, productID uuid.UUID) (int, error) {
	const query = `SELECT count(*) FROM product_images WHERE product_id = $1`
	var total int
	if err := r.querier(ctx).QueryRow(ctx, query, productID).Scan(&total); err != nil {
		return 0, fmt.Errorf("count product pictures: %w", err)
	}
	return total, nil
}

// ListPictures returns the product's pictures in display order. A nil slice is
// valid and means the product has no picture, which is a valid product.
func (r *ProductRepository) ListPictures(ctx context.Context, productID uuid.UUID) ([]model.Picture, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM product_images WHERE product_id = $1 ORDER BY position, id`,
		strings.Join(pictureColumns, ", "))
	rows, err := r.querier(ctx).Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("list product pictures: %w", err)
	}
	defer rows.Close()

	out := make([]model.Picture, 0)
	for rows.Next() {
		picture, err := scanPicture(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *picture)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// FindPicture returns one picture that belongs to the product, including the
// stored reference the use case needs to release the asset. A picture that does
// not belong to the product, or a product that does not exist, answers
// domainerr.ErrProductNotFound, so the two are indistinguishable (FR-016).
func (r *ProductRepository) FindPicture(ctx context.Context, productID, pictureID uuid.UUID) (*model.Picture, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM product_images WHERE product_id = $1 AND id = $2`,
		strings.Join(pictureColumns, ", "))
	return scanPicture(r.querier(ctx).QueryRow(ctx, query, productID, pictureID))
}

// AddPicture inserts a picture. An unknown product is reported as
// domainerr.ErrProductNotFound, translated from the foreign-key refusal rather
// than checked first, so no code path can store a picture with no product.
func (r *ProductRepository) AddPicture(ctx context.Context, picture *model.Picture) error {
	const query = `
		INSERT INTO product_images
			(id, product_id, public_id, secure_url, width, height, position, is_primary, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.querier(ctx).Exec(ctx, query,
		picture.ID,
		picture.ProductID,
		picture.PublicID,
		picture.URL,
		picture.Width,
		picture.Height,
		picture.Position,
		picture.IsPrimary,
		picture.CreatedAt,
	)
	if err != nil {
		return classifyPictureError("add product picture", err)
	}
	return nil
}

// DeletePicture removes one picture that belongs to the product. A picture that
// does not belong to the product, or a product that does not exist, answers
// domainerr.ErrProductNotFound.
func (r *ProductRepository) DeletePicture(ctx context.Context, productID, pictureID uuid.UUID) error {
	const query = `DELETE FROM product_images WHERE product_id = $1 AND id = $2`
	tag, err := r.querier(ctx).Exec(ctx, query, productID, pictureID)
	if err != nil {
		return fmt.Errorf("delete product picture: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.ErrProductNotFound
	}
	return nil
}

// SetPrimaryPicture makes one picture the product's main one, clearing any other
// in the same statement so the partial unique index is never violated. It is
// idempotent: naming the picture that is already primary is a no-op, not an
// error. A picture that does not belong to the product, or a product that does
// not exist, answers domainerr.ErrProductNotFound.
//
// The membership check comes first because the single UPDATE would otherwise
// clear every picture's flag for a picture that does not exist and still report
// a successful write. The UPDATE then sets is_primary to whether the row is the
// named picture, so exactly one picture is primary afterwards; PostgreSQL
// tolerates the swap within one statement because the demoted row's new version
// no longer conflicts.
func (r *ProductRepository) SetPrimaryPicture(ctx context.Context, productID, pictureID uuid.UUID) error {
	const belongs = `SELECT 1 FROM product_images WHERE product_id = $1 AND id = $2`
	var one int
	err := r.querier(ctx).QueryRow(ctx, belongs, productID, pictureID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.ErrProductNotFound
	}
	if err != nil {
		return fmt.Errorf("find product picture: %w", err)
	}

	// Two statements rather than one. PostgreSQL checks the partial unique index
	// as each row is written, so a single statement that both sets the new primary
	// and clears the old one can be rejected when the rows happen to be written in
	// the order that briefly has two primaries. Clearing first leaves zero main
	// pictures for an instant inside the transaction, which the index permits, and
	// the caller's product row lock keeps another promotion from observing it.
	const demote = `UPDATE product_images SET is_primary = false WHERE product_id = $1 AND id <> $2 AND is_primary`
	if _, err := r.querier(ctx).Exec(ctx, demote, productID, pictureID); err != nil {
		return fmt.Errorf("clear product primary picture: %w", err)
	}
	const promote = `UPDATE product_images SET is_primary = true WHERE product_id = $1 AND id = $2`
	if _, err := r.querier(ctx).Exec(ctx, promote, productID, pictureID); err != nil {
		return fmt.Errorf("set product primary picture: %w", err)
	}
	return nil
}

// list runs the shared paged read of the administrator list.
//
// The order is position, then created_at, then id, ascending: the same order as
// products_ordering_idx. The identifier breaks the remaining tie and is
// immutable, which is what makes the order reproducible across identical
// requests.
func (r *ProductRepository) list(ctx context.Context, predicate string, args []any, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	list, err := r.listProjection()
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	// predicate is built by this adapter from bound parameters and constant SQL,
	// never from caller-supplied text, so it cannot carry a value into the SQL.
	where := ""
	if predicate != "" {
		where = " WHERE " + predicate
	}

	var total int64
	countQuery := "SELECT count(*) FROM products" + where
	if err := r.querier(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	pageArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	pageQuery := fmt.Sprintf(
		"SELECT %s FROM products%s ORDER BY position, created_at, id LIMIT $%d OFFSET $%d",
		list, where, limitPos, offsetPos)
	rows, err := r.querier(ctx).Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.ProductListItem, 0, pageSize)
	for rows.Next() {
		item, err := scanListItem(rows)
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

// listProjection returns the product columns plus the three facts a list entry
// carries: how many pictures the product has, how many variants it has and the
// main picture's link. All three are read by a correlated subquery in the same
// statement, so a list of a page is one query rather than an N+1.
func (r *ProductRepository) listProjection() (string, error) {
	base, err := r.projection()
	if err != nil {
		return "", err
	}
	return base + `,
		(SELECT count(*) FROM product_images i WHERE i.product_id = products.id) AS picture_count,
		(SELECT count(*) FROM product_variants v WHERE v.product_id = products.id) AS variant_count,
		COALESCE((
			SELECT i.secure_url FROM product_images i
			WHERE i.product_id = products.id AND i.is_primary
			ORDER BY i.position, i.id
			LIMIT 1
		), '') AS image_url`, nil
}

// productView assembles a detail view: the product, its gallery, its variants
// and its card summary.
//
// visibleOnly selects the public read (the on-sale and out-of-stock variants
// only, FR-005) or the administrator read (every variant, FR-010). The summary is
// always computed over the full variant set, because the default variant and the
// lowest on-sale price are product facts independent of which variants the caller
// may see.
func (r *ProductRepository) productView(ctx context.Context, product *model.Product, visibleOnly bool) (*domainrepo.ProductView, error) {
	pictures, err := r.ListPictures(ctx, product.ID)
	if err != nil {
		return nil, err
	}
	variants, err := r.listVariants(ctx, product.ID)
	if err != nil {
		return nil, err
	}
	return &domainrepo.ProductView{
		Product:  *product,
		Pictures: pictures,
		Variants: visibleVariants(variants, visibleOnly),
		Summary:  summarizeProduct(product, pictures, variants),
	}, nil
}

// listVariants reads the product's variants in display order, whatever their sell
// state. It shares the variant projection and scan with the VariantRepository, so
// a variant read through the product detail and one read through the variant port
// rehydrate identically.
func (r *ProductRepository) listVariants(ctx context.Context, productID uuid.UUID) ([]model.Variant, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM product_variants WHERE product_id = $1 ORDER BY position, created_at, id`,
		strings.Join(variantColumns, ", "))
	rows, err := r.querier(ctx).Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("list product variants: %w", err)
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

// visibleVariants filters a product's variants for the public read (FR-005). The
// administrator read returns them all, so this is a no-op there.
func visibleVariants(variants []model.Variant, visibleOnly bool) []model.Variant {
	if !visibleOnly {
		return variants
	}
	out := make([]model.Variant, 0, len(variants))
	for _, variant := range variants {
		if variant.VisibleToCustomers() {
			out = append(out, variant)
		}
	}
	return out
}

// summarizeProduct derives the card figure: the lowest effective price among the
// product's on-sale variants and the default variant's image, falling back to the
// product's primary gallery picture (FR-002, FR-003). The variants are already in
// display order, so the first is the default variant.
func summarizeProduct(product *model.Product, pictures []model.Picture, variants []model.Variant) domainrepo.ProductSummary {
	var summary domainrepo.ProductSummary
	var lowest *int64
	for i := range variants {
		if !variants[i].Buyable() {
			continue
		}
		amount := variants[i].EffectivePrice(product.Price).Amount
		if lowest == nil || amount < *lowest {
			value := amount
			lowest = &value
		}
	}
	if lowest != nil {
		summary.Price = model.Price{Amount: *lowest, Currency: product.Price.Currency}
	}
	if len(pictures) > 0 {
		if picture, ok := model.PrimaryPicture(pictures); ok {
			summary.ImageURL = picture.URL
		} else {
			summary.ImageURL = pictures[0].URL
		}
	}
	if summary.ImageURL == "" && len(variants) > 0 && variants[0].Image != nil {
		summary.ImageURL = variants[0].Image.URL
	}
	return summary
}

// scanProduct reads one row of the projection declared in NewProductRepository.
// The number of destinations must match that projection exactly.
//
// The identifier is read but never rewritten; the code is the operator-facing
// value and the public link segment.
func scanProduct(row baserepo.Row) (*model.Product, error) {
	var product model.Product
	err := row.Scan(
		&product.ID,
		&product.Name,
		&product.Code,
		&product.Description,
		&product.Price.Amount,
		&product.Price.Currency,
		&product.CategoryID,
		&product.Position,
		&product.IsVisible,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}
	return &product, nil
}

// scanListItem reads one row of the list projection: the product columns
// followed by the picture count, the variant count and the main picture's link.
func scanListItem(row baserepo.Row) (domainrepo.ProductListItem, error) {
	var item domainrepo.ProductListItem
	var imageURL string
	err := row.Scan(
		&item.Product.ID,
		&item.Product.Name,
		&item.Product.Code,
		&item.Product.Description,
		&item.Product.Price.Amount,
		&item.Product.Price.Currency,
		&item.Product.CategoryID,
		&item.Product.Position,
		&item.Product.IsVisible,
		&item.Product.CreatedAt,
		&item.Product.UpdatedAt,
		&item.PictureCount,
		&item.VariantCount,
		&imageURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domainerr.ErrProductNotFound
	}
	if err != nil {
		return item, fmt.Errorf("scan product list item: %w", err)
	}
	item.ImageURL = imageURL
	return item, nil
}

// scanCatalogueEntry reads one row of catalogueColumns: a product collapsed with
// its variants (FR-001, FR-002).
func scanCatalogueEntry(row baserepo.Row) (domainrepo.CatalogueEntry, error) {
	var entry domainrepo.CatalogueEntry
	var imageURL string
	err := row.Scan(
		&entry.ID,
		&entry.Name,
		&entry.Code,
		&entry.Price.Amount,
		&entry.Price.Currency,
		&imageURL,
		&entry.Position,
		&entry.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entry, domainerr.ErrProductNotFound
	}
	if err != nil {
		return entry, fmt.Errorf("scan catalogue entry: %w", err)
	}
	entry.ImageURL = imageURL
	return entry, nil
}

// scanPicture reads one row of pictureColumns. It translates no-rows to the
// product not-found sentinel, because a picture addressed through a product is
// indistinguishable from that product when it is missing (FR-016).
func scanPicture(row baserepo.Row) (*model.Picture, error) {
	var picture model.Picture
	err := row.Scan(
		&picture.ID,
		&picture.ProductID,
		&picture.PublicID,
		&picture.URL,
		&picture.Width,
		&picture.Height,
		&picture.Position,
		&picture.IsPrimary,
		&picture.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan product picture: %w", err)
	}
	return &picture, nil
}

// classifyWriteError labels a failed product write and maps a constraint
// violation to the module sentinel that names the offending field.
func classifyWriteError(operation string, err error) error {
	if sentinel := classifyConstraintViolation(err); sentinel != nil {
		return sentinel
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// classifyConstraintViolation maps a PostgreSQL constraint refusal to the module
// sentinel a use case answers with, or returns nil for a failure this adapter
// does not recognise. Attribution is by the constraint name PostgreSQL reports,
// never by its message text.
func classifyConstraintViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case uniqueViolationCode:
		if pgErr.ConstraintName == constraintCodeKey {
			return domainerr.ErrProductCodeTaken
		}
	case foreignKeyViolationCode:
		if pgErr.ConstraintName == constraintCategoryFK {
			return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
		}
	}
	return nil
}

// classifyPictureError maps the picture-to-product foreign-key refusal to the
// product not-found sentinel. Any other failure is returned wrapped.
func classifyPictureError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolationCode && pgErr.ConstraintName == constraintImageProductFK {
		return domainerr.ErrProductNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
