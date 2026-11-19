---
name: threat-model
description: Threat model for a planned change, before code is written - assets, entry points, trust boundaries, STRIDE threats and abuse cases, turned into testable security requirements for the spec. Use as the security-engineer in threat-model mode when a change touches a trust boundary.
user-invocable: false
---

# Threat model

You model the change before it is built. Your output becomes part of the
spec, so make it concrete enough to test.

## 1. Understand the change

Read the spec and the code around where the change will land: the existing
entry points, how requests are authenticated, where data is stored, and what
crosses process or network boundaries.

## 2. Model

- **Runtime inventory**, first: what the change will really run under. Start
  from the spec's Runtime section when it has one, and complete it:
  - operating systems and filesystems (Linux, macOS, Windows, WSL);
  - shells (macOS defaults to zsh, not bash);
  - the real tool implementations, not the assumed ones: Claude Code runs
    bfs as `find` and ugrep as `grep`, and BSD and GNU tools differ;
  - language and toolchain versions;
  - the harnesses and their sandboxes.
  Check each threat below against each entry: a mitigation that holds for
  GNU `find` under bash on Linux may not hold for bfs under zsh on macOS.
- **Assets**: what is worth protecting (credentials, personal data, money,
  availability, integrity of records).
- **Actors**: anonymous users, authenticated users, other tenants, admins,
  internal services, an attacker with a stolen token.
- **Entry points and trust boundaries**: every place data or control crosses
  from less to more trusted.
- **Threats, by STRIDE at each boundary**: spoofing, tampering, repudiation,
  information disclosure, denial of service, elevation of privilege. Keep the
  ones that are plausible for this change; skip the generic. Ask two
  questions at every boundary:
  - **Parser differentials**: does the check read the input the same way as
    the program that acts on it? A guard that parses a command, path or URL
    itself can disagree with the shell, filesystem or client that runs it,
    and every disagreement is a bypass.
  - **Fail-closed denial of service**: can one bad input or state (a
    malformed file, a stale lock, an unreadable entry) block every call, not
    only the bad one?
- **Origin**, as in `review-protocol`: `introduced` (the change creates it),
  `worsened` (it exists, and the change makes it reachable, more likely or
  worse) or `pre-existing` (it exists, and the change leaves it as it was).
  Pre-existing threats are follow-up candidates, not requirements here.
- **Abuse cases**: "an authenticated user changes the order id in the URL to
  read another user's order".

## 3. Output

Markdown the lead merges into the spec:

```markdown
## Threat model
Add to the spec's Runtime section: <the entries it lacks, or none>
Assets: ...
Boundaries: ...

| Threat | Origin | Boundary | Runtimes affected | Abuse case | Mitigation |
|---|---|---|---|---|---|

### Pre-existing threats (follow-up candidates)
- <each pre-existing threat in the table, where it lives, the follow-up it needs>

## Security requirements
- SR-1: <a testable statement, e.g. "GET /orders/{id} returns 404 for an order
  that is not the caller's"> - test: <unit or acceptance, and what it asserts>
```

Every requirement must be checkable by a test, and name the test level
(unit, acceptance). Requirements cover introduced and worsened threats.
List open questions for the user separately, such as risk the spec should
accept rather than mitigate, and any high or critical pre-existing threat.
