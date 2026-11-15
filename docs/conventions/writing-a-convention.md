# Writing a convention

A convention, such as `clean-architecture` or `react-feature-based`, is a
**pack**: a folder with two files.

```
<name>/
  pack.json   the rules the gates enforce: languages, layers and what each may
              import, which layers need tests, default layer paths, function limits
  guide.md    what every agent is told about the convention: what belongs in
              each layer, naming, what reviewers look for
```

The engine knows no convention by name. It ships two packs and applies any
pack the project adds, so a convention of your own needs no engine change and
no release: you write a pack, propose it, and apply it like any config
change.

## Before you start: do you need a new convention?

| Your situation | What to do instead |
|---|---|
| Your code follows an existing convention under other directory names (`pkg/core`, `pkg/usecases`, ...) | Map the pack's layers onto your layout with `layers` in the component; see [Mapping a different layout](clean-architecture.md#mapping-a-different-layout). |
| One component needs other function limits | Give the component its own `limits`; see [Function limits](../configuration.md#function-limits). |
| Your code is not layered | Use no convention, with a `reason` on the component. Every other check still applies. |
| You want different layers, import rules, tests or limits for a kind of code | Write a convention. Read on. |
| You need a kind of rule packs cannot express, such as naming rules | That is an engine change; see [When a pack cannot express it](#when-a-pack-cannot-express-it). |

## Steps

1. **Draft it from the closest pack.**

   ```
   millennium-code-qc convention list
   millennium-code-qc convention new hexagonal --from clean-architecture
   ```

   This writes `millennium.proposal/conventions/hexagonal/pack.json` and
   `guide.md`: a copy of the source pack under the new name, with every
   limit written out. The folder is git-ignored, like the proposal.

2. **Edit `pack.json`**: the layers, what each may import, which need tests,
   where each lives per language, and the limits. The format is below; the
   JSON Schema in
   [`schema/millennium-pack.schema.json`](../../schema/millennium-pack.schema.json)
   gives your editor completion (`"$schema": "<path to it>"`).

3. **Edit `guide.md`.** Agents get the guide of every convention the
   repository uses in their session context, so keep it to what they need:
   what belongs in each layer, how to write code there, and what a reviewer
   checks. At most 8,000 characters; under 3,000 is a good target, so
   several guides fit in one briefing.

4. **Propose it.** `convention new` prints a proposal that adds the pack.
   Add the convention to the config in the same proposal:

   ```json
   {
     "version": 1,
     "packs": { "hexagonal": "millennium.proposal/conventions/hexagonal" },
     "config": { "version": 1, "languages": ["go"], "conventions": ["hexagonal"] }
   }
   ```

5. **Try it before applying.** An audit against the proposal shows what the
   rules find in the code you have:

   ```
   millennium-code-qc audit --proposal millennium.proposal.json --tier fast
   ```

   It should report the violations you expect, and nothing a person
   following the convention would consider correct.

6. **Apply it** yourself: `millennium-code-qc config apply`. It shows the
   pack's rules and what the change loosens, then writes the pack to
   `.millennium/conventions/hexagonal/`. Commit that folder with the config.
   Then check a few files with
   `millennium-code-qc config explain <file>`.

To change a pack later, edit a copy and propose it under the same name: the
proposal replaces the project's pack, and `config apply` lists anything it
weakens.

An agent can do steps 1 to 5 with you; only you can apply. The init-project
and adopt-project skills offer it when no existing pack fits.

## The pack format

The [feature-based React pack](../../code-qc/internal/infrastructure/packs/builtin/react-feature-based/pack.json),
shortened:

```json
{
  "format": 1,
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
  },
  "limits": {"functionLines": 50, "nesting": 3, "cognitive": 15},
  "suggest": {"dependencies": ["react"]}
}
```

**Top level:**

| Field | Required | Meaning |
|---|---|---|
| `format` | yes | `1`. |
| `name` | yes | The convention's name, as components list it: lowercase letters, digits and dashes, at most 40 characters. It must match the folder's name, and may not be a built-in pack's name. |
| `languages` | yes | The languages the convention governs. A component's files in other languages (shell scripts beside a Go service) are outside its rules, and a component must use at least one of them. |
| `layers` | yes | The layers, in order. |
| `paths` | yes | Per language, per layer: where the layer lives, relative to the component root. |
| `limits` | no | `functionLines`, `nesting` and `cognitive` for every function in components that use the convention. An omitted limit is the engine's default (50, 3, 15). |
| `suggest` | no | `{"dependencies": [...]}`: package names in a module's `package.json` that make `discover` suggest this pack over packs without hints. |

**Each layer:**

| Field | Meaning |
|---|---|
| `name` | Any name except `composition`, which is reserved for the composition root. |
| `mayImport` | The other layers this layer may import. Imports within the same layer are always allowed. |
| `thirdParty` | Whether the layer may import external packages, and code outside its component. The standard library is always allowed. |
| `tested` | Changed logic in this layer needs changed tests, and its changed lines must run in a test (the [`tests` gate](../checks.md#tests)). |
| `isolated` | Each match of the layer's path pattern is a separate unit, and units may not import each other: `src/features/*` makes every feature a unit, so `features/cart` cannot import `features/auth`. |

**Paths:**

- Patterns are relative to the component root. Each matches a directory or
  file and everything under it. `*` matches within one path segment.
- `composition` is the composition root (`main.go`, `src/app`). It may
  import every layer, and no layer may import it. It appears only under
  `paths`, never under `layers`.
- A file belongs to the first layer, in the order of `layers`, that has a
  matching pattern, and to `composition` last. Code in no layer is not
  checked for imports, but a layer that imports it is reported.
- A language listed in `languages` without an entry in `paths` blocks with
  `convention-unavailable`: the convention claims the language but cannot
  check it.
- Users override these paths per component with `layers`. Make the defaults
  the layout most projects of that kind already use, so `discover` finds it.

**What gets checked:** the `structure` gate reports `layer-import` for every
import a change adds that breaks the rules: an import of a layer not in
`mayImport`, of the composition root, of a sibling unit of an isolated layer,
of code in no layer, or of an external package where `thirdParty` is false.
Imports a file already had are left to `audit`. Imports are resolved for Go,
TypeScript (with the project's own module resolution, `tsconfig` `paths`
included) and Python; shell has no imports.

**How it is read:** like the config. Unknown fields, duplicate keys, a
symlinked folder or file, a `pack.json` over 64 KB, or a pack that does not
validate block every check with a message saying why.

## Where packs live

| Source | Location |
|---|---|
| Built in | inside the engine: `clean-architecture`, `react-feature-based` |
| The project | `.millennium/conventions/<name>/` |
| A local config | `<local config dir>/conventions/<name>/` |

Project packs sit inside `.millennium/`, so agents cannot change them, just
as they cannot change the config. To vary a built-in convention, copy it
under a new name: packs do not extend each other, so one pack is always the
whole rule set.

## When a pack cannot express it

A pack can express:
- which layer may import which;
- whether a layer may import third-party code;
- which layers need tests;
- isolated units within a layer;
- function limits.

Anything else is a new rule type in the engine, for example:
- naming rules;
- required files;
- calls banned in one layer;
- different limits per layer.

A new rule type is a change to this repository:
1. A field on `PackLayer` or `Pack` in `code-qc/internal/domain/pack.go`,
   validated in `NewPack`, and the loosening it can cause in `loosen.go`.
2. The same field in the pack DTO in
   `code-qc/internal/infrastructure/packs/packs.go`, and in
   `schema/millennium-pack.schema.json` (a test keeps the two in step).
3. The check itself, usually in the `structure` gate, with its rule name
   registered in the gate's list of rules (`gate/structure.go`), which also
   says whether it can be disabled.
4. The rule in [Checks](../checks.md), and this page.

Once it exists, every pack can use it. Discuss a new rule type in an issue
first: it is a design decision, recorded in `docs/design.md`.

A new **built-in** pack is a folder in
`code-qc/internal/infrastructure/packs/builtin/`, a page in
`docs/conventions/`, and an engine release.
