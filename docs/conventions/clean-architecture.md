# Clean Architecture

Select it with `"conventions": ["clean-architecture"]`, for a single project
or per component.

**Status:** enforced for Go. TypeScript and Python components that select it
block with `convention-unavailable` until their analyzers ship.

## Layers

Code is split into four layers, and dependencies point inward:

| Layer | Holds | May import | Third-party imports |
|---|---|---|---|
| `domain` | Entities, value types, domain errors | Nothing of the project | No |
| `application` | Ports (interfaces the core needs) and use-cases | `domain` | No |
| `infrastructure` | Adapters that implement ports: databases, HTTP clients, files | `domain`, `application` | Yes |
| `interface` | Delivery: HTTP handlers, CLI, DTOs, error mapping | `domain`, `application` | Yes |
| composition root | `main.go`: builds adapters, wires use-cases | Everything | Yes |

Every layer may import the standard library and its own layer. No layer may
import the composition root, and `infrastructure` and `interface` may not
import each other: delivery reaches adapters only through ports.

Imports of code that is in no layer, or outside the component, count as
outside imports: allowed where third-party imports are, blocked in `domain` and
`application`.

## Default layout for Go

Paths are relative to the component root (the directory with `go.mod` in a
typical service):

```
main.go                     composition root (also cmd/)
internal/
  domain/                   domain
  application/              application (port/, usecase/, ...)
  infrastructure/           infrastructure (one package per adapter)
  interface/                interface (web/, cli/, ...)
```

A package outside these directories is in no layer. The layer rules do not
restrict it, but layered code may not import it either; give it a layer or
move it.

## Mapping a different layout

When a component's layout differs, override the layer paths for that
component. Each override replaces the pack's paths for that layer:

```json
{
  "paths": ["services/kokoro-tts/app"],
  "languages": ["python"],
  "conventions": ["clean-architecture"],
  "layers": {
    "domain": ["*/domain.py"],
    "application": ["*/service.py"],
    "composition": ["*/main.py"]
  }
}
```

Use `composition` for the composition root. Check the result with
`millennium config explain <file>`.

## Adopting it in existing code

`millennium gate` checks only imports a change adds, so an existing codebase
with violations can adopt the convention immediately; touched code is held to
the rules. `millennium audit` lists every existing violation, which is the
refactoring backlog.

## Why these rules

The core (`domain`, `application`) describes what the software does without
knowing how it talks to the world, so it can be tested with plain fakes and
survives a change of database or framework. Keeping third-party packages out
of the core keeps that promise checkable.
