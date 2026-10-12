# Frontend Directory Structure

## Current layout

- `web/src/main.tsx`: the only DOM bootstrap; it validates `#root`, creates the React root, and enables `StrictMode`.
- `web/src/App.tsx`: current application shell and named `App` component.
- `web/src/*.test.tsx`: co-located unit/component tests.
- `web/src/styles.css`: current global tokens/reset and foundation-screen styles.
- `web/vite.config.ts`: Vite plugins, development `/api` proxy, and the production output path.
- `web/package.json` / `web/pnpm-lock.yaml`: the package-manager and reproducible dependency contract.

Vite writes directly to `internal/control/webui/dist`. That directory is generated and ignored; never hand-edit or import source from it. The Go `webui` build tag embeds it, while ordinary Go work uses `internal/control/webui/fallback`.

## Growth rules

The repository does not yet have enough product UI to justify a deep hierarchy. Keep the current flat `src/` layout until a real feature has multiple files. When it does:

- `src/features/<feature>/` owns feature components, hooks, schemas, and tests.
- `src/components/` is only for presentation reused by multiple features.
- `src/api/` owns the HTTP client, runtime decoders, and SSE event boundary.
- `src/routes/` owns route-level composition when routing is introduced.
- `src/styles/` owns shared tokens/global styles when `styles.css` becomes too broad.

Do not create empty directories or generic `utils`, `common`, or `types` dumping grounds in advance.

## Import direction

- Bootstrap/routes may import features and shared components.
- Features may import `api` and shared components; shared components do not import feature modules.
- UI code does not know SQLite schemas or Go internal models. It consumes explicit API contracts.
- Admin and public API types stay distinct even when fields overlap.

## Naming

- React component files and exported components use PascalCase.
- Hooks use `useX` and live with their owning feature until reused.
- Tests use `*.test.ts` / `*.test.tsx` next to the subject.
- CSS class names use readable lower-case kebab-case; do not encode database/API names into classes.
