# Better first drafts from the development agents

## Goal

The field-feedback session (branch `fix/field-feedback-blockers`, its
retrospective `docs/specs/field-feedback-blockers-retrospective.md` on that
branch) ran 11 fix rounds and logged 119 review findings. The reviewers and
the gates worked. Most findings were either already in the system or oversights
the engineers could have avoided: denylists over re-implemented parsers,
assumed runtimes (GNU tools, bash, Linux), fixes that reopened what they
replaced, tests that did not prove their claim, unmeasured hot paths, and
drifting docs. This change teaches the agents that write and plan the code to
avoid those classes up front, and gives the lead the process rules that keep
scope and fix loops in check.

Only prompt content changes: skills and agent definitions under `plugin/`, and
their documentation. The engine and its gates are unchanged.

## Acceptance criteria

Each criterion is checked by reading the named file at the head commit.

Engineers (`plugin/skills/engineering-standards/SKILL.md`,
`plugin/agents/software-engineer.md`):

- AC-1: A "Security checks" section says, for any code that decides whether
  something is allowed (a guard, a validator, a sanitiser, a permission
  check): allowlist rather than denylist; whatever cannot be classified is
  denied; do not trust a re-implementation of another program's parser, and
  if one is unavoidable accept only a subset shown to be read the same way,
  tested against the real program; and a rewrite or narrowing of a protection
  comes with a differential test showing that everything the old version
  denied is still denied.
- AC-2: A "Know the runtime" rule says to verify behaviour against the
  implementations the code really runs under (the spec's runtime inventory
  when it has one: operating systems, shells, the actual tool
  implementations, toolchain versions, harnesses) rather than assumed
  defaults, and to report what could not be run.
- AC-3: The tests section says a test must not derive its expected values
  from the code under test (no self-referential oracle), and that every
  AC and SR the report claims names a test that exists in the change.
- AC-4: When the spec's Constraints or the package name a hot path or a size,
  the engineer measures before and after and reports the numbers.
- AC-5: A "Fixing a finding" rule says: a fix closes its finding without
  reopening what it replaces; before reporting, check the fix against the
  earlier findings on the same code (listed in the package) and keep their
  tests passing; prefer the smallest change; a fix that needs a redesign is
  reported, not improvised.
- AC-6: The engineer updates the docs that describe the behaviour it changes
  when those docs are in its paths, and reports docs it could not update.

Planning (`plugin/skills/threat-model/SKILL.md`,
`plugin/skills/solution-design/SKILL.md`):

- AC-7: The threat model starts with a runtime inventory (operating systems,
  shells, the real tool implementations, language and toolchain versions,
  harnesses) and checks each threat against each.
- AC-8: The threat model's STRIDE pass names two prompts explicitly: parser
  differentials (the check and the executor read the same input differently)
  and fail-closed denial of service (one bad input or state blocks every call).
- AC-9: The threat model marks each threat as introduced by the change or
  pre-existing, and lists pre-existing ones separately as candidates for
  follow-up.
- AC-10: Solution design, when an option involves a security decision,
  defaults to the security-check principles of AC-1 and says so in the
  options' trade-offs.

The lead and the reviewers (`plugin/skills/tech-lead/SKILL.md`,
`plugin/skills/review-protocol/SKILL.md`):

- AC-11: Every review finding carries `origin`: `introduced`, `worsened` or
  `pre-existing`. The JSON format and its example include the field.
- AC-12: The tech-lead's triage: a `pre-existing` finding is filed as a
  follow-up by default; it is fixed in the change only when the change makes
  it reachable or worse, or the user asks. High and critical pre-existing
  findings still go to the user, with the default stated.
- AC-13: The tech-lead's fix loop: when one component draws findings in two
  consecutive review rounds, the lead stops patching, writes a short design
  note with options (fail closed by construction, narrow the scope, accept
  and document a limit) and asks the user before the next round.
- AC-14: A package that introduces a security mechanism the spec's threat
  model did not cover gets a short threat-model pass on it before dispatch.
- AC-15: The work-package template carries what engineers need to avoid
  repeat findings: the runtime inventory (from the spec), the earlier
  findings and their tests on the code it touches (for fix packages), the
  docs it must update (in its paths), and any hot path to measure.
- AC-16: Integration (step 6) checks that every AC and SR in the engineer's
  report names a test present in the diff, and reads a sample of them.
- AC-17: Integration stages what `git status --porcelain` lists except
  character devices and paths that do not exist, never by a name pattern.
