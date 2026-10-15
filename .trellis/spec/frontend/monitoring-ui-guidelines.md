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

- `/` is the public homepage; `/public` and `/public/…` remain public aliases.
  Only `/admin` and `/admin/…` mount `AuthGate`, including first-run setup.
  Match the path segment: `/administrator` must not mount the admin boundary.
  Public rendering never checks administrator setup/session or opens private
  SSE, even if the browser has a valid administrator cookie.
- Every homepage state (loading, error, login-required, populated, empty) retains a
  localized `/admin` link at the top right, with wrapping at 320px. The login
  form has no homepage link; the dashboard homepage button returns to `/`. Root navigation never mutates site settings or node homepage visibility.
  A restricted status401 shows a login entry at /admin?next=home; after
  authentication this fixed destination returns to /. Other query destinations
  must never become arbitrary redirects.
- `fetch().json()`, SSE `data`, and errors remain `unknown` until one shared
  decoder validates them. Components do not cast payloads.
- Session cookies remain HttpOnly; CSRF comes from the same-origin cookie or
  one-time login/setup response and is never persisted in browser storage.
- Logout unmounts the dashboard and closes private SSE. SSE cleanup also
  clears reconnect timers; retry delay is capped at 30 seconds.
- Security password changes use their own form mutation, with current/new/
  confirmation inputs and a synchronous pending guard. Confirmation mismatch
  sends no request. `400 current_password_invalid` retains the authenticated
  form; actual session `401` exits through the usual unauthorized boundary.
  On `204`, clear the form and unmount the dashboard, then show a localized
  success notice at login. Do not refresh dashboard data after this mutation,
  because every session was revoked. Passwords never enter browser storage.
- Session refresh applies only while the current state is authenticated with
  the same session ID. A late refresh response must never restore the dashboard
  after logout, session invalidation or a password change.
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
- The add-node form asks only for a display name. Load and strictly decode
  administrator installation settings; prefer saved entry to the current
  browser origin. Automatic localhost/loopback/non-HTTPS addresses or missing
  eligible release show settings guidance and disable command creation.
  Settings require an authenticated GET/PUT and persist defaults server-side,
  not in browser storage. Blank URL/version restore browser/startup defaults.
- Installation config must expose saved values separately from the effective
  version/source; a locally eligible release is not verified publication.
  Keep the settings save guard synchronous, abort outstanding reads before
  saving, and cancel requests on unmount. Settings-navigation from a blocked
  dialog closes it before mounting the settings section.
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
| `/` requested without a session | Public status page; top-right admin link remains available |
| Restricted homepage401 | Login-required state with fixed /admin?next=home CTA |
| Other status failure | Error and retry with admin link |
| `/admin` requested without a session | Administrator login or first-run setup |
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

- Route tests cover `/`, `/public`, `/admin`, `/admin/…`, and misleading prefixes
  such as `/administrator`. Public states assert an accessible `/admin` link;
  browser checks verify root makes no admin requests and clicking the link
  opens login/setup, with no overflow at 320px.
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

## Scenario: latency check management and retained history

### 1. Scope / Trigger

Use for latency-check task/node views, task editing, latest-result summaries
and detail/history selection. References: `ProbesPanel`, `ProbeTaskDialog`,
`ProbeDetailDialog`, `ProbeResult`, `SettingsDialog`, `probePresentation`,
`probeHistory` and the existing administrator CRUD/telemetry APIs.

### 2. Signatures

- `ProbeTask.updatedAt: string` strictly decodes existing `updated_at`.
- `probeHistoryQueries(taskId, nodeId, range, now): string[]`, where range is
  `24h | 7d | 30d` and each query has task/node IDs, resolution, from and to.
- `loadProbeHistory(taskId, nodeId, range, signal): Promise<ProbePoint[]>`.
- `MetricChart` supports optional `timeScale`; TCP enables it, preserving
  other chart defaults.

### 3. Contracts

