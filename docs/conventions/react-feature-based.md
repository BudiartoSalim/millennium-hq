# Feature-based React

Select it with `"conventions": ["react-feature-based"]` on a TypeScript
component, typically a web app:

```json
{ "paths": ["services/porto-webapp/web"], "languages": ["typescript"],
  "conventions": ["react-feature-based"] }
```

**Status:** enforced for TypeScript (`.ts`, `.tsx`). Plain `.js`/`.jsx` files
are not read.

## Layers

| Layer | Default paths (from the component root) | May import | Third-party | Tests required |
|---|---|---|---|---|
| `shared` | `src/components`, `src/hooks`, `src/lib`, `src/utils`, `src/types`, `src/config`, `src/stores`, `src/testing` | other shared code | Yes | Yes |
| `features` | `src/features/<name>` | its own feature, `shared` | Yes | Yes |
| composition root | `src/app`, `src/main.tsx`, `src/main.ts`, `src/index.tsx`, `src/index.ts`, `src/App.tsx` | everything | Yes | No |

Imports flow one way: shared, then features, then the app.

- **Features are isolated from each other.** Code in `src/features/cart` may
  not import `src/features/auth`, through a relative path or an alias such as
  `@/features/auth`; the engine resolves imports with TypeScript's own module
  resolution, `tsconfig` `paths` included. When two features need the same
  thing, move it to a shared folder; when a page needs both, compose them in
  `src/app`.
- **Shared code knows no feature.** A shared component that imports a
  feature is a finding.
- **The app composes.** Routes, providers and layouts in `src/app` may import
  any feature; nothing may import `src/app`.

Inside a feature, organise freely (`components/`, `hooks/`, `api/`,
`types/`, `stores/`); the rules apply at the feature boundary.

"Tests required" means changed logic must come with changed tests, and in the
full tier every changed line must run in a test, components included: a
component change needs a render test (for example Vitest or `node:test` with
a DOM library), and the test recipe must write an lcov report, see
[Test recipes](../configuration.md#test-recipes). Keep components thin and
put logic in hooks and plain modules, where tests are cheap.

Files under `src` that match no layer, such as `src/vite-env.d.ts`, are
checked for everything but layer rules; importing them from a layer counts as
an outside import, which `shared` and `features` allow.

## Mapping a different layout

Override any layer's paths per component, relative to the component root:

```json
{ "paths": ["web"], "languages": ["typescript"],
  "conventions": ["react-feature-based"],
  "layers": {
    "features": ["src/modules/*"],
    "shared": ["src/ui", "src/lib"],
    "composition": ["src/routes", "src/main.tsx"] } }
```

Each `*` in a `features` path marks where one feature ends: with
`src/modules/*`, `src/modules/billing` and `src/modules/profile` are separate
features. `millennium config explain <file>` shows the layer of any file.

## Adopting it in existing code

As with every convention, only imports a change adds are held to the layer
rules, so an existing cross-feature import does not block an unrelated
change. `millennium audit --path <component>` lists them all for a refactor.
