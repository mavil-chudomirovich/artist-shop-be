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

// table is the one table this module owns. The pictures and set membership live
// in product_images and product_set_items; this adapter is the only code that
// reads or writes all three.
const table = "products"

// The constraint names that carry a meaning this adapter has to translate. A
// collision is attributed by the constraint name PostgreSQL reports, never by
// the text of its message, because the message is localized and free to change
// while the constraint name is part of the schema (FR-028, FR-009, research D17).
const (
	constraintNormalizedSlug = "products_normalized_slug_key"
	constraintCategoryFK     = "products_category_fk"
	constraintSetMemberFK    = "product_set_items_member_fk"
	constraintSetSelfCK      = "product_set_items_not_self_ck"
	constraintSetPK          = "product_set_items_pkey"
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
// It is deliberately the fourteen response columns of data-model.md, matching
// the scan arity of scanProduct. The folding key normalized_slug appears in no
// projection, so it cannot leak into a response through this adapter even if a
// mapper were later changed by mistake: the value is derived on write and
// recomputed on read from the slug. The identifier is read but never named in an
// UPDATE, so no code path can rewrite it (FR-015).
var productColumns = []string{
	"id",
	"name",
	"slug",
	"description",
	"price_amount",
	"currency",
	"category_id",
	"position",
	"sell_state",
	"is_set",
	"is_preorder",
	"preorder_expected_at",
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

// ProductRepository persists products, their pictures and their set membership.
//
// It embeds share/repository.Base for its write plumbing and sets an explicit
// Columns projection, because the generic read paths reject an empty projection
// rather than falling back to `SELECT *`. The projection, not the physical
// table, drives the scan arity: a later `ALTER TABLE products ADD COLUMN` cannot
// break this adapter.
//
// It never calls the generic FindAll: the public reads carry the visibility
// predicate and the ordering tie-break, which the generic listing cannot express.
// Every write is a single statement except ReplaceSetMembers and
// SetPrimaryPicture, which run inside the caller's transaction; the adapter never
// opens one itself (Constitution I).
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
// The adapter recomputes normalized_slug from the slug it is about to store
// rather than trusting the entity's copy: the fold is deterministic, so
// recomputing yields exactly the key the entity already holds for a
// well-formed product, and no write path can store a stale or empty key
// (research D4). The unique index then has a correct value to enforce.
//
// A slug another product already holds is reported as
// domainerr.ErrProductSlugTaken so the use case can answer PRODUCT_SLUG_TAKEN.
// A category identifier no row carries is reported as domainerr.ErrProductInvalid
// naming model.FieldCategoryID (FR-033).
func (r *ProductRepository) Create(ctx context.Context, product *model.Product) error {
	const query = `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder,
			 preorder_expected_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	_, err := r.querier(ctx).Exec(ctx, query,
		product.ID,
		product.Name,
		product.Slug,
		model.FoldKey(product.Slug),
		product.Description,
		product.Price.Amount,
		product.Price.Currency,
		product.CategoryID,
		product.Position,
		product.SellState,
		product.IsSet,
		product.IsPreorder,
		product.PreorderExpectedAt,
		product.CreatedAt,
		product.UpdatedAt,
	)
	if err != nil {
		return classifyWriteError("create product", err)
	}
	return nil
}

// Update writes the editable columns of the product named by its identifier,
// recomputing the folding key from the slug it is about to store.
//
// It writes sell_state, is_preorder and preorder_expected_at as well, because the
// entity is the single place those values move — the transitions write the entity
// and this method persists it. It never names id in the SET list, so no code path
// can rewrite an identifier after creation (FR-015).
//
// A collision with a *different* product is reported as
// domainerr.ErrProductSlugTaken; writing the slug a product already holds
// succeeds, because the unique index excludes the row being updated, so a
// product is never a duplicate of itself (FR-028). A category identifier no row
// carries is reported as domainerr.ErrProductInvalid naming
// model.FieldCategoryID (FR-033). An unknown identifier is reported as
// domainerr.ErrProductNotFound.
func (r *ProductRepository) Update(ctx context.Context, product *model.Product) error {
	const query = `
		UPDATE products
		SET name                 = $2,
		    slug                 = $3,
		    normalized_slug      = $4,
		    description          = $5,
		    price_amount         = $6,
		    currency             = $7,
		    category_id          = $8,
		    position             = $9,
		    sell_state           = $10,
		    is_set               = $11,
		    is_preorder          = $12,
		    preorder_expected_at = $13,
		    updated_at           = now()
		WHERE id = $1`
	tag, err := r.querier(ctx).Exec(ctx, query,
		product.ID,
		product.Name,
		product.Slug,
		model.FoldKey(product.Slug),
		product.Description,
		product.Price.Amount,
		product.Price.Currency,
		product.CategoryID,
		product.Position,
		product.SellState,
		product.IsSet,
		product.IsPreorder,
		product.PreorderExpectedAt,
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

// Delete removes the row. Removal is a hard delete (research D13); the audit
// entry is what survives it, and the product's pictures and membership rows are
// removed with it through the cascading foreign keys. An unknown identifier is
// reported as domainerr.ErrProductNotFound.
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

// ListVisible returns a page of the products a customer may see, plus the total
// number of them.
//
// The whole predicate — (sell_state = 'ACTIVE' OR is_preorder) AND
// category_id = ANY($1) — runs in this query, so pagination and the total stay
// correct (FR-002, FR-006, research D1). The optional category filter is bound
// as its own parameter. An empty visible set answers an empty page without
// touching the database (FR-007).
func (r *ProductRepository) ListVisible(ctx context.Context, query domainrepo.VisibleListQuery) ([]domainrepo.ProductListItem, int64, error) {
	if len(query.VisibleCategoryIDs) == 0 {
		return []domainrepo.ProductListItem{}, 0, nil
	}
	args := []any{query.VisibleCategoryIDs}
	// The state half of the predicate mirrors model.Product.VisibleToCustomers: an
	// on-sale product, or a pre-order — but never a retired one, even when it still
	// carries the pre-order label (FR-002, FR-026, FR-027).
	predicate := "(sell_state = 'ACTIVE' OR (is_preorder AND sell_state <> 'DISCONTINUED')) AND category_id = ANY($1)"
	if query.CategoryID != nil {
		args = append(args, *query.CategoryID)
		predicate += fmt.Sprintf(" AND category_id = $%d", len(args))
	}
	return r.list(ctx, predicate, args, query.Page, query.PageSize)
}

// ListAll returns a page of every product, including the ones withheld from
// customers, plus the total number of them, in the same FR-009 order. It serves
// the administrator list (FR-011).
func (r *ProductRepository) ListAll(ctx context.Context, page, pageSize int) ([]domainrepo.ProductListItem, int64, error) {
	return r.list(ctx, "", nil, page, pageSize)
}

// FindVisibleBySlug returns one product a customer may see, with its pictures,
// addressing it by the slug a customer-facing link is built from.
//
// The same visibility predicate as ListVisible runs in this query, so a product
// that is not on sale and not a pre-order, retired, in a hidden category,
// removed or unknown answers domainerr.ErrProductNotFound, and the response never
// confirms that a hidden product exists (FR-003, research D1). An empty visible
// set answers not-found without touching the database.
func (r *ProductRepository) FindVisibleBySlug(ctx context.Context, slug string, visibleCategoryIDs []uuid.UUID) (*domainrepo.ProductView, error) {
	if len(visibleCategoryIDs) == 0 {
		return nil, domainerr.ErrProductNotFound
	}
	list, err := r.projection()
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT %s FROM products
		WHERE slug = $1
		  AND (sell_state = 'ACTIVE' OR (is_preorder AND sell_state <> 'DISCONTINUED'))
		  AND category_id = ANY($2)`, list)
	product, err := scanProduct(r.querier(ctx).QueryRow(ctx, query, slug, visibleCategoryIDs))
	if err != nil {
		return nil, err
	}
	return r.viewWithPictures(ctx, product)
}

// FindByID returns one product with its pictures for the administrator surface,
// including one withheld from customers. An unknown identifier is reported as
// domainerr.ErrProductNotFound (FR-011).
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
	return r.viewWithPictures(ctx, product)
}

