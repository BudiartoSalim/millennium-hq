# Feature-based React

A web app is split into shared code, isolated features, and the app that
composes them. Imports flow one way: shared, then features, then the app.
The gates enforce the import rules; this guide is what they cannot check.

| Layer | Default paths | May import |
|---|---|---|
| `shared` | `src/components`, `src/hooks`, `src/lib`, `src/utils`, `src/types`, `src/config`, `src/stores`, `src/testing` | other shared code, third-party code |
| `features` | `src/features/<name>` | its own feature, `shared`, third-party code |
| composition root | `src/app`, `src/main.tsx`, `src/main.ts`, `src/index.tsx`, `src/index.ts`, `src/App.tsx` | everything |

Both `shared` and `features` need unit tests for every change, components
included: a component change needs a render test, and every changed line
must run in a test.

## Writing code

- **A feature owns its components, hooks, API calls, types and state.**
  Inside a feature, organise freely (`components/`, `hooks/`, `api/`,
  `types/`); the rules apply at the feature boundary.
- **Features never import each other**, through a relative path or an
  alias such as `@/features/auth`. When two features need the same thing,
  move it to shared code; when a page needs both, compose them in
  `src/app`.
- **Shared code knows no feature.** A shared component that needs feature
  data takes it as props.
- **Keep components thin.** Put logic in hooks and plain modules, where
  tests are cheap; a component mostly renders.
- **Keep server state and UI state apart**: data from the API in the
  feature's API layer or a query cache, UI state in components or stores.
- **No effects for derived data**: compute it during render or memoise it.

## Reviewing

- Does each piece live in the feature that owns it, and is shared code
  really shared (used by more than one feature)?
- Does anything reach across features, or from shared code into a feature?
- Are components thin, with logic in hooks or modules that have their own
  tests?
- Are server state and UI state mixed, or effects used for derived data?

## Layout

The config can map the layers onto a different layout with `layers`. Each
`*` in a `features` path marks where one feature ends: with
`src/modules/*`, `src/modules/billing` and `src/modules/profile` are
separate features. Files under `src` in no layer (such as
`src/vite-env.d.ts`) are checked for everything but layer rules.
`millennium-code-qc config explain <file>` shows the layer of any file.
