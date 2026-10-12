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

## 4. Validation & Error Matrix

| Condition | UI result |
|---|---|
| Session `401` | Login screen; private stream stops |
| Login `mfa_required` | Labeled TOTP/recovery input appears |
| Telemetry `503` | Current node state remains visible; history shows degraded notice |
| Empty history/probe series | Textual empty state, not a zero-valued chart |
| Public payload has hostname/IP/OS/kernel/credential/history key | Decoder rejects the response; nothing is rendered |
| SSE parse/network failure | Source closes and bounded reconnect starts |

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