// FindByIDs returns every requested product that exists, without pictures, in
// one read. It is the read behind the cross-module ProductCatalog contract
// (specs/008-cart research D1): the whole set is answered by one indexed
// `= ANY(...)` query rather than one query per product. An identifier no product
// carries is simply absent from the result, which is not an error.
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

// LockProduct takes the product's row lock inside the caller's transaction, so
// the picture count that follows cannot be read by two concurrent uploads as the
// same value (research D6). An unknown identifier is reported as
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
// valid and means the product has no picture, which is a valid product
// (research D7).
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
// error (research D7). A picture that does not belong to the product, or a
// product that does not exist, answers domainerr.ErrProductNotFound.
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
	// the caller's product row lock keeps another promotion from observing it
	// (research D7).
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

// ReplaceSetMembers replaces the whole member list of a set, in the order given.
// It is called inside the same transaction as the product write, so a set is
// never stored without its members (FR-039). A member identifier no product
// carries, a duplicated member or a self-reference is reported as
// domainerr.ErrProductInvalid naming model.FieldMemberProductIDs.
func (r *ProductRepository) ReplaceSetMembers(ctx context.Context, setProductID uuid.UUID, memberIDs []uuid.UUID) error {
	const clearMembers = `DELETE FROM product_set_items WHERE set_product_id = $1`
	if _, err := r.querier(ctx).Exec(ctx, clearMembers, setProductID); err != nil {
		return fmt.Errorf("clear set members: %w", err)
	}
	const insert = `INSERT INTO product_set_items (set_product_id, member_product_id, position) VALUES ($1, $2, $3)`
	for position, memberID := range memberIDs {
		if _, err := r.querier(ctx).Exec(ctx, insert, setProductID, memberID, position); err != nil {
			return classifySetError("insert set member", err)
		}
	}
	return nil
}

