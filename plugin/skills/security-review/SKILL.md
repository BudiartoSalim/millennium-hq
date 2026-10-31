---
name: security-review
description: Security review of a change's diff - injection, authentication and authorization, secrets, crypto, input limits, data exposure, file and network access - with an attacker scenario for every finding. Use as the security-engineer in security-review mode.
user-invocable: false
---

# Security review

Follow the `review-protocol` skill for scope, findings and output. Your `id`
prefix is `SEC`. Start from the spec's security requirements (SR-n) and the
threat model, if there is one: each SR either holds in the code or is a
finding.

## Trace the data

For each entry point the change adds or touches (HTTP handler, CLI argument,
message consumer, file read, environment variable, webhook), follow untrusted
data to every sink it reaches, and check what stands between:

| Sink | Look for |
|---|---|
| SQL, NoSQL, LDAP queries | String-built queries instead of parameters |
| Shell, `exec`, subprocess | Arguments through a shell; user input as a flag (`-` prefix) |
| File paths | Traversal (`..`, absolute paths, symlinks), writes outside a fixed root |
| URLs fetched server-side | SSRF: user-controlled host, redirects, internal addresses, metadata endpoints |
| Templates, HTML, logs | Injection, XSS, log forging; unescaped output |
| Deserialisers | Untrusted data into pickle, YAML loaders, polymorphic JSON |
| Redirects | Open redirect to a user-supplied URL |

## Who may do what

- Every new operation checks authentication and then authorization, on the
  server, for the specific object (no IDOR: `/orders/{id}` checks that the
  order is the caller's).
- No privilege decided by a client-supplied field (role, user id, price).
- Defaults deny. A missing check in one handler of many is the usual bug.

## Secrets and crypto

- No secrets in code, logs, error messages, URLs or client bundles.
- Standard libraries only: no hand-rolled crypto, no MD5 or SHA-1 for
  security, a CSPRNG for tokens, constant-time comparison of secrets,
  password hashing with a slow KDF (argon2, bcrypt, scrypt).
- Tokens: expiry checked, signature algorithm pinned, audience and issuer
  checked.

## Limits and exposure

- Unbounded input: request bodies, uploads, pagination, regexes on user input
  (ReDoS), recursion depth, decompression.
- Errors and responses that leak internals, stack traces or other users' data.
- CORS, cookies (`Secure`, `HttpOnly`, `SameSite`), CSRF on state-changing
  requests.
- Time-of-check to time-of-use races on files and permissions.

## Findings

Each finding states the attacker: what they control, what they send, and
what they gain. Severity follows the consequence: remote code execution,
authentication bypass or another user's data is `critical`.
