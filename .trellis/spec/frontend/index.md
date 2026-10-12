# Frontend Development Guidelines

The Web application is a strict TypeScript React/Vite client embedded into the Go Control binary for production. The current UI is a foundation screen; future monitoring features must preserve the API and public-data boundaries in the reviewed design.

## Guides

| Guide | Owns |
|---|---|
| [Directory Structure](./directory-structure.md) | `web/` layout and Go asset output boundary |
| [Component Guidelines](./component-guidelines.md) | React composition, semantics, styling, and accessibility |
| [Hook Guidelines](./hook-guidelines.md) | Effect/subscription ownership and future data hooks |
| [State Management](./state-management.md) | Local, URL, server, session, and live SSE state |
| [Type Safety](./type-safety.md) | Strict TypeScript and runtime boundary validation |
| [Quality Guidelines](./quality-guidelines.md) | pnpm, lint, typecheck, tests, build, and embedded verification |
| [Monitoring UI](./monitoring-ui-guidelines.md) | Admin/public routes, API decoding, live SSE, charts, and destructive actions |

## Pre-Development Checklist

- Read the task PRD/design and identify whether the UI is authenticated admin UI or unauthenticated public status UI.
- Read [Type Safety](./type-safety.md) before adding an API response, SSE event, form schema, or shared model.
- Read [State Management](./state-management.md) and [Hook Guidelines](./hook-guidelines.md) before adding fetching, caching, timers, or subscriptions.
- Read [Component Guidelines](./component-guidelines.md) before adding reusable UI or interactive controls.
- Read [Monitoring UI](./monitoring-ui-guidelines.md) before changing the admin shell, public page, SSE projection, history charts, or maintenance controls.
- Search `web/src` for an existing component/type/helper before adding a duplicate.

## Quality Check

Run from `web/`:

```bash
corepack pnpm install --frozen-lockfile
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build
```

Then run `go test -tags webui ./...` from the repository root to verify the generated assets match the Go embed boundary.

Reference files: `web/package.json`, `web/tsconfig.app.json`, `web/eslint.config.js`, `web/src/App.tsx`, `web/src/App.test.tsx`, `web/vite.config.ts`, and `internal/control/webui/`.
