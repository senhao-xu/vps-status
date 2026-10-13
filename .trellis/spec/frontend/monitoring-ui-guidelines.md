# Monitoring UI

## 1. Scope / Trigger

Use this contract for administrator authentication, monitoring screens,
history/probe charts, live SSE, storage maintenance, and public status UI.

## 2. Signatures

The browser boundary is centralized in `src/api/contracts.ts` and
`src/api/client.ts`:

```ts
type Decoder<T> = (value: unknown) => T
function apiRequest<T>(path: string, decoder: Decoder<T>, options?): Promise<T>
function apiMutation(path: string, options?): Promise<void>
function decodePublicStatus(value: unknown): PublicStatus
```

`useNodeLiveStream(active, onEvent, onUnauthorized)` exclusively owns the
administrator `EventSource` lifecycle.

## 3. Contracts

- `fetch().json()`, SSE `data`, and errors remain `unknown` until one shared
  decoder validates them. Components do not cast payloads.
- Session cookies remain HttpOnly; CSRF comes from the same-origin cookie or
  one-time login/setup response and is never persisted in browser storage.
- Logout unmounts the dashboard and closes private SSE. SSE cleanup also
  clears reconnect timers; retry delay is capped at 30 seconds.
- Historical charts always include a named graphic and an accessible text
  table. Timestamps use the browser timezone; traffic periods state UTC.
- Destructive telemetry operations require the server's exact confirmation;
  clear-history and reset-all use distinct typed confirmations in the UI.
- Public state is decoded only by the exact-key public decoder. It is never
  derived from an administrator `Node`.
- Enrollment commands stay in dialog memory and are not persisted as UI
  preferences. Poll installation status by enrollment ID, associate consumed
  node ID from the status contract, stop on online/expired/401/404 or dialog
  unmount, and cancel pending requests/timers. Retries are bounded and never
  overlap requests. Revoke/regenerate stops old polling before mutation.
- A consumed-token conflict refreshes its status rather than creating a
  duplicate node. Online completion refreshes the administrator collection
  through its existing load owner and selects the returned node ID.
- A revocation `404` does not prove the token was unused: expired consumed
  rows may have been pruned. Stop regeneration, show missing-record guidance
  and ask the operator to check nodes; never automatically POST a replacement.
- Native installation dialogs provide keyboard dismissal, focus containment
  and restoration; all stages expose pending/error/status text. Theme changes
  retain the dialog and its secret only in memory.
  Focus the name input through its ref after `showModal()`; React autoFocus
  before the modal opens may leave Chromium focused on the close button.

## 4. Validation & Error Matrix

| Condition | UI result |
|---|---|
| Session `401` | Login screen; private stream stops |
| Login `mfa_required` | Labeled TOTP/recovery input appears |
| Telemetry `503` | Current node state remains visible; history shows degraded notice |
| Empty history/probe series | Textual empty state, not a zero-valued chart |
| Public payload has hostname/IP/OS/kernel/credential/history key | Decoder rejects the response; nothing is rendered |
| SSE parse/network failure | Source closes and bounded reconnect starts |
| Appearance bootstrap fails | Emerald fallback, rest of UI continues |
| Enrollment revocation/regeneration returns `409 enrollment_consumed` | Refresh current enrollment status; do not create another token |
| Enrollment revocation/regeneration returns `404 not_found` | Show missing-record guidance; do not create a replacement token |
| Enrollment status missing or polling repeatedly fails | Observable error; timers stop within the retry bound |

## 5. Good / Base / Bad Cases

- Good: initial node DTO and later SSE events pass through one reducer, so an
  older response cannot create a second live-state model.
- Base: a 320px viewport uses the horizontal nav and one-column panels while
  all controls remain native keyboard-operable elements.
- Bad: a chart communicates failure by red/green alone or hides all values in
  an inaccessible SVG.

## 6. Tests Required

- Decoders cover valid, malformed, missing, and forbidden-extra fixtures.
- Public fixtures assert hostname/IP/OS/kernel/credentials/history do not
  render.
- Chart tests assert `role=img`, an accessible label, and a table alternative.
- Enrollment tests verify sequential polling, terminal cancellation, stale
  completion ignored after cleanup, and no second enrollment on consumed
  conflict. Browser checks exercise command copy, focus return and theme
  changes during an open installation dialog.
- Run lint, strict typecheck, unit tests, production build, then tagged Go
  embed tests after every asset change.

## 7. Wrong vs Correct

### Wrong

```ts
const node = (await response.json()) as PublicNode
```

### Correct

```ts
const payload: unknown = await response.json()
const status = decodePublicStatus(payload)
```
