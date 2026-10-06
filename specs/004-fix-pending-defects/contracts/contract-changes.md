# Contracts: Fix Verification Defects

The feature adds **no endpoint** and changes **no request or response shape**. What changes
is the status and error code on two paths that already exist. Both are recorded here so the
contract stays the source of truth rather than drifting behind the code.

## 1. Avatar upload refusal — reason corrected

**Endpoint**: `POST /api/v1/users/me/avatar` (unchanged)

Nothing about the request changes: still a `multipart/form-data` body with one `file` part.

Only the response changes, and only in one branch.

| Condition | Before | After |
|---|---|---|
| Image over the 2 MB ceiling, **request length declared** | `413 PAYLOAD_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` |
| Image over the 2 MB ceiling, **no length declared** | `413 USER_AVATAR_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` (unchanged) |
| Image over the 2 MB ceiling, **length understated** | `413 USER_AVATAR_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` (unchanged) |
| Image exactly at 2 MB | `200` | `200` (unchanged) |
| Not JPEG, PNG or WebP by content | `400 USER_AVATAR_TYPE_UNSUPPORTED` | unchanged |
| Body that is not multipart, or no `file` part | `400 VALIDATION_ERROR` | unchanged |
| Media service unavailable or unconfigured | `503 USER_MEDIA_UNAVAILABLE` | unchanged |

**Client impact**: strictly narrower. `USER_AVATAR_TOO_LARGE` is now the only answer for an
oversized image, so a client no longer has to handle two different reasons for the same
mistake, and the generic reason once again means what it says everywhere else — a request
that is too large for a reason unrelated to the image.

**Not changed**: `PAYLOAD_TOO_LARGE` stays on every non-avatar route, including any JSON
request over the shared ceiling.

**Detail shape**: unchanged. The refusal names the `file` member and tells the client to
compress the image, as `USER_AVATAR_TOO_LARGE` already does.

## 2. Registration when delivery fails — status corrected

**Endpoint**: `POST /api/v1/auth/register` (unchanged)

Nothing about the request changes.

| Outcome | Before | After |
|---|---|---|
| Message delivered | `202` with the generic accepted body | `202` (unchanged) |
| **Message could not be delivered** | `500 INTERNAL_ERROR` | `503` with the new service-unavailable code |

**Body shape on the new failure path**:

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "The confirmation email could not be sent. Nothing was delivered to your address; request a new confirmation code and try again.",
    "requestId": "..."
  }
}
```

**Client impact**: a client can now tell this apart from a malformed request. `500` invites a
client to treat the failure as its own fault and retry blindly, which never works; `503`
invites it to tell the user to wait and retry, which does.

**Disclosure guarantee**: the body reveals no provider detail, no credential and no recipient
address, and it never reveals whether the address was already registered — a failed attempt
for an address that does not exist and one that does are indistinguishable.

**Preserved guarantee**: registration still cannot be used to discover whether an email is
registered. A successful attempt still answers the same generic `202`.

## 3. Account verification message retry

**Endpoint**: `POST /api/v1/auth/resend-verification` (unchanged)

No contract change. What changes is that a customer arriving here immediately after a failed
registration is no longer refused with the cooldown error — the promise made in the `503`
above is now true (FR-024).

| Situation | Before | After |
|---|---|---|
| New code requested within 60s of a **failed** delivery | `429`/`AUTH_RESEND_COOLDOWN` | accepted, new code issued |
| New code requested within 60s of a **successful** delivery | `AUTH_RESEND_COOLDOWN` | unchanged |
| New code requested after the cooldown elapses | accepted | unchanged |
| Repeated requests beyond the flow-level rate limit | refused | unchanged (FR-025) |

## 4. Provider error classification

Not an endpoint. A classification value recorded in the diagnostic, used only for the
operator-facing log line and the audit metadata.

| Classification | Means | Retried? |
|---|---|---|
| configuration | the provider rejected because this deployment is not set up correctly; a retry cannot change it | no |
| transient | the provider is temporarily unable; a retry may succeed | yes, within the budget |
| refused | the provider answered with a status that indicates the request itself was unacceptable | no |
| unreachable | the provider could not be contacted at all | yes, within the budget |

The values are this system's own. Provider wording is never stored, so a provider changing
its copy cannot alter what an operator sees (FR-022).

## 5. Startup refusal — operator-visible

Not an endpoint. When media upload is configured and the shared request-size ceiling is
strictly below the avatar route's own ceiling, the service refuses to start.

The message names the setting and both values, so the operator can correct it without reading
source or documentation (FR-020):

```
fatal: MAX_BODY_BYTES is 1048576 but the avatar upload route requires at least 2162688; raise MAX_BODY_BYTES or remove the media configuration
```

| Configuration | Outcome |
|---|---|
| media configured, ceiling **below** the avatar route's | refuses to start, message names both values |
| media configured, ceiling **equal** to the avatar route's | starts |
| media configured, ceiling **above** | starts |
| media **not** configured, any ceiling | starts, logs the avatar-unavailable consequence once, no credential value |

## Documentation obligations

The following change with the code, in the same change (Constitution VIII):

- `docs/api-reference.md` — the registration endpoint's status list gains the new failure
  branch; the avatar endpoint's `413` reason is now unambiguous.
- `docs/configuration.md` — the shared ceiling's entry states the coupling and the refusal.
- `specs/003-user-profile/contracts/openapi.yaml` — the avatar upload `413` description
  already names `USER_AVATAR_TOO_LARGE`, so it needs **no** change: the contract was
  correct and the code was wrong. Verified, not assumed.
- `specs/003-user-profile/contracts/error-codes.md` — records the new registration failure
  branch alongside the existing "codes deliberately not added" table.
- `docs/decisions/010-*.md` — the startup guard and the refusal-reason correction.
