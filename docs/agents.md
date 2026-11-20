# Agents and skills

The plugin ships seven agents and fourteen skills. The agents differ by what
they may do (tools, worktree, what they may read); the skills hold the
instructions. Skills are portable (the Agent Skills format); the agent files
are Claude Code's adapter.

## Leading a change

Run the tech lead in your main session:

```
/millennium:tech-lead add a cart total endpoint that applies the active discount
```

The lead first works out with you what to build and how: it researches the
problem, proposes options with their trade-offs, and waits for your decision.
Then it writes a spec with you, splits the work into packages for parallel
engineers, has the result reviewed and tested by fresh-context agents, records
a decision for every finding, and finishes only when the full gate passes.
You see each step and can overrule any decision. Sessions that do not use the
lead work as usual; the gates apply either way.

To run the same playbook in the background, ask for the `millennium:tech-lead`
agent. It stops and reports where the interactive lead would ask you.

The workflow, in order:

0. **Preflight**: the gates are on, the tree is clean, and the harness is
   ready for parallel work: the agent worktrees are git-ignored and, under
   Claude Code, subagent worktrees branch from `HEAD`
   (`worktree.baseRef: "head"`; pi always does). For a gap, the lead drafts
   a proposal of the current config for you to apply with
   `millennium-code-qc config apply`, which writes both, rather than changing
   protected settings itself. It proposes an acceptance-test path only when
   the default, `tests/acceptance`, or the configured one does not fit the
   layout, and does not wait for it.
1. **Understand and design** (the `solution-design` skill), scaled to the
   problem. For a small, clear change: read the code and ask what is
   unclear. For an open-ended one, such as "a message broker with delayed
   delivery, like SQS":
   - study how the reference system really behaves, from its documentation;
   - survey how others build it: patterns, prior art, known pitfalls;
   - test uncertain claims with throwaway spikes outside the repository;
   - propose two or three options with trade-offs and a recommendation.

   The lead may split the research across engineers with research briefs,
   in parallel. You decide; the decision and its sources go in the spec.
2. **Spec** with acceptance criteria (AC-n), non-goals, constraints, the
   runtime, risk and the chosen approach. The runtime is the inventory of
   what the code really runs under: operating systems, shells, the actual
   tool implementations, language and toolchain versions, and harnesses.
3. **Threat model** by the security engineer when the change touches a trust
   boundary; its security requirements (SR-n) join the spec, and its
   runtime additions the spec's Runtime section. A package that
   adds a security mechanism the threat model did not cover gets a short
   threat-model pass on it before dispatch.
4. **Work packages** with disjoint paths, contracts first, at most three
   engineers at once unless you agree to more. Each package carries the
   runtime inventory, the docs it must update (in its paths), any hot path
   to measure and, for a fix, the earlier findings on the code it touches
   with their tests.
5. **Engineers** in parallel, each in its own worktree; at the same time
   qa-tester writes the acceptance tests from the spec.
6. **Integration**: per package `millennium-code-qc scope` against its paths and the
   full gate in its worktree, from the commit the worktree was dispatched
   from (a worktree dispatched after other packages merged holds their
   changes too), a check that every criterion the engineer's report claims
   names a test that exists on the integrated branch and runs (its own, an
   earlier package's, or qa-tester's acceptance test; reading a sample),
   then a commit of what `git status` lists, minus the sandbox's placeholder
   devices and untracked entries whose path does not exist, and a merge;
   then the full gate on the integrated branch, from the feature's base.
   Docs the engineer could not update go to their package or a follow-up,
   its measurements are held to the spec's Constraints, and what it could
   not run goes into the summary's residual risks.
7. **Smoke test** by qa-tester.
8. **Review panel** in parallel: correctness and maintainability always;
   security and performance by risk; acceptance and exploratory testing.
