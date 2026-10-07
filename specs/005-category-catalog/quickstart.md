# Quickstart: Category Catalogue

Validation guide. Every scenario states the request, the expected answer, and how to tell a real
pass from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not.
A scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped,
because a container-backed check that skips is the failure mode this project has already paid
for once.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- Migration applied: `make migrate-up` — this feature adds `migrations/00005_category.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- An administrator token from `ADMIN_EMAIL` / `ADMIN_PASSWORD` in `.env`, obtained through
  `POST /api/v1/auth/login`, and a customer token for the role checks.

**On Windows, never send a JSON body inline.** PowerShell rewrites embedded quotes and the
request arrives malformed, which looks like a server defect and is not one. Write the body to a
file and send it with `--data-binary @file`:

```bash
cat > /tmp/body.json <<'JSON'
{ "name": "Tranh sơn dầu", "slug": "tranh-son-dau", "description": "Tác phẩm sơn dầu" }
JSON
curl -s -X POST "$BASE/admin/categories" -H "$ADMIN" -H 'Content-Type: application/json' \
  --data-binary @/tmp/body.json
```

Set `BASE=http://localhost:8080/api/v1`, `ADMIN="Authorization: Bearer <admin token>"` and
`CUSTOMER="Authorization: Bearer <customer token>"` once.

## Scenario 1 — an empty catalogue answers an empty list

```bash
curl -s "$BASE/categories"
```

| # | Expected |
|---|---|
| 1a | `200`, `data` is `[]`, `meta.total` is `0` |
| 1b | No error, and no `null` where a list belongs |

**A `500` here is a fail**: an empty catalogue is the normal starting state, not an error.

## Scenario 2 — the operator creates a category

```bash
curl -s -o /tmp/created.json -w '%{http_code}' -X POST "$BASE/admin/categories" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/body.json
```

| # | Expected |
|---|---|
| 2a | `201`, the body carries `id`, `name`, `slug`, `description`, `position`, `isVisible: true`, timestamps |
| 2b | `GET $BASE/categories` now lists it |
| 2c | `GET $BASE/categories/tranh-son-dau` answers `200` with the same `id` |

## Scenario 3 — the public shape is exactly four fields

```bash
curl -s "$BASE/categories" | python3 -m json.tool
```

| # | Expected |
|---|---|
| 3a | Each entry has exactly `id`, `name`, `slug`, `description` |
| 3b | No `isVisible` and no `position` in a public entry. **Their presence is a fail**: the public shape exists so the operator's management fields never reach a customer |
| 3c | No `normalizedName` or similar folding key anywhere in any response |

## Scenario 4 — a withheld category is invisible, and indistinguishable from one that never existed

Hide it, then look as a customer:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$BASE/admin/categories/$ID" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/hide.json   # {"isVisible": false}

curl -s "$BASE/categories"                    # the list
curl -s -o /tmp/hidden.json -w '%{http_code}' "$BASE/categories/tranh-son-dau"
curl -s -o /tmp/unknown.json -w '%{http_code}' "$BASE/categories/khong-ton-tai"
```

| # | Expected |
|---|---|
| 4a | The public list no longer contains it, and `meta.total` dropped by one |
| 4b | The direct fetch answers `404 CATEGORY_NOT_FOUND` |
| 4c | **The withheld answer and the never-existed answer are byte-identical except for `requestId`.** Diffing them is the check: any other difference lets a caller enumerate what the operator withheld |
| 4d | `GET $BASE/admin/categories/$ID` still answers `200` — the operator can see what a customer cannot |

## Scenario 5 — a duplicate name is refused with the field named

```bash
cat > /tmp/dup.json <<'JSON'
{ "name": "Tranh SƠN dầu", "slug": "tranh-son-dau-2" }
JSON
curl -s -X POST "$BASE/admin/categories" -H "$ADMIN" \
  -H 'Content-Type: application/json' --data-binary @/tmp/dup.json
