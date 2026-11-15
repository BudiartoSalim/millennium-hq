# Clean Architecture

Code is split into layers and dependencies point inward. The gates enforce
the import rules; this guide is what they cannot check.

| Layer | Holds | May import |
|---|---|---|
| `domain` | Entities, value types, domain errors, business rules | nothing of the project, no third-party code |
| `application` | Ports (interfaces the core needs) and use cases | `domain`, no third-party code |
| `infrastructure` | Adapters that implement ports: databases, HTTP clients, files, queues | `domain`, `application`, third-party code |
| `interface` | Delivery: HTTP handlers, CLI, DTOs, mapping errors to responses | `domain`, `application`, third-party code |
| composition root | `main.go`, `cmd/`, `src/main.ts`, `main.py`: builds adapters and wires use cases | everything |

`domain` and `application` need unit tests for every change, and every
changed line must run in a test. Use fakes for ports: plain structs or
objects implementing the interface, no mocking framework needed.

## Writing code

- **Business rules live in `domain`**, not in handlers, SQL or adapters. If
  a rule would survive a change of database or framework, it belongs there.
- **Use cases orchestrate.** A use case in `application` loads what it needs
  through ports, calls the domain, and saves through ports. It knows no
  HTTP, SQL or file formats.
- **Ports belong to the core.** `application` declares the interface in the
  terms it needs (`SaveOrder(order)`), and `infrastructure` implements it. A
  port shaped around one adapter's API (`ExecSQL`, `PutObject`) is a leak.
- **Adapters adapt, without policy.** No business decisions in
  `infrastructure`; translate between the outside world and the core.
- **The interface layer only translates**: parse and validate input, call a
  use case, map the result or error to the response.
- **The composition root only wires.** No logic in `main`.
- **Errors**: the domain defines its errors; adapters wrap outside errors
  with context; the interface layer maps them to responses. Never swallow
  one.

## Reviewing

- Is each piece in the right layer, not only allowed to import what it does?
- Does any rule leak outward (validation in a handler, a decision in an
  adapter)?
- Is a new port shaped by what the core needs, and does it have more than
  one plausible implementation or a test fake?
- Are use cases tested through their ports with fakes, including the error
  paths?

## Default layout

Paths are relative to the component root; the config can map them onto a
different layout with `layers`.

- Go: `internal/domain`, `internal/application`, `internal/infrastructure`,
  `internal/interface`; `main.go` and `cmd/` are the composition root.
- TypeScript: `src/domain`, `src/application`, `src/infrastructure`,
  `src/interface`; `src/main.ts` and `src/index.ts` are the composition root.
- Python: `<package>/domain` (or `domain.py`), `<package>/application`
  (or `service.py`, `services/`), `<package>/infrastructure`,
  `<package>/interface` (or `http_api.py`); `main.py` and `__main__.py` are
  the composition root.

Code in no layer is not restricted, but layered code may not import it:
give it a layer or move it. `millennium-code-qc config explain <file>` shows
the layer of any file.
