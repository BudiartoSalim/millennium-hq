# Clean Architecture

Select it with `"conventions": ["clean-architecture"]`, for a single project
or per component.

**Status:** enforced for Go, TypeScript and Python.

## Layers

Code is split into four layers, and dependencies point inward:

| Layer | Holds | May import | Third-party imports | Tests required |
|---|---|---|---|---|
| `domain` | Entities, value types, domain errors | Nothing of the project | No | Yes |
| `application` | Ports (interfaces the core needs) and use-cases | `domain` | No | Yes |
| `infrastructure` | Adapters that implement ports: databases, HTTP clients, files | `domain`, `application` | Yes | No |
| `interface` | Delivery: HTTP handlers, CLI, DTOs, error mapping | `domain`, `application` | Yes | No |
| composition root | `main.go`: builds adapters, wires use-cases | Everything | Yes | No |

"Tests required" means changed logic must come with changed tests, and in the
full tier every changed line must run in a test: see the
[`tests` gate](../checks.md#tests). Adapters are worth testing where it is
cheap, but the gate does not demand it.

The convention governs Go, TypeScript and Python; shell scripts in the same
component are outside its scope.

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

## Default layout for TypeScript

```
src/main.ts, src/index.ts   composition root
src/domain/                 domain
src/application/            application
src/infrastructure/         infrastructure
src/interface/              interface (http/, controllers, dto, ...)
```

Imports are resolved the way the TypeScript compiler resolves them, so
`tsconfig.json` `paths` aliases such as `@/domain/user` count as imports of
that file. Node built-ins (`node:fs`, `path`) are the standard library; any
package from `node_modules` is third-party.

## Default layout for Python

Python services are often laid out as one package of modules, so the default
accepts both directories and single modules, relative to the component root:

```
<package>/main.py, __main__.py         composition root
<package>/domain/ or domain.py         domain
<package>/application/, service.py,
          services/                    application
<package>/infrastructure/              infrastructure
<package>/interface/, http_api.py      interface
```

Other modules of the package (adapters such as `engine.py`) are in no layer.
Modules from the standard library (`sys.stdlib_module_names`) are standard; a
top-level package found in the component, or under its `src/`, is project
code; anything else is third-party.

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
`millennium-code-qc config explain <file>`.

## Adopting it in existing code

`millennium-code-qc gate` checks only imports a change adds, so an existing codebase
with violations can adopt the convention immediately; touched code is held to
the rules. `millennium-code-qc audit` lists every existing violation, which is the
refactoring backlog.

## Why these rules

The core (`domain`, `application`) describes what the software does without
knowing how it talks to the world, so it can be tested with plain fakes and
survives a change of database or framework. Keeping third-party packages out
of the core keeps that promise checkable.
