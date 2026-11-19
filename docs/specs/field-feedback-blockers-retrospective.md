# Field-feedback blockers: session report and retrospective

Branch `fix/field-feedback-blockers`, base `1626342` (millennium 0.3.1),
2026-11-18 to 2026-11-19. Led with `/millennium:tech-lead` on the very version
it patches. The spec and every finding with its disposition are in
`field-feedback-blockers.md`; the user-facing summary is in `CHANGELOG.md`.

## 1. In one paragraph

The task was to read a field report from another session (three engine
blockers and a dozen workflow gaps) and fix what would unblock it. The three
blockers (B1 a vanished cwd locks the hooks, B2 comments and erased types
reported as uncovered, B3 comment-only edits demand tests) were fixed and
gated in the first package round. Review then kept surfacing guard and
tests-gate bypasses next to them, and the user chose to fix them in the same
branch. The result: 67 commits, 106 files, +13,172/-422 lines, 19 engineer
packages, 11 review rounds after the first panel, 119 findings
(1 critical, 20 high, 40 medium, 53 low, 5 n/a). The final panel approved
with three low findings, deferred.

## 2. Is the gate automatic?

Partly. The hooks run the **fast tier** automatically when any agent stops
(and block it while anything fails). The **full tier**
(`gate --tier full --require-config`: tests, coverage, the self-audit) is a
manual step: the tech-lead playbook's step 10, or `scripts/verify.sh`.
Nothing in CI runs it on a pull request: `.github/workflows/` holds only the
release workflow (on tags) and the new Windows tests. During this session the
full tier ran in the sandbox with a temp-directory workaround (L-3), and the
self-audit's two network checks (OSV, `npm audit signatures`) could not run
there. So the PR has not had a clean full gate yet. Recommendation 7 below
proposes a PR workflow that makes it automatic.

## 3. What changed

