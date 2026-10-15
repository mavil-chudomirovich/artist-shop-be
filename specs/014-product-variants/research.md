# Research — Product Variants (014)

Phase 0 decisions. Each records what was chosen, why, and the alternatives rejected. The spec's
`Clarifications` (one dimension; each option a full product; the group owns no price/stock/state; the
customer buys the option; two slug namespaces; group gallery + option pictures; single derived price)
are inputs here, not open questions.

## D1 — How a variant group is represented

**Decision**: a new **`product_variant_groups`** table plus two nullable columns on the existing
`products` table — `variant_group_id` (FK to the group) and `variant_label` (the option's short
label). A product with `variant_group_id` set is an option of that group; a product with it null is
an ordinary standalone product exactly as today. **No parent-product row**: the group is not a
product and can never be bought.

**Rationale**: every existing consumer (cart, order, inventory, payment, the `ProductCatalog`
contract) keys on a **product id**; because an option *is* a product row, none of them change. A
standalone group table also keeps "a group has no price, stock or sell state" structurally true — the
columns do not exist — so no code path can accidentally sell a group.

**Alternatives rejected**:
- *A parent product row (self-reference)*: the parent would need a placeholder price (the column is
  `NOT NULL CHECK (> 0)`), a sell state, and a "not buyable" flag — polluting the product state
  machine and risking a buy of the parent; `catalog.go` maps `OnSale = product.Buyable()`, so a
  sellable-looking parent could leak into a cart.
- *A free-text `variant_group_slug` column with no table*: the group's own name/description/category/
  position/gallery would have nowhere to live, and there is no row to enforce label uniqueness or
  group deletion against.

## D2 — Where the group's own picture gallery lives

**Decision**: a new **`product_variant_group_images`** table mirroring `product_images` (id, group_id,
public_id, secure_url, width, height, position, is_primary, created_at), with the same partial unique
index "at most one primary".

**Rationale**: the spec (FR-018, Clarification Q2) gives the group its **own** gallery, and the
existing product picture rules (formats, 2 MB ceiling, ten-picture count, one primary) should apply
unchanged. Mirroring the table keeps the same media adapter and the same read/write shape rather than
inventing a polymorphic image table.

**Alternatives rejected**:
- *A polymorphic `images(owner_type, owner_id)` table*: rewrites the product picture path and every
  test that exercises it, for no gain — the product module already owns both.
