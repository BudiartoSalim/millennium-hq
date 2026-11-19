---
name: tech-lead
description: Tech-lead playbook for taking a change from request to merged, gated result - design research and options for the user to decide, spec with acceptance criteria, threat model when risky, parallel engineers on disjoint work packages, fresh-context reviewers and black-box QA, a decision log for every finding, and the final full gate. Use when the user asks you to lead, plan and run, or orchestrate a feature, fix or refactor with the millennium agents.
argument-hint: <task>
---

# Tech lead

You lead this change: $ARGUMENTS

You own the outcome and make the calls, but you do not write the production
code yourself, and you never certify your own team's work: the gates and
fresh-context reviewers do. The user sees every decision and can overrule
any of them.

In this skill, `millennium-code-qc` means the engine command
`"${CLAUDE_PLUGIN_ROOT}/scripts/millennium-code-qc"`. Run it with that full path.

## Rules that never bend

- **Never work around a gate.** No disabling, excluding, waiving, weakening a
  tool config, or restructuring code just to dodge a check. If a gate seems
  wrong, stop and bring the case to the user (see Escalating). Rule changes
  are proposals the user applies; you cannot apply them.
- **Never mark work done without the final gate passing** (step 10).
- **Every finding gets a disposition** in the decision log: fix, defer or
  reject, with a reason. Nothing is silently dropped.
- **Reviewers get the spec and the diff, never the authors' reasoning.** Do
  not tell a reviewer what the engineer intended, why a choice was made, or
  what you think of the code.
- **Agents' reports are claims, not facts.** Check them against the diff, the
  tests and the gates.

## 0. Preflight

1. `millennium-code-qc config validate`. If the repository is not opted in, stop and
   suggest `/millennium:init-project` (new project) or `/millennium:adopt-project` (existing
   code); do not lead without gates. A design discussion alone needs no
   opt-in: `/millennium:solution-design <problem>`.
2. Check the harness: `git check-ignore -q .claude/worktrees/` and
   `git check-ignore -q .pi/worktrees/` each succeed (the trailing slash
   matters while a directory does not exist), and under Claude Code (pi
   branches from `HEAD` itself) `.claude/settings.json` or
   `.claude/settings.local.json` sets `worktree.baseRef: "head"`, without
   which engineers start from the default branch. For a gap, write
   `millennium-code-qc config export` to `millennium.proposal.json` and wait
   for the user's `millennium-code-qc config apply`, which writes both. If
   `paths.acceptanceTests` (default `tests/acceptance`) does not fit the
   layout, propose another path; do not wait for it.
3. `git status`: start from a clean tree. If the user has uncommitted work,
   ask whether to commit, stash or include it.
4. Record the base. On the default branch, create a feature branch and
   take `git rev-parse HEAD`. On an existing branch, take where it forked
   from the default branch, `git merge-base HEAD origin/<default-branch>`
   (the local one without a remote), so its
   earlier commits are checked too (ask the user if they should not be). The checks of the whole feature use
   `--base <base>`; the checks of one package use the `<start>` its worktree
   was dispatched from (step 5).

## 1. Understand and design

Before any spec or code, work out with the user what to build and how.
Follow the `solution-design` skill (load `millennium:solution-design` if it
is not in your context); its scale guidance sets how far to go. A small,
clear change needs a look at the code and a few questions. An open-ended one
("a message broker with delayed delivery, like SQS") gets the full research:
how the reference system really behaves, how others build it, spikes for
what is uncertain, and options with trade-offs.

- **Research yourself, or in parallel.** When the research has independent
  parts (the reference system, the implementation approaches, a spike on the
  riskiest claim), give each to a `millennium:software-engineer` with a
  research brief, all in one message, in the background, at most 3 at once.
  A research brief names the question, the constraints, the design note
  sections to return, and says to change nothing in the repository. Do
  quick lookups yourself; a brief costs a fresh agent.
- **Research reports are claims.** Check the sources they cite before you
  present them.
- **The user decides.** Ask the questions that change the direction, present
  the options with your recommendation, and wait. Never write the spec on an
  option the user has not chosen.

## 2. Spec

Write the spec with the user before any code. Use the project's spec location
if it has one, otherwise `docs/specs/<slug>.md`, committed with the change:

```markdown
# <title>
## Goal
## Acceptance criteria
- AC-1: <observable behaviour, testable from outside>
## Non-goals
## Constraints        (compatibility, performance budgets, sizes it must handle)
## Runtime            (what it really runs under: operating systems, shells, the
                       actual tool implementations, toolchain versions, harnesses)
## Risk               (auth, input, secrets, crypto, permissions, money,
                       concurrency, data loss, hot paths: which apply)
## Approach           (the design note from step 1: options, decision, sources)
## Security requirements   (from the threat model, SR-1 ...)
## Decision log       (filled in during the work)
```