```

| # | Expected |
|---|---|
| 5a | `409 CATEGORY_NAME_TAKEN`, with `details[].field` naming `name` |
| 5b | The different **case** and the **extra surrounding whitespace** are what makes this a duplicate — a comparison that folds neither would answer `201` here |
| 5c | A Vietnamese name whose letters differ only in case is also refused. If only an ASCII name is tested, the fold is unproven |

Then the same for the slug:

| # | Expected |
|---|---|
| 5d | A second category with a new name but the existing slug answers `409 CATEGORY_SLUG_TAKEN`, `details[].field` naming `slug` |

## Scenario 6 — an edit that changes nothing about the name succeeds

```bash
cat > /tmp/same.json <<'JSON'
{ "description": "Mô tả mới" }
JSON
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$BASE/admin/categories/$ID" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/same.json
```

| # | Expected |
|---|---|
| 6a | `200`. A category is never a duplicate of itself |
| 6b | Re-sending the **same** name and slug together also answers `200` — this is the case a naive uniqueness check gets wrong |

## Scenario 7 — a slug that is not URL-safe is refused

| # | Body | Expected |
|---|---|---|
| 7a | `{"name":"A","slug":"Tranh Sơn Dầu"}` | `400 VALIDATION_ERROR`, `details[].field` naming `slug` |
| 7b | `{"name":"B","slug":"tranh--son"}` | `400`, double hyphen refused |
| 7c | `{"name":"C","slug":"tranh-"}` | `400`, trailing hyphen refused |
| 7d | `{"name":"D","slug":"-tranh"}` | `400`, leading hyphen refused |
| 7e | `{"name":"E","slug":"tranh-son-dau"}` | `201` — a valid slug is not refused |

**7a is the check that matters**: an accented slug stored without complaint would produce a link
that breaks the moment a client uses it.

## Scenario 8 — the bounds hold, and are counted in characters

| # | Body | Expected |
|---|---|---|
| 8a | a `name` of 120 Vietnamese characters | `201` — accepted at the bound |
| 8b | a `name` of 121 characters | `400`, the field named |
| 8c | a `description` of 2000 characters | `201` |
| 8d | a `description` of 2001 characters | `400` |

**8a is the check a byte-counted bound fails.** 120 Vietnamese characters occupy about 360
bytes; a bound counting bytes would refuse it at roughly a third of the allowance, and every
ASCII test would still pass.

## Scenario 9 — a hidden category is unreachable and the order never moves

| # | Action | Expected |
|---|---|---|
| 9a | Create four categories with positions `0`, `10`, `10`, `20` | `201` each |
| 9b | `GET $BASE/categories` twice, and compare the two orders | Identical, including the two sharing a position |
| 9c | Add a fifth category at position `10` | The three at `10` keep a stable relative order; the ones at `0` and `20` do not move |
| 9d | Rename the category at position `0` | The order does not change — the tie-break is not the name |

**9d catches an implementation that tie-breaks on the name**, which would silently reorder the
catalogue on a rename.

## Scenario 10 — role enforcement

| # | Request | Expected |
|---|---|---|
| 10a | `GET $BASE/admin/categories` with no token | `401 UNAUTHENTICATED` |
| 10b | `POST $BASE/admin/categories` with the customer token | `403 FORBIDDEN` |
| 10c | `DELETE $BASE/admin/categories/$ID` with the customer token | `403 FORBIDDEN` |
| 10d | `GET $BASE/categories` with no token | `200` — the public surface needs no token |

## Scenario 11 — removal, and what it leaves behind

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE "$BASE/admin/categories/$ID" -H "$ADMIN"
```

| # | Expected |
|---|---|
| 11a | `204` |
| 11b | Absent from the public list and from the administrator list |
| 11c | Reaching it by slug answers the same `CATEGORY_NOT_FOUND` |
| 11d | `SELECT action, outcome FROM audit_logs WHERE action = 'CATEGORY_DELETED' ORDER BY occurred_at DESC LIMIT 1` shows a row with a success outcome |
| 11e | A second `DELETE` of the same identifier answers `404`, not `500` |

## Scenario 12 — two operators create the same name at once

Run ten concurrent creates of the same name:

| # | Expected |
|---|---|
| 12a | Exactly one answers `201`; every other answers `409 CATEGORY_NAME_TAKEN` |
| 12b | **No answer is `500`.** A duplicate-key violation surfacing as an internal error is a fail — the operator typed a valid request and deserves to be told the name is taken |
| 12c | `SELECT count(*) FROM categories WHERE normalized_name = '...'` is `1` |

**12b and 12c together are the point**: the storage index is what makes the count one, and the
classification is what makes the loser's answer useful. An application-only uniqueness check
passes 12a under light timing and fails 12c under load.

## Scenario 13 — pagination

| # | Request | Expected |
|---|---|---|
| 13a | `?page=1&pageSize=2` with five categories | `200`, two entries, `meta.total` is `5` |
| 13b | `?page=3&pageSize=2` | The fifth category |
| 13c | `?pageSize=101` | `400 VALIDATION_ERROR`, `details[].field` naming `pageSize` |
| 13d | `?page=0` | `400`, the field named |

## Automated gates

```bash
make lint          # format, tidy, vet, golangci-lint
make test          # unit
make test-integration   # needs Docker; proves the storage constraints
```

The storage-level guarantees — both unique indexes, both shape checks, the slug pattern and the
three length bounds — are asserted against real PostgreSQL, because a fake repository that
enforces a unique index proves only that the fake was written twice.

If `make test-integration` reports success without executing — check for a skip — it is a false
green and does not count.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| Products belonging to a category | No product entity exists | Module 04 Product |
| Removal blocked while products reference the category | Same reason; the reference lives on the product | Module 04; recorded in `deferred.md` |
