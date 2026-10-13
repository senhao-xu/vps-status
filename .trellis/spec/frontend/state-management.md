# State Management

## Current status

The foundation uses only React local rendering and has no state-management or query-cache dependency. Keep that baseline until concrete server-state behavior proves a library is needed.

## State categories

- Component interaction state: local `useState`/`useReducer` in the owning feature.
- Shareable navigation/filter state: URL search/path state once routing is introduced.
- Server state: owned by one API/query layer with cancellation, freshness, and invalidation rules.
- Live state: SSE events decoded once, then reduced into the same typed node projection used by initial API data.
- Authentication/session state: one application boundary; do not copy session truth into arbitrary feature stores.

## Reducer rules

Use an exhaustive reducer when state changes by event/status/action. A node's online state, alert state machine, or live-event projection must have one transition owner rather than scattered `if` updates.

Derived state must refer to the server/event identifier and sequence supplied by the source. Do not invent a second cursor or let older responses overwrite newer SSE state.

For administrator snapshot reloads, `createNodeRefreshBuffer()` records up to
512 live events received while the request is in flight, then replays them
through `reduceNodeEvent` over the returned snapshot. If the buffer fills,
abort/restart the snapshot rather than dropping events or growing unbounded.

## Public/admin separation

- Public state is populated only from the public allowlist API/SSE contract.
- Never derive a public view by taking an admin object and deleting fields in the browser.
- Logout clears authenticated cached/server state and closes private SSE before navigating.

## Persistence

- Do not persist credentials, session cookies, TOTP material, recovery codes, notification secrets, or raw node reports in `localStorage`/`sessionStorage`.
- UI preferences may be persisted only after defining their schema/version and handling invalid old values.

### Display preference contract

Language, light/dark mode and a session-scoped visual theme override are
application-wide UI preferences.
`I18nProvider` and `ThemeProvider` own them at the `App` boundary; feature
components consume `useI18n()` / `useTheme()` and must not read storage or
`matchMedia` themselves.

```ts
localStorage['vps-status.language'] = JSON.stringify({ version: 1, value: 'en' | 'zh' })
localStorage['vps-status.theme'] = JSON.stringify({ version: 1, value: 'dark' | 'light' })
sessionStorage['vps-status.visual-theme'] = JSON.stringify({ version: 1, value: 'emerald' | 'classic' })
```

- Missing, malformed, unknown-version, or unsupported values fall back to the
  browser language (`zh*` -> `zh`, otherwise `en`) or system color preference.
- A storage read/write failure must leave the in-memory controls usable.
- Language changes update `document.documentElement.lang`; theme changes set
  `document.documentElement.dataset.theme` and `colorScheme`.
- Visual theme sets `document.documentElement.dataset.visualTheme` independently
  of the light/dark mode. Resolve session override first, then the decoded
  site default, then Emerald if appearance loading fails.
- Site default comes from `GET /api/v1/public/appearance` and is never written
  as a personal override. “Follow site” removes the sessionStorage entry;
  administrator saving `PUT /api/v1/admin/appearance` updates the provider's
  site value without forcing a browser override.
- Switching visual themes must not remount the dashboard or enrollment dialog;
  selection/filter/installation state belongs to its feature, not the theme.
- Ignore a late public appearance bootstrap after an administrator read/save
  has advanced the provider revision. AppearancePanel cancels its initial GET
  before PUT; language changes must not restart GET or unlock an in-flight PUT.
- Tests must cover valid values plus malformed, obsolete-version, and unknown
  enum values. Adding another language or theme requires extending the typed
  union, fallback validation, and those tests together.

## Avoid

- Adding global state for data used by one component tree.
- Duplicating server data in both a query cache and a custom global store.
- Optimistic updates for credential rotation, telemetry reset, or other destructive/security-sensitive operations unless rollback behavior is explicitly tested.
