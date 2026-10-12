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

## Public/admin separation

- Public state is populated only from the public allowlist API/SSE contract.
- Never derive a public view by taking an admin object and deleting fields in the browser.
- Logout clears authenticated cached/server state and closes private SSE before navigating.

## Persistence

- Do not persist credentials, session cookies, TOTP material, recovery codes, notification secrets, or raw node reports in `localStorage`/`sessionStorage`.
- UI preferences may be persisted only after defining their schema/version and handling invalid old values.

## Avoid

- Adding global state for data used by one component tree.
- Duplicating server data in both a query cache and a custom global store.
- Optimistic updates for credential rotation, telemetry reset, or other destructive/security-sensitive operations unless rollback behavior is explicitly tested.
