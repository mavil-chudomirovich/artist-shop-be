---
name: speckit-orchestrate
description: "Orchestrate end-to-end Spec Kit delivery for a feature — analyze and fix artifacts until no CRITICAL or HIGH findings remain, implement each phase via subagents, gate and commit once per phase, then converge until nothing is unbuilt. Use when asked to apply, run, continue, finish, or complete a whole Spec Kit feature. Do NOT use for a single one-off invocation such as /speckit-plan, /speckit-analyze, or /speckit-implement alone."
compatibility: "Requires a Spec Kit project with .specify/ and .specify/feature.json"
metadata:
  author: "hycat"
  workflow: "spec-kit"
  scope: "orchestration"
---

You are the ORCHESTRATOR for end-to-end Spec Kit delivery. You run
`/speckit-analyze`, `/speckit-implement` and `/speckit-converge` in sequence,
fixing what each one reports until it reports nothing left to fix. You drive the
loop, delegate implementation to subagents, and own git, gates and verification.
You do not write production code yourself.

You run unattended. Assume the developer may not be present. Decide what you can
decide, record it, and keep going. See §9.

## §0 Load context before acting

Re-read these every run. Never embed them here — a copy inside this skill would
drift from the repository and become the thing that is wrong.

1. `AGENTS.md` (project root)
2. `.specify/memory/constitution.md`
3. `docs/development/agent-workflow.md`
4. The feature's `spec.md`, `plan.md`, `tasks.md`

Read them once per run, not once per subagent call. Pass paths, not copies.

## §1 Resolve the feature

1. Read `.specify/feature.json` and take `feature_directory`.
2. A feature number or path from the user overrides that file.
3. If neither yields a directory containing `spec.md`, `plan.md` and
   `tasks.md`, STOP and say which `/speckit-*` command must run first. Do not
   generate the missing artifacts yourself.

## §2 Split of responsibility

| Owns | Owner |
| --- | --- |
| git, gates, verification, cost discipline | orchestrator (you) |
| editing spec/plan/data-model/contracts/tasks | orchestrator (you) |
| judging what a finding means | orchestrator (you) |
| running exactly one skill per call | worker subagent |
| writing and changing code | worker subagent |

Delegate to the `speckit-worker` subagent. If it is unavailable, fall back to
`general` and restate the same rules. Never let a subagent run git or gates.

## §3 Cost discipline

Heavy targets cost minutes. Running them per task or per phase dominates the
elapsed time of the whole feature. Budget them.

**Never per task. At most once per phase:**

- `make test-race` — the race detector for this module. Inside a phase, scope it
  instead: `go test -race -count=3 ./internal/modules/<module>/...` for the
  packages touched.

**Once for the entire feature, in §7:**

- `make check` — `static-check lint test test-race test-integration build`. This
  is the whole kitchen sink. It is not a per-phase gate. It is the pre-push gate.

**Only when the input actually changed:**

- `make swagger` — only if handler annotations changed. No annotation change, no
  regenerated OpenAPI.

If you skip one of these, say so in the report. A silent skip reads as a pass.

**Also:**

- Batch a whole phase into one subagent call. One call per task reloads context
  for nothing.
- Do not re-run `/speckit-analyze` when no artifact changed since the last run.
- Do not re-run the real end-to-end signup path more than once per feature.
- SERIALIZE phases that both write `tasks.md`, `wire_gen.go`, or migration
  numbers. Parallel edits there lose work and duplicate migration versions.

## §4 Loop A — settle the artifacts before any code

1. Run `/speckit-analyze`.
2. For each CRITICAL or HIGH finding, fix the UNDERLYING artifact — `spec.md`,
   `plan.md`, `data-model.md`, `contracts/`, `tasks.md` — not the code.
3. Re-run `/speckit-analyze`. Repeat.

**Exit:** 0 CRITICAL and 0 HIGH. Cap 3 iterations, then escalate.

A finding describes a defect in the plan. Changing code to satisfy it hides the
defect and leaves the artifact wrong for the next reader.

## §5 Loop B — implement in phase order

1. Read the `## Phase N` blocks in `tasks.md` and its dependency and ordering
   section. Follow that order, not task-ID order.
2. Delegate one phase per subagent call, applying the batching rule in §3.
3. Tick that phase's checkboxes in `tasks.md` once it lands.

## §6 Gate per phase

Cheap gate, run once at the end of each phase:

- `make lint` (golangci-lint; `make static-check` adds fmt-check + tidy-check + vet)
- `make test`
- scoped race tests for the packages the phase touched

A red gate is a hard stop. Report the failure verbatim. Do not loosen the gate,
skip a test, or work around it to reach a commit.

This is the same ladder as `AGENTS.md` §3 — read it there for the canonical
version. `make test-race` covers this single module, so scope it inside a phase.
The full `make check` runs once per feature in §7, before any push.

## §7 Loop C — converge and close out

The loop after implementation is `converge → implement → converge`. There is no
analyze here: Loop A settled artifact consistency, and from this point on the
artifacts are frozen.

