# Quickstart: Fix Verification Defects

Validation guide for the three corrected defects. Every scenario states the request, the
expected answer, and how to tell a real pass from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does
not. A scenario that *cannot run* in the current environment is reported as `BLOCKED` with
the reason — never silently skipped, because a skipped container-backed check is the exact
failure mode this feature exists to eliminate.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- API running with a local build: `make run`, or `go run ./cmd/api`.
- A customer account and an administrator account. The administrator comes from
  `ADMIN_EMAIL` and `ADMIN_PASSWORD` in `.env` via `make seed`.
- `curl` on the host.

Retrieve a customer token once and reuse it:

```bash
BASE=http://localhost:8080/api/v1
AUTH="Authorization: Bearer <customer access token>"
```

## Scenario 1 — an oversized image is refused with the image reason

The defect: a customer who declared their request length got the generic reason; one who did
not got the image reason. All three shapes below must now answer identically.

Prepare two real images — one over 2 MB, one well under it. The size must land **above
2,162,688 bytes** (the avatar route's ceiling plus its envelope room) and **below 4,194,304**
(the shared ceiling), otherwise the shared ceiling refuses the request before the avatar route
is ever reached and the scenario tests the wrong thing.

```bash
python3 - <<'PY'
import struct, zlib, os

def chunk(kind, data):
    body = kind + data
    return struct.pack('>I', len(data)) + body + struct.pack('>I', zlib.crc32(body) & 0xffffffff)

def png(width, height, path, randomise):
    # 8-bit RGB: one filter byte plus three bytes per pixel per row.
    row = bytes([0]) + os.urandom(width * 3) if randomise else bytes([0]) + b'\x7f\x7f\x7f' * width
    ihdr = struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)
    # Compression level 0 keeps random pixels from collapsing into a few bytes,
    # so the file is a real, decodable image of the intended size. A solid-colour
    # image compresses to almost nothing and would silently test the wrong thing.
    idat = zlib.compress(row * height, 0 if randomise else 6)
    data = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', ihdr) + chunk(b'IDAT', idat) + chunk(b'IEND', b'')
    open(path, 'wb').write(data)
    return len(data)

size = png(900, 900, 'over.png', randomise=True)
small = png(8, 8, 'small.png', randomise=False)
print(f"over.png  {size} bytes  (must be > 2162688 and < 4194304)")
print(f"small.png {small} bytes")
assert 2162688 < size < 4194304, f"over.png is {size} bytes: it must sit between the two ceilings"
PY
```

The script **asserts** the size is in range and fails loudly otherwise. Expected output is
about 2.4 MB for `over.png`; the exact figure varies because the pixels are random. A file
that lands outside the range makes every avatar scenario below pass for the wrong reason,
which is the failure mode this guide exists to prevent.

| # | Request | Expected |
|---|---------|----------|
| 1a | `curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/users/me/avatar -H "$AUTH" -F "file=@over.png"` | `413` with `USER_AVATAR_TOO_LARGE` |
| 1b | the same request with the body sent as chunks, so no length is declared | `413` with `USER_AVATAR_TOO_LARGE` |
| 1c | a small valid PNG | `200` |
| 1d | a `.txt` file renamed to `.png` | `400 USER_AVATAR_TYPE_UNSUPPORTED` |

**Confirming the response, not only the status** — a `413` alone is not a pass, because the
defect produced a `413` too:

```bash
curl -s -X POST $BASE/users/me/avatar -H "$AUTH" -F "file=@over.png"
# must contain "USER_AVATAR_TOO_LARGE" and must NOT contain "PAYLOAD_TOO_LARGE"
```

**The distinction that proves the defect is fixed**: scenario 1a previously answered
`PAYLOAD_TOO_LARGE`. If any shape still answers the generic reason, the fix is incomplete.

## Scenario 2 — the generic reason survives where it belongs

| # | Request | Expected |
|---|---------|----------|
| 2a | A JSON request larger than the shared ceiling | `413 PAYLOAD_TOO_LARGE` |

**Why this matters**: a fix that simply swapped the code everywhere would turn this into an
image error on a JSON route, which is worse than the original defect.

## Scenario 3 — the profile is untouched by a refusal

| # | Request | Expected |
|---|---------|----------|
| 3a | `GET $BASE/users/me` | `200`, `avatar` unchanged |
| 3b | inspect the stored row: `SELECT avatar_public_id, avatar_secure_url FROM users WHERE id='<id>'` | unchanged |

## Scenario 4 — registration when delivery cannot happen

The defect: `500` for a failure the customer did not cause, with an account left behind and
no way forward.

Make delivery fail. The simplest cause needs no provider account: point the mail setting at a
host that refuses connections, then restart the API.

```bash
SMTP_HOST=127.0.0.1 SMTP_PORT=9 make run   # port 9 discards; the send cannot succeed
```

| # | Request | Expected |
|---|---------|----------|
| 4a | `curl -s -X POST $BASE/auth/register -H 'Content-Type: application/json' -d '{"email":"probe@example.com","password":"Str0ng!Passw0rd"}'` | `503`, not `500` |
| 4b | the response body | names that nothing was delivered and that a new code can be requested; contains no provider detail and no address |
| 4c | `SELECT email, status FROM users WHERE email='probe@example.com'` | one row, `pending` — the account exists and is usable |
| 4d | `SELECT action, outcome, target_id FROM audit_logs WHERE target_id IS NOT NULL ORDER BY occurred_at DESC LIMIT 3` | a row for this registration with a failure outcome |
| 4e | the API log at that moment | one classified line; no code, no credential, no provider wording |

**Clean up** after the scenario so a later run is not confused:

```bash
docker compose exec -T db psql -U app -d artist_shop \
  -c "DELETE FROM users WHERE email='probe@example.com';"
```

The audit row is append-only by design and cannot be deleted — that is expected, not a
failure.

## Scenario 5 — the promise the `503` makes is true

This is the scenario that catches a half-finished fix. FR-006 tells the customer to request
a new code; the cooldown used to make that impossible for the first sixty seconds.

Immediately after scenario 4, **while the mail setting is still failing**:

| # | Request | Expected |
|---|---------|----------|
| 5a | `curl -s -X POST $BASE/auth/resend-verification -H 'Content-Type: application/json' -d '{"email":"probe@example.com"}'` | **not** the cooldown error |

Then restore a working mail setting, restart, and repeat:

| # | Request | Expected |
|---|---------|----------|
| 5b | resend again | accepted |
| 5c | verify the code from the mailbox | registration completes |

**The distinction that proves this scenario is real**: without the fix, 5a answers
`AUTH_RESEND_COOLDOWN`. That error, appearing here, means the cooldown disarm is missing.

## Scenario 6 — a flood is still refused

The cooldown was removed from the failure path, so the flow-level limit is the only thing
left and it must still hold.

| # | Request | Expected |
|---|---------|----------|
| 6a | 10 registration attempts in quick succession from one address | at least one is refused by the rate limit |

A run where every attempt succeeds is a **fail**, not a pass: FR-025 requires the flow-level
limit to still apply.

## Scenario 7 — the startup guard

| # | Configuration | Expected |
|---|---------------|----------|
| 7a | media configured, `MAX_BODY_BYTES=1048576` | API refuses to start; message names `MAX_BODY_BYTES` and both values |
| 7b | media configured, `MAX_BODY_BYTES=2162688` (equal to the route's ceiling) | API starts |
| 7c | media configured, `MAX_BODY_BYTES=4194304` | API starts |
| 7d | media credentials empty, `MAX_BODY_BYTES=1048576` | API starts, logs the avatar-unavailable consequence once |

**7a is the guard working**; the API exiting non-zero is the expected outcome, not a crash.
**7d** matters because a deployment without media must not be blocked by a ceiling that only
affects uploads.

## Scenario 8 — no secret escapes

Run scenarios 4 and 7 first so both failure paths have actually executed, then:

```bash
grep -riE 'api_key|api_secret|password|otp' logs/ 2>/dev/null
```

**Expected**: nothing from this feature. A credential or a verification code found in a log
is a **fail**, and FR-012 exists precisely because this path previously recorded a failure
with nothing to distinguish it from a success.

## Automated gates

The three defects each have failing tests written before the fix. After implementing:

```bash
make lint
make test
make test-integration
```

`make test-integration` needs Docker. If it reports success without executing — check for a
skip — the gate is a false green and does not count as a pass.

## Manual provider verification

Scenarios 1 to 8 need no provider account. Two outcomes can only be observed against the real
providers, and both are blocked until the operator's own configuration is correct:

| Outcome | Blocked by | What to do |
|---|---|---|
| A real upload succeeds and the stored width is at most 512 px | The media cloud name and credentials | Cloudinary console → Settings → Account details → Cloud name. **This is not the folder name.** A wrong value answers `401 Invalid cloud_name` |
| A verification message reaches a real inbox | The mail provider's authorised sending address | Authorise the sending address in the provider's console. A wrong value answers `525 Unauthorized IP address` |

**How to tell a configuration problem from an outage**: the log line's classification
distinguishes them, and neither ever contains provider wording or a credential. Report either
as a configuration item, not a defect in this feature.