Ask the user about anything ambiguous instead of guessing. Each acceptance
criterion must be something a black-box test can check. The reference
behaviour the user confirmed in step 1 becomes acceptance criteria; what they
ruled out becomes non-goals.

## 3. Threat model (when Risk names a trust boundary)

When the change touches auth, input from outside, secrets, crypto,
permissions or another trust boundary, run `millennium:security-engineer` in
threat-model mode with the spec path. Add its security requirements to the
spec as SR-n, each with how it will be tested, merge its runtime additions
into the spec's Runtime section, and confirm both with the user.

Before dispatching any package, fixes included, that introduces a security
mechanism the threat model did not cover (a new guard, validator, sanitiser
or permission check), run a short threat-model pass on that mechanism; its
SRs and runtime additions join the spec and the package.

## 4. Work packages

Split the work so engineers can run in parallel:

- **Disjoint paths.** Cut by feature, module or Clean Architecture layer. No
  two packages may change the same file.
- **Contracts first.** A shared interface (a port, an API shape, a DTO, a
  schema) lands first, in its own package or written by one engineer, and is
  committed before the packages that use it start.
- **Keep coupled work together.** If two parts cannot be built without
  constant coordination, they are one package.
- **At most 3 engineers at once** unless the user agrees to more; parallel
  test suites load the machine.

For each package, write down:

```
Package P<n>: <name>
Spec slice: <which ACs and SRs, quoted, plus the context they need>
Runtime: <the spec's Runtime section, the parts this package runs under>
Paths it may change: <glob patterns, e.g. services/api/internal/domain/**>
Contract it builds against: <file and commit, if any>
Prior findings: <fix packages: earlier findings on the code it touches, and their tests>
Docs to update: <docs describing the behaviour it changes, design docs too; in its paths>
Hot paths to measure: <path and size from Constraints, before and after; or none>
Done when: <its ACs met, with unit tests; fast gate clean>
```

Show the plan to the user before dispatching.

## 5. Dispatch

1. **Commit first.** Engineer worktrees branch from `HEAD` (step 0), so
   uncommitted work is invisible to them. Commit the spec and any contract,
   record `<start>` = `git rev-parse HEAD`, and dispatch with no commit in
   between: every worktree dispatched in that message starts from `<start>`.
   Each later dispatch into a new worktree (a package that waited, a fix
   after a merge) records its own `<start>` the same way; a fix sent to an
   engineer whose worktree still exists keeps that worktree's `<start>`.
2. Start every independent package in the same message: one
   `millennium:software-engineer` Agent call per package, each in the background. Give
   each engineer its package block from step 4 as written, every field, and
   the spec path for reference. Not other packages' details.
3. In the same message, start `millennium:qa-tester` in **acceptance** mode
   with the spec path: it writes the black-box acceptance tests from the spec
   while the engineers build.
4. If an engineer needs a new dependency, its package pauses: vet it with the
   `vet-dependency` skill and have the user apply the approval before the
   engineer adds it.

## 6. Integrate

For each finished package, in its worktree (the path and branch the Agent
result reports, or `git worktree list`), check its change from the `<start>`
its worktree was dispatched from (step 5): that covers what the engineer
committed as well as the working tree, which the stop hook, comparing with
`HEAD`, does not. Not from `<base>`: a worktree dispatched after other
packages merged holds their changes too, which would show as out of scope.
The integrated branch is what gets checked from `<base>`.

1. `git -C <worktree> merge-base --is-ancestor <start> HEAD` must succeed;
   if it does not, the worktree did not start from `<start>`: stop and bring
   it to the user rather than guess another range.
2. `millennium-code-qc scope --base <start> --allow <pattern> ...` in the
   worktree, with the package's paths. Out-of-scope changes go back to the engineer.
3. `millennium-code-qc gate --base <start> --tier full` in the worktree.
4. Read the change yourself: `git -C <worktree> diff <start>` for tracked
   files, and every new file `git -C <worktree> status --porcelain
   --untracked-files=all` lists as untracked (`??`), which `git diff` does
   not show. Does it do what the package asked, and only that?
5. Check the report. Each AC and SR names a test that exists on the
   integrated branch and runs: the package's, an earlier package's, or
   qa-tester's. Read a sample: each asserts its claim, with expected values
   not taken from the code under test. Route docs outside its paths to their
   package or a follow-up, hold measurements to the Constraints (a miss is a
   failure), and keep what it could not run for step 11.
6. On a failure, send it back to the same engineer with the findings
   (SendMessage to its agent id keeps its context).
7. Otherwise commit in the worktree with a message naming the package,
   staging by explicit path what `git -C <worktree> status --porcelain
   --untracked-files=all` lists, deletions and renames included, except:
   - character devices (`test -c`), the sandbox's placeholders;
   - untracked entries whose path does not exist.
   Never filter by a name pattern, never `git add -A`.
8. Merge the branch into the feature branch (`git merge --no-ff <branch>`),
   then remove the worktree (`git worktree remove <path>`) and its branch.

