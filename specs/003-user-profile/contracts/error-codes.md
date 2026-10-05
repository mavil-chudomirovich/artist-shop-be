# User Module Error Codes

Module-specific codes added to the shared catalogue
(`specs/002-cross-cutting-foundation/contracts/error-codes.md` and
`internal/share/httpx/errors.go`). Each code is stable and machine-readable; clients
should branch on `error.code`, never on `error.message`.

| Code | HTTP | Meaning | Client guidance |
|------|------|---------|-----------------|
| `USER_NOT_FOUND` | 404 | No account with that identifier | Administrator lookup only; do not retry |
| `USER_ADDRESS_NOT_FOUND` | 404 | The address does not exist **for the calling account**, or is hidden | Treated identically whether the id is unknown, hidden, or belongs to somebody else, so it never confirms another customer's address exists |
| `USER_INVALID_PHONE` | 400 | Not a valid Vietnamese mobile number | Check `error.details[].field == "phone"`; show the message next to the input |
| `USER_UNKNOWN_PROVINCE` | 400 | Province code is not in the official dataset | Refresh the province list; the client sent a stale or tampered value |
| `USER_UNKNOWN_WARD` | 400 | Ward code is not in the official dataset | Refresh the ward list for the selected province |
| `USER_WARD_PROVINCE_MISMATCH` | 400 | The ward does not belong to the chosen province | The two selects are out of sync; reload the ward list |
| `USER_AVATAR_TYPE_UNSUPPORTED` | 400 | The uploaded bytes are not JPEG, PNG or WebP | Ask for a supported image; do not retry unchanged |
| `USER_AVATAR_TOO_LARGE` | 413 | The image exceeds the 2 MB ceiling | Compress before uploading; not retryable unchanged |
| `USER_MEDIA_UNAVAILABLE` | 503 | The media service rejected or could not store the upload | Retryable with backoff. The profile is unchanged, so the customer can try again safely |

## Design notes

**Why `USER_AVATAR_TYPE_UNSUPPORTED` is 400 and not 415.** The request itself is a
well-formed `multipart/form-data` body, so the transport-level media type is
correct. What is unsupported is the *content of the uploaded file*, which is a field
validation problem. Using the shared `UNSUPPORTED_MEDIA_TYPE` (415) would conflate
"you sent the wrong content type for this endpoint" with "the file inside is the
wrong kind of image", which is a different client fix.

**Why the address 404 is deliberately vague.** Returning a distinct code for "exists
but belongs to someone else" would let any signed-in customer probe whether a given
address id exists in the system. One code for unknown / hidden / not-yours keeps
FR-006 and FR-013 honest.

**Why a failed upload leaves the profile unchanged.** `USER_MEDIA_UNAVAILABLE` is the
only code here where retrying makes sense, and it is safe to retry precisely because
the previous avatar was not touched.

## Codes deliberately not added

| Situation | Code used | Why not a module code |
|-----------|-----------|----------------------|
| Missing or invalid session | `UNAUTHENTICATED` (shared) | Already defined by the foundation for every module |
| Signed-in customer calling the administrator lookup | `FORBIDDEN` (shared) | The rule is role-based, not user-specific; the auth module already established `FORBIDDEN` for role denial |
| Address list page out of range | `VALIDATION_ERROR` (shared) | Pagination validation is a foundation concern |
| A structurally invalid address member, such as an empty `recipientName` on `PATCH` | `VALIDATION_ERROR` (shared) with `details[].field` naming the member | The rule is request shape, not a user-domain rule; the detail carries the member name, which is what the client needs (FR-020) |
| Media adapter returned an unmapped failure | `INTERNAL_ERROR` (shared) | Never leak provider detail to clients; the cause is logged with the correlation id |