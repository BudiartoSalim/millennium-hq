# Agents and skills

The plugin ships seven agents and eleven skills. The agents differ by what
they may do (tools, worktree, what they may read); the skills hold the
instructions. Skills are portable (the Agent Skills format); the agent files
are Claude Code's adapter.

## Leading a change

Run the tech lead in your main session:

```
/millennium:lead add a cart total endpoint that applies the active discount
```

The lead writes a spec with you, splits the work into packages for parallel
engineers, has the result reviewed and tested by fresh-context agents, records
a decision for every finding, and finishes only when the full gate passes.
You see each step and can overrule any decision. Sessions that do not use the
lead work as usual; the gates apply either way.

To run the same playbook in the background, ask for the `millennium:tech-lead`
agent. It stops and reports where the interactive lead would ask you.

The workflow, in order:

1. **Spec** with acceptance criteria (AC-n), non-goals, constraints and risk.
2. **Threat model** by the security engineer when the change touches a trust
   boundary; its security requirements (SR-n) join the spec.
3. **Work packages** with disjoint paths, contracts first, at most three
   engineers at once unless you agree to more.
4. **Engineers** in parallel, each in its own worktree; at the same time
   qa-tester writes the acceptance tests from the spec.
5. **Integration**: per package `millennium scope` against its paths and the
   full gate, then a merge; then the full gate on the integrated branch.
6. **Smoke test** by qa-tester.
7. **Review panel** in parallel: correctness and maintainability always;
   security and performance by risk; acceptance and exploratory testing.
8. **Triage**: every finding fixed (with a regression test), deferred (with a
   follow-up) or rejected (with evidence), in the spec's decision log; at most
   three fix rounds before the lead comes back to you.
9. **Final gate**: `millennium gate --base <base> --tier full --require-config`
   and the acceptance tests.
10. **Summary**: changes, criteria mapped to tests, the decision log, anything
    disabled or waived, and proposals waiting for you.

## Agents

| Agent | Skills | Tools | Isolation |
|---|---|---|---|
| `tech-lead` | lead | all | none |
| `engineer` | engineering-standards | read, edit, shell, web | own worktree |
| `qa-engineer` | review-protocol, correctness-review | read, shell, edit (proof tests only) | own worktree |
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
| `lead` | you (`/millennium:lead <task>`), the tech-lead agent | The tech-lead playbook above. |
| `vet-dependency` | you (`/millennium:vet-dependency npm express@4.21.2`), the lead | Vets a dependency and drafts its approval records into `millennium.proposal.json`. |
| `engineering-standards` | engineer | Scope discipline, conventions, tests, dependencies, working with the gates. |
| `review-protocol` | every reviewer | What a finding is, severities, and the JSON report format. |
| `correctness-review` | qa-engineer | Spec conformance, edge cases, error paths, test quality, failing tests as proof. |
| `security-review` | security-engineer | Source-to-sink review of a diff, authorization, secrets, crypto, limits. |
| `threat-model` | security-engineer | Assets, boundaries, STRIDE, abuse cases, testable security requirements. |
| `security-research` | security-engineer | Variant analysis, source-to-sink across the codebase, surface audits; local proof only. |
| `maintainability-review` | maintainability-reviewer | Architecture fit, naming, cohesion, duplication, readability. |
| `performance-review` | performance-reviewer | Complexity at stated sizes, queries, memory, blocking I/O, limits. |
| `qa-testing` | qa-tester | Acceptance, smoke and exploratory modes, black-box. |

Skills marked for agents are hidden from your `/` menu; you can still read
them in `plugin/skills/`.

## Reviewer reports

Every reviewer ends with one JSON object, so findings can be triaged and
logged the same way:

```json
{
  "reviewer": "qa-engineer",
  "mode": "correctness-review",
  "base": "<commit>",
  "head": "<commit>",
  "verdict": "changes-requested",
  "findings": [
    {
      "id": "QA-1",
      "severity": "high",
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
verdict is `approved` only without critical, high or medium findings.

## The engine from an agent's shell

The plugin has no `bin/` directory (claude.ai and Cowork refuse plugins that
have one), so `millennium` is not on agents' `PATH`. The session-start
context names the launcher's full path, and the skills use
`"${CLAUDE_PLUGIN_ROOT}/scripts/millennium"`, which Claude Code fills in.