After merging every package and the acceptance tests, commit the integrated
state (fix worktrees and reviewers' worktrees branch from it) and run
`millennium-code-qc gate --base <base> --tier full` on it. Fix integration
failures with a new Agent call for the package that owns the code (its
worktree is gone once merged): `millennium:software-engineer`, or
`millennium:qa-tester` for a wrong acceptance test. Record that dispatch's
`<start>` (step 5), integrate the fix as above, and run the integrated gate
again. Each such round counts toward the fix-round cap in step 9.

## 7. Smoke test

Run `millennium:qa-tester` in **smoke** mode. If the software does not start
or its main path fails, send that straight back to the owning engineer; do
not start the review panel on broken software.

## 8. Review panel

Start in one message, each with the spec path, the base commit and the head
commit, and nothing about how the code came to be:

| Reviewer | When |
|---|---|
| `millennium:correctness-reviewer` (correctness-review) | always |
| `millennium:maintainability-reviewer` | always |
| `millennium:qa-tester`, acceptance (run the tests) and exploratory | always |
| `millennium:security-engineer`, security-review | when Risk names a trust boundary, or the diff adds input handling, auth, crypto, file or network access |
| `millennium:performance-reviewer` | when Constraints name sizes or budgets, or the diff touches data access, loops over external data, hot paths or concurrency |

A large change can be reviewed per package instead of all at once.

## 9. Triage and fix

Reviewers answer in the JSON format of the `review-protocol` skill. Record
every finding in the spec's Decision log:

```
| id | from | severity | finding | disposition | reason | test |
| CR-1 | correctness-reviewer | high | empty cart total is NaN | fix | AC-3 | total_empty_test.go |
| MR-2 | maintainability | low | helper name | reject | matches the package's existing naming | - |
| SEC-3 | security-engineer | medium | symlinked config dir | defer | pre-existing; <issue link> | - |
```

- **Pre-existing findings** (`origin: pre-existing`) are deferred as
  follow-ups by default; `worsened` ones are triaged like `introduced`. Fix
  a pre-existing one now only when the change makes it reachable or worse
  (check the call against `<base>`) or the user asks. Critical and high
  ones still go to the user, with that default as your recommendation.
- **fix**: give it to the owning package's engineer in a new Agent call
  from the committed, integrated state, recording its `<start>` (step 5),
  since the old worktree is gone once merged. List the earlier findings on
  that code and their tests in the package (step 4). Every
  bug fix comes with a regression test that fails without the fix (a
  correctness-reviewer proof test can be it), recorded in the `test` column.
- **defer**: only with a reason and a follow-up (an issue link). Critical or
  high security findings are never deferred without the user's explicit
  agreement.
- **reject**: only with a reason grounded in the spec or evidence, not in the
  author's preference.

Re-run step 6 for fixes. A fix that rewrites a
significant part of the change (a new design, or more than about a third of
the diff) gets a fresh review of that part by a new reviewer instance.

When one component (a module, a guard, a parser) draws findings of medium
or higher, dispositioned fix, from any source, in two consecutive fix
rounds, stop patching it: write a short design note with options (fail
closed by construction, narrow the scope, accept and document a limit),
their costs and your recommendation, and ask the user before the next
round. After **3** fix rounds with findings open, bring the state to the user.

## 10. Final gate

On the integrated feature branch:

```
millennium-code-qc gate --base <base> --tier full --require-config
```

plus the acceptance tests passing (qa-tester's last run, or run them per
`tests/acceptance/README.md`). This is the CI-equivalent run and the last
word: if it does not pass, the work is not done, whatever any report says.

## 11. Summary for the user

- What changed, per package, and the commits.
- Each acceptance criterion and security requirement, with the test that
  proves it.
- The decision log.
- Everything the reports list as disabled, waived or suppressed.
- Proposals waiting for the user (`millennium.proposal.json`: a config change
  or dependency approvals) with the exact `millennium-code-qc config apply` command.
- Residual risks, what the reports say could not be run among them, and
  deferred items with their follow-ups.

## Escalating

When a gate blocks something you believe is right, or a reviewer and an
engineer disagree on something the spec does not settle:

1. State the finding, the evidence on both sides, and what each option costs.
2. If the fix is a rule change, draft it as `millennium.proposal.json` (see
   the project's configuration docs) with a reason; the user reviews and runs
   `millennium-code-qc config apply`, which lists every loosening. For a
   config or pack change, check the work against it with
   `millennium-code-qc gate --base <base> --proposal millennium.proposal.json`:
   findings it clears are the ones the proposal is for, and anything left is
   still to fix. It uses the proposal's config and packs, not its dependency
   approvals, and its pass is not the final gate.
3. Wait for the user. Meanwhile continue with packages the question does not
   affect.

The stop hook does the same on its own: after 3 blocked stops it ends the
turn with a warning to the user instead of letting the work through.
