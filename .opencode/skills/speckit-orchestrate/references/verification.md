# Verification discipline

Read this when verifying a phase, or before reporting any feature as complete.
Each rule exists because the defect behind it passed review and the automated
gates on feature 025.

## 1. A green gate is necessary, not sufficient

`make wire && make lint && make test-race` proves the code compiles and that the
assertions it contains hold. It cannot prove the assertions are the right ones,
or that the untested path works.

Feature 025 shipped a green suite and still failed in production with a
`stale_event` dead-letter, because no test crossed a real event with a real
consumer and a real database. Unit tests with a fixed clock cannot see ordering
bugs between producer and consumer.

## 2. Do not take a subagent's word for it

Every subagent report is a claim, not evidence. Before reporting a phase as
done, open the files it says it changed and confirm the change is there and is
what the task asked for. A subagent that reports success without writing
anything is common and silent.

## 3. Every bug-catching test needs a negative control

A test that has never been seen to fail proves nothing — it may assert nothing.

For any test written to catch a specific bug:

1. Break the code the way the bug happened.
2. Confirm the test goes RED, and fails for the right reason.
3. Restore the code.
4. Confirm the test goes GREEN.

Report both colours. "Test passes" without the red half is an incomplete claim.

## 4. Require one real end-to-end path per feature

Exercise the actual journey against real infrastructure: a real signup, a real
event on the real bus, a real database row. Assert the values you expect, not
merely that no error was returned.

## 5. When two documents disagree, stop

`spec.md` vs `plan.md`, or either vs `constitution.md`. Report the conflict
naming both sides and ask. Do not pick a winner, and do not quietly align the
loser to the winner — that erases a decision the developer may not have made.

## 6. Check producer/consumer clock ordering explicitly

Any event carrying a timestamp consumed by another service must be stamped at
processing time, not at registration or creation time. A registered-at timestamp
is always older than the consumer's last processed event, so the consumer treats
it as stale. Assert this with two different clocks.

## 7. Accidental side effects on real data

Before running a sweep, migration or backfill, confirm its date window cannot
reach pre-existing production rows.

On feature 025 the first test run left `starts_at` in the past, and the sweep
granted a plan to 8 existing accounts and sent them 8 emails. The code was
correct; the test fixture was not. A destructive-looking path is only as safe as
the date range you typed into it — check that range before running, not after.

## 8. One real path, then stop

The end-to-end signup path is the only check that crosses a real producer, a
real bus and a real database. Run it once per feature, near the end, and assert
concrete values. Running it after every phase buys nothing and costs more time
than any other check in the suite.
