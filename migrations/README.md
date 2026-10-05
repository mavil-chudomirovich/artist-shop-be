# Migrations

Versioned SQL migrations applied with [goose](https://github.com/pressly/goose)
and embedded into the binary (`embed.go`).

## Naming

`NNNNN_description.sql`, zero-padded and strictly increasing.

## Applying

- Automatically when the API starts (`MIGRATIONS_AUTO_APPLY=true`, the default),
  guarded by a PostgreSQL advisory lock so concurrent instances cannot apply the
  same migration twice.
- Manually with `go run ./cmd/migrate up|down|status|version`.

Every migration MUST provide both `-- +goose Up` and `-- +goose Down` sections so
the change can be rolled back (FR-004).

## Destructive migrations

A migration that drops or rewrites data (dropping a column/table, changing a type
non-losslessly, backfilling with loss) MUST NOT be merged on its own. It MUST
include an accompanying data-migration note describing:

1. What data is affected and how it is preserved, migrated, or intentionally
   discarded.
2. How to verify the migration on a copy of production data.
3. The rollback/restore procedure.

The note belongs in the pull request description and, for significant changes, in
this directory. Migrations without a Down section and without such a note are
rejected in review (FR-005).

## Append-only tables

`audit_logs` is append-only: `00002_audit_append_only.sql` installs triggers that
reject `UPDATE` and `DELETE`. New audit history is written with `INSERT ... ON
CONFLICT (event_id) DO NOTHING` so retries remain idempotent.
