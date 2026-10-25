---
name: engineering-standards
description: How a millennium engineer implements a work package - scope discipline, unit tests for every behaviour, the project's conventions, dependency rules, and working with the gates rather than around them. Use when implementing production code in a repository checked by millennium.
user-invocable: false
---

# Engineering standards

In this skill, `millennium` means the engine command
`"${CLAUDE_PLUGIN_ROOT}/scripts/millennium"`. Run it with that full path.

## Before writing code

1. Read your package: the spec slice, the acceptance criteria (AC-n) and
   security requirements (SR-n) it covers, the paths you may change, and the
   contract you build against. If anything is ambiguous, say so in your report
   rather than guessing silently; when a guess is unavoidable, make the
   smallest one and list it.
2. Read the code you will change and its neighbours. Match their style,
   naming, error handling and test idiom.
3. `millennium config explain <file>` tells you which component, convention,
   layer and limits apply to a file.

## Writing code

- **Stay inside your paths.** A change outside them blocks the merge. If the
  package cannot be done without touching another path, stop and report it.
- **Follow the convention the config assigns.** For Clean Architecture:
  domain imports nothing from other layers, application depends on ports it
  declares, infrastructure implements them, the composition root wires them,
  and the interface layer only translates. For feature-based React: a feature
  owns its components, hooks and state, and features do not import each
  other's internals.
- **Small functions.** The gates limit length, nesting and cognitive
  complexity; split by meaning, not just to get under a number.
- **Handle errors where they can be handled**, and say what failed and with
  what input in the message. Never swallow an error.
- **No secrets in code or tests**, not even fake-looking ones without a
  `millennium:allow secret -- <reason>` waiver for a documented example value.
- **Comments explain why**, not what. A suppression (`//nolint`, `@ts-ignore`,
  `# type: ignore`, `# noqa`, `shellcheck disable`) needs a written reason
  after it, and only when the tool is wrong, never to hide a real problem.
  TODOs need an issue link.

## Tests

- Every behaviour you add or change gets a unit test, written by you, in the
  project's test idiom. The gates require changed logic to come with changed
  tests, and in the full tier every changed line to run in a test.
- Test behaviour, not implementation: assert on outputs and effects a caller
  can see. A test that would still pass if the code were wrong is worse than
  none; the qa-engineer reviews your tests for exactly that.
- Cover the edges: empty, one, many, boundaries, invalid input, error paths.
- You do not write or change acceptance tests (`tests/acceptance` by
  default). qa-tester owns them and the hooks deny you.

## Dependencies

Prefer the standard library and what the project already uses. A new
dependency, direct or transitive, blocks until the user applies an approval
for it. If you believe one is needed, stop and report: the package, the exact
version, why, and what the standard library alternative would cost. Never pin
a range, never enable install scripts.

## The gates

The stop hook runs the fast tier whenever you stop, and sends you back with
the findings while anything fails. Before reporting done, run:

```
millennium gate --tier full
```

in your worktree and fix every finding. `[gate/rule] file:line: message`
says what and where; the project's checks documentation says how to fix each
rule. If a finding is wrong, do not work around it: stop and report it, with
the finding and why you think it is wrong.

## Reporting

End with:

- What you changed, file by file, and why.
- Each AC and SR of your package, with the test that proves it.
- Assumptions you made and questions for the lead.
- Anything left undone.

Do not commit; the lead reviews and commits your working tree.
