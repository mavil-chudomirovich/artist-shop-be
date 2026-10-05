# Phase 1 Data Model: Cross-Cutting Foundation

This feature introduces only the persistence required by the foundation: the
append-only audit log. Schema version tracking is owned by the migration tool.
Configuration is not persisted. All conventions here are binding for future
modules.

## Conventions

- **Primary keys**: UUID, database default `gen_random_uuid()`.
- **Timestamps**: `timestamptz`, stored in UTC; explicit `occurred_at` for domain
  time plus `created_at` for persistence time.
- **Identifiers**: SQL `snake_case`; JSON `lowerCamelCase`.
- **Money (future)**: integer minor units + ISO 4217 currency; never floating
  point.
- **Mutations**: audit rows are append-only (no application UPDATE/DELETE).

## Entity: AuditRecord (`audit_logs`)

An immutable record of a privileged (admin) action or payment event.

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK, default `gen_random_uuid()` | Surrogate key. |
| `event_id` | uuid | NOT NULL, UNIQUE | Idempotency key; prevents duplicates on retry (FR-014, SC-006). |
| `actor_id` | uuid | NULL | Account that performed the action; null for system/unauthenticated events. |
| `actor_role` | text | NULL | Role at time of action (e.g., `ADMIN`, `CUSTOMER`, `SYSTEM`). |
| `action` | text | NOT NULL | Stable action identifier (e.g., `PRODUCT_UPDATE`, `PAYMENT_CAPTURED`). |
| `target_type` | text | NULL | Entity type affected (e.g., `order`, `commission`). |
| `target_id` | text | NULL | Identifier of the affected entity. |
| `outcome` | text | NOT NULL | One of `SUCCESS`, `FAILURE`. |
| `metadata` | jsonb | NOT NULL, default `'{}'` | Non-sensitive context; MUST NOT contain secrets or unnecessary PII. |
| `correlation_id` | text | NULL | Ties the record to the originating request. |
| `occurred_at` | timestamptz | NOT NULL | When the action/event occurred (UTC). |
| `created_at` | timestamptz | NOT NULL, default `now()` | When the row was persisted. |

### Validation rules

- `event_id` unique across the table (idempotent retry).
- `action` and `outcome` are non-empty and drawn from a controlled vocabulary.
- `metadata` MUST be valid JSON and MUST NOT include secrets/tokens.
- Rows MUST NOT be updated or deleted by application code; enforce with revoked
  DML privileges and/or a guard trigger.

### Indexes

- PK on `id`.
- UNIQUE on `event_id`.
- `(actor_id, occurred_at DESC)` — activity by actor.
- `(action, occurred_at DESC)` — activity by action.
- `(target_type, target_id, occurred_at DESC)` — history of an entity.
- Optional: GIN on `metadata` for ad-hoc investigation.

### Relationships

- `actor_id` references the future account entity (module 01). No hard foreign
  key is added in this feature to avoid coupling the foundation to a module that
  does not exist yet; referential integrity is application-enforced until the
  auth module lands, at which point a migration may add the FK.

## Lifecycle / state

Audit records have no state machine. They are created once and never modified.
A write may be retried (same `event_id`); retries are absorbed by the unique
constraint and do not create new rows.

## Migration metadata

The migration tool maintains its own version table (`goose_db_version`). It is
not part of the domain model and is not exposed through any API.

## Configuration model (not persisted)

Configuration is a typed, validated in-memory structure populated from the
environment. Logical groups: server (host/port/timeouts), database
(dsn/pool sizes), logging (level/format), cors (allowed origins), rate limiting
(thresholds per route class), audit (queue size/retry bounds), environment name.
Secrets (DB password, future provider keys) are read from the environment and
never logged or persisted by the application.