- Task dialogs use existing POST/PATCH/DELETE routes and CSRF client. A
  synchronous request ref prevents duplicate writes; failed writes keep the
  form/dialog and its error visible. Success refreshes the existing parent
  snapshot owner. Pending dialogs cannot be dismissed; cancel/delete dialog
  initial focus is safe and focus returns to a still-connected trigger.
- Results are snapshots of one latest sample per assigned node, not a
  historical failure rate. Unassigned and waiting are explicit states.
- `updated_at` is stored in whole seconds. Ignore latest samples with
  `receivedAt < (floor(updatedAtMillis / 1000) + 1) * 1000`, including the
  ambiguous update second. A fresh sample in that second conservatively waits
  until a subsequent sample; never claim it proves the edited target succeeded.
- Visible management copy is 延迟检测 / Latency checks; method metadata and
  latency chart titles explicitly identify TCP connection latency. This naming
  change does not add ICMP/HTTP or change backend probe routes/protocol.
- `ProbesPanel` stores nullable `{ taskId, nodeId }` detail selection. Names,
  View details actions and node-result buttons open a native `SettingsDialog`;
  there is no auto-opened first task or permanent bottom history section.
  Task-only entry starts without a selected node; node entry carries its ID.
- History has an explicit assigned node; never silently select the first.
  Render history only inside an open detail dialog with a valid assignment.
  Task/node/range/refresh/config-update changes isolate history state and abort
  old requests. Closing unmounts and aborts; opening again starts a fresh view.
  Refresh derives current metadata from the parent snapshot, clears removed
  assignments permanently and closes a deleted task's dialog. Reassigning a
  node later must not resurrect a previously invalidated selection.
- Shared native-dialog lifecycle provides focus containment, Escape/cancel
  and restoration. When a viewed row disappears, a stable Add button ref is
  the focus fallback. Read-only history loading does not block dismissal.
- Retained buckets describe a task's history and may include earlier task
  configurations; do not discard them by `updatedAt`. Only latest/current
  outcomes use the update-second freshness filter above. Explain retained
  history semantics next to the modal's history controls.
- Retention tiers are non-overlapping: 1m recent24h, 10m older through7d,
  1h older through30d. Query all required tiers with one clock and shared
  cancellation. Align tier transitions to the same ceil10m/ceil1h boundaries
  as backend retention and adjacent `[from,to)` windows. Merge chronologically;
  finer tiers take precedence on duplicate timestamps. Any failed tier rejects
  the whole range; do not plot a misleading partial window.
- Mixed-resolution charts position x coordinates by elapsed time rather than
  sample index, and retain the existing accessible timestamp/value table.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| No assigned node / no current sample | Unassigned / waiting text, no invented zero |
| Old assignment or sample in/before update second | Excluded from current counts/results |
| One retained tier fails | History error and retry, task management remains usable |
| Task/node/range changes or modal closes while request pending | Old requests aborted; old curve removed |
| No detail open / task-only entry without a node | No probe-history requests |
| Assigned node removed / task deleted on refresh | Clear node selection / close modal and restore focus |
| Mutation fails / returns401 | Dialog error / existing unauthorized boundary |
| Repeated submit while pending | One mutation request |

### 5. Good / Base / Bad Cases

- Good: 30d queries hourly, ten-minute and minute windows and plots actual time.
- Base: an unassigned task can be saved and edited later; history explains why
  it has no samples.
- Bad: a 7d view queries only10m data, dropping the newest24h, or labels an
  old target's sample as the edited target's success.

### 6. Tests Required

Assert tier windows cover the complete requested range without overlaps;
merge ordering, all-or-error503/401, shared abort; updated_at validation and
same-second/latest-assignment filtering; timed chart coordinates. Browser
checks cover CRUD, pending/error/focus, node search, view/history selection,
and zh/en with both visual themes/modes at320px. Detail checks assert no eager
history, entry-node identity, close/switch cancellation, assignment removal
and reassign non-resurrection, deleted-task fallback focus and read-only
dismissal during pending/error history.

### 7. Wrong vs Correct