- *No group gallery (use the selected option's pictures only)*: q2 explicitly chose a shared banner
  gallery in addition to per-option pictures.

## D3 — Storage-level enforcement of the new invariants

**Decision**: enforce in the schema, not only in code:
- `products.variant_group_id` → FK `product_variant_groups(id)` **ON DELETE RESTRICT** (a group with
  options cannot be removed).
- Partial unique index `(variant_group_id, variant_label) WHERE variant_group_id IS NOT NULL`
  (option label unique within its group).
- Check `(variant_group_id IS NULL) = (variant_label IS NULL)` (a label belongs to a grouped product
  and to nothing else).
- Check `NOT (is_set AND variant_group_id IS NOT NULL)` (a combo set cannot be an option — D9).
- `products.variant_group_id` index (the option read and the FK both need it).
- Group: unique index on `slug`; the catalogue's slug-format check; name/slug/description length
  checks; FK to `categories(id)` ON DELETE RESTRICT; ordering index `(position, created_at, id)`.

**Rationale**: Constitution's rule that important constraints live at the storage layer and that a
single application check must not be the only guard (FR-022, SC-004). One-group-per-product is the
column's meaning itself: a product carries **one** `variant_group_id`, so "belongs to at most one
group" is a structural fact, not a check.

**Alternatives rejected**: application-only checks (a defect in one path would produce bad data);
a trigger (business logic in the schema, invisible to the domain and its tests — the module's D4/D8
pattern already refuses triggers without measured need).

## D4 — The merged public catalogue list

**Decision**: the public list (`GET /products`) becomes one SQL statement — a `UNION ALL` (in a CTE)
of two projected sources with a shared shape and a `type` discriminator (`'variant-group'` | `'product'`)
(D12):
- **groups**: visible group rows (D7) projected with name, slug, description, category, position,
  created_at, the group's primary image, and the price of the lowest-priced **visible** option;
- **standalone products**: `products WHERE variant_group_id IS NULL` under the existing visibility
  predicate, projected the same shape (option-less price and image).

The whole predicate runs in this statement; `COUNT(*)` over the same CTE gives the page total; the
order is `position, created_at, id` (the FR-009 order, identical across kinds).

**Rationale**: the module's established rule is that the visibility predicate and the total are
computed in SQL, so pagination is correct — dropping hidden rows in Go returns a short page and a
wrong total (the module's existing research D1). A union in one statement preserves that exactly.

**Alternatives rejected**:
- *Two paginated queries merged in Go*: cannot yield a correct cross-kind `total` or a stable
  interleaved order.
- *A database VIEW*: heavier to migrate and to keep in step; the union is a read in one adapter that
  already builds its own SQL.
- *Listing grouped products individually as well*: forbidden by FR-001/SC-002 — a group must appear
  once, so a product that has a group is excluded from the standalone side.

**Note (a recorded complexity)**: this is the one place the feature works harder than the catalogue
did; it is listed in the plan's Complexity Tracking.

## D5 — Ordering of a group's options

**Decision**: a group's options are ordered by the option product's existing **`position`**, then
`created_at`, then `id` — the same deterministic order a catalogue list uses, reusing a field the
operator can already set through the existing product edit.

**Rationale**: the spec does not require a separate option-order editor, and the product's `position`
already exists and is operator-settable; reusing it avoids a new column and a new endpoint while
still giving a deterministic, controllable order.

**Alternatives rejected**: a new `variant_option_position` column and endpoint (unneeded surface for
v1); ordering by label alphabetically (not operator-controllable, and meaningless for non-alphabetic
labels); ordering by insertion only (not controllable).

## D6 — Two slug namespaces, two routes

**Decision**: a group's `slug` is unique among groups (a unique index on the group table), and a
product's `slug` stays unique among products (the existing index). The two are addressed by distinct
public routes: groups at `GET /product-groups/{slug}`, products at `GET /products/{slug}`. The same
value may therefore exist once in each namespace.

**Rationale**: the project already separates namespaces by kind (categories at `/categories/{slug}`,
products at `/products/{slug}`), so this is the consistent choice and needs **no** cross-table
uniqueness machinery (Clarification Q1).

**Alternatives rejected**: a single shared namespace — would require a shared slug registry or an
application-level cross-table check to avoid two pages with the same slug, for a benefit the spec
does not ask for.

## D7 — Visibility predicate for options and groups

**Decision**:
- An **option** is visible exactly as any product is: its own category is visible **and** it is on
  sale or an announced non-retired pre-order (the existing `ListVisible` predicate). No new rule.
- A **group** is visible when its **category** is visible **and** at least one of its options is
  visible (`EXISTS` in SQL). A group with no visible option answers the same not-found a hidden group
  answers (FR-006, FR-008).
- The group's list **price** and **representative image**: price = `MIN(option.price_amount)` over
  visible options (the group's price, per Q3); the group's own primary picture is the card image
  (FR-005), falling back to the representative option's primary picture only if the group has none
  is **not** done — a group with no picture shows no picture, exactly like a product.

**Rationale**: FR-027 requires the existing rules to be reused, so "what a customer sees" keeps one
definition; the only new fact is "a group exists for the customer only if one of its options does",
which must be a SQL predicate to keep the list total correct (D4).

**Alternatives rejected**: hiding options by the group's category only (would let a hidden option
category still surface an option — inconsistent with the existing rule); a stored "group visible"
flag (a second source of truth that can drift from the options).

## D8 — Administrator surface

**Decision**:
- Group CRUD at `/api/v1/admin/product-groups`: `POST` (create), `GET` (list), `GET /{id}` (detail
  with images and options), `PATCH /{id}` (partial edit), `DELETE /{id}` (remove; D10).
- Group pictures mirror the product picture routes: `POST /{id}/images`,
  `DELETE /{id}/images/{imageId}`, `POST /{id}/images/{imageId}/primary`.
- **Attach / detach an option reuses the product's own create/edit**: `CreateProductRequest` and
  `UpdateProductRequest` gain optional `variantGroupId` and `variantLabel`. Attaching is a `PATCH`
  that sets both; detaching is a `PATCH` that clears both. No dedicated attach endpoint.

**Rationale**: an option is a product, and the product already has create/edit; adding two optional
members to an existing body is the smallest surface that satisfies FR-010/FR-011 while changing
nothing about the product's price, pictures, state or link (those are simply not touched). It also
keeps option ordering (D5) usable through the product's existing `position`.

**Alternatives rejected**: dedicated `POST /admin/product-groups/{id}/options` endpoints (a second
write path to the same product columns, with its own validation to keep in step); a separate "link"
table with no product columns (harder to keep one-group-per-product structural).

## D9 — Combo set and option are mutually exclusive

**Decision**: a same-row check constraint `NOT (is_set AND variant_group_id IS NOT NULL)` refuses a
product that is both a set and an option, at the storage layer; the domain reports it naming the
offending field.

**Rationale**: a set is a product in its own right, not a choice among alternatives (Edge Cases);
the two roles live on the same row, so a simple `CHECK` enforces it with no trigger.

**Alternatives rejected**: application-only validation (bypassable); a cross-row trigger (heavier,
and the two facts are columns of one row).

## D10 — Removing a group that still has options

**Decision**: the `products.variant_group_id` foreign key is **ON DELETE RESTRICT**, so removing a
group that still has options is refused with a module code that says so; the operator detaches the
options first (each becomes an ordinary product again). Removing an empty group succeeds.

**Rationale**: the spec's Assumption and FR-025 require that a group removal never deletes or orphans
an option. This is exactly the RESTRICT pattern the catalogue already uses for a category with
products (`products_category_fk`), so the module behaves consistently and the guarantee is at the
storage layer.

**Alternatives rejected**: `ON DELETE SET NULL` (silently detaches options on a group delete — hides
a destructive action); `ON DELETE CASCADE` (would delete the options — forbidden by FR-012).

## D11 — What does not change

**Decision**: no change to `internal/contracts/` (cart's `ProductCatalog`, inventory's
`InventoryAvailability`/`InventoryReservation`, product's `ProductAvailability`/`ProductLookup`), no
change to modules 05–08, no new infrastructure, no new cross-module read.

**Rationale**: because an option is a product, every cross-module flow already operates on it. The
availability of an option is therefore not surfaced on the group page (a cross-module read the spec
deliberately leaves out — Assumptions); the customer learns it when adding to the cart, exactly as
today.

**Alternatives rejected**: widening `ProductCatalog` or adding a catalogue consumer port — needless,
since the consumers already receive product ids.

## D12 — The catalogue entry discriminator is `type`

**Decision**: the merged public list entry carries a field named **`type`** with values
**`"product"`** (a standalone product) and **`"variant-group"`** (a group). Not `kind`, and not the
values `"group"`/`"product"`.

**Rationale**: `type` is the spelling the machine-readable contract
(`contracts/openapi.yaml`), `quickstart.md` and the frontend hand-off already use, and
`"variant-group"` matches the entity's own name so a reader does not have to translate. Applying it
across `plan.md`, `data-model.md` and `tasks.md` resolves the internal `kind`/`type` drift.

**Alternatives rejected**: `kind` with `"group"`/`"product"` — the shorter spelling, but it is the one
that was inconsistent across artifacts and `"group"` is ambiguous next to stock "groups".

## D13 — Attach / detach is a tri-state, encoded by presence

**Decision**: in the product create/edit HTTP bodies, `variantGroupId` and `variantLabel` are
**presence-tracked** on a `PATCH`: absent = keep the current attachment; present and `null` = detach;
present and non-null = attach (only when the product belongs to no group — a product already in a
group is refused with `PRODUCT_ALREADY_IN_VARIANT_GROUP` and must be detached first, per the spec's
Edge case and `error-codes.md`). The presentation DTO decodes each member with a small
`OptionalUUID` / `OptionalString` type that records whether the JSON key was present, and the
application input carries `VariantGroupID *uuid.UUID` together with `VariantGroupSet bool` (and the
same pair for the label).

**Rationale**: a plain `*uuid.UUID` cannot tell "omitted" from "explicitly null", so detach and
"keep current" would collide — FR-011 requires detach as a real operation. Presence tracking is the
minimal mechanism that preserves the flat contract shape (no nested object, no extra endpoint).

**Alternatives rejected**: a dedicated detach endpoint (extra surface the spec does not ask for); a
nested `variantGroup: {id, label}` object (changes the field shape the spec fixed as flat).

## D14 — Layout decisions settled during analysis

**Decision** (recorded so the work is auditable):
- Group HTTP request/response DTOs live in `internal/modules/product/presentation/dto/dto.go`, beside
  the existing product HTTP DTOs. The module has **no** `presentation/http/dto` package; the
  constitution lists `dto` as a sibling of `http` under `presentation`.
- Group and catalogue use cases are methods on the module's existing single `Service`; the
  `ProductService` interface is extended, not split into a sibling service, so the handler keeps one
  dependency.
- The shared test double lives in a new `internal/modules/product/application/implement/fakes_test.go`
  (the existing use-case tests keep their product fakes in-file).

**Rationale**: matches the delivered module layout and keeps the smallest diff.
