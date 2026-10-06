# Phase 1 Data Model: Fix Verification Defects

This feature adds **no table, no column and no migration**. It writes rows to tables that
already exist and it changes no stored state beyond what the existing rules already permit.
What follows records what each requirement touches, so the absence of a schema change is
verifiable rather than assumed.

## Existing entities this feature reads

### Account

The row a registration creates before its verification message is sent.

| Attribute | Type | Relevance here |
|---|---|---|
| `id` | uuid, PK | The reference the audit trace uses. Never restated as the email address in metadata |
| `email` | text, unique | Never copied into a client response, a log line or audit metadata |
| `status` | text | Unchanged. A failed delivery leaves the account pending, exactly as a successful delivery would have |
| `password_hash` | text | Untouched |

**Invariant preserved**: the account is created and committed before the message is
attempted. This feature does not reorder that. FR-007 makes the consequence explicit — the
account is kept, and its pending state is the state the existing resend flow already
operates on.

### Verification code

Not a table. A Redis entry keyed by the account's address, holding a hash of the code, with
its own time-to-live, plus two markers beside it.

| Entry | Purpose | Behaviour in this feature |
|---|---|---|
| the code hash | Verification | Unchanged. Created at issue time, expires on its existing lifetime |
| the *sent* marker | The resend cooldown | **Disarmed when delivery fails** (FR-024). A send that did not happen must not consume the cooldown budget |
| the *attempts* counter | Brute-force protection | Unchanged. A failed delivery does not consume an attempt, because no code was ever given to anyone |

**Invariant added**: only the most recently issued code is ever accepted. A code that a
newer request replaced can never complete verification, and the code issued for a failed
attempt does not outlive its existing lifetime (FR-018).

## Existing entity this feature writes

### Audit entry

The row that FR-011 requires a failed registration to leave behind. It does not exist today
for this path, which is the gap.

| Attribute | Value for a failed registration | Rule |
|---|---|---|
| `event_id` | freshly generated | must be unique; the writer already relies on this for idempotency |
| `actor_id` | empty | the customer is not authenticated when registering, so there is no actor to name |
| `actor_role` | empty | same reason; naming a role here would be inventing one |
| `action` | the registration action, newly added | identifies the event that did not complete |
| `target_type` | the account reference type | consistent with every other trace in the trail |
| `target_id` | the account's id | the reference, **never** the address |
| `outcome` | the failure outcome | the row states plainly that the event did not complete |
| `metadata` | the failure classification only | no code, no credential, no provider wording, no address |
| `correlation_id` | the request's | ties the audit row to the log line and to the client-visible request id |
| `occurred_at` | event time | unchanged convention |

**Invariant**: a successful registration leaves the same audit trace it leaves today. Only
the failed path's traceability changes (FR-013).

## State transitions

### Registration with a failed delivery

```
  account created (committed)        ← unchanged, happens first
        │
        ▼
  code issued, cooldown armed         ← unchanged, happens second
        │
        ▼
  delivery attempted ──── success ────► account pending, customer holds a code
        │
      failure
        │
        ▼
  classified                          ← new: transient or configuration
        │
        ├── configuration ──► not retried; operator sees why (FR-016)
        │
        └── transient ──► retried within budget, still failing ──► budget exhausted
                                                  │
                                                  ▼
                                    audit entry + classified log line + 503
                                    cooldown disarmed, code untouched
```

The recovery path the customer is pointed at:

```
  customer requests a new code immediately
        │
        ▼
  no cooldown stands in the way (FR-024) ──► flow-level rate limit still applies (FR-025)
        │
        ▼
  new code issued, replaces the previous one ──► previous code no longer accepted
```

## Avatar refusal — no state change

A refused avatar upload writes nothing. FR-005 requires the stored profile and any previously
stored avatar to be exactly as they were, which is why the refusal reason can change freely:
the reason is part of the response, not of the stored state.

The three shapes a client may send, and the single answer they must all receive:

| Client behaviour | Reached ceiling by | Answer |
|---|---|---|
| declares its length | the route's own ceiling, before the read | image-too-large reason |
| declares nothing | the handler's read ceiling | image-too-large reason |
| declares a length smaller than it sends | the handler's read ceiling, once the lie runs out | image-too-large reason |
| an image exactly at the ceiling | never | accepted |
| a non-avatar request over the shared ceiling | the shared ceiling | generic request-too-large reason, unchanged |

## What this feature does not change

- No schema, no migration, no column, no index.
- No stored state is rewritten, backfilled or repaired. Existing rows are left exactly as
  they are, including accounts that failed delivery before this feature.
- No new entity, no new identifier, no new external reference.