Wrong: query only `resolution=1h` for30d, or use `receivedAt >= updatedAt`
without accounting for the database timestamp precision.

Correct: use `loadProbeHistory` across all required tiers; `latestProbeResult`
applies assignment and configuration-update-second filtering centrally.

## Scenario: administrator settings panels

Settings panels use existing authenticated APIs and the parent snapshot refresh owner. `useAdminAction(onUnauthorized)` owns one cancellable write per panel with a synchronous request guard, observable pending/error/success, abort on unmount and the usual 401 auth exit. A failed write keeps its form/dialog draft; do not close a dialog on completion of the legacy `mutate` helper, which catches failure internally.

- Alert creation dialogs use native modal focus/cancel behavior. Conditional provider fields require distinct keyed subtrees: switching Webhook/Telegram clears old fields rather than reusing an uncontrolled password value as a plain-text Chat ID. Named scope controls submit IDs for nodes and names for groups. Offline rules send zero threshold/hysteresis. Channel secrets only live in an open dialog.
- Site settings contain title, description and the inverted enabled control “Access requires login”. They send no node_ids. Scalar drafts derive pristine values from refreshed config, retain dirty overrides and successful normalized values until matching acknowledgement, and preserve edits made while saving. Node settings own homepage_visible independently of hidden; the admin decoder accepts a missing legacy field as false and rejects a present malformed field. The homepage always consumes the strict status DTO, including after login; it never mounts private SSE or renders admin Node objects.
- Storage keeps independent `clear_history` and `reset_all_telemetry` inputs, checks exact strings again in the handler, disables all operations during pending writes and clears only the successful operation's confirmation. Maintenance and destructive actions are separate sections.
- MFA setup/confirm/disable retain secrets and recovery codes only in component memory. After a successful confirm/disable, a committed boolean override reflects the write while session refresh is pending/failed. Keep one-time recovery codes on refresh failure, show a retry that only refreshes the session, and block further MFA mutations until refresh succeeds. Dismissing codes must never re-offer setup based on a stale session. Password change continues its independent session-revocation contract.

Validation: unit checks for selection retention, saved draft acknowledgement/old snapshots, homepage login/return and per-node visibility; isolated browser provider switching in both directions, failed writes/retries, duplicate submit/cancel/focus, pristine/dirty refresh, exact cleanup confirmations, real MFA write with failed session refresh, password logout, zh/en and both themes/modes at320px.


## Scenario: monitor-style TCP configuration

- `parseProbeTarget(value)` accepts an explicit host:port authority or bracketed IPv6 and returns existing `{host, port}` API fields. Reject missing/out-of-range/noninteger ports, ambiguous unbracketed IPv6, whitespace, URL schemes/paths/userinfo/query/fragment. Backend retains authoritative unicast/hostname validation.
- New dialog defaults interval60, all available node IDs, autoJointrue. Editing derives refreshed task by stable ID while uncontrolled draft fields stay mounted; absent/deleted task cannot turn an edit into a create.
- `ProbeTask.autoJoin` decodes `auto_join` strictly when present, defaultsfalse only for missing legacy field. Writes send bool `auto_join`; legacy server DTO compatibility does not justify silently accepting malformed types.
- Group filter sentinel cannot collide with group names (including empty/all). Search and group filters never change chosen IDs. Select/deselect shown affect only visible IDs. `probeSelection` merges current default/assigned IDs with explicit boolean overrides, so newly auto-assigned nodes appear while user deselections remain deselected across refresh.
- Compact no-node guidance allows saving for future auto-join; do not display protocol internals such as payload/Banner restrictions in the configuration flow. Failed/pending writes preserve draft, prevent duplicate submission and pending dismissal;409 probe_auto_join_limit has localized actionable feedback.
- Verification: parser/selection/decoder unit cases; real IPv6 create/edit, group/search hidden-selection preservation, default60/all/true, migrated legacyfalse, real later enrollment and disabled toggle, refreshed assignments vs explicit choice, errors/pending/focus, zh/en and themes at320px.
