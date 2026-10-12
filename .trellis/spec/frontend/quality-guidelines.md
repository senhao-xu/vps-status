# Frontend Quality Guidelines

## Package manager and dependencies

- Use the `packageManager` version in `web/package.json` and commit `web/pnpm-lock.yaml`.
- CI installs with `pnpm --dir web install --frozen-lockfile`; dependency updates must update the manifest and lockfile together.
- Do not add a runtime dependency for functionality available clearly in React, the browser, or a small local function. Document why each framework-level dependency is needed.

## Required checks

From `web/`:

```bash
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build
```

From the repository root after build:

```bash
go test -tags webui ./...
go build -tags webui ./cmd/control
```

The Vite build output must remain `internal/control/webui/dist`, matching `assets_built.go`. The directory is generated and not committed.

## Tests

- Co-locate unit/component tests with source using Vitest.
- Assert accessible/user-visible behavior rather than implementation details.
- API decoders need valid, missing, malformed, and extra-sensitive-field fixtures.
- Public-page tests include negative assertions for hostname, IP, OS/kernel, credentials, and history.
- Subscription tests cover StrictMode cleanup, reconnect bounds, out-of-order events, and logout.

## Review checklist

- ESLint has no suppressed rules added solely for the change.
- TypeScript uses no `any` or unchecked external-data assertions.
- Loading, empty, error, stale, and unauthorized states are intentional.
- Interactive controls are keyboard accessible and labeled.
- Layout works at the current 320px minimum and does not rely on color alone.
- No secret or private field appears in DOM, browser storage, console output, source map fixtures, or public bundles.
- A frontend build is followed by the tagged Go embed test/build so CI cannot pass with missing production assets.

## Avoid

- Editing generated `internal/control/webui/dist` files.
- Using snapshots as the only behavioral assertion.
- Checking `node_modules`, coverage, or generated dist into Git.
- Letting the development `/api` proxy hide a production path or authentication mismatch; production tests exercise the embedded Control route.
