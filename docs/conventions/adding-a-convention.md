# Adding a convention

A convention, such as `clean-architecture` or `react-feature-based`, is a
**pack**: a JSON file that names layers, says which layer may import which,
and says where each layer lives for each language. The engine is generic and
applies whatever packs it ships with.

Packs are embedded in the engine binary, so an agent cannot weaken a pack by
editing a file on disk. As a consequence, a new convention is a change to
this repository and ships in an engine release. A project cannot define a
convention in its own config.

## Before you start: do you need a new convention?

| Your situation | What to do instead |
|---|---|
| Your code follows an existing convention under other directory names (`pkg/core`, `pkg/usecases`, ...) | Map the pack's layers onto your layout with `layers` in the component; see [Mapping a different layout](clean-architecture.md#mapping-a-different-layout). No engine change. |
| Your code is not layered | Use no convention, with a `reason` on the component. Every other check still applies. |
| You need one extra rule, such as "HTTP handlers live in a package named `web`" or "every `http.Server` sets timeouts" | That is a new **rule type** in the engine, not a pack (see [When a pack cannot express it](#when-a-pack-cannot-express-it)). |
| You want a different set of layers with their own import rules | A new convention. Read on. |

## Where conventions live

| Place | What it holds |
|---|---|
| `code-qc/internal/infrastructure/packs/builtin/<name>.json` | **The pack**: layers, import rules, default paths per language. Embedded with `go:embed`. |
| `code-qc/internal/domain/config.go` | The `Convention` constant and the `Conventions()` allowlist. A pack whose name is not on the list fails validation. |
| `code-qc/internal/domain/component.go` | Config rules that involve a convention, such as "`react-feature-based` needs `typescript` in the component's languages". |
| `code-qc/internal/domain/discover.go` | `bestConvention`: which pack `discover` suggests for a module. The layout fit is computed for every pack automatically. |
| `schema/millennium.schema.json` | `$defs.convention.enum`, for editor completion. `schema_sync_test.go` fails when it drifts from `Conventions()`. |
| `docs/conventions/<name>.md` | The user documentation of the convention. |
| `plugin/skills/` | The guidance agents follow: `init-project` (when to choose it), `engineering-standards` (how to write code in it), `maintainability-review` (what to review). |

## The pack format

The [feature-based React pack](../../code-qc/internal/infrastructure/packs/builtin/react-feature-based.json),
annotated:

```json
{
  "name": "react-feature-based",
  "languages": ["typescript"],
  "layers": [
    {"name": "shared",   "mayImport": [],         "thirdParty": true, "tested": true},
    {"name": "features", "mayImport": ["shared"], "thirdParty": true, "tested": true, "isolated": true}
  ],
  "paths": {
    "typescript": {
      "shared": ["src/components", "src/hooks", "src/lib"],
      "features": ["src/features/*"],
      "composition": ["src/app", "src/main.tsx"]
    }
  }
}
```

**Top level:**

| Field | Meaning |
|---|---|
| `name` | The convention's name, as users write it in `conventions`. Lowercase with dashes, and on the `Conventions()` allowlist. |
| `languages` | The languages the convention governs. A component's files in other languages (shell scripts next to a Go service) are outside its scope. |
| `layers` | The layers, in order. |
| `paths` | Per language, per layer: where the layer lives, relative to the component root. |

**Each layer:**

| Field | Meaning |
|---|---|
| `name` | Any name except `composition`, which is reserved. |
| `mayImport` | The other layers this layer may import. Imports within the same layer are always allowed. |
| `thirdParty` | Whether the layer may import external packages, and code outside its component. The standard library is always allowed. |
| `tested` | Changed logic in this layer needs changed tests, and its changed lines must run in a test (the [`tests` gate](../checks.md#tests)). |
| `isolated` | Each match of the layer's path pattern is a separate unit, and units may not import each other. `src/features/*` makes every feature a unit, so `features/cart` cannot import `features/auth`. |

**Paths:**

- Patterns are relative to the component root. Each matches a directory or
  file and everything under it. `*` matches within one path segment.
- `composition` is the composition root (`main.go`, `src/app`). It may import
  every layer, and no layer may import it. It appears only under `paths`,
  never under `layers`.
- A file belongs to the first layer, in the order of `layers`, that has a
  matching pattern, and to `composition` last. Code in no layer is not
  checked for imports, but a layer that imports it is reported.
- A language listed in `languages` without an entry in `paths` blocks with
  `convention-unavailable`: the convention claims the language but cannot
  check it. Leave a language out of `languages` instead, until its paths
  exist.
- Users override these paths per component with `layers`. Your defaults
  should match the layout most projects of that kind already use, so that
  `discover` finds it.

**What gets checked:** the `structure` gate reports `layer-import` for every
import a change adds that breaks the rules: an import of a layer not in
`mayImport`, of the composition root, of a sibling unit of an isolated layer,
of code in no layer, or of an external package where `thirdParty` is false.
Imports a file already had are left to `audit`. The language analyzers
resolve each import to a file: Go module paths, TypeScript through the
project's own module resolution (`tsconfig` `paths` included), and Python
imports. Only Go, TypeScript and Python produce imports today.

## Steps

The examples add a hypothetical `hexagonal` convention.

1. **The allowlist.** In `code-qc/internal/domain/config.go`, add the
   constant and put it in `Conventions()`:

   ```go
   ConventionHexagonal Convention = "hexagonal"
   ```

2. **The pack.** Add `code-qc/internal/infrastructure/packs/builtin/hexagonal.json`.
   Start from the layers' dependency rules, then the default paths for each
   language you can support well.

3. **Config rules, if any.** If the convention only makes sense with
   certain languages, add the check in `domain/component.go` next to the
   `react-feature-based` one. Mention it in the schema's top-level
   `description`, which lists what the schema cannot express.

4. **Discovery, if the pack should be suggested.** `discover` already
   reports how many of a module's files the new pack's default layout
   places in a layer. To have it suggest the pack, extend `bestConvention`
   in `domain/discover.go` with the signal that selects it (the React check
   is the existing example).

5. **The schema.** Add the name to `$defs.convention.enum` in
   `schema/millennium.schema.json`.

6. **Tests**, in the same change (see [AGENTS.md](../../AGENTS.md#testing)):
   - `packs/packs_test.go`: the builtin pack loads and has the layers you
     expect.
   - `domain/pack_test.go`: for each layer, an allowed import passes and a
     forbidden one is reported. Also cover isolation and third-party imports
     if the pack uses them, and the composition root.
   - `domain/config_test.go` and `domain/component_test.go`: the name is
     accepted, and any language rule from step 3 is enforced.
   - `domain/discover_test.go`, if you changed `bestConvention`.

7. **Docs.**
   - `docs/conventions/<name>.md`. Follow the existing two: how to select
     it, its status, the layers table, the default layout per language,
     mapping a different layout, adopting it in existing code, and why the
     rules are what they are.
   - `docs/configuration.md`: the list of packs in the `conventions` row.
   - `docs/README.md`: a row in the guides table.
   - The convention lists in `README.md` and the packs list in
     `docs/design.md`.

8. **Agent guidance.** Agents only know about a convention if a skill tells
   them:
   - `init-project`: when to choose it, in one sentence;
   - `engineering-standards`: how to write code in it;
   - `maintainability-review`: what a reviewer checks beyond the import
     rules (the gate covers those).

9. **Verify and try it.** Run `bash scripts/verify.sh`. Then build the
   engine (`cd code-qc && go build -o millennium-code-qc .`) and try it on a
   real project laid out that way:

   ```
   MILLENNIUM_CODE_QC=/path/to/millennium-code-qc
   "$MILLENNIUM_CODE_QC" discover                  # is the layout recognised?
   "$MILLENNIUM_CODE_QC" config explain <file>     # does each file land in the right layer?
   "$MILLENNIUM_CODE_QC" audit --proposal millennium.proposal.json
   ```

   The audit should report the violations you expect, and nothing that a
   person following the convention would consider correct code.

10. **Release.** A new pack changes the engine, so users get it with the
    next engine release: `scripts/release-build.sh <version> --lock`, then
    the steps in [Releasing](../releasing.md).

## When a pack cannot express it

A pack can express only these:
- which layer may import which;
- whether a layer may import third-party code;
- which layers need tests;
- isolated units within a layer.

Anything else is a new rule type in the engine, for example:
- naming rules;
- required files;
- calls banned in one layer;
- different limits per layer.

A new rule type is a larger change:
1. A field on `PackLayer` or `Pack` in `domain/pack.go`, validated in
   `NewPack`.
2. The same field in the pack DTO in `infrastructure/packs/packs.go`, which
   rejects unknown fields.
3. The check itself, usually in the `structure` gate, with its rule name
   registered in the gate's list of rules (`gate/structure.go`), which also
   says whether it can be disabled.
4. That rule in [Checks](../checks.md). A rule that reports a check failing
   to run must not be disableable, and belongs in the list in
   [Configuration](../configuration.md#disabling-a-check).

Once it exists, every pack can use it. Discuss a new rule type in an issue
first: it is a design decision, recorded in `docs/design.md`.