| Area | Change |
|---|---|
| B1, vanished cwd | The hook walks to the nearest existing parent; only "does not exist" walks, every other error blocks. |
| B2, uncovered lines | Analyzers report runtime lines (Go, TypeScript via the compiler's emit, Python); zero-hit non-runtime lines are not reported. |
| B3, comment-only edits | A runtime-token digest of base and working versions; equal digests need no test change. Unknown facts count as changed. |
| Generated markers | No longer exempt files in tested layers; declare them with `exclude`. |
| Guard paths | Protected paths matched as segments anywhere, case-folded, Win32-normalised, through symlinks and junctions, in every worktree; unresolvable locations and roots block. |
| Shell guard | Round-4 floor plus a provably-read-only exception: a character whitelist and per-program option allowlists, checked by a 17k-command sweep. |
| Tests gate | `line-directive`, `foreign-coverage`, coverage-bounds and `ignored-directive` rules; runtime facts only on request (hook latency). |
| CI | A Windows workflow for the Windows-only code (not yet run). |

## 4. How the session went

| Round | Packages | Main content | Next check found |
|---|---|---|---|
| 0 | P1, P1b, P2, P3 | B1, B2, B3; the threat model's SRs; generated markers (user decision) | 21 (2 high): pre-existing guard bypasses, performance, missing tests; the threat model had added 8 before any code |
| 1 | F1, F2 | Every worktree protected, case-folding, `..` refusal; runtime facts on request; `//line` | 19 (1 high): coverage check fails whole modules, protection by enumeration leaks |
| 2 | G1, G2 | Protected paths "anywhere"; coverage checks only for judged files | 13 (2 high): guard off in non-opted-in checkouts, `node:coverage` in strings |
| 3 | H1, H2 | Symlinked locations, Win32 names, scope matching; Node directives on raw lines | 5 (a regression: dangling links block everything) |
| 4 | J1 | Own path walker, junctions, all worktrees' locations | 7 (a Windows regression, untested Windows code) |
| 5 | K1 | Fail closed on unknown locations; shell-guard rewrite; Windows CI | 6 (a high regression: quoted paths pass the rewritten shell guard) |
| 6 | L1, L2 | Shell-guard matcher fixes; worktree-root rule | 8 (1 high): writes through variables and pipes to `sh` |
| 7 | M1 | Redesign: round-4 floor plus a read-only allowlist (user decision) | 7 (5 high): comments, here-docs, bfs `-rm`, `printf -v` |
| 8 | N1 | Allowlist fails closed on what the scanner may misread | 4 (1 high): zsh glob qualifiers |
| 9 | O1 | No parentheses, option allowlists for rg and git | 4 (2 high): `git log` option passes, zsh `<->` |
| 10 | P1 | Character whitelist; attached git values | 6 (1 critical regression: `&>`; 2 high) |
| 11 | Q1 | No `&>`, single-digit descriptors, grep allowlist | approved, 3 low |

The playbook's cap of three fix rounds was reached after round 3; the user
chose to continue at each later decision point.

## 5. The findings, by theme

119 findings by source: security-engineer 61, correctness-reviewer 30,
qa-tester 15, maintainability-reviewer 10, performance-reviewer 8, engineers 6,
the lead 3 (several were found by two reviewers). The themes overlap; counts
are approximate.

| Theme | About | Example |
|---|---|---|
| Shell-guard parser differentials | 35 | The scanner read `ls #'⏎printf x > .claude/settings.json` as one read-only `ls`; bash ran the `printf`. Then here-docs, zsh glob qualifiers, `<->`, `&>`, `10>`. |
| Filesystem aliasing | 30 | A worktree moved with `mv`, a newline in its path, `.MILLENNIUM`, `.pi -> conf`, junctions, `chmod 000`, `/proc/self/cwd`. |
| Pre-existing, next to the change | 20 | Generated markers, case-insensitive paths, writes into the main checkout from a worktree: none introduced by this branch. |
| Regressions introduced by a fix | 14 | Each rewrite closed its findings and opened a new hole or a block-everything path (rounds 3, 4, 5, 9, 10). |
| Wrong assumptions about the runtime | 12 | Claude Code's `find` is bfs, `grep` is ugrep, macOS runs zsh, Go 1.23 stopped following junctions, `git log` parses options in two passes. |
| Tests that do not prove the claim | 9 | The SR-3 end-to-end test missing; a differential test using the code's own oracle; Windows code never run; a sweep gap. |
| Performance on the hook path | 8 | A TypeScript transpile per file on every Stop; a `git worktree list` per tool call. |
| Docs drifting from code | 8 | `docs/design.md` describing an older shell rule; option lists not matching. |
| Architecture and duplication | 8 | Policy in the CLI layer; a rule written twice. |
| Lead process | 4 | The integration filter dropped `.github/`; worktrees not based on HEAD; `.claude/worktrees/` not ignored. |

## 6. Where the skills fell short

The fresh-context reviewers did their job: nearly every finding came with a
proof, and many were real bypasses. The cost came from what happened before
review. Each row names the gap and a proposed change.

### Gap 1: no line between "introduced" and "pre-existing" (about 20 findings, most of the scope)

Reviewers were told to review the diff, but the security reviews (rightly)
followed the threat into neighbouring code. The review protocol has no field
for it, and the tech-lead playbook has no default for such findings, so each
one became a decision, and high ones cannot be deferred without the user. The
branch tripled in scope.

**Proposal.** `review-protocol`: every finding carries
`origin: introduced | worsened | pre-existing`. `tech-lead` step 9: a
pre-existing finding is filed as a follow-up by default (the user still
decides for high and critical), unless the change makes it reachable or worse.

### Gap 2: no exit from a non-converging fix loop (rounds 5 to 11)

The field report itself said this (its item P2: "after two rounds of findings
on the same function, step back to a design-level option"). It happened
again: the shell guard was patched by enumerating dangerous forms for three
rounds before the floor-plus-allowlist design, and then hardened for four more.
The cap of three rounds stopped the work but did not say what to do next.

**Proposal.** `tech-lead` step 9: when one component draws findings in two
consecutive rounds, stop patching. Write a short design note with options
(fail closed by construction, narrow the scope, accept and document a limit)
and ask the user before the next round. Count rounds per component, not only
per change.

### Gap 3: security checks written as denylists over re-implemented parsers (about 35 findings)

Most shell-guard rounds came from one choice: the guard re-implemented bash's
tokenizer and looked for dangerous forms. Every difference from bash or zsh
was a bypass. Convergence came only with an allowlist of characters, programs
and options, where uncertainty denies.

**Proposal.** `engineering-standards`, a "Security checks" section:
- Allowlist, not denylist; whatever the check cannot classify is denied.
- Do not trust a re-implementation of another program's parser. If you must,
  accept only a subset you can show both read the same way, and test that
  subset against the real program.
- A rewrite of a protection comes with a differential test: everything the
  old version denied is still denied (this branch added one only in round 6;
  it caught the later regressions).

The same three bullets belong in `solution-design` as defaults when a design
involves a security decision.

### Gap 4: the runtime was assumed, not inventoried (about 12 findings)

Engineers and the threat model assumed GNU tools, bash and Linux. The code
runs under Claude Code (bfs as `find`, ugrep as `grep`), zsh on macOS, Windows
with junctions, and pi without an OS sandbox.

**Proposal.** `threat-model` step 1 lists the runtime: OSes, shells, the
actual tool implementations, language and toolchain versions, and the
harnesses. Each threat is checked against each. Two STRIDE prompts are added:
parser differentials (the guard and the executor read the same input
differently) and fail-closed denial of service (one bad input blocks every
call).

### Gap 5: the threat model ran once, on the first design

It produced 17 good SRs, but the design changed several times (worktree
listing, protected paths anywhere, the walker, the shell guard). Each new
mechanism was reviewed only after it was built.

**Proposal.** `tech-lead`: a package that introduces a new security mechanism
not in the spec gets a short threat-model pass on that mechanism before
dispatch, not only a review after it.

### Gap 6: claims taken from reports (about 9 test findings, 1 lead mistake)

Engineers reported "SR-3 tested" when the end-to-end test was missing, and a
differential test compared the code against its own oracle. The lead checked
scope, the gate and the diff, not that each named test existed and asserted
the claim.

**Proposal.** `tech-lead` step 6 adds a check: every AC and SR in the
package's report names a test that exists in the diff; a sample of them is
read. `engineering-standards`: a test must not derive its expected values
from the code under test.

### Gap 7: performance and docs were outside the package's view (16 findings)

Engineers met the functional rows but did not measure the hook path, and
`docs/design.md` was often outside a package's paths, so it drifted.

**Proposal.** `engineering-standards`: if the spec's Constraints name a hot
path, measure before and after and report it. `tech-lead` step 4: a package
that changes documented behaviour gets those docs (including the design row)
in its paths.

### Gap 8: the lead's own integration steps

The lead's staging filter for the sandbox's placeholder files matched names,
so it also dropped `.github/` (R6-SEC-W). Worktrees were based on `main`
because this repository's settings lack `worktree.baseRef: "head"`, and
`.claude/worktrees/` was not ignored.

**Proposal.** `tech-lead` step 6: stage what `git status` lists except
character devices, never by name pattern. Preflight (step 0) checks
`worktree.baseRef` and the ignore entry, and says what to fix.

### Gap 9: no acceptance-test home (the field report's R2, again)

This repository has no `paths.acceptanceTests`, so the qa-tester could not
keep black-box tests and worked exploratorily only (L-2).

**Proposal.** `tech-lead` preflight: when the config has no acceptance path,
propose one before dispatch.

## 7. What went well

- Fresh-context reviewers found real defects with proofs, including bypasses
  that predate this branch and would have shipped.
- The decision log made 119 findings traceable; every one has a disposition.
- Fail-closed held: no fix loosened a gate, and the user made every scope and
  design call (generated markers, opt-in, the shell-guard design, each extra
  round).
- The B1, B2 and B3 fixes were done and gated in the first round, and held up
  through every later review.

## 8. Recommended next steps

1. Run `bash scripts/verify.sh` and
   `millennium-code-qc gate --base 1626342 --tier full --require-config`
   outside the sandbox before merging, and watch the first Windows workflow
   run.
2. Apply the skill changes of section 6, in order of payoff: 1 and 2 (scope
   and the convergence rule), then 3 and 4 (security-check principles and the
   runtime inventory), then 6, 7, 8 and 9.
3. File the deferred rows of the decision log as issues, grouped: filesystem
   aliases (with the Windows items), per-target opt-in, the guard
   use-case refactor, and the three low findings of the last panel.
4. Add a PR workflow that runs `scripts/verify.sh` (which includes the
   full-tier self-audit) without third-party actions, like the release
   workflow, so the full gate is automatic.
5. Add `worktree.baseRef: "head"` to this repository's `.claude/settings.json`.
