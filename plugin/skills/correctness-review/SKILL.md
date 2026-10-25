---
name: correctness-review
description: Correctness review of a change against its spec - acceptance criteria met, edge cases, error paths, state and concurrency, and whether the tests actually prove the behaviour - with failing tests as proof. Use as the qa-engineer on every integrated change.
user-invocable: false
---

# Correctness review

Follow the `review-protocol` skill for scope, findings and output; this skill
says what to look for. Your `id` prefix is `QA`.

## 1. The spec, criterion by criterion

For each acceptance criterion and security requirement:

- Which code implements it, and does it do exactly that, including the
  wording's edges ("at most", "every", "within", "unless")?
- Which test proves it? A criterion with no test is a `high` finding.
- Does the change do anything the spec lists as a non-goal, or that nothing in
  the spec asks for?

## 2. Edge cases and error paths

Walk each changed function with inputs chosen to break it:

- empty, one, many; zero, negative, maximum, off-by-one at every boundary;
- missing, null, duplicate and malformed input; unicode, very long strings;
- the error branch of every call that can fail: is the error handled,
  reported with context, and is state left consistent? Partial failure
  half-way through a sequence of writes?
- time: time zones, DST, clock skew, timeouts, retries, idempotency of a
  retried operation;
- concurrency: shared state without synchronisation, check-then-act races,
  ordering assumptions, goroutine, promise or task leaks.

## 3. The tests

The engineer wrote their own tests, so judge them hard:

- **Would the test fail if the code were wrong?** Mentally break the code
  (flip a comparison, drop a branch, return early, return a constant). If no
  test notices, that is a finding against the tests.
- Assertions on observable behaviour, not on internals or mock call counts
  that merely restate the implementation.
- Mocks that make the test pass whatever the code does, or that encode the
  same misunderstanding as the code.
- Edge cases from section 2 with no test.
- Tests that depend on order, time, network or randomness and will flake.

Test readability is the maintainability reviewer's concern, not yours.

## 4. Prove it

For every `critical` or `high` bug you can reproduce, write a failing test in
your own worktree, in the project's test idiom, next to the code's existing
tests. Run it and put its path and the failure in `proof`. Name it after the
behaviour, so it can become the regression test for the fix. Never change the
code under review.
