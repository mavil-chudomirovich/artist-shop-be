# Phase 0 Research: Fix Verification Defects

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: Where the avatar refusal reason is decided

**Decision**: Change the reason reported by the avatar route's own body limit, and leave the
generic reason intact everywhere else.

**Rationale**: The refusal a customer sees for an oversized image is produced in two places,
and they disagree. The route carries its own ceiling that refuses on the *declared* request
length before the handler reads a byte, and that path reports the generic request-too-large
reason. The handler's own read ceiling, reached when no length was declared or the declared
length was a lie, already reports the correct image-too-large reason. So the same mistake
produces two different answers depending on the client. The shared middleware is used by
every other route, and those routes have no image semantics, so it must keep the generic
reason.

**Alternatives considered**: Remove the route-level ceiling so the handler's read ceiling
decides everything — rejected, because the refusal then arrives late, after part of the
upload has been buffered, and the declared-length case stays unbounded until the read
finishes. Add a size hint to the shared middleware so it can distinguish uploads from other
requests — rejected: it puts an image concept into a layer that serves JSON routes too.

## D2: How the shared ceiling is compared against the avatar ceiling

**Decision**: A single exported figure owned by module User, read by the composition root,
which refuses to start when the shared setting is strictly below it.

**Rationale**: Two numbers have to agree: the shared request ceiling and the ceiling an
avatar route installs. Today nothing compares them, and nothing can, because the second is
a route's concern. The figure must have exactly one owner or the comparison protects the
wrong number. The composition root is the only place that sees both, and it is already where
the modules are wired, so the check costs one call and one clear failure.

**Alternatives considered**: Compare inside the shared configuration — rejected under
Constitution I, which forbids the shared layer from importing a module's types; the only way
to do it is to copy the literal, creating the second copy this is meant to eliminate. Warn
instead of refusing — rejected: a warning scrolls past, and the failure then surfaces as a
customer-visible symptom that points at the wrong setting.

**Boundary detail**: strictly below refuses; equal starts. Equal is safe because the route's
own ceiling is the binding constraint at that point, and refusing it would break a
configuration that works.

**Coverage limit, recorded rather than hidden**: the guard lives in the API composition, so
`cmd/migrate` and `cmd/seed` are not covered. That is correct rather than an omission — they
do not build the avatar route and do not serve uploads.

## D3: Whether a failed delivery consumes the resend cooldown

**Decision**: No. A failed delivery disarms the cooldown marker; the customer may request a
new code immediately.

**Rationale**: The cooldown marker is armed at code-issue time, which is *before* the send is
attempted. So a failed delivery leaves the customer inside a cooldown they cannot see, for a
reason they did not cause. FR-006 promises them they can request a new code; that promise is
false for exactly the sixty seconds when they most need it. Disarming the marker on failure
makes the promise true. This is also the more honest behaviour: the marker records "a message
was sent recently", and no message was sent.

**Alternatives considered**: Keep the cooldown and state the wait in the `503` — rejected:
accurate, but it hands the customer a dead end caused by our infrastructure failure. Waive the
cooldown only on the first failure per account — rejected: it adds per-account state to solve
a problem the unconditional disarm already solves.

**Why this is not an abuse risk**: the cooldown limits how often a message is sent. Removing
it on the failure path only means a failed send does not count toward that budget. The
flow-level rate limit still applies to the registration request and to the retry, so the
sending rate stays bounded. FR-025 requires that limit to be asserted, not assumed.

## D4: What a failed registration leaves behind

**Decision**: Both traces — an audit entry with a failed outcome identifying the account it
created, and one classified log line for the operator.

**Rationale**: The account creation is a real state change and Constitution VI requires it to
be auditable. Today a failed registration creates an account and writes **no** audit row at
all, so an operator reconciling accounts has no record that anything happened. One trace is
not enough on its own: the audit row is for reconciliation and cannot be read at a glance
during an incident, and a log line alone is not the durable record the constitution asks
for. Neither may carry the verification code, a credential, or the provider's own wording —
the account is identified by reference, and the reason by classification.

**Alternatives considered**: Audit only — rejected, an operator diagnosing an incident would
have to query stored data first. Log only — rejected, it leaves the constitution's audit
requirement unmet. Record the failure as a successful registration — rejected, it would state
something untrue.

## D5: How a delivery failure is classified before deciding to retry

**Decision**: Classify on the provider's own status category, not on its message text.

**Rationale**: Retry is only correct for a failure that can resolve itself, and refusing is
only correct for one that cannot. Providers change their wording between releases; keying on
the text means a copy edit silently converts a permanent failure into a retried one, which
slows every request that hits it. The status category is the stable contract.

**Consequence for the observed failure**: the mail provider rejected our test send with an
"unauthorized IP address" status, which is a configuration problem — an IP must be authorised
in the provider's console before any send can succeed. Retrying it cannot help, so that class
is refused immediately, and the operator sees a classification that says so.

**Recorded limitation**: a provider that reports a permanent configuration problem under a
status category indistinguishable from a transient one would be retried and then refused after
the budget runs out. The customer-visible outcome is identical either way; only the latency
differs.

## D6: How a verification code behaves after a failed delivery

**Decision**: Unchanged. The code is created, stored and expired exactly as today, and only
the most recently issued code is ever accepted.

**Rationale**: Changing a code's lifecycle is a security change, and this feature exists to
fix three reporting and recovery defects — not to reopen how codes are stored. The one
condition worth pinning is that a code replaced by a newer request can never complete
verification, and that a code issued for a failed attempt does not outlive its existing
lifetime. Both hold today and both are now asserted.

**Alternatives considered**: Issue a code only once the provider accepts the message —
rejected: it would make account creation depend on provider latency, and a provider timeout
would fail a legitimate registration. Clear the code when delivery fails — rejected, it is
a second lifecycle rule with no benefit here, and it would make the customer's immediate retry
behave differently from their second one.

## D7: Whether a durable sending queue is warranted

**Decision**: No. Retry in-request within a bounded budget.

**Rationale**: A durable queue means new infrastructure — a worker, its storage, its retry
policy and its own failure mode — for a failure mode the mail provider does not exhibit.
The observed failure was a configuration problem, which no queue can fix. The transient case
a bounded retry does cover is the one worth covering.

**Alternatives considered**: Answer the ordinary accepted response and send later — rejected,
it hides the failure from the customer and is the mechanism that made the original defect so
hard to diagnose. Rejected at the clarify stage, recorded here so the reasoning survives.

## D8: Where the code changes land

**Decision**: Three locations, all existing files — the avatar refusal reason in the user
module's presentation layer, the retry and cooldown disarm in the auth module's application
layer, and the startup guard in the API composition.

**Rationale**: Each defect already has an owner, and none of them needs a new abstraction. The
presentation layer owns what a refusal says. The application layer owns whether a send is
retried and what state a failure leaves. The composition owns whether the service starts. A
feature this size that invented a new cross-cutting abstraction would cost more to understand
than it saves.

**Layering check**: `domain` is untouched in all three. The retry classification and the
cooldown disarm are business rules, so they belong in `application`, not in the transport
adapter; the adapter exposes the disarm, the rule decides when to use it.

## Open questions carried into implementation

None. Every decision above is settled; the tasks phase can proceed without asking.