9. **Triage**: every finding fixed (with a regression test), deferred (with a
   follow-up) or rejected (with evidence), in the spec's decision log, whose
   `test` column records each fix's regression test.
   - A `pre-existing` finding (see [Reviewer reports](#reviewer-reports)) is
     a follow-up by default; it is fixed in the change only when the change
     makes it reachable or worse, or you ask. Critical and high ones still
     come to you, with that default stated.
   - **Convergence rule**: when one component draws findings of medium or
     higher, dispositioned fix, from any source, in two consecutive fix
     rounds, the lead stops patching it, writes a short
     design note with options (fail closed by construction, narrow the
     scope, accept and document a limit), and asks you before the next
     round.
   - At most three fix rounds in all before the lead comes back to you.
10. **Final gate**: `millennium-code-qc gate --base <base> --tier full --require-config`
   and the acceptance tests.
11. **Summary**: changes, criteria mapped to tests, the decision log, anything
    disabled or waived, and proposals waiting for you.

## Agents

| Agent | Skills | Tools | Isolation |
|---|---|---|---|
| `tech-lead` | tech-lead | all | none |
| `software-engineer` | engineering-standards; solution-design for research briefs | read, edit, shell, web | own worktree |
| `correctness-reviewer` | review-protocol, correctness-review | read, shell, edit (proof tests only) | own worktree |
| `qa-tester` | qa-testing | all but Agent | own worktree |
| `security-engineer` | review-protocol, security-review, threat-model, security-research | read, shell, web | none, read-only |
| `maintainability-reviewer` | review-protocol, maintainability-review | read, shell | none, read-only |
| `performance-reviewer` | review-protocol, performance-review | read, shell | none, read-only |

- **Worktrees branch from your `HEAD`.** `config apply` sets
  `worktree.baseRef: "head"` in the project's settings, so engineers start
  from the lead's committed work, not the default branch. The lead commits
  before dispatching for that reason.
- **Engineers do not commit.** The stop hook checks their working tree; the
  lead checks the branch's scope and gates, then commits and merges.
- **The engineer cannot change acceptance tests**, and **qa-tester cannot read
  code inside components** (it may read documents there by path). The hooks
  enforce both; see [Hooks](hooks.md).
- **"Read-only" reviewers have a shell**, to run `git diff` and the engine.
  Their instructions forbid changes, and their findings go to the lead, who
  never merges a reviewer's edits; it is a convention backed by review, not a
  sandbox rule.

## Skills

| Skill | Invoked by | What it is |
|---|---|---|
| `tech-lead` | you (`/millennium:tech-lead <task>`), the tech-lead agent | The tech-lead playbook above. |
| `solution-design` | you (`/millennium:solution-design <problem>`), the lead, an engineer with a research brief | Research and design before building: the reference behaviour, approaches others use, spikes, options with trade-offs, a recommendation. An option with a security decision defaults to the security-check principles (below) and says so in its trade-offs. Needs no opt-in. |
| `init-project` | you (`/millennium:init-project [committed\|local]`) | Sets up a new project: languages, conventions, a draft config, an optional layout skeleton. |
| `adopt-project` | you (`/millennium:adopt-project [committed\|local]`) | Onboards existing code: `millennium-code-qc discover`, convention matching, a dry-run audit, the draft config. |
| `vet-dependency` | you (`/millennium:vet-dependency npm express@4.21.2`), the lead | Vets a dependency and drafts its approval records into `millennium.proposal.json`. |
| `engineering-standards` | engineer | Scope discipline, conventions, tests, dependencies, working with the gates; the security-check principles (below); verifying behaviour against the real runtime, not assumed defaults; tests whose expected values do not come from the code under test; measuring named hot paths; fixes that do not reopen earlier findings; updating the docs in its paths. |
| `review-protocol` | every reviewer | What a finding is, severities, each finding's origin, and the JSON report format. |
| `correctness-review` | correctness-reviewer | Spec conformance, edge cases, error paths, test quality, failing tests as proof. |
| `security-review` | security-engineer | Source-to-sink review of a diff, authorization, secrets, crypto, limits. |
| `threat-model` | security-engineer | The runtime inventory first, then assets, boundaries, STRIDE (with parser differentials and fail-closed denial of service named), abuse cases, testable security requirements; each threat checked against the runtime and marked with its origin (introduced, worsened or pre-existing), the pre-existing ones listed as follow-up candidates; its runtime additions go to the spec's Runtime section. |
| `security-research` | security-engineer | Variant analysis, source-to-sink across the codebase, surface audits; local proof only. |
| `maintainability-review` | maintainability-reviewer | Architecture fit, naming, cohesion, duplication, readability. |
| `performance-review` | performance-reviewer | Complexity at stated sizes, queries, memory, blocking I/O, limits. |
| `qa-testing` | qa-tester | Acceptance, smoke and exploratory modes, black-box. |

The **security-check principles** apply to any code that decides whether
something is allowed (a guard, a validator, a sanitiser, a permission check):
an allowlist rather than a denylist; whatever cannot be classified is denied;
no trust in a re-implementation of another program's parser, and where one
is unavoidable, only a subset shown to be read the same way, tested against
the real program; and a rewrite or narrowing of a protection comes with a
differential test showing everything the old version denied is still
denied. `engineering-standards` holds them for engineers, and
`solution-design` uses them as the default for design options.

The **runtime inventory** lives in the spec's Runtime section. The design
lists it at every scale, the threat model completes it, the lead passes it
to each work package, and engineers verify behaviour against it.

The skills hold no convention-specific rules: the engineer and the
maintainability reviewer follow the guide of each convention the repository
uses, which every agent's briefing carries (see
[Convention guides](hooks.md#convention-guides)). A project's own
convention reaches them the same way as a built-in one.

Skills marked for agents are hidden from your `/` menu; you can still read
them in `plugin/skills/`.

## Reviewer reports

Every reviewer ends with one JSON object, so findings can be triaged and
logged the same way:

```json
{
  "reviewer": "correctness-reviewer",
  "mode": "correctness-review",
  "base": "<commit>",
  "head": "<commit>",
  "verdict": "changes-requested",
  "findings": [
    {
      "id": "CR-1",
      "severity": "high",
      "origin": "introduced",
      "category": "edge-case",
      "file": "services/cart/internal/domain/total.go",
      "line": 42,
      "title": "Total of an empty cart is NaN",
      "evidence": "average divides by len(items) without a zero check",
      "failure_scenario": "GET /cart/total on a new cart returns NaN; AC-3 requires 0",
      "suggested_fix": "return 0 for an empty cart before dividing",
      "proof": "internal/domain/total_empty_test.go fails: got NaN, want 0"
    }
  ],
  "not_reviewed": ["db/017.sql: needs a database to judge"]
}
```

Severities: `critical` (data loss, breach, main path broken), `high` (a
criterion or security requirement not met, a bug users will hit), `medium`
(a less common path, or a defect that will cause bugs soon), `low`. A
verdict is `approved` only without critical, high or medium findings that
are `introduced` or `worsened`; pre-existing ones are reported but do not
block it.

Every finding carries an `origin`: `introduced` (the change adds the
problem), `worsened` (it was there before, and the change makes it reachable,
more likely or worse) or `pre-existing` (it was there before, and the change
leaves it as it was). A reviewer that cannot tell says `introduced`. The
lead files pre-existing findings as follow-ups by default (see Triage
above), so a review that follows a threat into neighbouring code does not
grow the change.

## The engine from an agent's shell

The plugin has no `bin/` directory (claude.ai and Cowork refuse plugins that
have one), so `millennium-code-qc` is not on agents' `PATH`. The session-start
context names the launcher's full path, and the skills use
`"${CLAUDE_PLUGIN_ROOT}/scripts/millennium-code-qc"`, which Claude Code fills in.
In pi, the `millennium` section of the system prompt names the command, and
the agents run through the `millennium_agent` tool; see [pi](pi.md).
