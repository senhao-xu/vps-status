# Homepage and Public Status Boundary

## 1. Scope / Trigger

Use this contract for site settings, node homepage visibility, status access,
and the safe status projection. The homepage is a core feature; its access
policy is separate from the data fields allowed in its response.

## 2. Signatures

```text
GET /api/v1/admin/public-page
PUT /api/v1/admin/public-page  {enabled, title, description, node_ids?}
GET /api/v1/admin/nodes       node.homepage_visible: boolean
PATCH /api/v1/admin/nodes/{id} {name?, group?, hidden?, homepage_visible?}
GET /api/v1/public/status    {page, nodes}
```

Storage remains public_page(singleton_id=1, enabled, title, description,
updated_at) and public_nodes(node_id, published_at), introduced in schema7.
Schema11 enables only rows whose updated_at=0; saved access choices and all
existing memberships stay intact. New installations have an open homepage.

## 3. Contracts

- enabled=true permits anonymous status access; enabled=false requires an
  existing administrator session through api.auth(false,...). Authenticated
  responses use the same strict safe DTO and effective page.enabled=true.
- New enrollment adds public_nodes membership within the node/credential/
  auto-join transaction. At MaxPublicNodes=1000, enrollment still succeeds
  with homepage_visible=false. Existing node membership is never expanded by
  the migration. The admin hidden flag is independent of homepage visibility.
- Site PUT is authenticated and CSRF-protected. Omitted node_ids preserves
  current membership inside the transaction; an explicit list is a legacy
  complete replacement, including [] to clear it. The site UI omits node_ids.
- Partial node PATCH merges metadata and homepage visibility against committed
  state in one transaction; do not read/merge/write a stale handler snapshot.
- The status DTO is a separate type, never nodeDTO with fields removed.
  Top-level fields are exactly page and nodes. Page fields remain exactly
  enabled, title, description. Status access never adds administrator fields.
- Node fields are exactly display name/group/online, current uptime/CPU/
  memory/disk/RX/TX, and current UTC-month RX/TX totals. Missing/offline values
  are null. Telemetry unavailability affects only month totals.
- Status never includes stable node IDs, hostname, IP, OS, kernel, architecture,
  virtualization, credentials, probe targets, notifications, or history.

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Empty/overlong title, description over512bytes, duplicate/missing/deleted node | 400 invalid_input; unchanged transaction |
| Admin mutation or restricted status without a valid session | 401 unauthorized |
| Admin mutation without matching CSRF | 403 csrf_invalid |
| Manually show a node beyond the1000node cap | 409 homepage_node_limit; node metadata and membership unchanged |
| New enrollment at homepage cap | Registration succeeds, homepage_visible=false |
| Open homepage | 200 safe status DTO |
| Restricted homepage with valid administrator | 200 same safe DTO, effective enabled=true |
| Telemetry unavailable | Live fields continue; month totals null |
| Node soft deletion | Explicitly remove membership in deletion transaction |

## 5. Good / Base / Bad Cases

- Good: save site title without sending node_ids; a concurrently enrolled node
  keeps its default homepage membership.
- Base: fresh installation serves an open empty homepage, then new nodes appear.
- Bad: reuse nodeDTO for a logged-in homepage or infer visibility from hidden.
  Both make the read-only projection depend on unrelated administrator state.

## 6. Tests Required

- Schema10-to11 backup stays schema10 and preserves explicit access choices,
  old memberships and node metadata. Untouched settings become enabled.
- Enrollment, homepage cap, partial node updates, legacy full list replacement,
  omitted membership and concurrent enrollment/visibility saves are atomic.
- Restricted status tests cover absent/invalid/expired/revoked/valid sessions.
- Both open and authenticated status test sentinel private fields and exact keys;
  assert no IDs, host facts, credentials, history, points, bucket_start or raw
  cumulative report counters, even with unavailable telemetry.
- Frontend strict decoder rejects unknown fields. Login uses fixed
  /admin?next=home and returns to /; no arbitrary redirect destination.

## 7. Wrong vs Correct

Wrong: read node or node_ids in a handler, wait for a request body, then replace
storage values from that snapshot. Enrollment and another edit can be lost.

Correct: pass optional fields to storage, merge committed values inside its
transaction, and build status exclusively through the explicit public DTO.
