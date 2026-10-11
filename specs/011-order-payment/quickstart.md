# Quickstart: Online Order Payment

Validation guide. Every scenario states the request, the expected answer, and how to tell a real pass
from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not. A
scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped.

**A note on the paid path**: the order reaches `PAID` only when the provider confirms, and the provider's
webhook cannot be simulated over plain HTTP. Scenarios marked *test* drive the webhook handler directly
against the provider's **documented signature vectors**, so the crypto is proven without a live account.

## Prerequisites

- Dependencies running: `make up-tools`.
- Migration applied: `make migrate-up` — this feature adds `migrations/00012_payment.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- A signed-in customer token (`CUSTOMER`) with **an order awaiting payment**, and an **administrator**
  token (`ADMIN`).
- An order in `PAYMENT_PENDING` with goods held: from feature 010 — check out, then let the operator
  confirm (`POST /api/v1/admin/orders/{id}/confirm`).
- PayOS settings present in `.env`: `PAYOS_CLIENT_ID`, `PAYOS_API_KEY`, `PAYOS_CHECKSUM_KEY`,
  `PAYOS_BASE_URL`, `PAYMENT_RETURN_URL`, `PAYMENT_CANCEL_URL`. **Leave `PAYOS_*` empty and the module
  runs with a log-only fake provider** — every non-provider scenario still runs.
- A public webhook URL for a real end-to-end payment (the VPS domain, or a tunnel); see *not verified
  here* at the end.

Set `BASE=http://localhost:8080/api/v1`, `CUSTOMER=...`, `ADMIN=...`, `O=<order id>`.

**On Windows, never send a JSON body inline.** Write the body to a file and send it with
`--data-binary @file`.

## Scenario 1 — the customer starts a payment

| # | Action | Expected |
|---|---|---|
| 1a | `POST $BASE/orders/{O}/payment` | `200`, `status: "PENDING"`, an `amount` equal to the order total, a `checkoutUrl` |
| 1b | `POST $BASE/orders/{O}/payment` **again** | `200`, the **same** `checkoutUrl` — the live payment was reused (no second one) |
| 1c | `POST $BASE/orders/{O}/payment` with another customer's order id | `404 PAYMENT_ORDER_NOT_FOUND` |
| 1d | `POST $BASE/orders/{O}/payment` for an order still awaiting the artist | `409 PAYMENT_ORDER_NOT_PAYABLE` |
| 1e | `GET $BASE/orders/{O}/payment` | `200`, the same payment and its status |

**1b is the check the clarification settled**: a repeat start reuses the live link.

## Scenario 2 — the webhook applies the payment exactly once *(test)*

Driven against the provider's documented signature vectors (no live account):

| # | Action | Expected |
|---|---|---|
| 2a | a webhook with the documented valid signature and matching amount | `200`; the payment is `PAID`; the order is `PAID`; the goods are **sold** (physical fell, held cleared) |
| 2b | the **same** webhook again | `200`; nothing applies a second time (physical unchanged) |
| 2c | a webhook whose signature was tampered with | `401 PAYMENT_WEBHOOK_INVALID_SIGNATURE`; the order is untouched |
| 2d | a webhook for an unknown order code | `404 PAYMENT_NOT_FOUND`; nothing is recorded (only a warning is logged) |
| 2e | a webhook with a **different amount** | `200` (the provider is answered); the payment is `UNMATCHED`; the order is **not** `PAID` |

**2b and 2e are the two halves of the guarantee**: a replay does nothing, and a mismatch never
corrupts the order.

## Scenario 3 — a superseded or late confirmation is never applied *(test)*

| # | Action | Expected |
|---|---|---|
| 3a | create a payment, then **edit the order** (feature 010) — the order returns to `PENDING` and the artist re-confirms it | the order is `PAYMENT_PENDING` again at a **new version** |
| 3b | the **old** payment's confirmation arrives | `200`; the old payment is `UNMATCHED`; the order is **not** `PAID` |
| 3c | a confirmation for an order cancelled or expired | `200`; `UNMATCHED`; the order is not `PAID` |

**3b is the check the version is for**: after a re-confirmation the order carries the same status at a
new version, so only the version tells the shop the link is stale.

## Scenario 4 — cancelling an unpaid order cancels its payment

| # | Action | Expected |
|---|---|---|
| 4a | start a payment, then `POST $BASE/orders/{O}/cancel` | `200`; the order is `CANCELLED`; the payment is `CANCELLED`; the goods return to availability |
| 4b | the provider refuses the cancel | the order is still `CANCELLED` and the goods still released; the failure is logged (not an error answer) |

## Scenario 5 — the operator's desk

| # | Action | Expected |
|---|---|---|
| 5a | `GET $BASE/admin/payments?status=UNMATCHED` | `200`, the reconciliation queue only, newest first |
| 5b | `GET $BASE/admin/payments` with a customer's session | `403 FORBIDDEN` |

## Scenario 6 — storage and concurrency guarantees (integration)

| # | Action | Expected |
|---|---|---|
| 6a | insert a payment with a status outside the four | refused by `payments_status_ck` |
| 6b | insert a payment with `amount = 0` | refused by `payments_amount_ck` |
| 6c | insert two payments with the same `order_code` | refused by `payments_order_code_key` |
| 6d | insert two payments with the same confirmed `provider_reference` | refused by the partial unique index |
| 6e | mark a payment `UNMATCHED` **without** a reason, or give a reason to any other state | refused by `payments_unmatched_ck` |
| 6f | delete a payment | its events are gone with it |
| 6g | two concurrent confirmations of the same webhook | both answer `200` and the sale is applied **once** |

## Automated gates

```bash
make lint              # format, tidy, vet, golangci-lint
make test              # unit, including the signatures (documented vectors) and the state machine
make test-integration  # needs Docker; proves apply-once, the stale version, concurrency and the schema
make swagger           # regenerate docs/swagger after the handler annotations
```

If `make test-integration` reports success without executing — check for a skip — it is a false green.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A **real** PayOS payment (link → bank transfer → webhook) | Needs live merchant credentials and a public webhook URL | Run once against the real provider when the credentials and domain exist |
| PayOS webhook URL registration (`POST /confirm-webhook`) | A one-time setup step against the live provider | Recorded in `deferred.md` as an operational task |
| Refunds, multi-currency, recurring payments | Out of MVP scope (ADR 015 §5) | A later feature each |
