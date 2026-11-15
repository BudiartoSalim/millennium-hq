---
name: maintainability-review
description: Maintainability and convention review of a change - architecture and layer fit, naming, cohesion, duplication, abstraction level, error messages, comments, and the readability of code and tests. Use as the maintainability-reviewer on every integrated change.
user-invocable: false
---

# Maintainability review

Follow the `review-protocol` skill for scope, findings and output. Your `id`
prefix is `MR`. The gates already enforce function length, nesting,
complexity, layer imports and formatting; judge what they cannot.

Use `"${CLAUDE_PLUGIN_ROOT}/scripts/millennium-code-qc" config explain <file>` to see
which convention and layer the config assigns to a file.

## Architecture and conventions

- **The convention's guide**: the guide of each convention the repository
  uses is in your session context (or `millennium-code-qc convention show
  <name>` prints it). Review against it, its Reviewing section first: is each
  piece in the right layer, not only allowed to import what it does? Does
  any rule leak into a layer that should not know it?
- **Consistency**: does the change follow the patterns the surrounding code
  already uses (errors, logging, configuration, naming)? A new pattern needs a
  reason.

## Code

- Names say what a thing is or does, in the domain's words. No `data`,
  `info`, `manager`, `util` without a reason.
- One responsibility per function and type; a boolean parameter that switches
  behaviour usually means two functions.
- Duplication of logic (not just of lines) that will drift apart.
- Abstractions earn their place: no interface with one implementation and no
  prospect of another, no layers of indirection for one call.
- Error messages a user or operator can act on: what failed, with what input.
- Comments explain why; a comment restating the code is noise, and a stale
  one is a bug.
- Dead code, commented-out code, unused parameters.

## Tests

Readability only (the correctness-reviewer judges whether tests prove the behaviour):
test names describe behaviour, setup is not repeated beyond reason, a failure
message says what went wrong, and table-driven or parameterised tests are
used where cases differ only in data.

## Severity

Maintainability findings are rarely `high`. Use `medium` for a design problem
that will cause bugs or block the next change, `low` for the rest. Prefer few
findings that matter over many that do not.
