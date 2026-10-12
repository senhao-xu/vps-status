# Type Safety

## Compiler contract

`web/tsconfig.app.json` enables `strict`, `isolatedModules`, bundler resolution, and `noEmit`. Keep these settings enabled. `web/tsconfig.node.json` separately covers Vite configuration. Do not weaken strictness or add broad exclusions to make a feature compile.

## Type ownership

- Component props and local feature types live beside their owner.
- HTTP responses, errors, and SSE events are defined/decoded in `src/api/` once that boundary exists.
- Admin and public DTOs are separate types. Public types never contain sensitive optional fields “just in case.”
- Protocol/database Go models are not manually mirrored as one giant frontend domain type.

## Runtime validation

TypeScript types do not validate JSON. Treat `fetch().json()`, SSE payloads, storage values, and URL values as `unknown` until one boundary decoder validates and normalizes them.

No runtime validation library is established. The first implementation may use small type guards for narrow contracts or propose a library when multiple complex schemas exist; if a library is added, update this guide and centralize its usage.

## Required patterns

- Use discriminated unions for async status, alert status, connection state, and event variants.
- Exhaustively switch on event/status unions; use a `never` assertion or equivalent so new variants fail typechecking.
- Keep timestamps as explicit wire strings at the boundary and convert once to the presentation format required by a component.
- Prefer `unknown` plus validation over `any` or double assertions.

## Current examples

- `web/src/main.tsx` narrows `document.getElementById` before calling `createRoot` and throws a clear initialization error for the impossible missing mount point.
- `web/vite.config.ts` uses `defineConfig` rather than an untyped exported object.

## Avoid

- `any`, `as unknown as T`, non-null assertions on API/DOM data, or unchecked `JSON.parse` results.
- Repeating local casts for the same response field in multiple components.
- Optional fields used to represent mutually exclusive states.
- Frontend types imported from generated Go/database internals without a reviewed API contract.