- AC-18: Preflight checks that the project's harness settings set
  `worktree.baseRef: "head"` and that agent worktrees are git-ignored, and
  proposes an acceptance-test path when the config has none; it tells the
  user what to fix rather than changing protected settings.
- AC-19: The spec template gains a "Runtime" section (the inventory) next to
  Constraints.

Documentation:

- AC-20: `docs/agents.md` describes the new rules where it describes the
  skills (the origin field, the convergence rule, the security-check
  principles, the runtime inventory).

## Non-goals

- Engine or gate changes, a CI workflow for the full gate, a plugin version
  bump or release.
- Rewriting the skills beyond these rules. Keep each skill's voice, length
  and structure; add rules where they belong.

## Constraints

- Skills stay short and scannable: each added rule is a few lines. The
  tech-lead skill may grow by at most about 40 lines; the others by at most
  about 25 each.
- Wording follows the house style of the existing skills: plain, imperative,
  no filler.

## Risk

No trust boundary in code. Risk is prompt drift: a rule that contradicts
another skill, or one so long that agents skim it.

## Approach

Rules taken from the retrospective's section 6 (gaps 1 to 9), placed in the
skill of the agent that acts on them, so each agent carries only what it
needs. The lead's rules make the engineers' rules reach them: the
work-package template passes the runtime inventory, prior findings and docs.

## Decision log

| id | from | severity | finding | disposition | reason |
|---|---|---|---|---|---|
| CR-1 / MR-6 | correctness, maintainability | high | Preflight proposes an acceptance path and waits whenever the config sets none, though the engine defaults to `tests/acceptance` (this repository relies on it); the background lead would stop there. It also does not name `config apply` as the fix for baseRef and the ignore entries. | fix | Propose a path only when the effective path (default included) does not fit, without waiting; name `config apply`. |
| CR-2 / MR-1 | correctness, maintainability | medium | Dispatch (step 5.2), engineering-standards' "Before writing code" and software-engineer.md list only the old package fields, so Runtime, Prior findings, Docs to update and Hot paths may never reach the engineer. | fix | Hand over the whole package block; name the fields on the engineer's side. |
| CR-3 | correctness-reviewer | medium | "A test that exists in your change / in the diff" cannot be met for acceptance-level SRs (qa-tester's) or tests from earlier packages; step 6.5 would loop. | fix | Name an existing test or the acceptance test; the lead checks it exists on the integrated branch and runs. |
| CR-4 / MR-3 | correctness, maintainability | medium | security-research defaults to `pre-existing` against review-protocol's "when unsure, introduced"; threat-model uses two origin values with other meanings; qa-testing does not say which value when the base shows the bug. | fix | One vocabulary (review-protocol's three values) everywhere; "usually pre-existing" only for audits with no change under review. |
| CR-5 / MR-4 | correctness, maintainability | medium | The convergence rule counts undefined "review rounds" (ordinary fixes are not re-reviewed), and fires on any finding, low or rejected included. | fix | Trigger on fix-dispositioned findings of medium or higher, from any source, against one component in two consecutive fix rounds. |
| CR-7 / MR-2 | correctness, maintainability | medium | The threat model writes its own `Runtime:` line, and step 3 merges only SRs back, so its runtime additions never reach packages. | fix | The threat model returns additions to the spec's Runtime section; step 3 merges them. |
| CR-6 | correctness-reviewer | low | Reporting pre-existing problems blocks an `approved` verdict, though they are deferred by default. | fix | The verdict counts introduced and worsened findings only. |
| CR-8 | correctness-reviewer | low | Prior findings "and their tests" have no record: the decision log has no test column. | fix | Add a `test` column (the regression test of a fix). |
| CR-9 | correctness-reviewer | low | Nothing in the lead's playbook acts on the new report items (docs outside paths, measurements, what could not run). | fix | Integration step 5 routes, compares and carries them into the summary. |
| CR-10 / MR-5 | correctness, maintainability | low | solution-design's runtime sentence is garbled and in section 2, which small changes skip; the template files it under Reference behaviour. | fix | Move to section 1 or the scale guidance; give the template a Runtime line. |
| CR-11 | correctness-reviewer | low | Preflight checks misreport: `git check-ignore` needs a trailing slash and one path per call; the settings file is not named; baseRef does not apply under pi. | fix | Exact commands; name the project's Claude Code settings file; skip baseRef under pi. |
| MR-7 | maintainability-reviewer | low | Integration item 6 is one 11-line paragraph that buries the staging rule. | fix | Split into three items; docs/agents.md mentions both staging exceptions. |
| CR-12 | correctness-reviewer | low | engineering-standards grows +28 against about 25. | fix | Tighten while editing. |