// ListSetMembers returns the products inside a set, in the order they are
// listed. It serves the administrator detail only: the public shape does not
// enumerate a set's contents (FR-039, research D18). A nil slice means the
// product is not a set, or the set has no members.
func (r *ProductRepository) ListSetMembers(ctx context.Context, setProductID uuid.UUID) ([]domainrepo.SetMember, error) {
	const query = `
		SELECT p.id, p.name, p.slug
		FROM product_set_items s
		JOIN products p ON p.id = s.member_product_id
		WHERE s.set_product_id = $1
		ORDER BY s.position, p.id`
	rows, err := r.querier(ctx).Query(ctx, query, setProductID)
	if err != nil {
		return nil, fmt.Errorf("list set members: %w", err)
	}
	defer rows.Close()

	out := make([]domainrepo.SetMember, 0)
	for rows.Next() {
		var member domainrepo.SetMember
		if err := rows.Scan(&member.ID, &member.Name, &member.Slug); err != nil {
			return nil, fmt.Errorf("scan set member: %w", err)
		}
		out = append(out, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// list runs the shared paged read for one of the three audiences.
//
// The order is position, then created_at, then id, ascending: the same order as
// products_ordering_idx and the only order FR-009 permits. The identifier breaks
// the remaining tie and is immutable, which is what makes the order reproducible
// across identical requests.
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

// listProjection returns the product columns plus the two picture facts a list
// entry carries: how many pictures the product has and the main picture's link.
// Both are read by a correlated subquery in the same statement, so a list of a
// page is one query rather than an N+1. The public mapper uses only the link and
// the administrator mapper uses both (FR-004, FR-011).
func (r *ProductRepository) listProjection() (string, error) {
	base, err := r.projection()
	if err != nil {
		return "", err
	}
	return base + `,
		(SELECT count(*) FROM product_images i WHERE i.product_id = products.id) AS picture_count,
		COALESCE((
			SELECT i.secure_url FROM product_images i
			WHERE i.product_id = products.id AND i.is_primary
			ORDER BY i.position, i.id
			LIMIT 1
		), '') AS image_url`, nil
}

// viewWithPictures assembles the detail view: a product plus its pictures in
// display order. It is the shape both detail reads answer with.
func (r *ProductRepository) viewWithPictures(ctx context.Context, product *model.Product) (*domainrepo.ProductView, error) {
	pictures, err := r.ListPictures(ctx, product.ID)
	if err != nil {
		return nil, err
	}
	return &domainrepo.ProductView{Product: *product, Pictures: pictures}, nil
}

// scanProduct reads one row of the projection declared in NewProductRepository.
// The number of destinations must match that projection exactly.
//
// The folding key is not read: it is in no projection and is never needed by a
// response. It is derived from the slug just scanned with the domain's pure
// folding function, because the entity's invariant — the key always agrees with
// the slug — must hold for a product rehydrated from storage exactly as it does
// for a constructed one.
func scanProduct(row baserepo.Row) (*model.Product, error) {
	var product model.Product
	var sellState string
	err := row.Scan(
		&product.ID,
		&product.Name,
		&product.Slug,
		&product.Description,
		&product.Price.Amount,
		&product.Price.Currency,
		&product.CategoryID,
		&product.Position,
		&sellState,
		&product.IsSet,
		&product.IsPreorder,
		&product.PreorderExpectedAt,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}
	product.SellState = constant.SellState(sellState)
	product.NormalizedSlug = model.FoldKey(product.Slug)
	return &product, nil
}

// scanListItem reads one row of the list projection: the fourteen product
// columns followed by the picture count and the main picture's link.
func scanListItem(row baserepo.Row) (domainrepo.ProductListItem, error) {
	var item domainrepo.ProductListItem
	var sellState string
	var imageURL string
	err := row.Scan(
		&item.Product.ID,
		&item.Product.Name,
		&item.Product.Slug,
		&item.Product.Description,
		&item.Product.Price.Amount,
		&item.Product.Price.Currency,
		&item.Product.CategoryID,
		&item.Product.Position,
		&sellState,
		&item.Product.IsSet,
		&item.Product.IsPreorder,
		&item.Product.PreorderExpectedAt,
		&item.Product.CreatedAt,
		&item.Product.UpdatedAt,
		&item.PictureCount,
		&imageURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domainerr.ErrProductNotFound
	}
	if err != nil {
		return item, fmt.Errorf("scan product list item: %w", err)
	}
	item.Product.SellState = constant.SellState(sellState)
	item.Product.NormalizedSlug = model.FoldKey(item.Product.Slug)
	item.ImageURL = imageURL
	return item, nil
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
		if pgErr.ConstraintName == constraintNormalizedSlug {
			return domainerr.ErrProductSlugTaken
		}
	case foreignKeyViolationCode:
		switch pgErr.ConstraintName {
		case constraintCategoryFK:
			return domainerr.InvalidProductField(model.FieldCategoryID, "does not exist")
		case constraintSetMemberFK:
			return domainerr.InvalidProductField(model.FieldMemberProductIDs, "does not exist")
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

// classifySetError maps a membership refusal to the member field: a member that
// does not exist, a duplicated member and a self-reference are all problems with
// the member list the operator supplied (FR-039).
func classifySetError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case constraintSetMemberFK, constraintSetPK, constraintSetSelfCK:
			return domainerr.InvalidProductField(model.FieldMemberProductIDs, "is not a valid member")
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