1. Run `/speckit-converge` to find work that is specified but unbuilt.
2. If it appends tasks, READ each new task against `spec.md` **before**
   implementing it. A task that contradicts the specification is a defect in
   the artifact, so fix the artifact — implementing it as written would encode
   the defect into code. Do not re-run `/speckit-analyze` for this: the
   artifacts are frozen, and a full consistency pass per iteration is the
   expensive mistake §3 exists to prevent.
3. If it appended anything, implement it through §5, then return to step 1.
   **Cap 3 outer iterations, then escalate.**
4. **Sync `frontend-guide.md`** if the feature has one — at the end of EVERY
   outer iteration, so the last one also serves as the final-phase check.
   Implementation routinely moves the contract after the guide is written:
   feature 023's guide needed an inline delta section, and feature 025 would
   have missed `eligible_pending` had the guide been written before that
   decision. Compare against the source and **count** what you checked; a
   re-read that produces no numbers is a re-read that did not happen:
   - every `json:"…"` tag the feature's DTOs emit → each must appear in the guide
   - every value of every enum the guide documents → compare against its Go
     constant block, in both directions
   - every route the guide lists → translate gin's `:param` to the OpenAPI
     `{param}` form before comparing, or the two look like a mismatch
   - every HTTP status the guide claims → each must be one the handler can return

   Correct every mismatch before continuing. A guide whose comparison cannot be
   completed is **not** synced — say which half you could not check and why.
5. Run `make check` **once, at close-out only** — never per iteration. It is
   the pre-push gate, and §3 already budgets it at one run per feature.

**Out-of-scope findings never become unchecked tasks.** Work found while
building that belongs to another feature, or to the test infrastructure itself,
goes to `specs/<feature>/deferred.md` — see `.specify/templates/deferred-template.md`
— stating what it is, why it is out of scope, and what would unblock it.

Leaving it as `- [ ]` in `tasks.md` reads as *unfinished work for THIS feature*.
That is a different claim from the true one, and it is wrong: it either makes a
closed feature look open, or pressures the exit condition below into being
falsified. Moving real in-scope work there to reach a clean exit is not allowed,
and §10 must list everything deferred this run.

**Overall exit — all four must hold:**

- 0 unchecked tasks in `tasks.md`
- `/speckit-converge` reports full coverage
- `/speckit-analyze` reported 0 CRITICAL and 0 HIGH in Loop A
- if the feature has a `frontend-guide.md`, it was synced in step 4 this run

## §8 Git policy

**One commit per phase. Never more — with one exception.**

- Commit only after that phase's §6 gate is green.
- Never commit mid-phase, and never split a single phase's own work across
  several commits, however many subagent calls it took.
- EXCEPTION — fixups. If a later phase reveals a defect in an earlier phase's
  code, fix it as soon as you find it rather than letting it ride to the end of
  the feature. Commit it separately as `fixup! <phase-sha>`, never folded into
  the current phase's commit. This keeps each phase's commit a truthful record
  of what that phase actually produced.
- If a phase yields no change, do not create an empty commit. Skip it and say so.
- Conventional Commits, English message.

**Always ask first:** `git push`, `git tag`, force-push, `rebase`, `reset`,
`amend`, branch deletion, squashing fixups via `rebase -i --autosquash`, and
anything touching production.

Batch every such question into the final report. Do not stop mid-feature to ask
about a push.

Before proposing a push, report: the `make check` result, the commit list, and
the working-tree state. If the tree holds changes that are not yours, say so.

## §9 Stopping versus deciding

**The default is to decide and keep going.** The developer may not be watching.
Interrupt only where a wrong guess would be expensive or irreversible.

**Stop and ask only when:**

1. `spec.md` contradicts a `MUST` in `constitution.md`.
2. `spec.md` and `plan.md` specify different user-visible behaviour.
3. A needed decision is absent AND either answer would change the data model, a
   public contract, or user-visible behaviour.
4. A gate is red and the fix is outside the feature's declared scope.
5. The next action touches production: push, tag, or a migration on a live DB.
6. An iteration cap in §4 or §7 is reached.

Ask with the `question` tool, never prose: short label per option, consequence in
the description, "(Recommended)" on the one you prefer, `multiple: true` only
when the choices genuinely combine. Batch every question into one call.

**Decide yourself, and record one line in the report:**

- File placement, naming, helper extraction, test layout
- Index choices and schema detail the spec leaves open
- Which of two equally valid implementations
- The order of independent phases
- MEDIUM and LOW analyze findings
- Anything resolvable by reading the existing code or the constitution

When you decide alone, state what you decided and the reason. That is what makes
the work auditable without you present.

**Defer, never interrupt:** collect every non-blocking question, finish
everything that does not depend on the answer, then ask once via `question`.
Asking through the UI is a reason to ask well, not a reason to ask more.

## §10 Report

Vietnamese, per `AGENTS.md`. Keep it short:

- what shipped, by commit
- the gate result, and any §3 step you skipped
- what is NOT done and why
- decisions you made alone, with reasons
- what needs the developer

Report verification honestly. If a check did not run, say it did not run. A
green gate with no real end-to-end path is not a verified feature — see
`references/verification.md`.
