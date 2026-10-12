# Hook Guidelines

## Current status

No custom hooks or data-fetching library exist in the foundation. Do not introduce a global query/state dependency merely to prepare for future features. Establish a hook only when a concrete API, form, or subscription requires it.

## Ownership

- Keep a hook in `src/features/<feature>/` while it serves one feature.
- Move it to a shared location only after a second real consumer appears and both require the same behavior.
- Hooks may compose the API client but must not parse separate private versions of the same response/event. Runtime decoding belongs to one `src/api` boundary.

## Effects and subscriptions

- Every effect that opens SSE, installs an event listener, starts a timer, or creates an abort controller returns cleanup.
- Effects must tolerate React `StrictMode` setup/cleanup replay from `web/src/main.tsx`.
- Use `AbortController` for request cancellation and an explicit reconnect/backoff owner for SSE. Do not leave reconnect timers after unmount or logout.
- Do not mirror derived props/server data into local state through effects; derive during render or in a reducer.

## Naming and results

- Hook names start with `use` and describe the owned behavior, such as `useNodeLiveStream` rather than `useData`.
- Return a discriminated result for async work (`idle/loading/success/error`) instead of unrelated loading/error/data booleans.
- Mutation hooks expose pending/error state and invalidate/refetch through one owner; components must not manually patch multiple caches.

## Testing

- Test cleanup, cancellation, reconnect bounds, and stale-response ordering for network hooks.
- Use fake timers only at the hook boundary and always restore them.
- A hook consuming public data needs a fixture proving forbidden fields are ignored/rejected rather than rendered.

## Avoid

- Fetch calls scattered through components.
- Missing effect dependencies or linter suppression used to silence them.
- Hook modules that create singleton mutable state on import.
- A hook whose name suggests reading data but performs an unrelated destructive mutation.
