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

- **Assets**: what is worth protecting (credentials, personal data, money,
  availability, integrity of records).
- **Actors**: anonymous users, authenticated users, other tenants, admins,
  internal services, an attacker with a stolen token.
- **Entry points and trust boundaries**: every place data or control crosses
  from less to more trusted.
- **Threats, by STRIDE at each boundary**: spoofing, tampering, repudiation,
  information disclosure, denial of service, elevation of privilege. Keep the
  ones that are plausible for this change; skip the generic.
- **Abuse cases**: "an authenticated user changes the order id in the URL to
  read another user's order".

## 3. Output

A markdown section the lead pastes into the spec:

```markdown
## Threat model
Assets: ...
Boundaries: ...

| Threat | Boundary | Abuse case | Mitigation |
|---|---|---|---|

## Security requirements
- SR-1: <a testable statement, e.g. "GET /orders/{id} returns 404 for an order
  that is not the caller's"> - test: <unit or acceptance, and what it asserts>
```

Every requirement must be checkable by a test, and name the test level
(unit, acceptance). List open questions for the user separately, such as
risk the spec should accept explicitly rather than mitigate.
